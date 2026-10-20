package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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
//
// R1 comes first so a mission left behind (STG's walks of 10-15 to 10-19) is
// closed whole whatever blocked its task. A recommendation is only shown:
// nothing is done on it when nobody answers.

// The options a recommendation names, as the card lists them (1-based, the
// question store's recommended index).
const (
	recNone  = 0
	recRetry = 1
	recDrop  = 2
	recClose = 3
)

// The rules' bounds.
const (
	recOldMission   = 24 * time.Hour // R1, R3: a mission older than this is old
	recOverHours    = 3              // R1: hours since it started, as a multiple of its budget's
	recQuiet        = 24 * time.Hour // R2: nothing of the mission ran for this long
	RecommendWhyMax = 160            // the most bytes of a recommendation's why
)

// recommendation is what a card recommends: an option (recNone for none),
// the rule that chose it ("" for none) and why, one line.
type recommendation struct {
	option int
	rule   string
	why    string
}

// The kinds of block a recommendation tells apart, by how the task was
// blocked (blockedcard.go lists who blocks).
const (
	blockFailed     = "failed"     // its checks failed in its last round, or it handed in nothing usable (failWork)
	blockBudget     = "budget"     // its mission's budget lets it run no longer (BlockOverBudget)
	blockChain      = "chain"      // the Owner dropped its only check (DecideBlocked)
	blockDependency = "dependency" // a task it depends on was rejected or cancelled (Dispatch)
	blockLost       = "lost"       // an external task whose attempt ended on its host
	blockOther      = "other"
)

// chainBlockPrefix starts the reason of a task blocked because the Owner
// dropped its only check (decideBlocked).
const chainBlockPrefix = "the Owner chose not to do its "

// blockKind is the kind of a block, from its reason and whether it was failed
// work.
func blockKind(reason string, failed bool, effect string) string {
	switch {
	case failed:
		return blockFailed
	case strings.HasPrefix(reason, chainBlockPrefix):
		return blockChain
	case strings.HasSuffix(reason, budgetBlockSuffix):
		return blockBudget
	case strings.HasPrefix(reason, "dependency "):
		return blockDependency
	case effect == "external":
		return blockLost
	}
	return blockOther
}

// recFacts are what the rules read.
type recFacts struct {
	mission, role    string
	kind             string
	created, now     time.Time
	budgetHours      int       // the mission's budget in hours, 0 when it has none to read
	lastWork         time.Time // an attempt of the mission's last heartbeat (its lease, then each beat while it runs); zero when none ever ran
	round, maxRounds int
	// The task's latest check: its method and verdict, the verdict of the
	// deterministic check on the same attempt ("" for none), and whether it
	// is of this round (no move to ready after it).
	check, verdict, determined string
	checkedThisRound           bool
}

// recommend applies the rules in order.
func recommend(f recFacts) recommendation {
	age := f.now.Sub(f.created)
	quiet := f.lastWork.IsZero() || f.now.Sub(f.lastWork) >= recQuiet
	switch {
	case age > recOldMission && f.budgetHours > 0 && age.Hours() >= float64(recOverHours*f.budgetHours):
		return rec(recClose, "R1", fmt.Sprintf("started %s, used %d× its %d hours, %s", ago(age), int(age.Hours())/f.budgetHours,
			f.budgetHours, lastWorkText(f)))
	case f.kind == blockChain && quiet:
		return rec(recClose, "R2", "the Owner dropped its only check, and "+quietText(f))
	case f.kind == blockChain:
		return rec(recDrop, "R2", fmt.Sprintf("the Owner dropped its only check; other work in %s %s, so drop only this task", f.mission, lastWorkText(f)))
	case f.kind == blockBudget && age < recOldMission && (!f.checkedThisRound || f.verdict == "pass"):
		return rec(recRetry, "R3", "only the budget stopped it")
	case f.kind == blockBudget && age >= recOldMission:
		return rec(recNone, "", fmt.Sprintf("only the budget stopped it, but %s started %s and is under %d× its hours", f.mission, ago(age), recOverHours))
	case f.kind == blockBudget:
		return rec(recNone, "", "the budget stopped it after a check of this round that did not pass")
	case f.kind == blockFailed && f.check == "review" && f.verdict == "fail" && f.determined == "pass":
		return rec(recNone, "R4", "needs your judgment: the checks pass, the reviewer failed it")
	case f.kind == blockFailed && f.check == "deterministic" && f.verdict == "fail":
		return rec(recDrop, "R5", fmt.Sprintf("its checks failed in its last round (%d of %d)", f.round, f.maxRounds))
	case f.kind == blockFailed && f.role == "plan":
		return rec(recNone, "", "its plan was not used in its last round")
	case f.kind == blockFailed && f.check == "":
		return rec(recNone, "", "its last round handed in nothing the checks could use")
	case f.kind == blockFailed:
		return rec(recNone, "", fmt.Sprintf("no rule covers its last check (%s, %s)", f.check, f.verdict))
	case f.kind == blockDependency:
		return rec(recNone, "", "a task it depends on was rejected or cancelled")
	case f.kind == blockLost:
		return rec(recNone, "", "its attempt ended on its host without a result")
	}
	return rec(recNone, "", "no rule covers why it is blocked")
}

