package store

import ()

// Closing the pending checks of a stopped task (11-06: two of M-308's three
// budget cards were for checks of tasks already stopped). A check is a
// verification or a review; ensureCheck and stillChecked (accept.go) use only
// the checks of a subject that is verifying, so one whose subject is blocked,
// cancelled or rejected can never decide anything: a subject given one more
// round runs again, and its new attempt gets checks of its own. The
// controller's pass cancels such a check while it is pending (proposed,
// awaiting_owner, ready, paused or blocked) before it can be blocked for
// budget or carded, and closes its open card as moot (the next
// WithdrawStaleCards withdraws it from the question store). A leased or
// running check is left to end on its own, and a gate task to its batch. A
// check cancelled this way does not hold its mission open (close.go).
//
// Locks, in the kernel's order (a subject before its check, as Conclude and
// DecideBlocked take them): the subject shared, so it cannot move to ready
// between the read and the cancel, then the check; both read again under
// them. The subject's own moves never cancel its checks: the cancel is this
// pass's alone.

// The states of a stopped subject and of a pending check, as SQL lists.
const (
	stoppedStates = `('blocked','cancelled','rejected')`
	pendingStates = `('proposed','awaiting_owner','ready','paused','blocked')`
)
