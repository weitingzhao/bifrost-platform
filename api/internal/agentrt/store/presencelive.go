package store

import (
	"time"
)

// Historical host-presence values remain for stored-event interpretation.
// The dispatch claim loop and presence controller have been retired.

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