func rec(option int, rule, why string) recommendation {
	return recommendation{option: option, rule: rule, why: clipBytes(why, RecommendWhyMax)}
}

// ago is how long ago something was, in whole hours under two days, else in
// whole days.
func ago(d time.Duration) string {
	switch h := int(d.Hours()); {
	case h < 1:
		return "under an hour ago"
	case h == 1:
		return "1 hour ago"
	case h < 48:
		return fmt.Sprintf("%d hours ago", h)
	default:
		return fmt.Sprintf("%d days ago", h/24)
	}
}

// lastWorkText says when an attempt of the mission last ran: nothing yet,
// nothing for a day or more (since its date, UTC), or how long ago.
func lastWorkText(f recFacts) string {
	switch {
	case f.lastWork.IsZero():
		return "nothing has run"
	case f.now.Sub(f.lastWork) >= recQuiet:
		return "nothing ran since " + f.lastWork.UTC().Format("01-02")
	}
	return "ran " + ago(f.now.Sub(f.lastWork))
}

// quietText says that nothing of the mission ran for a day or more.
func quietText(f recFacts) string {
	if f.lastWork.IsZero() {
		return "nothing in " + f.mission + " has run"
	}
	return "nothing in " + f.mission + " ran since " + f.lastWork.UTC().Format("01-02")
}

// clipBytes is s cut to at most n bytes, at a rune's start.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// line is the card's first line.
func (r recommendation) line(labels []string) string {
	if r.option == recNone {
		return "No recommendation: " + r.why
	}
	return "Recommended: " + labels[r.option-1] + " — " + r.why
}

// recommendBlocked reads what the rules need of blocked task t, in the
// caller's transaction (which holds t and its mission): its block's reason
// and whether it was failed work, its detail, and its mission's budget (ok
// false for none).
func recommendBlocked(ctx context.Context, tx pgx.Tx, t taskRow, d taskDetail, reason string, failed bool, s spend, ok bool) (recommendation, error) {
	f := recFacts{mission: t.mission, role: t.role, kind: blockKind(reason, failed, t.effect), round: d.round, maxRounds: d.maxRounds}
	if ok {
		f.budgetHours = s.budget.MaxWallHours
	}
	var last *time.Time
	if err := tx.QueryRow(ctx, `SELECT m.created_at, clock_timestamp(),
	    (SELECT max(a.heartbeat_at) FROM agentrt.attempt a JOIN agentrt.task u ON u.task_id = a.task_id WHERE u.mission_id = m.mission_id)
	  FROM agentrt.mission m WHERE m.mission_id = $1`, t.mission).Scan(&f.created, &f.now, &last); err != nil {
		return recommendation{}, err
	}
	if last != nil {
		f.lastWork = *last
	}
	// The task's latest check, as recorded (a verification's event names the
	// work it checks and that work's attempt), the other checks of that
	// attempt, and whether the task went back to ready after it.
	var event int64
	var attempt string
	err := tx.QueryRow(ctx, `SELECT e.event_id, COALESCE(e.attempt_id, ''), e.body_json->>'method', e.body_json->>'verdict'
	  FROM agentrt.event e WHERE e.task_id = $1 AND e.event_type = 'verification_recorded' ORDER BY e.event_id DESC LIMIT 1`, t.id).
		Scan(&event, &attempt, &f.check, &f.verdict)
	if errors.Is(err, pgx.ErrNoRows) {
		return recommend(f), nil
	}
	if err != nil {
		return recommendation{}, err
	}
	var readied bool
	if err = tx.QueryRow(ctx, `SELECT
	    COALESCE((SELECT body_json->>'verdict' FROM agentrt.event WHERE task_id = $1 AND attempt_id = $2 AND event_type = 'verification_recorded'
	      AND body_json->>'method' = 'deterministic' ORDER BY event_id DESC LIMIT 1), ''),
	    EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed' AND body_json->>'to' = 'ready' AND event_id > $3)`,
		t.id, attempt, event).Scan(&f.determined, &readied); err != nil {
		return recommendation{}, err
	}
	f.checkedThisRound = !readied
	return recommend(f), nil
}
