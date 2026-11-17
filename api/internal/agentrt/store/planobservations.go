package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// A binding's last persisted successful read ages to Stale after planObservationTTL;
// only then does a source that keeps failing withdraw a done that rests on it
// (a Gitea blip does not flip a step and its dependents). A pass refreshes
// successful bindings when any persisted success is planPassRefresh old, so
// unchanged answers cost one event per root refresh, not one per binding
// per round. A persisted success may lag the real last success by
// up to planPassRefresh (Work 632, coordinator recommendation).
const (
	planObservationTTL = 25 * time.Minute
	planPassRefresh    = 10 * time.Minute
	planRoundTimeout   = 5 * time.Minute
)

// PlanSources are controller-owned adapters, never request payloads. History
// must read complete Git history, including reverts.
type PlanSources struct {
	History func(context.Context, string, string, string, string) (landing.MainHistory, error)
}

type planObservation struct {
	V             int      `json:"v"`
	Kind          string   `json:"kind"`
	Root          string   `json:"root"`
	Step          string   `json:"step"`
	Binding       string   `json:"binding"`
	Version       int      `json:"version"`
	Digest        string   `json:"binding_sha256"`
	Source        string   `json:"source_id"`
	SourceVersion int64    `json:"source_version"`
	Status        string   `json:"status"`
	Value         string   `json:"value"`
	Evidence      []string `json:"evidence"`
	LastValue     string   `json:"last_value,omitempty"`
	LastSuccess   string   `json:"last_success,omitempty"`
	Reason        string   `json:"reason"`
	Changed       bool     `json:"changed"`
}

func (n planObservation) validate() error {
	if n.V != 1 || n.Kind != "agentrt.plan_observation/v1" || n.Root == "" || n.Step == "" || n.Binding == "" || n.Version < 1 || len(n.Digest) != 64 || !shaPattern.MatchString(n.Digest) || n.Source != n.Root+":"+n.Digest || n.SourceVersion < 1 || (n.Status != "Fresh" && n.Status != "Unknown") || (n.Value != "true" && n.Value != "false" && n.Value != "unknown") || n.Evidence == nil {
		return invalid("invalid plan observation")
	}
	if n.Status == "Fresh" && n.LastSuccess == "" || n.Value == "true" && len(n.Evidence) == 0 || n.Reason == "" {
		return invalid("observation needs server time, reason and success evidence")
	}
	if n.Status == "Unknown" && n.Value != "unknown" {
		return invalid("unreadable source cannot report a verdict")
	}
	if n.LastSuccess != "" {
		if _, err := time.Parse(time.RFC3339Nano, n.LastSuccess); err != nil {
			return invalid("invalid observation time")
		}
	}
	return nil
}
func observationStream(root string) string     { return "plan-observation:" + root }
func observationKey(b planview.Binding) string { return b.Root + ":" + b.Digest() }
func passStream(root string) string            { return "plan-observation-pass:" + root }

// planObservationPass is one collector pass over a root: for each binding
// still adopted, the database time of its last successful read. A binding
// this pass could not read keeps the time of its last success, so one failed
// read does not make an unchanged answer Stale (PV2 re-review 2026-10-26).
// Times are the database's: an event's own time is the writer's clock, which
// a pod or a test may skew.
type planObservationPass struct {
	V     int               `json:"v"`
	Kind  string            `json:"kind"`
	Root  string            `json:"root"`
	At    string            `json:"at"`
	Fresh map[string]string `json:"fresh"`
}

func (p planObservationPass) validate() error {
	if p.V != 1 || p.Kind != "agentrt.plan_observation_pass/v1" || p.Root == "" || p.Fresh == nil {
		return invalid("invalid plan observation pass")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.At); err != nil {
		return invalid("invalid plan observation pass time")
	}
	for d, at := range p.Fresh {
		if len(d) != 64 || !shaPattern.MatchString(d) {
			return invalid("invalid plan observation pass")
		}
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			return invalid("invalid plan observation pass time")
		}
	}
	return nil
}

