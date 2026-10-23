package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Business issues, slice 1 (DESIGN-crew-intake-2026-10-22.md §10 row 1):
// the store and the database together. Every text here is synthetic; no
// person's real words go into a fixture (§3.1).

const intakeGatesText = `rule_id: intake-gates
rule_version: 1
direction: initial
effective_at: 2026-01-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
classes:
  small: [brief_confirmed, classified, owner_priority, quota, rule_in_force]
  medium: [brief_confirmed, classified, owner_priority, design, review, quota, rule_in_force]
  large: [brief_confirmed, classified, owner_priority, design, review, owner_decisions, quota, rule_in_force]
max_live_per_product: 1
exceptions: [answer_only, fault_small]
`

const devEnvelopeText = `rule_id: standing-envelope:dev
rule_version: 1
direction: initial
effective_at: 2026-01-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:codex]
hosts: [h1]
`

// issueWorld is a world with the intake rule and the dev standing envelope
// in force, two people (the Owner and p2), an intake client for each, a
// crew thread and the Owner as writers, and a seat's report to cite.
type issueWorld struct {
	*world
	crew, owner, dots, dotsOwner store.Writer
	seat                         string
	keys                         int
	mu                           sync.Mutex
}

func newIssueWorld(t *testing.T) *issueWorld {
	t.Helper()
	return issueWorldOn(t, newWorld(t))
}

// issueWorldOn sets the issue fixture up on a world.
func issueWorldOn(t *testing.T, w *world) *issueWorld {
	t.Helper()
	return issueWorldOf(t, w, true)
}

// issueWorldOf is issueWorldOn, the intake-gates rule stored or not.
func issueWorldOf(t *testing.T, w *world, gates bool) *issueWorld {
	t.Helper()
	iw := &issueWorld{world: w,
		crew:      store.Writer{Actor: "thread:crew-1", Token: "operator"},
		owner:     store.Writer{Actor: "owner", Token: "admin"},
		dots:      store.Writer{Actor: "agent:dots-p2", Token: "intake:dots-p2"},
		dotsOwner: store.Writer{Actor: "agent:dots-owner", Token: "intake:dots-owner"},
	}
	if gates {
		iw.putRule(t, intakeGatesText)
	}
	iw.putRule(t, devEnvelopeText)
	for id, name := range map[string]string{store.OwnerPerson: "Owner", "person:p2": "Person two"} {
		_, err := store.PutPerson(ctx, iw.rt, iw.owner, id, name)
		ok(t, err)
	}
	for _, c := range []store.IntakeClientSetup{
		{ClientID: iw.dots.Actor, OnBehalfOf: "person:p2", Products: []string{"screening"}},
		{ClientID: iw.dotsOwner.Actor, OnBehalfOf: store.OwnerPerson, Products: []string{"screening", "finance"}},
	} {
		_, err := store.SetIntakeClient(ctx, iw.rt, iw.owner, c)
		ok(t, err)
	}
	iw.seat = iw.report(t, "brief")
	return iw
}

func (iw *issueWorld) key() string {
	iw.mu.Lock()
	defer iw.mu.Unlock()
	iw.keys++
	return fmt.Sprintf("k-%d", iw.keys)
}

// putRule stores a rule row as the Owner signed it (the test's signature).
func (iw *issueWorld) putRule(t *testing.T, text string) {
	t.Helper()
	r, err := rules.Parse([]byte(text))
	ok(t, err)
	body, err := r.BodyJSON()
	ok(t, err)
	sig := any("test")
	if r.Direction == rules.Tighten {
		sig = nil
	}
	_, err = iw.rt.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $6, $7, $8)`, r.ID, r.Version, string(body), text, r.Direction, sig,
		r.Effective, r.Expires)
	ok(t, err)
}

// report is the report of a finished task of role (a seat's answer when the
// role is brief), written as the import writes old rows.
func (iw *issueWorld) report(t *testing.T, role string) string {
	t.Helper()
	var id string
	ok(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PutArtifact(ctx, tx, "controller", "report", "text/markdown", []byte("seat answer "+store.NewTaskID()))
		return err
	}))
	task, att := store.NewTaskID(), store.NewAttemptID()
	iw.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.task (task_id, mission_id, off_plan, off_plan_reason, role, task_class, title,
	  spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id)
	  VALUES ('%s', 'M-1', true, 'a seat brief', '%s', 'brief', 'seat brief', '%s', 'none', 'accepted', 60, 'controller')`, task, role, iw.spec),
		fmt.Sprintf(`INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch, lease_expires_at,
	  heartbeat_at, state, report_artifact_id) VALUES ('%s', '%s', 1, 'exec:claude', 1, now(), now(), 'finished', '%s')`, att, task, id))
	return id
}

