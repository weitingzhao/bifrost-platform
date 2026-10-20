package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClaimFor is ClaimNext with a lease of the test's choosing, so a test can
// start from a lease that has lapsed, or is past its grace, without waiting.
func ClaimFor(ctx context.Context, db DB, r ClaimRequest, ttl time.Duration) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: ttl, effects: dispatched})
}

// ClaimExternalFor also hands out external work, which round 2 never
// dispatches: the fixture for the takeover rules of external tasks.
func ClaimExternalFor(ctx context.Context, db DB, r ClaimRequest, ttl time.Duration) (*Claim, error) {
	return claimNext(ctx, db, r, claimOpts{ttl: ttl, effects: append([]string{"external"}, dispatched...)})
}

// ReplayRoute decides a route from the inputs a route_decided body recorded:
// the route, the exclusions and the input's fingerprint.
func ReplayRoute(inputs map[string]any) ([]string, []any, string, error) {
	in, err := replayInput(inputs)
	if err != nil {
		return nil, nil, "", err
	}
	fp, err := in.fingerprint()
	route, excluded := in.decide()
	return route, excluded, fp, err
}

// IssueExamLater is IssueExam with the clock moved on by later, so a test
// can let a paper lapse without waiting.
func IssueExamLater(ctx context.Context, db DB, req ExamRequest, set ExamSet, later time.Duration) (*ExamPaper, error) {
	return issueExam(ctx, db, req, set, later)
}

// RecordExamAnswersLater is RecordExamAnswers with the clock moved on by
// later.
func RecordExamAnswersLater(ctx context.Context, db DB, host, token string, sub ExamSubmission, set ExamSet, later time.Duration) (map[string]any, error) {
	return recordExamAnswers(ctx, db, host, token, sub, set, later)
}

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
}

// FreezeEventClock makes every event written from now on carry the time at,
// and returns the function that undoes it.
func FreezeEventClock(at time.Time) func() {
	eventClock = func() time.Time { return at }
	return func() { eventClock = time.Now }
}

// MoveTaskForTest moves a task as the kernel does: the fixture that leases a
// given task directly (LeaseAttempt) moves it to leased with it.
func MoveTaskForTest(ctx context.Context, tx pgx.Tx, actor, taskID, to string) error {
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	return moveTask(ctx, tx, actor, t, to, "test")
}

// FailWorkForTest blocks a task as failed work (failWork).
func FailWorkForTest(ctx context.Context, tx pgx.Tx, taskID string) error {
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	return failWork(ctx, tx, "controller", t, "test: checks failed with no round left")
}

// LiveForTest is a live attempt as the projection reads one: its state,
// whether its host is known, when the host last polled, and its elapsed
// seconds.
func LiveForTest(id, state string, hostKnown bool, polled *time.Time, elapsed *int64, started time.Time) *ProgressAttempt {
	a := &ProgressAttempt{AttemptID: id, HostID: "h1", State: state, hostKnown: hostKnown, hostPolled: polled, ElapsedS: elapsed, started: started}
	if !started.IsZero() {
		a.StartedAt = started.UTC().Format(time.RFC3339Nano)
	}
	return a
}

// SortLiveForTest is the projection's order of live attempts.
func SortLiveForTest(list []*ProgressAttempt) []string {
	m := map[string]*ProgressAttempt{}
	for _, a := range list {
		m[a.AttemptID] = a
	}
	var ids []string
	for _, a := range sortedLive(m) {
		ids = append(ids, a.AttemptID)
	}
	return ids
}

// LimitPendingChangesForTest lowers the most plan changes needs_you lists,
// and returns the function that restores it.
func LimitPendingChangesForTest(n int) func() {
	old := pendingChangeLimit
	pendingChangeLimit = n
	return func() { pendingChangeLimit = old }
}

// WorstForTest is the status a mission shows of its tasks' statuses.
func WorstForTest(statuses ...string) string { return worst(statuses) }

// ChangeBlocksForTest is what changes would hold up, read in a transaction
// of their own.
func ChangeBlocksForTest(ctx context.Context, db DB, changes []PlanChange) (any, error) {
	var out any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = changeBlocks(ctx, tx, changes)
		return err
	})
	return out, err
}

// RefuseMailForTest is refuseMail, the end of a message that cannot become a
// brief.
var RefuseMailForTest = refuseMail

// DecidePageForTest lowers how many waiting missions a decision pass reads
// at a time, and returns the function that restores it.
func DecidePageForTest(n int) func() {
	old := decidePageSize
	decidePageSize = n
	return func() { decidePageSize = old }
}