// remoteRead is a binding's Git answer, read before the collector's
// transaction so a slow source holds no lock.
type remoteRead struct {
	read    bool
	at      time.Time
	history landing.MainHistory
	err     error
}

type planObservationPosition struct{ pass, observation int64 }

// Both positions come from one committed statement snapshot. Missing streams
// are position zero; a first collector creating either stream fences this read.
func readPlanObservationPosition(ctx context.Context, tx pgx.Tx, root string) (p planObservationPosition, err error) {
	err = tx.QueryRow(ctx, `SELECT
 COALESCE((SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$1),0),
 COALESCE((SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$2),0)`, passStream(root), observationStream(root)).Scan(&p.pass, &p.observation)
	return
}

// Each short clock transaction finishes before any remote call. The deadline
// bounds the root, including an in-flight read; unread bindings have read=false.
func readPlanRemotes(ctx context.Context, db DB, src PlanSources, bindings []planview.Binding) ([]remoteRead, error) {
	rctx, cancel := context.WithTimeout(ctx, planRoundTimeout)
	defer cancel()
	remote := make([]remoteRead, len(bindings))
	var start time.Time
	for i, b := range bindings {
		if rctx.Err() != nil {
			break
		}
		if b.Source != "git" || b.Retired() {
			remote[i].read = true
			continue
		}
		var at time.Time
		if err := InTx(rctx, db, func(tx pgx.Tx) error {
			return tx.QueryRow(rctx, `SELECT clock_timestamp()`).Scan(&at)
		}); err != nil {
			if ctx.Err() == nil && rctx.Err() != nil {
				break
			}
			return nil, err
		}
		if start.IsZero() {
			start = at
		}
		if at.Sub(start) >= planRoundTimeout || rctx.Err() != nil {
			break
		}
		remote[i] = readRemote(rctx, src, b)
		remote[i].read, remote[i].at = true, at
	}
	return remote, ctx.Err()
}

func readRemote(ctx context.Context, src PlanSources, b planview.Binding) remoteRead {
	var r remoteRead
	if src.History == nil {
		r.err = errors.New("history unavailable")
		return r
	}
	r.history, r.err = src.History(ctx, b.Repo, "", b.Result, "")
	return r
}