func inum(t *testing.T, m map[string]any, k string) int64 {
	t.Helper()
	switch v := m[k].(type) {
	case json.Number:
		n, err := v.Int64()
		ok(t, err)
		return n
	case int:
		return int64(v)
	case int64:
		return v
	}
	t.Fatalf("%s is %T %v in %v", k, m[k], m[k], m)
	return 0
}

func (iw *issueWorld) state(t *testing.T, id string) store.IssueView {
	t.Helper()
	v, err := store.IssueState(ctx, iw.rt, id)
	ok(t, err)
	return v
}

// submit brings an issue for p2 by her agent.
func (iw *issueWorld) submit(t *testing.T, title string) string {
	t.Helper()
	out, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "complaint",
		Title: title, OnBehalfOf: "person:p2", Words: []store.IssueWords{{Text: "words of " + title, SaidAt: time.Now(), Where: "the screen page"}},
		Reading: "the agent reads " + title})
	ok(t, err)
	return out["issue_id"].(string)
}

// brief drafts a brief and returns its confirmation's ask.
func (iw *issueWorld) brief(t *testing.T, id, text string) int64 {
	t.Helper()
	out, err := store.DraftBrief(ctx, iw.rt, iw.crew, iw.key(), store.BriefDraft{IssueID: id, ExpectedVersion: iw.state(t, id).Version, Text: text})
	ok(t, err)
	return inum(t, out, "ask_id")
}

func (iw *issueWorld) answer(t *testing.T, w store.Writer, id string, ask int64, verdict string) (map[string]any, error) {
	t.Helper()
	return store.AnswerAsk(ctx, iw.rt, w, iw.key(), store.AskAnswer{IssueID: id, ExpectedVersion: iw.state(t, id).Version, AskID: ask,
		Words: "yes, that is it (" + verdict + ")", SaidAt: time.Now(), Verdict: verdict})
}

func (iw *issueWorld) classify(t *testing.T, id, class, category string) error {
	t.Helper()
	_, err := store.ClassifyIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueClassification{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		RiskClass: class, Category: category, SeatReport: iw.seat, Reason: "the seat judged it " + class})
	return err
}

func (iw *issueWorld) prioritize(t *testing.T, id, p string) error {
	t.Helper()
	_, err := store.RecordOwnerEvidence(ctx, iw.rt, iw.owner, iw.key(), store.OwnerEvidence{IssueID: id, Priority: p,
		BriefSHA256: iw.state(t, id).BriefSHA256})
	return err
}

func (iw *issueWorld) decide(t *testing.T, id, decision, option string) error {
	t.Helper()
	v := iw.state(t, id)
	_, err := store.RecordOwnerEvidence(ctx, iw.rt, iw.owner, iw.key(), store.OwnerEvidence{IssueID: id, Decision: decision, Option: option,
		BriefSHA256: v.BriefSHA256, DesignSHA256: v.DesignSHA256})
	return err
}

func (iw *issueWorld) design(t *testing.T, id, text string, decisions ...store.DesignDecision) error {
	t.Helper()
	_, err := store.PutDesign(ctx, iw.rt, iw.crew, iw.key(), store.IssueDesign{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Text: text, Decisions: decisions})
	return err
}

func (iw *issueWorld) review(t *testing.T, id, last string, round int, reviewer string) error {
	t.Helper()
	_, err := store.RecordReview(ctx, iw.rt, iw.crew, iw.key(), store.IssueReview{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Report: "Review of the design.\n\n" + last + "\nEND OF REPORT\n", ReviewerTool: reviewer, AuthorTool: "claude", Round: round})
	return err
}

