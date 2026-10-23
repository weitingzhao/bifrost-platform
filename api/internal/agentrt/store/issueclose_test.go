package store_test

import (
	"fmt"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const trialCommit = "0123456789abcdef0123456789abcdef01234567"

func (iw *issueWorld) trial(t *testing.T, id string, commitment int, mission string) (int64, error) {
	t.Helper()
	out, err := store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.AskReadyToTry, Text: "try the screen on STG: the list shows again", Commitment: commitment, MissionID: mission, Env: "stg",
		Commits: []string{trialCommit}})
	if err != nil {
		return 0, err
	}
	return inum(t, out, "ask_id"), nil
}

func (iw *issueWorld) closeCheck(t *testing.T, id string, ask int64, result string) error {
	t.Helper()
	_, err := store.RecordCloseCheck(ctx, iw.rt, iw.crew, iw.key(), store.IssueCloseCheck{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		AskID: ask, Result: result, SeatReport: iw.seat, Excerpt: "her words match the delivery"})
	return err
}

func (iw *issueWorld) close(t *testing.T, w store.Writer, id, conclusion string, ask int64, reason string) error {
	t.Helper()
	_, err := store.CloseIssue(ctx, iw.rt, w, iw.key(), store.IssueClose{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Conclusion: conclusion, AskID: ask, Reason: reason})
	return err
}

func (iw *issueWorld) askOf(t *testing.T, id, purpose string) int64 {
	t.Helper()
	var ask int64
	ok(t, iw.rt.QueryRow(ctx, `SELECT max(issue_ask_id) FROM agentrt.issue_ask WHERE issue_id = $1 AND purpose = $2`, id, purpose).Scan(&ask))
	return ask
}

// Closing as resolved or partial rests on the person's own answer to this
// very delivery, for the current brief, and the seat's newest check of it
// supporting the close (A5, R2-A4): no answer, a brief's confirmation passed
// off as a trial's, a partly answer for resolved, no seat check, or a newest
// check that does not support it, are each refused.
func TestIssueCloseNeedsTheServedPersonsAnswer(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the filter is lost")
	iw.readySmall(t, id, "improvement")
	confirm := iw.askOf(t, id, store.AskConfirmBrief)
	out, err := iw.take(t, id, fitting)
	ok(t, err)
	m := out["mission_id"].(string)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, confirm, ""), store.ErrState, "not a trial")
	trial, err := iw.trial(t, id, 1, m)
	ok(t, err)
	if v := iw.state(t, id); v.State != "awaiting_user" || v.WaitingOn != "user" {
		t.Fatalf("a trial asked: %+v", v)
	}
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, trial, ""), store.ErrState, `the answer to ask`)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, confirm, ""), store.ErrState, "not its newest")
	// Silence, the agent's judgement or the crew's never close it.
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, 0, "it works now"), store.ErrState, "not its newest")
	_, err = iw.answer(t, iw.dots, id, trial, "partly")
	ok(t, err)
	if v := iw.state(t, id); v.State != "active" || v.WaitingOn != "crew" {
		t.Fatalf("after her answer: %+v", v)
	}
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, trial, ""), store.ErrState, `"partly", not confirmed`)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionPartial, trial, ""), store.ErrState, "no seat has checked")
	ok(t, iw.closeCheck(t, id, trial, "supports"))
	ok(t, iw.closeCheck(t, id, trial, "does_not"))
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionPartial, trial, ""), store.ErrState, "newest check of the answer")
	ok(t, iw.closeCheck(t, id, trial, "supports"))
	ok(t, iw.close(t, iw.crew, id, store.ConclusionPartial, trial, ""))
	if v := iw.state(t, id); v.State != "closed" || v.Conclusion != store.ConclusionPartial {
		t.Fatalf("closed: %+v", v)
	}
	// A seat checks an answer given; an unanswered ask is not checked.
	other := iw.submit(t, "another")
	ask := iw.brief(t, other, "brief")
	wantIs(t, iw.closeCheck(t, other, ask, "supports"), store.ErrState, "not answered")
}