// CollectPlanObservations is one pass. Per root: remote sources are read
// first, outside any transaction; then, under the root's lock, each binding
// still adopted gets its answer, written only when it differs from the last
// one, and the pass records when each binding last answered. A failed read writes
// nothing: the last answer stands until it is Stale. No step lock is taken:
// an observation is keyed by the binding's digest, so one written for a
// binding a concurrent adoption just replaced is never read for its successor.
func CollectPlanObservations(ctx context.Context, db DB, src PlanSources) (int, error) {
	var first *planFactsSnapshot
	if err := InTx(ctx, db, func(tx pgx.Tx) error { var e error; first, e = readPlanFacts(ctx, tx); return e }); err != nil {
		return 0, err
	}
	roots := make([]string, 0, len(first.Docs))
	for r := range first.Docs {
		roots = append(roots, r)
	}
	slices.Sort(roots)
	count := 0
	var errs []error
	for _, root := range roots {
		bindings, err := first.Docs[root].Bindings()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(bindings) == 0 {
			continue
		}
		var position planObservationPosition
		if err = InTx(ctx, db, func(tx pgx.Tx) error {
			var e error
			position, e = readPlanObservationPosition(ctx, tx, root)
			return e
		}); err != nil {
			errs = append(errs, err)
			continue
		}
		remote, err := readPlanRemotes(ctx, db, src, bindings)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var written int
		var failed []error
		var skipped bool
		err = InTx(ctx, db, func(tx pgx.Tx) error {
			written, failed, skipped = 0, nil, false
			if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742120,hashtext($1))`, passStream(root)); e != nil {
				return e
			}
			currentPosition, e := readPlanObservationPosition(ctx, tx, root)
			if e != nil {
				return e
			}
			if currentPosition != position {
				skipped = true
				return nil
			}
			s, e := readPlanFacts(ctx, tx)
			if e != nil {
				return e
			}
			var now time.Time
			if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				return e
			}
			last, had := s.Passes[root]
			fresh := map[string]string{}
			// Carry persisted successes for every still-adopted binding,
			// including those this round did not reach before its deadline.
			if doc := s.Docs[root]; doc != nil {
				currentBindings, e := doc.Bindings()
				if e != nil {
					return e
				}
				for _, b := range currentBindings {
					if at := s.Observations[b.Digest()].LastSuccess; at != "" {
						fresh[b.Digest()] = at
					}
				}
			}
			succeeded, refresh := false, false
			for i, b := range bindings {
				if !remote[i].read {
					continue
				}
				current, ok := s.find(b.Step, b.Atom, b.ID, b.Version)
				if !ok || current.Digest() != b.Digest() {
					continue
				}
				if b.Retired() {
					continue // no source to read: it stays Unknown (planview.Binding.Retired)
				}
				old := s.Observations[b.Digest()]
				n := planObservation{V: 1, Kind: "agentrt.plan_observation/v1", Root: b.Root, Step: b.Step, Binding: b.ID, Version: b.Version, Digest: b.Digest(), Source: observationKey(b), SourceVersion: old.SourceVersion + 1, Status: "Fresh", Value: "unknown", Evidence: []string{}, Reason: "No trusted verification yet"}
				if e = readPlanSource(ctx, tx, remote[i], b, &n); e != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					failed = append(failed, fmt.Errorf("plan observation %s/%s: %w", b.Root, b.ID, e))
					continue
				}
				at := remote[i].at
				if at.IsZero() { // database-only binding, read in this transaction
					at = now
				}
				succeeded = true
				persisted, parseErr := time.Parse(time.RFC3339Nano, old.LastSuccess)
				if parseErr != nil || at.Sub(persisted) >= planPassRefresh {
					refresh = true
				}
				fresh[b.Digest()] = timeText(at)
				n.LastValue = n.Value
				n.LastSuccess = timeText(at)
				n.Changed = true
				if old.Digest != "" && n.Status == old.Status && n.Value == old.Value && n.Reason == old.Reason && fmt.Sprint(n.Evidence) == fmt.Sprint(old.Evidence) {
					continue // the same answer: the pass below keeps it fresh
				}
				did, e := recordPlanObservation(ctx, tx, n)
				if e != nil {
					return e
				}
				if did {
					written++
				}
			}
			// A failure never writes a pass. Refresh against each binding's
			// persisted success, not the last pass's time: another binding's
			// recovery must not postpone this one's refresh.
			if !succeeded || had && sameKeys(last.Fresh, fresh) && !refresh {
				return nil
			}
			body, e := jsonObject(planObservationPass{V: 1, Kind: "agentrt.plan_observation_pass/v1", Root: root, At: timeText(now), Fresh: fresh})
			if e != nil {
				return e
			}
			_, e = Append(ctx, tx, Event{Stream: passStream(root), Type: "note", Actor: "controller", Body: map[string]any{"text": "Plan verification pass", "plan_observation_pass": body}})
			return e
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		count += written
		if skipped {
			slog.Info("Plan observation round skipped", "root", root, "reason", "collector stream advanced during remote reads")
		}
		errs = append(errs, failed...)
	}
	return count, errors.Join(errs...)
}

func sameKeys[A, B any](a map[string]A, b map[string]B) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func recordPlanObservation(ctx context.Context, tx pgx.Tx, n planObservation) (bool, error) {
	if err := n.validate(); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742120,hashtext($1))`, n.Source); err != nil {
		return false, err
	}
	body, err := jsonObject(n)
	if err != nil {
		return false, err
	}
	var old []byte
	err = tx.QueryRow(ctx, `SELECT body_json->'plan_observation' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'plan_observation'->>'source_id'=$2 AND (body_json->'plan_observation'->>'source_version')::bigint=$3`, observationStream(n.Root), n.Source, n.SourceVersion).Scan(&old)
	if err == nil {
		parsed, e := canon.Parse(old)
		if e != nil {
			return false, e
		}
		had, _ := canon.Encode(parsed)
		want, _ := canon.Encode(body)
		if string(had) != string(want) {
			return false, fmt.Errorf("%w: source version changed", ErrConflict)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	_, err = Append(ctx, tx, Event{Stream: observationStream(n.Root), Type: "note", Actor: "controller", PlanNode: n.Step, Body: map[string]any{"text": "Plan verification observed", "plan_observation": body}})
	return err == nil, err
}

func readPlanSource(ctx context.Context, tx pgx.Tx, remote remoteRead, b planview.Binding, n *planObservation) error {
	switch b.Source {
	case "owner_signoff":
		var id int64
		var raw []byte
		e := tx.QueryRow(ctx, `SELECT event_id, body_json FROM agentrt.event WHERE event_type='owner_signoff' AND plan_node_id=$1 AND actor_id='owner' AND body_json->>'ref'=$2 ORDER BY seq DESC LIMIT 1`, b.Step, b.SignoffRef()).Scan(&id, &raw)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		var signed acceptanceSignoff
		if json.Unmarshal(raw, &signed) != nil || !signed.matches(b) {
			n.Reason = "Owner signoff lacks matching acceptance evidence"
			return nil
		}
		if e := haveArtifacts(ctx, tx, signed.Evidence); e != nil {
			if !errors.Is(e, ErrState) {
				return e
			}
			n.Reason = "Owner signoff evidence artifacts are missing"
			return nil
		}
		n.Value = "true"
		n.Evidence = append([]string{fmt.Sprintf("event:%d", id)}, signed.Evidence...)
		n.Reason = "Owner signed this acceptance version and evidence"
	case "git":
		if remote.err != nil {
			return remote.err
		}
		h := remote.history
		switch h.State {
		case landing.Unknown:
			return fmt.Errorf("incomplete history")
		case landing.Never:
			n.Value = "false"
			n.Reason = "Not landed"
			return nil
		case landing.Reverted:
			n.Value = "false"
			n.Reason = "Result reverted"
			n.Evidence = []string{h.Commit}
			return nil
		case landing.Effective:
			if h.Commit == "" {
				return fmt.Errorf("missing main result")
			}
		default:
			return fmt.Errorf("unknown history state")
		}
		n.Evidence = []string{b.Repo + "@" + h.Commit}
		n.Value = "true"
		n.Reason = "Result effective on main"
	}
	return nil
}

// checkPlanObservation holds plan observations and their passes to the
// controller (the vocabulary asks it of every event: vocab.go).
func checkPlanObservation(ev Event) error {
	if raw, ok := ev.Body["plan_observation"]; ok {
		var n planObservation
		if err := strictViewValue(raw, &n); err != nil {
			return err
		}
		if ev.Actor != "controller" || ev.Type != "note" || ev.Stream != observationStream(n.Root) || ev.PlanNode != n.Step {
			return invalid("plan observations are controller-owned")
		}
	}
	if raw, ok := ev.Body["plan_observation_pass"]; ok {
		var p planObservationPass
		if err := strictViewValue(raw, &p); err != nil {
			return err
		}
		if err := p.validate(); err != nil {
			return err
		}
		if ev.Actor != "controller" || ev.Type != "note" || ev.Stream != passStream(p.Root) || ev.PlanNode != "" {
			return invalid("plan observation passes are controller-owned")
		}
	}

	return nil
}
