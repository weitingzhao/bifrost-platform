package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// The main reconciler (S3 must 3, CUT §10, §10.6). An operation at main
// (main_intent, or a main_result other than non_ff) holds its mission with no
// time limit (landingHolds); this is what moves it on, from what main shows.
// It reads no rule and checks no eligibility: a rule withdrawn or expired
// does not stop it (§2.3 reconcile).

// PushTimeout is how long one push to main may take before it is given up
// (S3's push must guarantee it, CUT §10.6 2A). MainFence is how long after a
// main_intent a missing R is taken to mean the push never landed: the
// timeout and a margin. Before it, absence proves nothing.
const (
	PushTimeout = 2 * time.Minute
	MainFence   = PushTimeout + 10*time.Minute
)

// MainEvidence is what main shows for an operation's R: main's head as
// fetched, and whether R is in its history. HasR false is a real negative
// (a complete history without R), never a read that failed.
type MainEvidence struct {
	Head string
	HasR bool
}

// MainEvidenceFunc reads main for R. A failure is an error, never HasR false.
type MainEvidenceFunc func(ctx context.Context, repo, r string) (MainEvidence, error)

// MainCandidate is an operation the reconciler reads main for, with the
// state it was listed in: the locked write is refused if it moved since.
type MainCandidate struct {
	Op, Root, Phase, Outcome, R, M0 string
	IntentAt                        time.Time
	// Late: it once had a main_intent and ended failed{bound} as never sent;
	// main is read for a late landing (§10.6 2A).
	Late bool
}

// ReconcileDenied ends every main_result{denied} failed{denied}, from the
// stream alone (no Git). It returns the operations it ended.
func ReconcileDenied(ctx context.Context, db DB, repo string) ([]string, error) {
	stream := "landing:" + repo
	var ended []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		ended = nil
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		for _, op := range latestOps(s) {
			o := s.Operations[op]
			if o.Phase != "main_result" || o.Outcome != "denied" {
				continue
			}
			if err := appendMainRecord(ctx, tx, stream, o.Task, map[string]any{"kind": "failed", "op": op, "why": "denied"}); err != nil {
				return err
			}
			ended = append(ended, op)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ended, nil
}

// MainCandidates lists what the reconciler reads main for: each root's latest
// operation at main other than denied, and each operation that once had a
// main_intent and ended failed{bound}, with no late-landing record yet.
func MainCandidates(ctx context.Context, db DB, repo string) ([]MainCandidate, error) {
	stream := "landing:" + repo
	var out []MainCandidate
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		intents, err := mainIntents(ctx, tx, stream)
		if err != nil {
			return err
		}
		for _, op := range latestOps(s) {
			o := s.Operations[op]
			if o.AtMain() && !(o.Phase == "main_result" && o.Outcome == "denied") {
				out = append(out, MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, IntentAt: intents[op]})
			}
		}
		for op, at := range intents {
			o, ok := s.Operations[op]
			if !ok || o.Phase != "failed" || o.Outcome != "bound" {
				continue
			}
			seen, err := hasAnomaly(ctx, tx, stream, op, "late_landing")
			if err != nil {
				return err
			}
			if !seen {
				out = append(out, MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, IntentAt: at, Late: true})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Op < out[j].Op })
		return nil
	})
	return out, err
}

// ReconcileMainOne writes what main's evidence, read with no lock held,
// says about one candidate (§10.2, §10.6). Under the stream lock it checks
// the operation is still as listed; if it moved, the evidence is stale and
// nothing is written. It returns what it did ("" for nothing).
//
//	main_intent, R in main          → main_result{unknown}, landed
//	main_intent, R absent, fenced   → main_result{unknown}, failed{bound} (head = M0)
//	                                  main_result{non_ff} (head ≠ M0)
//	main_result{unknown}            → the same without the first record
//	main_result{ok}, R in main      → landed
//	main_result{ok}, R absent       → one anomaly record (ok_missing); the Owner
//	                                  confirms failed{main_lost} on a card
//	failed{bound} after an intent,
//	R in main                       → one anomaly record (late_landing)
//
// Absence before the fence (MainFence after the main_intent) writes nothing:
// a push may still be landing.
func ReconcileMainOne(ctx context.Context, db DB, repo string, c MainCandidate, ev MainEvidence) (string, error) {
	stream := "landing:" + repo
	did := ""
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = ""
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[c.Op]
		if !ok || o.Task != c.Root || o.Phase != c.Phase || o.Outcome != c.Outcome || o.R != c.R || o.M0 != c.M0 ||
			(!c.Late && s.LatestOperation(c.Root) != c.Op) {
			return nil // moved since it was listed: the evidence is stale
		}
		now := DecisionAt()
		fenced := !c.IntentAt.IsZero() && !now.Before(c.IntentAt.Add(MainFence))
		write := func(records ...map[string]any) error {
			for _, r := range records {
				if err := appendMainRecord(ctx, tx, stream, c.Root, r); err != nil {
					return err
				}
			}
			return nil
		}
		unknown := map[string]any{"kind": "main_result", "op": c.Op, "outcome": "unknown"}
		switch {
		case c.Late:
			if !ev.HasR {
				return nil
			}
			did = "late_landing"
			return anomaly(ctx, tx, stream, c, ev, "late_landing")
		case c.Phase == "main_result" && c.Outcome == "ok":
			if ev.HasR {
				did = "landed"
				return write(map[string]any{"kind": "landed", "op": c.Op, "R": c.R})
			}
			seen, err := hasAnomaly(ctx, tx, stream, c.Op, "ok_missing")
			if err != nil || seen {
				return err
			}
			did = "ok_missing"
			return anomaly(ctx, tx, stream, c, ev, "ok_missing")
		case c.Phase == "main_intent" || c.Phase == "main_result" && c.Outcome == "unknown":
			var first []map[string]any
			if c.Phase == "main_intent" {
				first = append(first, unknown)
			}
			switch {
			case ev.HasR:
				did = "landed"
				return write(append(first, map[string]any{"kind": "landed", "op": c.Op, "R": c.R})...)
			case !fenced:
				return nil
			case ev.Head == c.M0:
				did = "unsent"
				return write(append(first, map[string]any{"kind": "failed", "op": c.Op, "why": "bound"})...)
			default:
				did = "non_ff"
				return write(map[string]any{"kind": "main_result", "op": c.Op, "outcome": "non_ff"})
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return did, nil
}

// ReconcileMain is one pass: denied first (no Git), then each candidate's
// evidence read with no transaction open, then its locked write. A read that
// fails skips that operation only; the errors are joined. It never reads a
// rule.
func ReconcileMain(ctx context.Context, db DB, repo string, read MainEvidenceFunc) (map[string]string, error) {
	done := map[string]string{}
	denied, err := ReconcileDenied(ctx, db, repo)
	if err != nil {
		return done, err
	}
	for _, op := range denied {
		done[op] = "denied"
	}
	if read == nil {
		return done, nil
	}
	cands, err := MainCandidates(ctx, db, repo)
	if err != nil {
		return done, err
	}
	var errs []error
	for _, c := range cands {
		ev, err := read(ctx, repo, c.R)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Op, err))
			continue
		}
		did, err := ReconcileMainOne(ctx, db, repo, c, ev)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Op, err))
			continue
		}
		if did != "" {
			done[c.Op] = did
		}
	}
	return done, errors.Join(errs...)
}

