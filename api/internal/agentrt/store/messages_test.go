package store_test

import (
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Two batches of one inbox acknowledged at once, overlapping, never deadlock
// (review 264 B3): the receiver's lock comes first.
func TestAcknowledgingTheSameInboxAtOnce(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime) // one connection each: a pgx.Conn is not for two goroutines
	seat := store.Writer{Actor: "seat:chief_of_staff", Token: "fixture"}
	for round := 0; round < 30; round++ {
		var m1, m2 string
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			if m1, err = store.PostMessage(ctx, tx, "owner", seat.Actor, "ask", []byte("1")); err != nil {
				return err
			}
			m2, err = store.PostMessage(ctx, tx, "owner", seat.Actor, "ask", []byte("2"))
			return err
		}))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, batch := range [][]string{{m1, m2}, {m2, m1}} {
			wg.Add(1)
			go func(i int, batch []string) {
				defer wg.Done()
				_, errs[i] = store.AckMessages(ctx, pool, seat, batch)
			}(i, batch)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d, batch %d: %v", round, i, err)
			}
		}
	}
}
