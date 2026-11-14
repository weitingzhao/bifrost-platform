package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// An assessment's report and its record (TD-391; assess.go). The report is
// one JSON object; it is taken only whole: an option of the item, a
// confidence, two to four reasons each citing refs the spec gave (each ref is
// recorded as the record it names, so the Console links to it), and what the
// seat could not check. What the report says is advice, recorded on the
// item's stream and read by nothing that acts.

// Bounds of an assessment.
const (
	minReasons        = 2
	maxReasons        = 4
	maxReasonRunes    = 300
	maxReasonRefs     = 4
	maxUnchecked      = 5
	maxUncheckedRunes = 200
)

// approvalOptions are an approval's options as an assessment names them.
var approvalOptions = []AssessOption{{Key: "approve", Label: "Approve"}, {Key: "reject", Label: "Reject"}}

// ApprovalOptions are an approval's options for its assessment.
func ApprovalOptions() []AssessOption { return slices.Clone(approvalOptions) }

// assessState is what an item's stream says: whether it was asked (when,
// its task, its option keys and refs) and its outcome, an assessment (the
// option it recommends) or why there is none.
type assessState struct {
	requested   bool
	asked       bool
	askedAt     time.Time
	task        string
	options     []string
	refs        map[string]any
	done        bool
	recommended string
	unavailable string
}

// assessStateOf reads the item's stream; the caller holds it when it acts on
// what it reads.
func assessStateOf(ctx context.Context, tx pgx.Tx, stream string) (assessState, error) {
	var st assessState
	rows, err := tx.Query(ctx, `SELECT COALESCE(task_id, ''), body_json, at FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
	  ORDER BY seq`, stream)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var task string
		var body map[string]any
		var at time.Time
		if err = rows.Scan(&task, &body, &at); err != nil {
			return st, err
		}
		if _, ok := body["assessment_requested"].(map[string]any); ok {
			st.requested = true
		}
		if ask, ok := body["ask"].(map[string]any); ok && !st.asked {
			st.asked, st.askedAt, st.task = true, at, task
			st.refs, _ = ask["refs"].(map[string]any)
			if keys, ok := ask["options"].([]any); ok {
				for _, k := range keys {
					if s, ok := k.(string); ok {
						st.options = append(st.options, s)
					}
				}
			}
		}
		if a, ok := body["assessment"].(map[string]any); ok && !st.done {
			st.done = true
			st.recommended, _ = a["recommended"].(string)
		}
		if why, ok := body["unavailable"].(string); ok && !st.done {
			st.done, st.unavailable = true, why
		}
	}
	return st, rows.Err()
}

// late says whether an assessment asked and not in has run past its time.
func (st assessState) late(now time.Time) bool {
	return st.asked && !st.done && now.Sub(st.askedAt) >= AssessTimeout
}

func timeoutText() string { return fmt.Sprintf("%d minutes", int(AssessTimeout/time.Minute)) }

// noneInTime is why an assessment past its time has none.
func noneInTime() string { return "none came within " + timeoutText() }

// stopAssessment stops an assessment's mission that has not ended.
func stopAssessment(ctx context.Context, tx pgx.Tx, mission, why string) error {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`, mission).Scan(&state); err != nil {
		return err
	}
	if !slices.Contains([]string{"proposed", "active", "paused"}, state) {
		return nil
	}
	return MoveMission(ctx, tx, "controller", mission, "stopped", why)
}

// EndAssessments records unavailable every assessment that can no longer
// come: its task blocked, cancelled or otherwise ended without one, or none
// within AssessTimeout of its ask; its task is cancelled (a live attempt
// with it) and its mission stopped. It returns how many it ended.
func EndAssessments(ctx context.Context, db DB) (int, error) {
	if err := endUnstartedAssessments(ctx, db); err != nil {
		return 0, err
	}
	type open struct{ mission, stream string }
	var list []open
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT mission_id, scope_json->>'assessment_of' FROM agentrt.mission
		  WHERE scope_json ? 'assessment_of' AND state IN ('proposed','active','paused') ORDER BY created_at, mission_id`)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (open, error) {
			var o open
			return o, r.Scan(&o.mission, &o.stream)
		})
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, o := range list {
		did, err := endAssessment(ctx, db, o.mission, o.stream)
		if err != nil {
			errs = append(errs, fmt.Errorf("assessment %s: %w", o.stream, err))
		}
		if did {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// endAssessment ends one assessment's mission when its assessment can no
// longer come. Locks as a mission's close takes them: its live attempts, its
// tasks (a task held by another pass is left to the next), the item's
// stream, the mission.
func endAssessment(ctx context.Context, db DB, mission, stream string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		attempts, err := liveOf(ctx, tx, mission, "FOR UPDATE OF a")
		if err != nil {
			return err
		}
		left, err := unfinishedOf(ctx, tx, mission, "FOR NO KEY UPDATE NOWAIT")
		if err != nil {
			return err
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		st, err := assessStateOf(ctx, tx, stream)
		if err != nil {
			return err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		why := ""
		if st.done {
			// The outcome is recorded: an accepted assessment's mission is
			// closed as done (close.go); anything left after one that is
			// unavailable is cancelled with its mission.
			if st.unavailable == "" || len(left) == 0 {
				return nil
			}
			why = "the assessment is unavailable: " + st.unavailable
		} else {
			var state, reason string
			if err = tx.QueryRow(ctx, `SELECT t.state, COALESCE((SELECT e.body_json->>'reason' FROM agentrt.event e WHERE e.task_id = t.task_id
			    AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1), '')
			  FROM agentrt.task t WHERE t.task_id = $1`, st.task).Scan(&state, &reason); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			switch {
			case !st.asked:
				why = "it was never asked"
			case state == "blocked":
				why = "its task was blocked: " + oneLineRunes(reason, 200)
			case slices.Contains([]string{"cancelled", "rejected", "accepted"}, state):
				why = "its task ended " + state + " without one"
			case st.late(now):
				why = noneInTime()
			default:
				return nil
			}
			why = oneLineRunes(why, assessFieldRunes)
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: mission, TaskID: st.task,
				Body: map[string]any{"text": "AI assessment unavailable: " + why, "unavailable": why}}); err != nil {
				return err
			}
			why = "the assessment is unavailable: " + why
		}
		if err = cancelAll(ctx, tx, attempts, left, why); err != nil {
			return err
		}
		did = true
		return stopAssessment(ctx, tx, mission, why)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "55P03" {
		return false, nil // a task held by another pass: the next pass looks again
	}
	return did, err
}