// ProposeUnboundedForTest proposes a mission as a platform before the
// proposal bounds did (review 320 A1): the same key and request, no bounds.
func ProposeUnboundedForTest(ctx context.Context, db DB, w Writer, key string, p MissionProposal) (map[string]any, error) {
	p, err := p.check()
	if err != nil {
		return nil, err
	}
	return idempotentAs(ctx, db, w, "missions", key, p.request(), nil, func(tx pgx.Tx) (map[string]any, error) {
		return proposeMissionTx(ctx, tx, w, p, key, nil)
	})
}

// EventTypesForTest is the closed vocabulary and the types with a body.
func EventTypesForTest() (vocabulary, written []string) {
	for t := range eventTypes {
		vocabulary = append(vocabulary, t)
	}
	for t := range bodies {
		written = append(written, t)
	}
	return vocabulary, written
}

// BlockedCardBodyForTest is the card of a task of role in M-9 blocked for
// failing round 2 of 2: its title, its block's reason, the Owner's last words
// on a card of that block and its last check's report, a review's ("" for no
// check). It recommends closing the mission with the longest why there is
// (BlockedCardWhyForTest).
func BlockedCardBodyForTest(role, title, reason, words, report string) string {
	return BlockedCardBodyInForTest("bifrost-platform", role, title, reason, words, report)
}

// BlockedCardBodyInForTest is BlockedCardBodyForTest for a task in repo.
func BlockedCardBodyInForTest(repo, role, title, reason, words, report string) string {
	var check *checkSummary
	if report != "" {
		check = &checkSummary{method: "review", verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
			excerpt: checkExcerptOf("review", []byte(report))}
	}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-9", role: role, effect: "branch"},
		title: title, repo: repo, reason: reason, words: words, round: 2, maxRounds: 2, failed: true, card: 2, check: check,
		rec: recommendation{option: recClose, rule: "R1", why: BlockedCardWhyForTest}})
}

// BlockedCardWhyForTest is the longest why a card's recommendation has.
var BlockedCardWhyForTest = strings.Repeat("w", RecommendWhyMax)

// BlockedCardBodyWithCheckForTest is the card of an implementation in M-9
// blocked for failing round 2 of 2, whose last check by method failed with
// report: its title, its block's reason and the Owner's last words.
func BlockedCardBodyWithCheckForTest(method, report, title, reason, words string) string {
	check := &checkSummary{method: method, verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
		excerpt: checkExcerptOf(method, []byte(report))}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-9", role: "implement", effect: "branch"},
		title: title, repo: "bifrost-platform", reason: reason, words: words, round: 2, maxRounds: 2, failed: true, card: 2, check: check,
		rec: recommendation{option: recClose, rule: "R1", why: BlockedCardWhyForTest}})
}

// BlockedCardBodyFullForTest is the card of an implementation in repo, M-3,
// blocked for failing round 2 of 2, whose last check by method failed with
// report: its title, its block's reason, the Owner's last words, what one
// more round adds to the budget and the recommendation's why (closing M-3).
func BlockedCardBodyFullForTest(repo, method, report, title, reason, words, budget, why string) string {
	check := &checkSummary{method: method, verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
		excerpt: checkExcerptOf(method, []byte(report))}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-3", role: "implement", effect: "branch"},
		title: title, repo: repo, reason: reason, words: words, round: 2, maxRounds: 2, failed: true, card: 1, check: check,
		budget: budget, rec: recommendation{option: recClose, rule: "R1", why: why}})
}

// BlockedCardBodyLeadForTest is BlockedCardBodyFullForTest's card whose
// last check's quote is led by lead, which is never cut.
func BlockedCardBodyLeadForTest(lead, budget, why string) string {
	check := &checkSummary{method: "deterministic", verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
		excerpt: checkExcerpt{lead: lead, text: "the output's end", fromEnd: true}}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-3", role: "implement", effect: "branch"},
		title: "t", repo: "bifrost-platform", reason: "r", round: 2, maxRounds: 2, failed: true, card: 1, check: check,
		budget: budget, rec: recommendation{option: recClose, rule: "R1", why: why}})
}

// BlockedPageForTest sets how many blocked tasks a pass reads at a time and
// returns the function that restores it.
func BlockedPageForTest(n int) func() {
	old := blockedPage.Swap(int64(n))
	return func() { blockedPage.Store(old) }
}

// ReworkSpecAsRefused is the spec refusePlan writes for the next round of
// the planning spec specID, refused for why: reworkSpec with the reason
// worded as refusePlan words it, read back.
func ReworkSpecAsRefused(ctx context.Context, db DB, specID string, round int, why string) ([]byte, error) {
	var body []byte
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		id, err := reworkSpec(ctx, tx, specID, round, oneLineRunes("plan not used: "+why, 500))
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, id).Scan(&body)
	})
	return body, err
}

// ParseAssessmentForTest is parseAssessment: a report read as an assessment
// of an item with these option keys and refs (TD-391).
func ParseAssessmentForTest(text string, options []string, refs map[string]any) (map[string]any, string) {
	return parseAssessment(text, options, refs)
}
