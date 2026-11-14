package store_test

import (
	"time"
)

// Regression tests for review 188.

// within reports what ch gives within d, or false.
func within[T any](ch <-chan T, d time.Duration) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		var zero T
		return zero, false
	}
}
