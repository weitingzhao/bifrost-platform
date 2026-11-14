package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// RuleChoiceForTest is ruleChoice for a card of kind that recommends rec,
// whose AI assessment came recommending assessed ("": none asked), under a
// rule with or without assessment: veto, on a card with options.
func RuleChoiceForTest(kind, rec, assessed string, veto bool, options []string) (option, why string) {
	st := assessState{asked: assessed != "", done: assessed != "", recommended: assessed}
	option, why, _ = ruleChoice(kind, rec, st, time.Now(), veto, options)
	return option, why
}

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

// BlockSpentForTest blocks, as the budget pass did before 2026-11-06, every
// ready task (not a gate's) of an active mission that has used its
// budget's attempts or is past its hours, with the reason that pass wrote:
// such blocks are still in the store, and their cards are still answered.
// It returns how many it blocked.
func BlockSpentForTest(ctx context.Context, db DB) (int, error) {
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
		  WHERE t.state = 'ready' AND m.state = 'active' ORDER BY t.task_id`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if member, err := isGateMember(ctx, tx, id); err != nil || member {
				if err != nil {
					return err
				}
				continue
			}
			t, err := readTask(ctx, tx, id, true)
			if err != nil {
				return err
			}
			s, ok, err := spendOf(ctx, tx, t)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			var used int
			var hours float64
			if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id WHERE t.mission_id = $1),
			    EXTRACT(EPOCH FROM clock_timestamp() - created_at)::float8 / 3600 FROM agentrt.mission WHERE mission_id = $1`, t.mission).Scan(&used, &hours); err != nil {
				return err
			}
			why := ""
			switch {
			case used >= s.budget.MaxAttempts:
				why = fmt.Sprintf("mission %s has used %d of its %d attempts", t.mission, used, s.budget.MaxAttempts)
			case hours >= float64(s.budget.MaxWallHours):
				why = fmt.Sprintf("mission %s is past its %d hours (it started %.1f hours ago)", t.mission, s.budget.MaxWallHours, hours)
			default:
				continue
			}
			if err = moveTask(ctx, tx, "controller", t, "blocked", why+budgetBlockSuffix); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
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
// report: its title, its block's reason, the Owner's last words and the
// recommendation's why (closing M-3).
func BlockedCardBodyFullForTest(repo, method, report, title, reason, words, why string) string {
	check := &checkSummary{method: method, verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
		excerpt: checkExcerptOf(method, []byte(report))}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-3", role: "implement", effect: "branch"},
		title: title, repo: repo, reason: reason, words: words, round: 2, maxRounds: 2, failed: true, card: 1, check: check,
		rec: recommendation{option: recClose, rule: "R1", why: why}})
}

