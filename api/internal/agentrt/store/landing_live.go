package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// The live landing's second gate and its result (S3 B1, CUT §2.5 steps 6–7,
// §11.4). Three steps, never one: the authorization transaction here writes
// reserved and main_intent; the push to main is the caller's, outside any
// transaction, only by the caller that committed this intent and only before
// its absolute deadline; RecordPushResult writes what the remote said under
// the stream lock, accepting whatever the main reconciler wrote first.

// LiveObservation is what the gate reads outside the database under its
// locks: main's head, the latest CI for R, and the release window.
type LiveObservation struct {
	Main          string
	CI            landing.CIRun
	WindowBlocked bool
}

// Authorization is a committed main_intent: the only licence to push, held
// by the caller that wrote it. Deadline is the intent's time plus
// PushTimeout; the push never starts at or past it.
type Authorization struct {
	Op, Root, R, M0 string
	IntentSeq       int64
	IntentAt        time.Time
	Deadline        time.Time
	RuleVersion     int
	QuotaDay        string
}

// ErrLandingDayChanged: the UTC day turned between the gate's locks and its
// decision; the transaction rolls back whole and the next pass takes the
// new day's quota lock first (review 660 R-03).
var ErrLandingDayChanged = errors.New("landing gate: the UTC day changed during the gate")

// AuthorizeLandingPush runs the live second gate for op in one transaction,
// in the global lock order: capability (shared), the tenant's landing rule
// (shared), the plan step, the task, the mission (FOR SHARE), the day's
// quota stream, the repository stream. Under them it reads the facts (Git
// included) and observes main, CI and the window, then takes the decision
// time (CUT §9.7) and selects the rule again at it. It writes nothing unless
// every check passes: the repository is not stopped (B6), the operation is a
// live branch_pushed with R on M0, EligibleToPush, main is still M0, the
// window is free, and R's latest CI, created after prepared, succeeded. Then
// it reserves the day's quota for the root (once a day per root) and writes
// main_intent, returning the Authorization. A refusal is a Decision and no
// error; the caller parks or fails the operation on its next pass.
func AuthorizeLandingPush(ctx context.Context, db DB, src LandingSource, op string, observe func(context.Context, landing.Operation) (LiveObservation, error)) (Authorization, landing.Decision, error) {
	const stream = "landing:bifrost-platform"
	var auth Authorization
	var decision landing.Decision
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		auth, decision = Authorization{}, landing.Decision{}
		state, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, exists := state.Operations[op]
		if !exists {
			return fmt.Errorf("unknown landing operation")
		}
		var tenant, step, mission string
		if err = tx.QueryRow(ctx, `SELECT m.tenant, COALESCE(t.plan_node_id,''), m.mission_id FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, o.Task).Scan(&tenant, &step, &mission); err != nil {
			return err
		}
		day := DecisionAt().UTC().Format("2006-01-02")
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1, hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
			return err
		}
		if step != "" {
			if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE`, step); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR SHARE`, o.Task); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission); err != nil {
			return err
		}
		quota := "landing-quota:" + tenant + ":" + day
		if err = lockStream(ctx, tx, quota); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		if stopped, err := repoStopped(ctx, tx, stream); err != nil || stopped {
			decision = landing.Decision{Reason: "stopped"}
			return err
		}
		f, err := LandingFacts(ctx, tx, src, "prod", "live", o.Task, op, "", DecisionAt())
		if err != nil {
			return err
		}
		o = f.Operation
		if o.Phase != "branch_pushed" || o.DryRun || o.R == "" || o.M0 == "" {
			decision = landing.Decision{Reason: "not_ready"}
			return nil
		}
		obs, err := observe(ctx, o)
		if err != nil {
			return err
		}
		// The reads above took as long as they took: decide now, on the
		// rule in force now (CUT §9.7, review 656 B-B1).
		final := DecisionAt()
		if final.UTC().Format("2006-01-02") != day {
			return ErrLandingDayChanged
		}
		g, _, err := landingFactsDB(ctx, tx, src, "prod", "live", o.Task, op, final)
		if err != nil {
			return err
		}
		g.SingleCommit, g.Changes, g.History, g.DependenciesEffective = f.SingleCommit, f.Changes, f.History, f.DependenciesEffective
		if decision = landing.EligibleToPush(g, final); !decision.Allowed {
			return nil
		}
		switch {
		case obs.Main != o.M0:
			decision = landing.Decision{Reason: "main_changed"}
			return nil
		case obs.WindowBlocked:
			decision = landing.Decision{Reason: "window"}
			return nil
		case obs.CI.R != o.R || obs.CI.Status != "Succeeded" || !obs.CI.CreatedAt.After(o.PreparedAt) || obs.CI.CreatedAt.After(final):
			decision = landing.Decision{Reason: "ci"}
			return nil
		}
		q, err := LandingState(ctx, tx, quota)
		if err != nil {
			return err
		}
		if _, reserved := q.Reservations[o.Task]; !reserved {
			if _, err = Append(ctx, tx, Event{Stream: quota, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
				Body: map[string]any{"text": "Landing quota reserved", "landing": map[string]any{"v": landing.Version, "kind": "reserved", "op": op, "root": o.Task}}}); err != nil {
				return err
			}
		}
		// main_intent needs R's success on the stream (protocol): the CI the
		// gate decided on, recorded unless it already is.
		if o.CI.String("run") != obs.CI.Run || o.CI.String("status") != obs.CI.Status || !o.CI.Time("created_at").Equal(obs.CI.CreatedAt) {
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
				Body: map[string]any{"text": "Landing ci_seen at the second gate", "landing": map[string]any{"v": landing.Version, "kind": "ci_seen", "op": op,
					"run": obs.CI.Run, "status": obs.CI.Status, "R": o.R, "created_at": obs.CI.CreatedAt.Format(time.RFC3339Nano)}}}); err != nil {
				return err
			}
		}
		a, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
			Body: map[string]any{"text": fmt.Sprintf("Landing main_intent; second gate rule version %d", g.Rule.Version),
				"landing": map[string]any{"v": landing.Version, "kind": "main_intent", "op": op, "R": o.R, "M0": o.M0}}})
		if err != nil {
			return err
		}
		auth = Authorization{Op: op, Root: o.Task, R: o.R, M0: o.M0, IntentSeq: a.Seq, IntentAt: a.At,
			Deadline: a.At.Add(PushTimeout), RuleVersion: g.Rule.Version, QuotaDay: day}
		decision = landing.Decision{Allowed: true}
		return nil
	})
	if err != nil {
		return Authorization{}, landing.Decision{}, err
	}
	return auth, decision, nil
}

// RecordPushResult writes what the remote said about auth's push, under the
// stream lock (CUT §2.5 step 7). The operation must still be at this intent:
// if the main reconciler already moved it (it read main first), its record
// stands and nothing is written; the push is never repeated. ok records
// main_result{ok}: landed follows from main's evidence (§10); denied records
// main_result{denied}, which ReconcileDenied ends failed{denied} with the
// repository stopped (B6); non_ff and unknown are recorded as they are.
func RecordPushResult(ctx context.Context, db DB, auth Authorization, outcome string) (bool, error) {
	const stream = "landing:bifrost-platform"
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[auth.Op]
		if !ok || o.Phase != "main_intent" || o.R != auth.R {
			return nil // the reconciler moved it first: its record stands
		}
		if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: auth.Root,
			Body: map[string]any{"text": "Landing main_result: " + outcome, "landing": map[string]any{"v": landing.Version, "kind": "main_result", "op": auth.Op, "outcome": outcome}}}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

// repoStopped: a failed{denied} stopped the repository and no Owner's
// resume has answered that stop's generation yet (B6, CUT §11.4).
func repoStopped(ctx context.Context, tx pgx.Tx, stream string) (bool, error) {
	var stopped, resumed int
	err := tx.QueryRow(ctx, `SELECT
	    COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0),
	    COALESCE(max((body_json->'landing_resume'->>'generation')::int), 0)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&stopped, &resumed)
	return stopped > resumed, err
}