// readySmall makes an issue takeable at class small: a brief confirmed by
// p2's agent, classified small, the Owner's p2.
func (iw *issueWorld) readySmall(t *testing.T, id, category string) {
	t.Helper()
	ask := iw.brief(t, id, "brief of "+id)
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", category))
	ok(t, iw.prioritize(t, id, "p2"))
}

var fitting = store.MissionProposal{Title: "work on the issue", Goal: "make it work", Success: []string{"it works"}, MaxAttempts: 40, MaxWallHours: 48}

func (iw *issueWorld) take(t *testing.T, id string, m store.MissionProposal) (map[string]any, error) {
	t.Helper()
	return store.TakeIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueTake{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Commitment: "C: we fix " + id, Mission: m})
}

func (iw *issueWorld) liveWork(t *testing.T, product string) int {
	t.Helper()
	return iw.count(t, `SELECT count(*) FROM agentrt.issue_work w JOIN agentrt.issue i USING (issue_id) WHERE i.product = '`+product+`'
	  AND w.ended_at IS NULL`)
}

func (iw *issueWorld) endMission(t *testing.T, mission, to string) {
	t.Helper()
	ok(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, to, "test") }))
}

func wantIs(t *testing.T, err, target error, contains string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v; want %v", err, target)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("err = %v; want it to say %q", err, contains)
	}
}

// Taking twice is refused: one issue has one live work (B2), whether the
// second take comes after the first or beside it, and a stale version is
// refused before anything is checked; a retry of the same key gets the
// first answer. Only one mission ever bears the issue's work id.
func TestIssueDoubleTakeRefused(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the list is empty")
	iw.readySmall(t, id, "improvement")
	v := iw.state(t, id).Version
	key := iw.key()
	take := func(k string, version int) (map[string]any, error) {
		return store.TakeIssue(ctx, iw.rt, iw.crew, k, store.IssueTake{IssueID: id, ExpectedVersion: version, Commitment: "C1", Mission: fitting})
	}
	out, err := take(key, v)
	ok(t, err)
	if out["state"] != "active" || out["mission_state"] != "active" || inum(t, out, "commitment") != 1 {
		t.Fatalf("taken: %v", out)
	}
	again, err := take(key, v)
	ok(t, err)
	if again["mission_id"] != out["mission_id"] {
		t.Fatalf("a retry of the key made %v, the first %v", again["mission_id"], out["mission_id"])
	}
	_, err = take(iw.key(), v)
	wantIs(t, err, store.ErrStale, "is at version")
	_, err = take(iw.key(), iw.state(t, id).Version)
	wantIs(t, err, store.ErrState, "already has live work")
	if n := iw.count(t, `SELECT count(*) FROM agentrt.mission WHERE work_id = '`+id+`'`); n != 1 {
		t.Fatalf("%d missions bear %s", n, id)
	}

	// Two takes at once, of one issue at one version: one wins.
	other := iw.submit(t, "the chart is blank")
	iw.endMission(t, out["mission_id"].(string), "stopped") // free the product's one slot
	iw.readySmall(t, other, "improvement")
	v = iw.state(t, other).Version
	pool := iw.db.Pool(t, iw.db.Runtime)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = store.TakeIssue(ctx, pool, iw.crew, iw.key(), store.IssueTake{IssueID: other, ExpectedVersion: v, Commitment: "C1",
				Mission: fitting})
		}()
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("two takes at once: %v, %v", errs[0], errs[1])
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.mission WHERE work_id = '`+other+`'`); n != 1 {
		t.Fatalf("%d missions bear %s after two takes at once", n, other)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.issue_work WHERE issue_id = '`+other+`'`); n != 1 {
		t.Fatalf("%d works of %s", n, other)
	}
}

