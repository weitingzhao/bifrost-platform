package store

import (
	"errors"
)

// Accepting a salvaged result (design v5 §5.2 /salvage, T34 case 8; review
// 152 A3; the Owner chose option A on 2026-10-14). An attempt that lost its
// lease stays lost; once the grace has passed its host may salvage its
// result, which persists it (Salvage). The controller checks that result as
// it checks a submitted one: a deterministic verification pinned to the lost
// attempt, then, with precheck verify, a review, their specs marked salvage.
// The pass that accepts (the review's, else the deterministic check's)
// accepts the lost attempt, and the same transaction cancels the task's live
// new attempt, if there is one (attempt_cancelled), and accepts the task:
// one accepted attempt, nothing left running for it. The task does not move
// to verifying meanwhile: its new attempt keeps running, and a salvaged
// result that fails its check moves nothing.
//
// A salvaged result is checked only while it can still be the task's result:
// the task is ready, leased or running (not paused or blocked, and not past a
// submit), no later attempt was submitted, the attempt has no verification
// yet, and it is checked in the round it was pinned in. Whichever comes first
// wins: a new attempt that submits before the acceptance closes the salvage
// check; an acceptance that comes first fences the new attempt, whose next
// call is refused, and its host ends it.

// errLiveMoved: the subject's live attempt is not the one locked before the
// subject was; the next pass looks again, with the new one locked in order.
var errLiveMoved = errors.New("store: the subject's live attempt changed")