// BlockedCardBodyLeadForTest is BlockedCardBodyFullForTest's card whose
// last check's quote is led by lead, which is never cut.
func BlockedCardBodyLeadForTest(lead, why string) string {
	check := &checkSummary{method: "deterministic", verdict: "fail", attempt: "a_01M5B8BF2MZ2577S8MWMF0EJVQ", round: 2,
		excerpt: checkExcerpt{lead: lead, text: "the output's end", fromEnd: true}}
	return blockedCardBody(blockedCard{task: taskRow{id: "t_01M5B8BF2MZ2577S8MWMF0EJVQ", mission: "M-3", role: "implement", effect: "branch"},
		title: "t", repo: "bifrost-platform", reason: "r", round: 2, maxRounds: 2, failed: true, card: 1, check: check,
		rec: recommendation{option: recClose, rule: "R1", why: why}})
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

// ProposeIssueMissionForTest proposes a mission as take_issue would (its
// work id an issue's, outside the dev envelope) but passing ask, which
// take_issue never does: askMission refuses a BI- mission's card (review
// 522 A1).
func ProposeIssueMissionForTest(ctx context.Context, tx pgx.Tx, w Writer, workID string, ask Ask) (map[string]any, error) {
	p, err := MissionProposal{WorkID: workID, Title: "t", Goal: "g", Success: []string{"s"}, MaxAttempts: 100, MaxWallHours: 48,
		issueTake: true}.check()
	if err != nil {
		return nil, err
	}
	return proposeMissionTx(ctx, tx, w, p, "k-ask", ask)
}

// The presence reads' SQL, for EXPLAIN (review 526 B2).
var (
	OpenAlertsSQL  = openAlertsSQL
	LatestClaimSQL = latestClaimSQL
)

// ProgressAtForTest is the projection at at, a host's silence counted from no
// earlier than since.
func ProgressAtForTest(ctx context.Context, db DB, at, since time.Time) (map[string]any, error) {
	return progressAt(ctx, db, at, since)
}

// BadLandingGate and CheckLandingGateAppend check landing_gate records (tests).
var BadLandingGate = badLandingGate
var CheckLandingGateAppend = checkLandingGateAppend

// ClaimPinnedFor is ClaimPinned with a lease of the test's choosing (a lease
// already past its grace, for salvage).
func ClaimPinnedFor(ctx context.Context, db DB, r PinnedClaimRequest, ttl time.Duration) (map[string]any, error) {
	return claimPinned(ctx, db, r, ttl)
}

// ReadBackClaimPinned is the read-back an exact claim whose commit failed
// makes: the answer its key stored, if any.
func ReadBackClaimPinned(ctx context.Context, db DB, host, key, task string) (map[string]any, bool) {
	return readBackKey(ctx, db, hostActor(host), "claim_pinned", key, map[string]any{"task_id": task})
}

// PinnedReviewForTest fits a review's spec of subject's attempt as the
// controller does when it proposes it: the time limit.
func PinnedReviewForTest(ctx context.Context, tx pgx.Tx, subject, attempt string) (time.Duration, error) {
	spec := checkSpec{check: "review", subject: subject, attempt: attempt}
	// The review is of the attempt's pushed commit (review 691: the facts are
	// read for that commit only).
	if err := tx.QueryRow(ctx, `SELECT COALESCE(pushed_commit, '') FROM agentrt.attempt WHERE attempt_id = $1`, attempt).Scan(&spec.commit); err != nil {
		return 0, err
	}
	return pinnedReview(ctx, tx, subject, &spec)
}

// ViewExecutionProgressForTest is the execution_progress viewExecutionSummary
// writes on nodes, given the plan view's tasks, keyed by plan_node_id.
func ViewExecutionProgressForTest(nodes, tasks []map[string]any) map[string]map[string]any {
	viewExecutionSummary(nodes, tasks, planview.Graph{}, viewAdoption{})
	out := map[string]map[string]any{}
	for _, n := range nodes {
		out[n["plan_node_id"].(string)] = n["execution_progress"].(map[string]any)
	}
	return out
}

// QuestionSeenBodyForTest is the question_seen body of q as read, checked
// against the event's vocabulary.
func QuestionSeenBodyForTest(id string, q Question) (map[string]any, error) {
	body, _, err := questionSeenBody(id, q)
	if err != nil {
		return nil, err
	}
	return body, checkBody("question_seen", body)
}

// SelfCheckForTest says whether c is a self-check an ending keeps, and the
// attempt_computed body it makes, checked against the event's vocabulary.
func SelfCheckForTest(c SelfCheck) error {
	if err := c.check(); err != nil {
		return err
	}
	return checkBody("attempt_computed", map[string]any{"end_how": "exit", "conclusion": "done", "self_check": c.body()})
}

// ParseLandingGrantForTest is parseLandingGrant: a landing_grant body's
// closed-schema and §5.5/§12 check, without a database.
func ParseLandingGrantForTest(v any) (LandingGrant, error) { return parseLandingGrant(v) }

// FloorTreeStaleForTest is floorTreeStale: why M0..R is not what a floor
// grant of repo pinned, "" when it is.
func FloorTreeStaleForTest(repo string, pinned []GrantFloorPath, tree landing.FloorTree) string {
	return floorTreeStale(repo, pinned, tree)
}

// BadLandingRefusalForTest is badLandingRefusal, and RefusalAppendForTest
// checkLandingRefusalAppend: a landing_refusal body and its event.
func BadLandingRefusalForTest(v any) string { return badLandingRefusal(v) }
func RefusalAppendForTest(ev Event) error   { return checkLandingRefusalAppend(ev) }

// SeenCopiesForTest reads the same copies the step timeline uses.
func SeenCopiesForTest(ctx context.Context, tx pgx.Tx, kind, key string, refs []string) ([]SeenCopy, error) {
	return seenCopies(ctx, tx, kind, key, refs, nil)
}

// StgGateAuthorityForTest is the STG gate's landing authority for op as it
// reads the landing stream, each of roots bound to bifrost-platform in
// tenant dev: whether op used a grant itself, the batch grant it rests on,
// the grant attached and whether it is, and the batch that grant names.
func StgGateAuthorityForTest(ctx context.Context, db DB, op string, roots []string, at time.Time) (used bool, batchGrant, grantID string, has bool, members []string, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		bindings := map[string]stgBinding{}
		for _, r := range roots {
			bindings[r] = stgBinding{Root: r, Tenant: "dev", Repo: "bifrost-platform"}
		}
		f, err := stgReadGateFacts(ctx, tx, s, bindings, at)
		if err != nil {
			return err
		}
		a := f.Authority[op]
		used, batchGrant, grantID, has, members = a.UsedGrant, a.BatchGrant, a.Grant.GrantID, a.HasGrant, a.BatchMembers
		return nil
	})
	return
}