// A write at a version the issue has left is refused with the issue as it
// is, and writes nothing.
func TestIssueStaleVersionRefused(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the export fails")
	before := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'issue:`+id+`'`)
	cur := iw.state(t, id)
	iw.brief(t, id, "brief one")
	_, err := store.DraftBrief(ctx, iw.rt, iw.crew, iw.key(), store.BriefDraft{IssueID: id, ExpectedVersion: cur.Version, Text: "brief two"})
	var stale *store.StaleError
	if !errors.As(err, &stale) || stale.Issue.Version != cur.Version+1 || stale.Issue.State != "clarifying" {
		t.Fatalf("a stale draft: %v (%+v)", err, stale)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'issue:`+id+`'`); n != before+1 {
		t.Fatalf("%d events after one brief and a stale one; want %d", n, before+1)
	}
	// The same for an answer to an ask read before the brief changed.
	ask := iw.brief(t, id, "brief two")
	_, err = store.AnswerAsk(ctx, iw.rt, iw.dots, iw.key(), store.AskAnswer{IssueID: id, ExpectedVersion: cur.Version, AskID: ask,
		Words: "yes", Verdict: "confirmed"})
	wantIs(t, err, store.ErrStale, "is at version")
}

// A mission bearing an issue's work id is made only by take_issue: refused
// by every other path in the store, and, written as an older binary writes
// it, refused by the database at commit with nothing left.
func TestIssueMissionOnlyByTake(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "a reserved id")
	for _, work := range []string{id, "BI-99", "BI-x"} {
		p := fitting
		p.WorkID = work
		_, err := store.ProposeMission(ctx, iw.rt, iw.crew, iw.key(), p, nil)
		wantIs(t, err, store.ErrInvalid, "only by take_issue")
	}
	err := store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		_, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-50", WorkID: id, Title: "t", Goal: "g",
			Deadline: time.Now().Add(time.Hour)})
		return err
	})
	wantIs(t, err, store.ErrInvalid, "only by take_issue")
	// The older binary's POST /missions, which has no such check:
	// mission_proposed and the row, then active inside the envelope, with no
	// issue work. The database refuses it at commit.
	err = store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-50"), Type: "mission_proposed", Actor: "controller",
			MissionID: "M-50", Body: map[string]any{"title": "t"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.mission (mission_id, work_id, title, goal, success_json, scope_json, tenant, state,
		  deadline_at, budget_json, created_by_actor_id) VALUES ('M-50', $1, 't', 'g', '{}', '{"max_action":"branch"}', 'dev', 'proposed',
		  now() + interval '1 hour', '{}', 'controller')`, id); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-50", "active", "inside the envelope")
	})
	wantErr(t, err, "only take_issue gives")
	if n := iw.count(t, `SELECT count(*) FROM agentrt.mission WHERE mission_id = 'M-50'`); n != 0 {
		t.Fatal("the refused mission was left")
	}
	// A mission's work id is never changed to one either.
	wantErr(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-2"), Type: "mission_amended", Actor: "controller",
			MissionID: "M-2", Body: map[string]any{"reason": "relabel"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET work_id = $1, version = version + 1 WHERE mission_id = 'M-2'`, id)
		return err
	}), "only take_issue gives")
}

