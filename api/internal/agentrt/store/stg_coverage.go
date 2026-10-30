package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// failed{in_main} predates B5 and keeps its history in refusalEvidence's
// text. Read precisely that history, never A or the current main head.
var stgInMainHistory = regexp.MustCompile(`; main history effective at commit ([0-9a-f]{40}), read at [0-9a-f]{40}\)`)

func stgInMainCommit(text string) string {
	m := stgInMainHistory.FindAllStringSubmatch(text, -1)
	if len(m) == 1 {
		return m[0][1]
	}
	return ""
}

// StgCoverageCandidates includes newly discovered results even when their
// release evidence was recorded in an earlier pass. Budget is irrelevant.
func StgCoverageCandidates(s StgState) []StgMemberKey {
	var out []StgMemberKey
	for op, o := range s.Legacy.Operations {
		if o.DryRun || s.Legacy.LatestOperation(o.Task) != op {
			continue
		}
		r := o.R
		if o.Phase == "failed" && o.Outcome == "in_main" {
			r = s.InMain[op]
			if s.Disposals[op].Record.String("why") != "owner_delivered" {
				continue
			}
		} else if !o.EverLanded || o.Reverted {
			continue
		}
		m := StgMemberKey{o.Task, op, r}
		old := s.Members[m]
		if !landingSHA.MatchString(r) || s.covered(m) || old.To == "delivered" || old.To == "covered" || old.To == "disposed" || s.Withheld[m].Seq > 0 || s.Disposals[op].Record.String("why") == "owner" {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Op < out[j].Op })
	return out
}

func (s StgState) checkCoverageProof(r StgRecord) error {
	e := s.Evidence[r.String("stg_evidence")]
	m := stgMember(r)
	if e.Seq == 0 || r.String("head") != e.Record.String("P") || r.String("verdict") != "observed" {
		return fmt.Errorf("STG coverage observation must name its recorded release and P")
	}
	for _, candidate := range StgCoverageCandidates(s) {
		if candidate == m {
			return nil
		}
	}
	return fmt.Errorf("STG coverage member is no longer eligible")
}

func (s StgState) canCover(m StgMemberKey, p string) bool {
	if s.effective(m, p) {
		return true
	}
	// O-3: Owner's decision plus H in the release's history. It does not
	// fabricate a landed operation or require the abandoned result branch.
	r := s.Validity[StgValidityKey{m, p}].Record
	return s.InMain[m.Op] == m.R && s.Disposals[m.Op].Record.String("why") == "owner_delivered" && r["has_r"] == true
}

// StgCoverageEvents builds a historical Git observation and its coverage
// atomically. stg_evidence separates a read at immutable P from current-main
// observations consumed by B3/B4; it must never refresh a closing round.
func StgCoverageEvents(s StgState, evidence string, m StgMemberKey, ev MainEvidence, at time.Time) ([]Event, error) {
	r := StgRecord{"v": MainEvidenceVersion, "op": m.Op, "root": m.Root, "R": m.R, "M0": m.R,
		"head": ev.Head, "has_r": ev.HasR, "validity": mainValidity(ev), "reverted_by": revertedBy(ev),
		"verdict": "observed", "read_at": ev.ReadAt.UTC().Format(time.RFC3339Nano), "stg_evidence": evidence}
	if ev.ReadAt.IsZero() || ev.ReadAt.After(at) || ev.Unreadable || !ev.HasR || (s.InMain[m.Op] != m.R && validityOf(ev) != "effective") {
		return nil, fmt.Errorf("STG coverage needs a completed positive observation at P")
	}
	if err := s.checkCoverageProof(r); err != nil {
		return nil, err
	}
	if why := badLandingMain(map[string]any(r)); why != "" {
		return nil, fmt.Errorf("%s", why)
	}
	cover := StgRecord{"v": "1", "kind": "covers", "scope": "ext", "evidence": evidence, "root": m.Root, "op": m.Op, "R": m.R}
	return []Event{
		{Stream: StgReleaseStream, Type: "note", Actor: "controller", TaskID: m.Root, Body: map[string]any{"text": "STG release history observed", "landing_main": map[string]any(r)}},
		{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "STG release covers result", "stg_release": map[string]any(cover)}},
	}, nil
}

// CommitStgCoverage uses the same none fence and CAS as control decisions.
// Both the proof and covers commit, or neither does; slow Git reads hold no
// locks. Close and Owner disposition serialize on this repository stream.
func CommitStgCoverage(ctx context.Context, db DB, tenant string, expected int64, evidence string, m StgMemberKey, ev MainEvidence) (bool, error) {
	written := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		written = false
		enabled, err := stgControlEnabled(ctx, tx, tenant, func() bool { return true })
		if err != nil || !enabled {
			return err
		}
		if err := lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err := StgReleaseState(ctx, tx)
		if err != nil || s.LastSeq != expected {
			return err
		}
		events, err := StgCoverageEvents(s, evidence, m, ev, DecisionAt())
		if err != nil {
			return err
		}
		for _, event := range events {
			if _, err := Append(ctx, tx, event); err != nil {
				return err
			}
		}
		written = true
		return nil
	})
	return written && err == nil, err
}
