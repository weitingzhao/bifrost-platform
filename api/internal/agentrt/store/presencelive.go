package store

import (
	"time"
)

// Historical host-presence values remain for stored-event interpretation.
// The dispatch claim loop and presence controller have been retired.

// PruneSilence preserves the retired dispatcher's prune allowance.
const PruneSilence = 2*time.Minute + 30*time.Second + 15*time.Second

// hostLiveJoins is the legacy host-contact SQL fragment.
var hostLiveJoins = `CROSS JOIN LATERAL (SELECT ` + headSQL(`'prune:host:' || h.host_id`) + ` AS head OFFSET 0) ph
	  CROSS JOIN LATERAL (SELECT h.last_poll_at AS at OFFSET 0) hc`

// nullTime is t, or SQL NULL for the zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