// An earlier delivery's answer, an answer of an earlier brief, and work
// taken for an earlier brief never close or deliver the current scope
// (R2-A4): a new trial supersedes the one before; the brief does not change
// while work is live or a trial is open; after B1 → B2, the trial answered
// under B1 does not close it, and B1's work is not tried under B2.
func TestIssueOldAnswersDoNotClose(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "the sort is wrong")
	iw.readySmall(t, id, "improvement")
	out, err := iw.take(t, id, fitting)
	ok(t, err)
	m := out["mission_id"].(string)
	first, err := iw.trial(t, id, 1, m)
	ok(t, err)
	_, err = iw.answer(t, iw.dots, id, first, "confirmed")
	ok(t, err)
	ok(t, iw.closeCheck(t, id, first, "supports"))
	second, err := iw.trial(t, id, 1, m)
	ok(t, err)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, first, ""), store.ErrState, "not its newest")
	// While the trial is open, and while the work is live, the brief stays.
	_, err = store.DraftBrief(ctx, iw.rt, iw.crew, iw.key(), store.BriefDraft{IssueID: id, ExpectedVersion: iw.state(t, id).Version, Text: "B2"})
	wantIs(t, err, store.ErrState, "has live work")
	_, err = iw.answer(t, iw.dots, id, second, "confirmed")
	ok(t, err)
	iw.endMission(t, m, "done")
	// B2, after the trial was answered and the work ended.
	b2 := iw.brief(t, id, "brief B2: more than before")
	ok(t, iw.closeCheck(t, id, second, "supports")) // a check of B2's time, of an answer of B1's
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, second, ""), store.ErrState, "not its newest")
	_, err = iw.answer(t, iw.dots, id, b2, "confirmed")
	ok(t, err)
	// B1's work is not tried under B2, by the store or past it.
	_, err = iw.trial(t, id, 1, m)
	wantIs(t, err, store.ErrState, "taken for an earlier brief")
	b2sha := iw.state(t, id).BriefSHA256
	wantErr(t, iw.issueEventOf(t, id, m, "issue_asked", "thread:crew-1", map[string]any{"text_id": 1, "ask_id": 9999, "purpose": "ready_to_try"},
		fmt.Sprintf(`INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id, bound_sha256, brief_sha256,
		  commitment, mission_id, env) OVERRIDING SYSTEM VALUE SELECT 9999, '%s', 'person:p2', 'ready_to_try', issue_text_id, sha256, '%s', 1, '%s', 'stg'
		  FROM agentrt.issue_text WHERE issue_id = '%s' AND text_kind = 'reply' LIMIT 1`, id, b2sha, m, id)), "foreign key")
	// New work for B2: classified and prioritized again, taken, tried, closed.
	ok(t, iw.classify(t, id, "small", "improvement"))
	ok(t, iw.prioritize(t, id, "p2"))
	out, err = iw.take(t, id, fitting)
	ok(t, err)
	if inum(t, out, "commitment") != 2 {
		t.Fatalf("the second work: %v", out)
	}
	third, err := iw.trial(t, id, 2, out["mission_id"].(string))
	ok(t, err)
	_, err = iw.answer(t, iw.dots, id, third, "confirmed")
	ok(t, err)
	ok(t, iw.closeCheck(t, id, third, "supports"))
	ok(t, iw.close(t, iw.crew, id, store.ConclusionResolved, third, ""))
	// Reopened, every trial asked before is superseded: none closes it again.
	_, err = store.ReopenIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueNote{IssueID: id, ExpectedVersion: iw.state(t, id).Version, Reason: "it broke again"})
	ok(t, err)
	if v := iw.state(t, id); v.State != "classified" || v.Conclusion != "" {
		t.Fatalf("reopened: %+v", v)
	}
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionResolved, third, ""), store.ErrState, "not its newest")
}

