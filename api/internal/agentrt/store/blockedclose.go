package store

import ()

// Closing a whole mission on a blocked task's card (the Owner 2026-10-20:
// cancelling a check blocked the work it checked and made another card, so
// clearing STG's 16 missions left over from its walks took some forty
// answers). Every blocked card has a third option, "Close mission M-n";
// choosing it, in one transaction:
//   - every task of the mission that has not ended (accepted, rejected or
//     cancelled) is cancelled, whatever its state, and a live attempt of one
//     is cancelled first (attempt_cancelled), as accepting a salvaged result
//     cancels the live attempt it replaces (adopt.go): its host's next call
//     is refused and agentd ends it. A cancelled task gets no card, so a
//     check cancelled here blocks nothing;
//   - the card closes as owner_wait_closed, outcome closed, with the answer
//     kept (question_seen);
//   - the mission ends as a goal's mission whose tasks have all ended does
//     (close.go): done when an implementation (or a brief) of it was
//     accepted, else stopped; a paused mission, which may only stop, is
//     stopped. The move names the card and the answer, the Owner's decision.
//
// Its other open cards are then withdrawn by the next WithdrawStaleCards,
// which withdraws every open blocked card of a mission that has ended,
// however it ended (review 420 C1: such a card held a place in the question
// store's three until it expired). The pass that reads them closed records
// how (withdrawn), or, if the Owner answered one first, that it moved
// nothing (moot): an answer on a mission that has ended moves nothing, so a
// second "close" is a no-op. Plan-change and mission cards are not blocked
// cards and are left as they are.
//
// WithdrawStaleCards also withdraws a blocked card asked before cards had
// the third option (two options, no recommendation; STG and PROD held some
// when this came): the pass after asks its block again with a card of its
// own kind, so every open blocked card can close its mission.

// The third option of a blocked card, and its key in the question store.
const (
	CardCloseKey  = "c"
	OutcomeClosed = "closed" // the Owner closed the task's mission on its card
)

// BlockedClose is the third option of a card of a task of mission.
func BlockedClose(mission string) string { return "Close mission " + mission }

// blockedOptions are a card's options, in the question store's order.
func blockedOptions(mission string) []string {
	return []string{BlockedRetry, BlockedDrop, BlockedClose(mission)}
}