// Taking needs the evidence the rule's class lists, of the current brief and
// design, not a state that was passed (A6): each missing item is named.
func TestIssueTakeNeedsEvidence(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the totals are wrong")
	_, err := iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "not classified")
	// Nothing is classified or designed before the person confirms the brief.
	ask := iw.brief(t, id, "brief B1")
	wantIs(t, iw.classify(t, id, "small", "fault"), store.ErrState, "no confirmed brief")
	wantIs(t, iw.design(t, id, "design"), store.ErrState, "no confirmed brief")
	_, err = iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "improvement"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "3 owner_priority: the Owner has set no priority")
	// later is no go; the Owner's own priority only; a seat's report must be
	// a brief task's.
	ok(t, iw.prioritize(t, id, "later"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "the Owner set later")
	_, err = store.RecordOwnerEvidence(ctx, iw.rt, iw.crew, iw.key(), store.OwnerEvidence{IssueID: id, Priority: "p1", BriefSHA256: iw.state(t, id).BriefSHA256})
	wantIs(t, err, store.ErrInvalid, "the Owner decides")
	_, err = store.RecordOwnerEvidence(ctx, iw.rt, store.Writer{Actor: "controller"}, iw.key(), store.OwnerEvidence{IssueID: id, Priority: "p1",
		BriefSHA256: iw.state(t, id).BriefSHA256, QuestionID: "q1", AnsweredBy: "card-auto-answer v1", Channel: "rule"})
	wantIs(t, err, store.ErrInvalid, "not by a rule")
	_, err = store.ClassifyIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueClassification{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		RiskClass: "small", Category: "fault", SeatReport: iw.report(t, "implement")})
	wantErr(t, err, "report of a brief task")
	// The database holds a priority to the Owner's writers, whatever the store
	// does: a thread's row is refused.
	wantErr(t, iw.issueEvent(t, id, "issue_priority_set", "thread:crew-1", map[string]any{"check_id": 9999, "priority": "p1",
		"brief_sha256": iw.state(t, id).BriefSHA256}, `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, result,
		created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9999, '`+id+`', 'priority', '`+iw.state(t, id).BriefSHA256+`', 'p1', 'thread:crew-1')`),
		"does not write a issue_priority_set event")
	ok(t, iw.prioritize(t, id, "p1"))

	// The rule: missing, the take is refused; expired too.
	none := issueWorldOf(t, newWorld(t), false)
	nid := none.submit(t, "no rule at all")
	none.readySmall(t, nid, "improvement")
	_, err = none.take(t, nid, fitting)
	wantIs(t, err, store.ErrState, "no intake-gates rule is in force")
	other := newIssueWorld(t)
	oid := other.submit(t, "no rule")
	other.readySmall(t, oid, "improvement")
	other.putRule(t, strings.NewReplacer("rule_version: 1", "rule_version: 2", "direction: initial", "direction: tighten",
		"effective_at: 2026-01-01T00:00:00Z", "effective_at: 2026-01-02T00:00:00Z", "expires_at: 2100-01-01T00:00:00Z",
		"expires_at: 2026-01-03T00:00:00Z").Replace(intakeGatesText))
	_, err = other.take(t, oid, fitting)
	wantIs(t, err, store.ErrState, "no intake-gates rule is in force")
	// A rule that does not read is none either: its stored body is not what
	// its text says.
	third := newIssueWorld(t)
	tid := third.submit(t, "an unreadable rule")
	third.readySmall(t, tid, "improvement")
	_, err = third.rt.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) SELECT rule_id, 2, jsonb_set(body_json, '{max_live_per_product}', '20'), replace(body_text, 'rule_version: 1', 'rule_version: 2'),
	  'loosen', 'test', 'test', '2026-01-02T00:00:00Z', expires_at FROM agentrt.rule WHERE rule_id = 'intake-gates'`)
	ok(t, err)
	_, err = third.take(t, tid, fitting)
	wantIs(t, err, store.ErrState, "no intake-gates rule is in force")

	// A new brief leaves the old evidence counting for nothing.
	ask = iw.brief(t, id, "brief B2")
	wantIs(t, iw.classify(t, id, "small", "improvement"), store.ErrState, "no confirmed brief")
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "1 brief_confirmed")
	_, err = iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "2 classified")
	if !strings.Contains(err.Error(), "3 owner_priority") {
		t.Fatalf("B1's priority counted for B2: %v", err)
	}
	ok(t, iw.classify(t, id, "small", "improvement"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "3 owner_priority")
	ok(t, iw.prioritize(t, id, "p2"))
	_, err = iw.take(t, id, fitting)
	ok(t, err)
}

// Medium and large: a design, its independent review passed, the Owner's
// choices at the design's points, all of the current brief and design.
func TestIssueTakeNeedsTheDesignsEvidence(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the method of the screen")
	ask := iw.brief(t, id, "brief")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "medium", "improvement"))
	ok(t, iw.prioritize(t, id, "p1"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "4 design")
	ok(t, iw.design(t, id, "design D1"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "5 review")
	// A review by the author's own tool is no review, in the store and in the
	// database.
	wantIs(t, iw.review(t, id, "通过", 1, "claude"), store.ErrInvalid, "does not review its own design")
	// A conclusion is read from the report's last line, nothing else.
	_, err = store.RecordReview(ctx, iw.rt, iw.crew, iw.key(), store.IssueReview{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Report: "通过 so far, but\nmore to say\n", ReviewerTool: "codex", AuthorTool: "claude", Round: 1})
	wantIs(t, err, store.ErrInvalid, "neither 通过 nor 先修")
	ok(t, iw.review(t, id, "先修：A1", 1, "codex"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "5 review")
	ok(t, iw.review(t, id, "先修：A1", 2, "codex"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "5 review")
	ok(t, iw.decide(t, id, "review_dispute", "proceed"))
	if _, err = iw.take(t, id, fitting); err != nil {
		t.Fatalf("fix after two rounds, the Owner's proceed: %v", err)
	}

	// Large: the design lists D1 (go on under A); the Owner chose B, then A.
	lg := iw.submit(t, "a new table")
	ask = iw.brief(t, lg, "brief")
	_, err = iw.answer(t, iw.dots, lg, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, lg, "large", "method_change"))
	ok(t, iw.prioritize(t, lg, "p1"))
	ok(t, iw.design(t, lg, "design with a point", store.DesignDecision{Key: "D1", Proceed: []string{"A"}}))
	ok(t, iw.review(t, lg, "通过", 1, "codex"))
	ok(t, iw.decide(t, lg, "D1", "B"))
	_, err = iw.take(t, lg, fitting)
	wantIs(t, err, store.ErrState, "6 owner_decisions: the Owner has not chosen to go on at D1")
	ok(t, iw.decide(t, lg, "D1", "A"))
	// A new design: the review and the choice were of the old one.
	ok(t, iw.design(t, lg, "design, second version", store.DesignDecision{Key: "D1", Proceed: []string{"A"}}))
	_, err = iw.take(t, lg, fitting)
	wantIs(t, err, store.ErrState, "5 review")
	if !strings.Contains(err.Error(), "6 owner_decisions") {
		t.Fatalf("the old choice counted for the new design: %v", err)
	}
	ok(t, iw.review(t, lg, "通过", 2, "codex")) // the issue's second round
	ok(t, iw.decide(t, lg, "D1", "A"))
	// The product's one slot is the medium issue's: stop its mission first.
	_, err = iw.take(t, lg, fitting)
	wantIs(t, err, store.ErrState, "7 quota")
	var m string
	ok(t, iw.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.issue_work WHERE issue_id = $1`, id).Scan(&m))
	iw.endMission(t, m, "stopped")
	_, err = iw.take(t, lg, fitting)
	ok(t, err)
}

