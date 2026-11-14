package store

import (
	"time"
)

// What a blocked task's card recommends (the Owner 2026-10-20, clearing
// STG's cards: a card gave no recommendation, so nothing said which answer
// to choose). Fixed rules, no model: the first that applies gives the
// recommended answer and one line of why; with none, the card says why it
// recommends nothing. The card's body starts with that line, and the
// recommended option is the question's recommended one, the why its detail
// (the Console draws it as the main button with the why beside it).
//
//	R1 an old mission far past its time: created over a day ago and over 3×
//	   its budget's hours since: close the mission.
//	R2 blocked because the Owner dropped its only check: close the mission
//	   when nothing of it ran for a day, else do not do this task.
//	R3 only the budget stopped it, the mission is under a day old, and the
//	   task's latest check passed or it has no check in this round: one more
//	   round.
//	R4 its last round's review failed and its deterministic check passed: no
//	   recommendation, the Owner judges.
//	R5 its last round's deterministic check failed: do not do this task.
//	R6 every executor on its route was withdrawn after failing it in a row
//	   (withdrawn.go, TD-395): do not do this task. A failure every executor
//	   repeats is the task's or its setup's, and one more round only repeats
//	   it until that is fixed; on STG, where a rule answers the card with
//	   what it recommends, one more round would offer it to each executor
//	   again, block it again and ask again, spending the mission's budget
//	   round after round.
//
// R1 comes first so a mission left behind (STG's walks of 10-15 to 10-19) is
// closed whole whatever blocked its task. A recommendation is only shown:
// nothing is done on it when nobody answers.

// The rules' bounds.
const (
	recOldMission   = 24 * time.Hour // R1, R3: a mission older than this is old
	recOverHours    = 3              // R1: hours since it started, as a multiple of its budget's
	recQuiet        = 24 * time.Hour // R2: nothing of the mission ran for this long
	RecommendWhyMax = 160            // the most bytes of a recommendation's why
)