// stopRepo records the repository stopped by op's denied push, in the
// transaction that ends it failed{denied}: the stop and the ending commit
// together or not at all (B6).
func stopRepo(ctx context.Context, tx pgx.Tx, stream, repo, op, root string) error {
	var gen int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0) + 1 FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&gen); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": "Landing stopped for the repository: a push to main was denied; the Owner resumes it",
			"landing_stop": map[string]any{"repo": repo, "op": op, "root": root, "generation": gen}}})
	return err
}

// badLandingStop is why a stop or resume body is refused, or "".
func badLandingStop(name string, v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return name + " is not an object"
	}
	if r, _ := m["repo"].(string); r == "" {
		return name + " needs repo"
	}
	g, ok := m["generation"].(int64)
	if !ok {
		if f, isF := m["generation"].(float64); isF && f == float64(int64(f)) {
			g, ok = int64(f), true
		} else if i, isI := m["generation"].(int); isI {
			g, ok = int64(i), true
		}
	}
	if !ok || g < 1 {
		return name + " generation is not a positive integer"
	}
	if name == "landing_stop" {
		if op, _ := m["op"].(string); op == "" {
			return "landing_stop needs op"
		}
		if !landingRoot.MatchString(fmt.Sprint(m["root"])) {
			return "landing_stop root is not a task"
		}
	}
	return ""
}

// checkLandingStopAppend: only the controller stops or resumes, on the
// repository stream, never as a landing record.
func checkLandingStopAppend(ev Event) error {
	_, stop := ev.Body["landing_stop"]
	_, resume := ev.Body["landing_resume"]
	if !stop && !resume {
		return nil
	}
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != "landing:bifrost-platform" {
		return fmt.Errorf("a landing stop or resume is written by the controller on the repository stream")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("a landing stop or resume is not a landing record")
	}
	return nil
}