// Answers are bound (A5): to an ask of this issue, open, of a purpose that
// takes the verdict, by a writer that speaks for the person asked, for the
// current brief; an agent's own proposal is confirmed by the Owner himself;
// a withdrawal comes before the issue is taken.
func TestIssueAnswersAreBound(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	a := iw.submit(t, "hers")
	own, err := store.SubmitIssue(ctx, iw.rt, iw.dotsOwner, iw.key(), store.IssueSubmission{Product: "screening", Kind: "request",
		Title: "his", OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "his words"}}})
	ok(t, err)
	o := own["issue_id"].(string)
	askA := iw.brief(t, a, "brief of hers")
	askO := iw.brief(t, o, "brief of his")
	// Another issue's ask; an issue the agent does not see; a verdict the
	// purpose does not take.
	_, err = iw.answer(t, iw.dots, a, askO, "confirmed")
	wantIs(t, err, store.ErrInvalid, "has no ask")
	_, err = iw.answer(t, iw.dots, o, askO, "confirmed")
	wantIs(t, err, store.ErrInvalid, "there is no issue")
	_, err = iw.answer(t, iw.dotsOwner, a, askA, "confirmed")
	wantIs(t, err, store.ErrInvalid, "there is no issue")
	_, err = iw.answer(t, iw.dots, a, askA, "partly")
	wantIs(t, err, store.ErrInvalid, "is answered")
	// A thread relays her answer only as the Owner's relay.
	_, err = store.AnswerAsk(ctx, iw.rt, iw.crew, iw.key(), store.AskAnswer{IssueID: a, ExpectedVersion: iw.state(t, a).Version, AskID: askA,
		Words: "she said yes", Verdict: "confirmed"})
	wantIs(t, err, store.ErrInvalid, "as the Owner's relay")
	// Past the store: the Owner's agent answering for her is refused.
	wantErr(t, iw.issueEvent(t, a, "issue_answered", "agent:dots-owner", map[string]any{"ask_id": askA, "text_ids": []any{int64(9999)},
		"verdict": "confirmed", "channel": "intake_local", "person": "person:p2"},
		`INSERT INTO agentrt.issue_text (issue_text_id, issue_id, text_kind, person, channel, body, sha256, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES (9999, '`+a+`', 'response', 'person:p2', 'intake_local', 'yes', encode(sha256('yes'), 'hex'), 'agent:dots-owner')`),
		"writes only on issues it sees")
	// A re-brief while she reads: the version she read and the ask are gone.
	v := iw.state(t, a).Version
	iw.brief(t, a, "brief of hers, again")
	_, err = store.AnswerAsk(ctx, iw.rt, iw.dots, iw.key(), store.AskAnswer{IssueID: a, ExpectedVersion: v, AskID: askA, Words: "yes", Verdict: "confirmed"})
	wantIs(t, err, store.ErrStale, "is at version")
	_, err = iw.answer(t, iw.dots, a, askA, "confirmed")
	wantIs(t, err, store.ErrStale, "no longer open")
	askA = iw.askOf(t, a, store.AskConfirmBrief)
	_, err = iw.answer(t, iw.dots, a, askA, "confirmed")
	ok(t, err)
	_, err = iw.answer(t, iw.dots, a, askA, "confirmed")
	wantIs(t, err, store.ErrStale, "no longer open")
	// His own issue: the Owner on the Console, or his agent.
	_, err = iw.answer(t, iw.owner, o, askO, "confirmed")
	ok(t, err)

	// An agent's own proposal: its brief is the Owner's to confirm.
	prop, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "proposal", Title: "an idea",
		OnBehalfOf: store.SelfIssue, Proposal: "add a column"})
	ok(t, err)
	p := prop["issue_id"].(string)
	askP := iw.brief(t, p, "brief of the idea")
	if v := iw.state(t, p); v.WaitingOn != "owner" || v.AsksPerson != store.OwnerPerson || v.ServedPerson != "" {
		t.Fatalf("a proposal's brief waits on: %+v", v)
	}
	_, err = iw.answer(t, iw.dots, p, askP, "confirmed")
	wantIs(t, err, store.ErrInvalid, "the Owner himself")
	_, err = iw.answer(t, iw.dotsOwner, p, askP, "confirmed")
	wantIs(t, err, store.ErrInvalid, "there is no issue")
	_, err = iw.answer(t, iw.owner, p, askP, "confirmed")
	ok(t, err)

	// Withdrawn: before the take, and it closes only as declined on it.
	w := iw.submit(t, "never mind")
	askW := iw.brief(t, w, "brief")
	_, err = iw.answer(t, iw.dots, w, askW, "withdrawn")
	ok(t, err)
	ok(t, iw.close(t, iw.crew, w, store.ConclusionDeclined, askW, "she withdrew it"))
	taken := iw.submit(t, "taken")
	iw.readySmall(t, taken, "improvement")
	out, err := iw.take(t, taken, fitting)
	ok(t, err)
	trial, err := iw.trial(t, taken, 1, out["mission_id"].(string))
	ok(t, err)
	_, err = iw.answer(t, iw.dots, taken, trial, "withdrawn")
	wantIs(t, err, store.ErrState, "a withdrawal comes before")
	// Declining her issue is the Owner's, not the crew's.
	wantIs(t, iw.close(t, iw.crew, a, store.ConclusionDeclined, 0, "not now"), store.ErrState, "declined by the Owner")
	ok(t, iw.close(t, iw.owner, a, store.ConclusionDeclined, 0, ""))
	// An agent's own proposal the crew may decline, with a reason.
	wantIs(t, iw.close(t, iw.crew, p, store.ConclusionDeclined, 0, ""), store.ErrState, "with a reason")
	ok(t, iw.close(t, iw.crew, p, store.ConclusionDeclined, 0, "out of scope"))
}

