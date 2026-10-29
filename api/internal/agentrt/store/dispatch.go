package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const dispatchPage = 500

// Dispatch is one pass of readying proposed tasks: a task of an active
// mission whose dependencies are all accepted becomes ready (its route is
// decided and the hosts woken); one with a dependency rejected or cancelled
// is blocked, with that reason. It reads every proposed task of an active
// mission, a page at a time after a cursor, so tasks that wait never keep
// later ones from being looked at (review 175 A3). One task that fails does
// not stop the others. It returns how many tasks it moved.
func Dispatch(ctx context.Context, db DB) (int, error) {
	moved := 0
	var errs []error
	var at any
	var after string
	for {
		type row struct {
			id      string
			created time.Time
		}
		var page []row
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id, t.created_at FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
			  WHERE t.state = 'proposed' AND m.state = 'active'
			    AND ($1::timestamptz IS NULL OR (t.created_at, t.task_id) > ($1::timestamptz, $2::text))
			  ORDER BY t.created_at, t.task_id LIMIT $3`, at, after, dispatchPage)
			if err != nil {
				return err
			}
			page, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
				var x row
				return x, r.Scan(&x.id, &x.created)
			})
			return err
		}); err != nil {
			return moved, errors.Join(append(errs, err)...)
		}
		for _, x := range page {
			did, err := dispatchOne(ctx, db, x.id)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", x.id, err))
			}
			if did {
				moved++
			}
		}
		if len(page) < dispatchPage {
			return moved, errors.Join(errs...)
		}
		at, after = page[len(page)-1].created, page[len(page)-1].id
	}
}

func dispatchOne(ctx context.Context, db DB, id string) (bool, error) {
	hook("dispatching", id)
	moved := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		// The task first, exclusively: AddDependency takes it the same way
		// and adds an edge only to a proposed task (review 175 A1), so the
		// edges read below are all there are until this commits.
		t, err := readTask(ctx, tx, id, true)
		if err != nil || t.state != "proposed" {
			return err
		}
		// The dependencies, after the task (a dependent before what it waits
		// for), held so none changes state before this commits.
		rows, err := tx.Query(ctx, `SELECT s.task_id, s.state FROM agentrt.task_dependency d
		  JOIN agentrt.task s ON s.task_id = d.depends_on_task_id WHERE d.task_id = $1 ORDER BY s.task_id FOR SHARE OF s`, id)
		if err != nil {
			return err
		}
		type dep struct{ id, state string }
		deps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (dep, error) {
			var d dep
			return d, r.Scan(&d.id, &d.state)
		})
		if err != nil {
			return err
		}
		blocked, waiting := "", false
		for _, d := range deps {
			switch d.state {
			case "accepted":
			case "rejected", "cancelled":
				if blocked == "" {
					blocked = fmt.Sprintf("dependency %s is %s", d.id, d.state)
				}
			default:
				waiting = true
			}
		}
		// The mission last (task before mission), shared, so it cannot stop
		// or pause before this commits; a task of a mission that is not
		// active moves neither way (review 175 B2).
		var mstate string
		if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, t.mission).Scan(&mstate); err != nil {
			return err
		}
		switch {
		case mstate != "active":
			return nil
		case blocked != "":
			moved = true
			return moveTask(ctx, tx, "controller", t, "blocked", blocked)
		case waiting:
			return nil
		}
		moved = true
		return moveTask(ctx, tx, "controller", t, "ready", "")
	})
	return moved && err == nil, err
}
