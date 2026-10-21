package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The end of a gate batch (gates.go; CUT-td397-gate-runs-2026-10-20.md §3,
// reviews td397-2 A1, A2): every pass, whatever the deployment read or the
// rules say, each open batch of the environment is looked at in a
// transaction of its own. A ready member that cannot run is cancelled with
// why: its batch's deployment is no longer what the environment runs, the
// batch is past its deadline, its budget is spent or cannot be read, its
// standing envelope is no longer in force, every executor was withdrawn
// from it (TD-381), or no host took it for GateUnclaimed. A member a host
// holds is left to the lease sweep and the hard limit; once it is back at
// ready it is looked at again. A batch whose tasks are exactly its list, all
// ended, is closed: done when every one was accepted (which says each gate
// ran, not that it passed), stopped when any was cancelled, with a note of
// how many passed, failed and could not run.
//
// Locks: the members to move first, in task id order and NOWAIT (one a
// claim holds leaves the batch to the next pass), then the mission, then
// the events; never a task lock once the mission or its stream is held, as
// a claim takes task, mission, stream.

// EndGates is one pass over env's open gate batches. It returns how many
// tasks it cancelled and batches it closed.
func EndGates(ctx context.Context, db DB, env string) (int, error) {
	var ids []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT mission_id FROM agentrt.mission WHERE scope_json ? 'gate_run' AND created_by_actor_id = 'controller'
		  AND state = 'active' AND scope_json->'gate_run'->>'env' = $1 ORDER BY mission_id`, env)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, id := range ids {
		k, err := endBatch(ctx, db, env, id)
		n += k
		if err != nil {
			errs = append(errs, fmt.Errorf("gate batch %s: %w", id, err))
		}
	}
	return n, errors.Join(errs...)
}

// errLocked: a member is held elsewhere; the batch waits for the next pass.
var errLocked = errors.New("store: a gate member is locked")

// gateMemberRow is a member of a batch as endBatch reads it.
type gateMemberRow struct {
	entry gateEntry
	state string
}

func endBatch(ctx context.Context, db DB, env, mission string) (int, error) {
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		b, ok, err := readGateBatch(ctx, tx, mission, "")
		if err != nil || !ok || b.state != "active" || b.run.Env != env {
			return err
		}
		members, regular, err := batchMembers(ctx, tx, b)
		if err != nil {
			return err
		}
		cancel := map[string]string{}
		for _, m := range members {
			if m.state != "ready" {
				continue
			}
			why, err := cannotRun(ctx, tx, b, m.entry.Task)
			if err != nil {
				return err
			}
			if why != "" {
				cancel[m.entry.Task] = why
			}
		}
		closing := regular
		for _, m := range members {
			if !ended(m.state) && cancel[m.entry.Task] == "" {
				closing = false
			}
		}
		if len(cancel) == 0 && !closing {
			return nil
		}
		hook("gate end: read", mission)
		lock := []string{}
		for _, m := range members {
			if closing || cancel[m.entry.Task] != "" {
				lock = append(lock, m.entry.Task)
			}
		}
		slices.Sort(lock)
		if _, err = tx.Exec(ctx, `SELECT task_id FROM agentrt.task WHERE task_id = ANY($1) ORDER BY task_id FOR NO KEY UPDATE NOWAIT`, lock); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "55P03" {
				return errLocked
			}
			return err
		}
		if _, _, err = readGateBatch(ctx, tx, mission, "FOR NO KEY UPDATE"); err != nil {
			return err
		}
		for _, id := range lock {
			why := cancel[id]
			if why == "" {
				continue
			}
			t, err := readTask(ctx, tx, id, false) // locked above
			if err != nil || t.state != "ready" {
				if err != nil {
					return err
				}
				continue
			}
			// Read again under the locks: what was read before them may have
			// changed (a budget grown, a deployment recorded).
			if why, err = cannotRun(ctx, tx, b, id); err != nil {
				return err
			}
			if why == "" {
				continue
			}
			if err = moveTask(ctx, tx, "controller", t, "cancelled", "the gate could not run: "+why); err != nil {
				return err
			}
			n++
		}
		if !closing {
			return nil
		}
		members, regular, err = batchMembers(ctx, tx, b)
		if err != nil || !regular {
			return err
		}
		for _, m := range members {
			if !ended(m.state) {
				return nil
			}
		}
		to, note, err := batchSummary(ctx, tx, b, members)
		if err != nil {
			return err
		}
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(mission), Type: "note", Actor: "controller", MissionID: mission,
			Body: map[string]any{"text": note}}); err != nil {
			return err
		}
		if err = MoveMission(ctx, tx, "controller", mission, to, note); err != nil {
			return err
		}
		n++
		return nil
	})
	if errors.Is(err, errLocked) {
		return 0, nil
	}
	return n, err
}

// ended is whether a task's state is final.
func ended(state string) bool {
	return state == "accepted" || state == "cancelled" || state == "rejected"
}

// batchMembers is the batch's members in its list's order and whether the
// mission's tasks are exactly its list, each a member.
func batchMembers(ctx context.Context, tx pgx.Tx, b gateBatch) ([]gateMemberRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT task_id, state FROM agentrt.task WHERE mission_id = $1`, b.mission)
	if err != nil {
		return nil, false, err
	}
	states := map[string]string{}
	for rows.Next() {
		var id, state string
		if err = rows.Scan(&id, &state); err != nil {
			rows.Close()
			return nil, false, err
		}
		states[id] = state
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	regular := len(states) == len(b.run.Gates)
	var out []gateMemberRow
	for _, e := range b.run.Gates {
		state, have := states[e.Task]
		if !have {
			regular = false
			continue
		}
		_, member, err := gateMemberOf(ctx, tx, b, e.Task)
		if err != nil {
			return nil, false, err
		}
		if !member {
			regular = false
			continue
		}
		out = append(out, gateMemberRow{entry: e, state: state})
	}
	return out, regular, nil
}

// cannotRun is why a ready member of batch b cannot run now, or "".
func cannotRun(ctx context.Context, tx pgx.Tx, b gateBatch, taskID string) (string, error) {
	var now time.Time
	var ready *time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp(), (SELECT max(at) FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' AND body_json->>'to' = 'ready')`, taskID).Scan(&now, &ready); err != nil {
		return "", err
	}
	if d := newRecordedDeploys(ctx, tx).get(b.run.Env); d.ok && (d.commit != b.run.Commit || !sameRef(d.ref, b.run.Ref)) {
		return fmt.Sprintf("what %s runs is now %s", b.run.Env, short(d.commit)), nil
	}
	if !now.Before(b.created.Add(GateBatchHours * time.Hour)) {
		return fmt.Sprintf("its batch is past its deadline (%d hours)", GateBatchHours), nil
	}
	m, err := readMissionBudget(ctx, tx, b.mission, "")
	if err != nil {
		return "", err
	}
	s, ok, err := m.spend(ctx, tx, ruleCache{}, b.mission)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "its budget cannot be read", nil
	case s.why() != "":
		return s.why(), nil
	}
	if b.envelope != "" {
		env, err := inForce(ctx, tx, b.envelope)
		if err != nil {
			return "", err
		}
		if env == nil {
			return "its standing envelope " + b.envelope + " is no longer in force", nil
		}
	}
	why, err := noneLeft(ctx, tx, taskID, b.envelope)
	if err != nil || why != "" {
		return strings.TrimPrefix(why, withdrawnBlockMark), err
	}
	if ready != nil && !now.Before(ready.Add(GateUnclaimed)) {
		return fmt.Sprintf("no host took it for %s", GateUnclaimed), nil
	}
	return "", nil
}

// batchSummary is how the batch closes and the note that says why: how many
// of its gates passed, failed and could not run.
func batchSummary(ctx context.Context, tx pgx.Tx, b gateBatch, members []gateMemberRow) (string, string, error) {
	pass, fail, cancelled := 0, 0, 0
	for _, m := range members {
		if m.state == "cancelled" {
			cancelled++
			continue
		}
		var verdict string
		err := tx.QueryRow(ctx, `SELECT verdict FROM agentrt.verification WHERE task_id = $1 AND method = 'gate' AND check_ref = $2
		  ORDER BY created_at DESC LIMIT 1`, m.entry.Task, m.entry.Test).Scan(&verdict)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			cancelled++
		case err != nil:
			return "", "", err
		case verdict == "pass":
			pass++
		default:
			fail++
		}
	}
	to := "done"
	if cancelled > 0 {
		to = "stopped"
	}
	return to, fmt.Sprintf("the gates of %s at %s ended: %d passed, %d failed, %d could not run", b.run.Env, short(b.run.Commit),
		pass, fail, cancelled), nil
}