// already_possible: the newest ask an answer to check, confirmed, for the
// current brief, no live work; and what the standing exception still needs
// (the brief confirmed, classified, the rule in force).
func TestIssueAlreadyPossible(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "can it do this")
	ask := iw.brief(t, id, "brief")
	reply := func() int64 {
		out, err := store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
			Purpose: store.AskCheckAnswer, Text: "it already can: open the second tab"})
		ok(t, err)
		return inum(t, out, "ask_id")
	}
	check := reply()
	_, err := iw.answer(t, iw.dots, id, check, "confirmed")
	ok(t, err)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionAlreadyPossible, check, ""), store.ErrState, "the brief is not confirmed")
	_, err = iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "guidance"))
	second := reply()
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionAlreadyPossible, check, ""), store.ErrState, "not its newest")
	_, err = iw.answer(t, iw.dots, id, second, "rejected")
	ok(t, err)
	wantIs(t, iw.close(t, iw.crew, id, store.ConclusionAlreadyPossible, second, ""), store.ErrState, `"rejected", not confirmed`)
	third := reply()
	_, err = iw.answer(t, iw.dots, id, third, "confirmed")
	ok(t, err)
	ok(t, iw.close(t, iw.crew, id, store.ConclusionAlreadyPossible, third, ""))
}

