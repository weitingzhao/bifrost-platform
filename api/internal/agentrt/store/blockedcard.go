package store

import (
	"context"
	"time"
)

// A blocked task's decision card (design v5 §5.2 控制器「验收编排」: checks that
// fail with no round left block the task 「并提一条决策卡」; §6.2: an external
// task blocked after its attempt was lost gets one once that attempt's end is
// confirmed; T26's first run, 2026-10-19: a blocked task had no way out, so
// its mission stayed active for ever).
//
// The controller's pass gives every blocked task that needs one a card on
// the question store (AskBlocked), one open card per task at a time, and
// records it as owner_wait_opened, naming the task, the card and the
// task_state_changed that blocked it: the same block never gets a second
// open card, and a task blocked again gets a new one. The card has two
// answers: one more round (blocked -> ready; a task blocked for failing its
// last round gets one round more, round_no and max_rounds + 1) or not doing
// it (blocked -> cancelled), and a third closes its whole mission
// (blockedclose.go). DecideBlocked reads the answer and acts on it in one
// transaction with owner_wait_closed, which says how the card ended; a
// dropped task (outcome dropped) no longer holds its mission open
// (close.go). Each card recommends one answer, or says why it recommends
// none, by fixed rules (blockedrecommend.go). "None of these" with the
// Owner's words, a card that expired or was withdrawn, closes the card and
// moves nothing: the task is still blocked, and the next pass asks again
// with a new card (his words on it).
//
// One more round needs no room in the mission's budget: its attempts and
// hours cap nothing (missionbudget.go). Not doing a verification or a
// review blocks the work it checks too (with no check it can never be
// accepted), and that task then gets a card of its own.
//
// Which blocked tasks get a card:
//   - an implementation whose checks failed in its last round (accept.go) and
//     a planning task whose plan was refused in its last round
//     (planresult.go): one more round runs it again, a plan with the refusal
//     added to its spec as a rework round adds it;
//   - a task blocked at dispatch because a task it depends on was rejected or
//     cancelled (propose.go): one more round runs it without that work;
//   - a ready task blocked before for its mission's budget (missionbudget.go;
//     nothing blocks for it any more): one more round runs it again;
//   - a ready task whose preparation failed three times in a row, or that no
//     executor may take any more (withdrawn.go, preparefailed.go): one more
//     round tries it again;
//   - an external task whose attempt was lost or ended on its host
//     (attempt.go), once every such attempt has a confirmed end: an
//     attempt_ended whose host cleared the process group (group_cleared).
//     Never while an earlier attempt may still be acting: an end reported
//     with the group not cleared gets no card, and since /ended takes one
//     report per attempt, such a task waits for the Owner outside the
//     runtime (v5 §6.2 「或 Owner 手工关闭」; round 2 dispatches no external
//     work, claim.go).
//
// Not given one:
//   - a brief (brief.go): its sender has already been answered that the
//     chief of staff could not answer and may ask again, re-running a brief
//     is none of the Owner's three duties, and the question store keeps at
//     most three of the runtime's cards open at once, which a failing brief
//     must not fill ahead of a plan's or a mission's;
//   - a task of a mission that is not active or paused (a planning task
//     blocked because its mission ended, planresult.go): nothing is left to
//     decide;
//   - a check of a task that is stopped (blocked, cancelled or rejected): it
//     can never be used, and the controller's pass cancels it
//     (stoppedchecks.go).
//   - a task a stop holds: an uncleared control of kind stop on the task
//     itself or on its mission (kernel.go's held). A stop is a hold only the
//     Owner lifts, and while it holds no answer on a card can move the task
//     (the coordinator, 2026-11-06: the Owner answered the cards of stopped
//     work). Its open card is withdrawn (WithdrawStaleCards), an answer on
//     one moves nothing (moot), and once the stop is lifted a task still
//     blocked gets a new card. Only the task's own stop and its mission's: a
//     check whose subject is stopped is the bullet above's.

// The options of a blocked task's card, in the order the question store keys
// them: the first (CardApplyKey) gives it one more round, the second
// (CardRefuseKey) does not do it, the third (CardCloseKey, BlockedClose)
// closes its mission.
const (
	BlockedRetry = "Give it one more round"
	BlockedDrop  = "Do not do this task"
)

// The outcomes owner_wait_closed records. OutcomeDropped is the one that lets
// the task's mission close without it (close.go).
const (
	OutcomeRetried = "retried"
	OutcomeDropped = "dropped"
	OutcomeNone    = "none"
	OutcomeMoot    = "moot" // answered after the task moved on, its mission ended or a stop held it: nothing moves
)

// AnsweredCard is one of the runtime's cards the Owner answered that the
// runtime has not acknowledged: an unacknowledged answer counts against the
// runtime's question quota.
type AnsweredCard struct {
	ID         string
	Number     int
	TaskID     string // empty when the card names no task
	AnsweredAt time.Time
	// ByRule: a card-auto-answer rule answered it, and the rule's pass
	// finishes it (its note) and acknowledges it (AnswerByRule).
	ByRule bool
}

// TaskWait was what an answered card's task is now. No task is in work and
// the tasks' table is not read (store cut 4, 2026-11-14): no such task is
// found, so an answered card that names one is acknowledged as nobody's.
func TaskWait(ctx context.Context, db DB, task string) (state string, stopped, found bool, err error) {
	return "", false, false, nil
}
