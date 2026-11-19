// Package readslot bounds the reads of one store that do not take a context
// (the approval and question stores read their own files).
package readslot

import (
	"context"
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// ErrBusy: as many reads of this store as allowed are still running.
var ErrBusy = errors.New("agentrt source: earlier reads have not returned")

// Slots caps the reads of one store in flight: a read that never
// returns keeps its slot, so a stuck store holds a bounded number of
// goroutines, and further reads answer unknown at once (review 161 B1).
type Slots struct{ slots chan struct{} }

func New(n int) *Slots { return &Slots{slots: make(chan struct{}, n)} }

// Within runs a read that does not take a context (the approval and question
// stores read their files themselves) and gives up when ctx ends; the read's
// slot is freed only when the read returns.
func Within[T any](ctx context.Context, reads *Slots, read func() (T, bool, error)) (T, bool, error) {
	var zero T
	select {
	case reads.slots <- struct{}{}:
	default:
		return zero, false, ErrBusy
	}
	type result struct {
		v   T
		ok  bool
		err error
	}
	out := make(chan result, 1)
	safego.Go("agentrt-source-read", func() {
		defer func() { <-reads.slots }()
		v, ok, err := read()
		out <- result{v, ok, err}
	})
	select {
	case r := <-out:
		return r.v, r.ok, r.err
	case <-ctx.Done():
		return zero, false, ctx.Err()
	}
}