// A class only rises, in the store and in the database; merging stays within
// one person's untaken issues of one product (A7, B2), and raises the class
// of the one kept.
func TestIssueRiskOnlyRisesAndMergesStayWithOnePerson(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	a, b := iw.submit(t, "a"), iw.submit(t, "b")
	for _, id := range []string{a, b} {
		ask := iw.brief(t, id, "brief of "+id)
		_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
		ok(t, err)
	}
	ok(t, iw.classify(t, a, "small", "improvement"))
	ok(t, iw.classify(t, b, "large", "method_change"))
	wantIs(t, iw.classify(t, b, "medium", "improvement"), store.ErrState, "only rises")
	wantErr(t, iw.issueEvent(t, b, "issue_classified", "thread:crew-1", map[string]any{"check_id": 1, "risk_class": "small", "category": "fault",
		"brief_sha256": iw.state(t, b).BriefSHA256, "seat_report": iw.seat},
		`UPDATE agentrt.issue SET risk_class = 'small', version = version + 1 WHERE issue_id = '`+b+`'`), "only rises")
	// Across people: refused by the store and by the database.
	his, err := store.SubmitIssue(ctx, iw.rt, iw.dotsOwner, iw.key(), store.IssueSubmission{Product: "screening", Kind: "complaint",
		Title: "his b", OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "his words"}}})
	ok(t, err)
	h := his["issue_id"].(string)
	merge := func(from, into string) error {
		_, err := store.MergeIssues(ctx, iw.rt, iw.crew, iw.key(), store.IssueMerge{From: from, Into: into,
			FromVersion: iw.state(t, from).Version, IntoVersion: iw.state(t, into).Version})
		return err
	}
	wantIs(t, merge(h, a), store.ErrState, "not one person's issues")
	wantErr(t, iw.issueEvent(t, h, "issue_merged", "thread:crew-1", map[string]any{"into": a},
		`UPDATE agentrt.issue SET duplicate_of = '`+a+`', state = 'closed', conclusion = 'merged', version = version + 1 WHERE issue_id = '`+h+`'`),
		"not one person's issues")
	// One person's: b into a; a rises to large; b's words stay with b.
	ok(t, merge(b, a))
	if v := iw.state(t, b); v.State != "closed" || v.Conclusion != store.ConclusionMerged || v.DuplicateOf != a {
		t.Fatalf("merged: %+v", v)
	}
	if v := iw.state(t, a); v.RiskClass != "large" {
		t.Fatalf("kept: %+v", v)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.issue_text WHERE issue_id = '`+b+`' AND text_kind = 'words'`); n != 1 {
		t.Fatalf("b's words: %d", n)
	}
	_, err = store.ReopenIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueNote{IssueID: b, ExpectedVersion: iw.state(t, b).Version})
	wantIs(t, err, store.ErrState, "stays so")
	// An issue ever taken is not merged (first batch).
	c, d := iw.submit(t, "c"), iw.submit(t, "d")
	iw.readySmall(t, c, "improvement")
	_, err = iw.take(t, c, fitting)
	ok(t, err)
	wantIs(t, merge(d, c), store.ErrState, "work was taken for")
}

// Parking frees nothing and is refused under live work (R2-A3); a parked
// issue is resumed where it was; the conclusion's guard holds past the store.
func TestIssueParkAndResume(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "park me")
	ask := iw.brief(t, id, "brief")
	_, err := store.ParkIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueNote{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Reason: "waiting for the season; resume in December"})
	ok(t, err)
	if v := iw.state(t, id); v.State != "parked" {
		t.Fatalf("parked: %+v", v)
	}
	// Her answer to the ask parked away is refused: parking superseded it.
	_, err = iw.answer(t, iw.dots, id, ask, "confirmed")
	wantIs(t, err, store.ErrStale, "no longer open")
	_, err = store.ResumeIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueNote{IssueID: id, ExpectedVersion: iw.state(t, id).Version})
	ok(t, err)
	if v := iw.state(t, id); v.State != "clarifying" {
		t.Fatalf("resumed: %+v", v)
	}
	// Closing written past the store with another event type is refused.
	wantErr(t, iw.issueEvent(t, id, "issue_replied", "thread:crew-1", map[string]any{"text_id": 1},
		`UPDATE agentrt.issue SET state = 'closed', conclusion = 'resolved', version = version + 1 WHERE issue_id = '`+id+`'`),
		"closed by issue_state_changed")
	// And a skipped version.
	wantErr(t, iw.issueEvent(t, id, "issue_state_changed", "thread:crew-1", map[string]any{"change": "resume"},
		`UPDATE agentrt.issue SET waiting_on = 'owner', version = version + 2 WHERE issue_id = '`+id+`'`), "one at a time")
}
