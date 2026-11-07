package store

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

// SettleInMain is the drill pass's database step for roots whose change
// reached main another way: a root whose latest operation on repo's landing
// stream ended failed{in_main}, not disposed of yet, with a landed note on
// its mission stream (RecordLandings), is disposed of as main_delivered,
// naming that main commit. It is the controller's own record, not the
// Owner's: the in_main card is then not asked (a disposition skips its
// trigger), and one already open is withdrawn by SettleLandingCard. It reads
// no Git. Each root is read again under the task, mission, then stream locks
// and recorded at most once. It returns how many it recorded.
func SettleInMain(ctx context.Context, db DB, repo string) (int, error) {
	stream := "landing:" + repo
	var due []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		state, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		for op, o := range state.Operations {
			if o.Task == "" || state.LatestOperation(o.Task) != op || o.Phase != "failed" || o.Outcome != "in_main" {
				continue
			}
			why, err := landingDisposed(ctx, tx, stream, o.Task, op)
			if err != nil {
				return err
			}
			if why != "" {
				continue
			}
			main, err := rootLandedCommit(ctx, tx, o.Task)
			if err != nil {
				return err
			}
			if main != "" {
				due = append(due, o.Task)
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	sort.Strings(due)
	n := 0
	for _, root := range due {
		appended := false
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			appended = false
			var mission string
			if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, root).Scan(&mission); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission); err != nil {
				return err
			}
			if err := lockStream(ctx, tx, stream); err != nil {
				return err
			}
			// Read again under the locks: a claim, a card answer or another
			// pass may have moved the root since the list.
			state, err := LandingState(ctx, tx, stream)
			if err != nil {
				return err
			}
			op := state.LatestOperation(root)
			if o := state.Operations[op]; op == "" || o.Phase != "failed" || o.Outcome != "in_main" {
				return nil
			}
			if why, err := landingDisposed(ctx, tx, stream, root, op); err != nil || why != "" {
				return err
			}
			main, err := rootLandedCommit(ctx, tx, root)
			if err != nil || main == "" {
				return err
			}
			if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root, MissionID: mission,
				Body: map[string]any{"text": "Landing settled: the change is on main as " + main[:12],
					"landing_disposed": map[string]any{"v": "1", "root": root, "op": op, "why": "main_delivered", "main": main}}}); err != nil {
				return err
			}
			appended = true
			return nil
		}); err != nil {
			return n, err
		}
		// Counted only once committed.
		if appended {
			n++
		}
	}
	return n, nil
}
