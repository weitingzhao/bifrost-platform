package store

import ()

// A ready task no executor may take any more (TD-395; PROD M-22, 2026-10-20:
// codex, cursor and claude each failed its only task three times in a row,
// TD-381 withdrew all three, and the task sat at ready with no card, its
// mission open, nobody told).
//
// The controller's pass blocks a ready task of an active mission once every
// candidate on its route that may still take it has been withdrawn by the
// backoff (retry.go), naming each and the attempts it failed; the task then
// gets the blocked card (blockedcard.go), which recommends not doing it
// (blockedrecommend.go R6). "Give it one more round" offers it once more to
// each withdrawn executor. The pass reads the tasks' state, not a change of
// it, so a task left this way before the pass existed is blocked by its
// first run.
//
// Not blocked here: a gate task (gates.go ends one that cannot run itself),
// a task with no candidate left that was never withdrawn
// (an empty route, candidates retired or excluded: no answer on a card makes
// one), a brief (its sender is answered by brief.go, and briefs get no
// card), a task or mission a stop or pause holds, and work of a mission
// started in a standing envelope whose rule is not in force.

// withdrawnBlockMark starts the reason of a task blocked because every
// executor on its route was withdrawn (a card's recommendation tells such a
// block by it).
const withdrawnBlockMark = "no executor on its route may take it any more: "
