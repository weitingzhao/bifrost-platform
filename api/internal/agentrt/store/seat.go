package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Seats the platform registers itself (W-31 round 4, core block C-2; the
// coordinating thread 2026-10-29, closing the gap C-1 left: how the
// coordinator's proposer seat comes to be). Seats are otherwise only the
// seed's (seed.go), and the seed runs once, on an empty database. A seat
// here is registered at the platform's start, once, and only while the
// platform's own token table names a token of that seat (platform-auth.yaml:
// the Owner adds it, as he adds a host's token): no route writes a seat, and
// a seat no token names stays unregistered. Registering it grants nothing by
// itself: a pin needs the Owner's signed envelope naming the seat (pin.go).
var RegisterableSeats = []string{"seat:coordinator"}

// EnsureSeats registers each seat of RegisterableSeats that configured
// names (the seat tokens the platform loaded) and that is not registered
// yet, with one actor_registered each; a seat registered already is left as
// it is, and an actor of that name of another kind is an error. Two
// processes starting together register it once (the registration lock).
func EnsureSeats(ctx context.Context, db DB, configured []string) ([]string, error) {
	var done []string
	for _, seat := range RegisterableSeats {
		if !slices.Contains(configured, seat) {
			continue
		}
		var added bool
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			added = false
			if err := writerLock(ctx, tx, seat); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742109)`); err != nil {
				return err
			}
			var kind string
			err := tx.QueryRow(ctx, `SELECT actor_kind FROM agentrt.actor WHERE actor_id = $1`, seat).Scan(&kind)
			switch {
			case err == nil && kind == "seat":
				return nil
			case err == nil:
				return fmt.Errorf("%w: actor %s is a %s, not a seat", ErrState, seat, kind)
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
			if _, err = Append(ctx, tx, Event{Stream: "system", Type: "actor_registered", Actor: "controller",
				Body: map[string]any{"actors": []string{seat}, "note": "a seat the platform's token table names, registered at its start"}}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'seat', $2)`,
				seat, "Seat "+seat[len("seat:"):]); err != nil {
				return err
			}
			added = true
			return nil
		})
		if err != nil {
			return done, fmt.Errorf("store: register %s: %w", seat, err)
		}
		if added {
			done = append(done, seat)
		}
	}
	return done, nil
}