// The standing exception for a small fault: no priority from the Owner, and
// then only a mission inside standing-envelope:dev; outside it the take
// fails whole and no mission is left.
func TestIssueFaultExceptionStaysInsideTheEnvelope(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the page crashes")
	ask := iw.brief(t, id, "brief")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "fault"))
	big := fitting
	big.MaxAttempts = 100
	missions := iw.count(t, `SELECT count(*) FROM agentrt.mission`)
	_, err = iw.take(t, id, big)
	wantIs(t, err, store.ErrState, "not inside standing-envelope:dev")
	if n := iw.count(t, `SELECT count(*) FROM agentrt.mission`); n != missions {
		t.Fatalf("a refused take left %d missions", n-missions)
	}
	out, err := iw.take(t, id, fitting)
	ok(t, err)
	if out["exception"] != rules.IntakeFaultSmall || out["mission_state"] != "active" {
		t.Fatalf("taken: %v", out)
	}
	var exception string
	ok(t, iw.rt.QueryRow(ctx, `SELECT body_json->>'exception' FROM agentrt.event WHERE event_type = 'issue_taken'`).Scan(&exception))
	if exception != rules.IntakeFaultSmall {
		t.Fatalf("issue_taken says exception %q", exception)
	}
}

// The product's slots are its live work, counted in its lock (R2-A3): a
// parked issue does not free one (parking live work is refused, by the store
// and the database), a mission waiting for the Owner holds one, and only a
// mission's end frees it, through the database's trigger.
func TestIssueQuotaCountsLiveWork(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	a, b, c := iw.submit(t, "a"), iw.submit(t, "b"), iw.submit(t, "c")
	for _, id := range []string{a, b, c} {
		iw.readySmall(t, id, "improvement")
	}
	out, err := iw.take(t, a, fitting)
	ok(t, err)
	ma := out["mission_id"].(string)
	_, err = iw.take(t, b, fitting)
	wantIs(t, err, store.ErrState, "7 quota: screening has live work for 1 issues, its limit 1")
	// Parking the live one is refused, and so is the same state written past
	// the store.
	_, err = store.ParkIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueNote{IssueID: a, ExpectedVersion: iw.state(t, a).Version, Reason: "later"})
	wantIs(t, err, store.ErrState, "has live work")
	wantErr(t, iw.issueEvent(t, a, "issue_state_changed", "thread:crew-1", map[string]any{"change": "park"},
		`UPDATE agentrt.issue SET state = 'parked', version = version + 1 WHERE issue_id = '`+a+`'`), "not parked while its work is live")
	if iw.liveWork(t, "screening") != 1 {
		t.Fatal("the slot moved")
	}
	// The mission ends: its work ends in the same statement, the slot frees.
	iw.endMission(t, ma, "done")
	if iw.liveWork(t, "screening") != 0 || iw.count(t, `SELECT count(*) FROM agentrt.issue_work WHERE ended_at IS NOT NULL`) != 1 {
		t.Fatal("the done mission's work did not end")
	}
	// A mission waiting for the Owner holds the slot. Slice 1 puts up no
	// card for it (review 522 A1): it stays proposed, waiting, no card asked.
	big := fitting
	big.MaxAttempts = 100
	out, err = iw.take(t, b, big)
	ok(t, err)
	if out["mission_state"] != "proposed" || out["question_id"] != nil || out["waiting"] == nil {
		t.Fatalf("outside the envelope: %v", out)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.mission WHERE mission_id = $1 AND state = 'proposed'`, out["mission_id"]); n != 1 {
		t.Fatalf("the mission outside the envelope is not waiting: %v", out)
	}
	_, err = iw.take(t, c, fitting)
	wantIs(t, err, store.ErrState, "7 quota")
	iw.endMission(t, out["mission_id"].(string), "stopped")
	_, err = iw.take(t, c, fitting)
	ok(t, err)
	// Another product has its own slot.
	fin, err := store.SubmitIssue(ctx, iw.rt, iw.dotsOwner, iw.key(), store.IssueSubmission{Product: "finance", Kind: "request", Title: "a report",
		OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "a report please"}}})
	ok(t, err)
	fid := fin["issue_id"].(string)
	ask := iw.brief(t, fid, "brief")
	_, err = iw.answer(t, iw.owner, fid, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, fid, "small", "improvement"))
	ok(t, iw.prioritize(t, fid, "p3"))
	_, err = iw.take(t, fid, fitting)
	ok(t, err)
	// Work ends only with its mission: not written past the trigger.
	wantErr(t, iw.issueEvent(t, c, "issue_state_changed", "thread:crew-1", map[string]any{"change": "close"},
		`UPDATE agentrt.issue_work SET ended_at = now() WHERE issue_id = '`+c+`'`), "a issue_state_changed event does not write agentrt.issue_work")
}

// issueEvent writes one issue event and then stmt, in one transaction, as a
// writer past the store would.
func (iw *issueWorld) issueEvent(t *testing.T, id, typ, actor string, body map[string]any, stmt string) error {
	t.Helper()
	return iw.issueEventOf(t, id, "", typ, actor, body, stmt)
}

// issueEventOf is issueEvent with the envelope naming a mission.
func (iw *issueWorld) issueEventOf(t *testing.T, id, mission, typ, actor string, body map[string]any, stmt string) error {
	t.Helper()
	body["issue_id"] = id
	return store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.IssueStream(id), Type: typ, Actor: actor, MissionID: mission,
			Body: body}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, stmt)
		return err
	})
}
