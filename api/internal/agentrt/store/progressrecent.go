package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// recentView is the last ended tasks, by the event that ended them, and the
// last failed attempts, whatever came of their tasks (SPEC §2.6, T06). Both
// read the events newest first through the (event_type, at) index, and break
// ties of time by event id at every step, so the same events always give the
// same lists. A task withdrawn (progressstop.go) says by whom and why; a task
// whose change is on main says that instead (status finished, on_main and its
// reason), whatever its state.
func recentView(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	// No task ends and no attempt fails any more (store cut 4, 2026-11-14).
	return map[string]any{"tasks": rawJSON("[]"), "failed_attempts": rawJSON("[]")}, nil
}