func latestOps(s landing.State) []string {
	roots := map[string]bool{}
	for _, o := range s.Operations {
		if o.Task != "" {
			roots[o.Task] = true
		}
	}
	var ops []string
	for root := range roots {
		if op := s.LatestOperation(root); op != "" {
			ops = append(ops, op)
		}
	}
	sort.Strings(ops)
	return ops
}

// mainIntents is when each operation's main_intent was written.
func mainIntents(ctx context.Context, tx pgx.Tx, stream string) (map[string]time.Time, error) {
	rows, err := tx.Query(ctx, `SELECT body_json->'landing'->>'op', min(at) FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing'->>'kind' = 'main_intent'
	  GROUP BY 1`, stream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var op string
		var at time.Time
		if err := rows.Scan(&op, &at); err != nil {
			return nil, err
		}
		out[op] = at
	}
	return out, rows.Err()
}

func appendMainRecord(ctx context.Context, tx pgx.Tx, stream, root string, r map[string]any) error {
	r["v"] = landing.Version
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": fmt.Sprintf("Main reconciled: %v %v", r["kind"], r["outcome"]), "landing": r}})
	return err
}

func hasAnomaly(ctx context.Context, tx pgx.Tx, stream, op, kind string) (bool, error) {
	var seen bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  AND body_json->'landing_anomaly'->>'op' = $2 AND body_json->'landing_anomaly'->>'kind' = $3)`, stream, op, kind).Scan(&seen)
	return seen, err
}

// anomaly records, once per operation and kind, what only the Owner can
// settle: a push reported ok whose R main does not show (ok_missing), or an
// operation ended as never sent whose R main now shows (late_landing).
func anomaly(ctx context.Context, tx pgx.Tx, stream string, c MainCandidate, ev MainEvidence, kind string) error {
	text := map[string]string{
		"ok_missing":   "Main reconciled: the push was reported ok, but main does not have R; the Owner confirms whether it is lost",
		"late_landing": "Main reconciled: an operation ended as never sent, but main now has R; the Owner looks",
	}[kind]
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: c.Root,
		Body: map[string]any{"text": text, "landing_anomaly": map[string]any{"op": c.Op, "root": c.Root, "kind": kind, "R": c.R, "M0": c.M0, "head": ev.Head}}})
	return err
}

// badLandingAnomaly is why an anomaly body is refused, or "".
func badLandingAnomaly(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return "landing_anomaly is not an object"
	}
	for _, k := range []string{"op", "root", "kind", "R"} {
		if s, _ := m[k].(string); s == "" {
			return "landing_anomaly needs " + k
		}
	}
	for _, k := range []string{"M0", "head"} {
		if _, ok := m[k].(string); !ok {
			return "landing_anomaly needs " + k
		}
	}
	if m["kind"] != "ok_missing" && m["kind"] != "late_landing" {
		return "landing_anomaly kind is not ok_missing or late_landing"
	}
	return ""
}

// checkLandingAnomalyAppend: only the controller records an anomaly, on the
// repository stream, under the root it names.
func checkLandingAnomalyAppend(ev Event) error {
	v, exists := ev.Body["landing_anomaly"]
	if !exists {
		return nil
	}
	m, _ := v.(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != "landing:bifrost-platform" || m == nil || ev.TaskID == "" || ev.TaskID != m["root"] {
		return fmt.Errorf("landing_anomaly is written by the controller on the repository stream under its root")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("landing_anomaly is not a landing record")
	}
	return nil
}
