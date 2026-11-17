package store

import (
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// Executor pins (W-31 round 4, core blocks C-0 and C-1; bifrost-trade-infra
// agent-config/work/multi-agent/DESIGN-coordinator-landing-v3-2026-10-29.md
// §2.1–§2.3, §7.1). A task pinned to the external host is leased only there
// (claim.go reads no pinned task for an ordinary host; the database holds
// both ways, 0010). Who may set a pin is decided here, in the write's
// transaction, after the writer is checked (ensureWriter): only a proposal
// over the tasks route, by a seat writing with its own token, never relayed,
// when a signed standing envelope in force for the mission's tenant has an
// external_hosts entry for the pin, of an implementation with a branch
// effect, no subject, in a repository and role the entry lists. The entry's
// proposer is read and kept but no longer has to be the writer (cleanup wave
// 1, 2026-11-11). The X-Bifrost-Session header the proposal came with is
// recorded as via_session and grants nothing (§2.3).

// ExecutorPins are the pins there are (0010 task_executor_pin_check).
var ExecutorPins = []string{"external:coordinator"}

// ErrPinNotPermitted: a pinned task the writer may not propose; nothing of
// the batch was written. It is an ErrInvalid.
var ErrPinNotPermitted = fmt.Errorf("%w: pin_not_permitted", ErrInvalid)

// ErrMixedSource: a pinned task declares no provider, or one besides its
// entry's (§2.8, F4 A: the first version takes only the coordinator's own
// work); nothing of the batch was written. It is an ErrInvalid.
var ErrMixedSource = fmt.Errorf("%w: mixed_source_not_supported", ErrInvalid)

// badViaSession says why a via_session is refused, "" when it is a
// session id or empty.
func badViaSession(v any) string {
	s, ok := v.(string)
	switch {
	case !ok:
		return "is not a string"
	case s != "" && !actor.IsSession(s):
		return fmt.Sprintf("is %q, not a session id", s)
	}
	return ""
}
