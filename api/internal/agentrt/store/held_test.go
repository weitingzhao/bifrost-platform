package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Helpers for tests that hold a row lock against the call they test.

// whileHeld runs a call the test holds a lock for and returns its error: a
// call that waited for the lock instead of giving up would never return while
// it is held, so after 30 seconds the test fails rather than the package
// timing out (review 322 A2).
func whileHeld(t *testing.T, what string, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatalf("%s waited for a lock the test holds", what)
		return nil
	}
}

// releaseLater rolls a holder back after d, on its own goroutine, and gives
// the wait for that: the test and its cleanup wait for the rollback before
// anything else touches the transaction, so the two never run at once
// (review 323 B).
func releaseLater(t *testing.T, tx pgx.Tx, d time.Duration) func() error {
	t.Helper()
	finished := make(chan struct{})
	var err error
	go func() {
		defer close(finished)
		time.Sleep(d)
		err = tx.Rollback(ctx)
	}()
	t.Cleanup(func() { <-finished })
	return func() error {
		<-finished
		return err
	}
}
