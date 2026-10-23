package store_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 522 (round 1), the fixes the coordinator took: A1, A2, A3, A7, A8
// and the scope part of A6. The guard matrix (B1) is issueguards_test.go.

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// textSQL inserts one issue text with the id the event names; person "" is
// none.
func textSQL(id int64, issue, kind, person, channel, body, actor string) string {
	p := "NULL"
	if person != "" {
		p = "'" + person + "'"
	}
	return fmt.Sprintf(`INSERT INTO agentrt.issue_text (issue_text_id, issue_id, text_kind, person, channel, body, sha256, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', '%s', %s, '%s', '%s', encode(sha256(convert_to('%s', 'UTF8')), 'hex'), '%s')`,
		id, issue, kind, p, channel, body, body, actor)
}

// A1: slice 1 puts no card up for a BI- mission. take_issue passes no ask:
// a mission outside the envelope stays proposed, waiting for the Owner, and
// holds the product's slot (TestIssueQuotaCountsLiveWork); a path that
// passes an ask for a BI- mission is refused before any card is asked.
func TestIssueMissionGetsNoCard(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "outside the envelope")
	iw.readySmall(t, id, "improvement")
	big := fitting
	big.MaxAttempts = 100
	out, err := iw.take(t, id, big)
	ok(t, err)
	if out["mission_state"] != "proposed" || out["question_id"] != nil {
		t.Fatalf("taken outside the envelope: %v", out)
	}
	asked := 0
	ask := func(context.Context, store.Card) (string, error) { asked++; return "q-1", nil }
	other := iw.submit(t, "another")
	err = store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		_, err := store.ProposeIssueMissionForTest(ctx, tx, iw.crew, other, ask)
		return err
	})
	wantIs(t, err, store.ErrInvalid, "its card is not this one")
	if asked != 0 || iw.count(t, `SELECT count(*) FROM agentrt.mission WHERE work_id = $1`, other) != 0 {
		t.Fatalf("a card was asked (%d) or a mission left for %s", asked, other)
	}
}

// A2: an agent's own proposal always needs the Owner's priority: the
// standing exception for a small fault is for what a person served uses. A
// person's small fault still takes it (TestIssueFaultExceptionStaysInsideTheEnvelope).
func TestIssueOwnProposalNeedsTheOwnersPriority(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	out, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "proposal",
		Title: "a fix it proposes", OnBehalfOf: store.SelfIssue, Proposal: "the export button fails; fix it"})
	ok(t, err)
	id := out["issue_id"].(string)
	ask := iw.brief(t, id, "brief of the proposal")
	_, err = iw.answer(t, iw.owner, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "fault"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "3 owner_priority: the Owner has set no priority")
	ok(t, iw.prioritize(t, id, "p2"))
	out, err = iw.take(t, id, fitting)
	ok(t, err)
	if out["exception"] != nil {
		t.Fatalf("taken by the exception: %v", out)
	}
}

// A3: D covers the decision table and the text together: the same text
// with a point removed, or with other options under which the work goes on,
// is another D, so the review and the Owner's choices of the old one count
// for nothing; a text that carries its own table must agree with the one
// given; the Owner decides only points the design has.
func TestIssueDesignHashCoversItsPoints(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "a new method")
	ask := iw.brief(t, id, "brief")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "large", "method_change"))
	ok(t, iw.prioritize(t, id, "p1"))
	const text = "The design. D1 is the storage: A a new table, B a column."
	d1A := store.DesignDecision{Key: "D1", Proceed: []string{"A"}}
	ok(t, iw.design(t, id, text, d1A))
	first := iw.state(t, id).DesignSHA256
	ok(t, iw.review(t, id, "通过", 1, "codex"))
	wantIs(t, iw.decide(t, id, "D9", "A"), store.ErrInvalid, "has no point D9")
	ok(t, iw.decide(t, id, "D1", "B"))
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "6 owner_decisions: the Owner has not chosen to go on at D1")
	// The same text, the point removed: another D; the old review is void.
	ok(t, iw.design(t, id, text))
	if v := iw.state(t, id).DesignSHA256; v == first {
		t.Fatal("removing a point kept D")
	}
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "5 review")
	// The same text, the point back with B allowed too: another D again;
	// the Owner's B was on the first D and does not count.
	ok(t, iw.design(t, id, text, store.DesignDecision{Key: "D1", Proceed: []string{"B", "A"}}))
	third := iw.state(t, id).DesignSHA256
	if third == first {
		t.Fatal("changing the options kept D")
	}
	_, err = iw.take(t, id, fitting)
	wantIs(t, err, store.ErrState, "6 owner_decisions")
	// A text carrying its own table: it must agree with the one given, and
	// alone it gives the same D as the table given beside the text.
	carried := "Decisions (each point the Owner decides: the options under which the work goes on):\nD1: A\n---\n" + text
	wantIs(t, iw.design(t, id, carried, store.DesignDecision{Key: "D1", Proceed: []string{"B"}}), store.ErrInvalid, "own decision table says")
	ok(t, iw.design(t, id, carried))
	if v := iw.state(t, id).DesignSHA256; v != first {
		t.Fatalf("a text carrying the table D1: A is D %s, not %s", v, first)
	}
	// What is stored and hashed is that document, table first.
	var body string
	ok(t, iw.rt.QueryRow(ctx, `SELECT t.body FROM agentrt.issue i JOIN agentrt.issue_text t ON t.issue_text_id = i.design_text_id
	  WHERE i.issue_id = $1`, id).Scan(&body))
	if body != carried {
		t.Fatalf("the stored design is %q", body)
	}
}

// A7: the review rounds are numbered in turn, and after two the Owner
// decides: a skipped or repeated round is refused, a third only after his
// another_round on the current design, in the store and in the database.
func TestIssueReviewRoundsStopAtTwo(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "a medium change")
	ask := iw.brief(t, id, "brief")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "medium", "improvement"))
	ok(t, iw.prioritize(t, id, "p1"))
	ok(t, iw.design(t, id, "design one"))
	wantIs(t, iw.review(t, id, "先修：A1", 2, "codex"), store.ErrState, "is round 1, not 2")
	ok(t, iw.review(t, id, "先修：A1", 1, "codex"))
	wantIs(t, iw.review(t, id, "先修：A1", 1, "codex"), store.ErrState, "is round 2, not 1")
	ok(t, iw.design(t, id, "design two"))
	ok(t, iw.review(t, id, "先修：A2", 2, "codex"))
	ok(t, iw.design(t, id, "design three"))
	wantIs(t, iw.review(t, id, "通过", 3, "codex"), store.ErrState, "needs the Owner's another_round")
	// Past the store too.
	v := iw.state(t, id)
	wantErr(t, iw.issueEvent(t, id, "issue_reviewed", "thread:crew-1", map[string]any{"check_id": 9500, "check_kind": "review", "result": "pass",
		"brief_sha256": v.BriefSHA256, "design_sha256": v.DesignSHA256, "text_id": 9501},
		textSQL(9501, id, "review", "", "thread", "通过", "thread:crew-1")+";"+
			`INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, design_sha256, result, report_text_id,
			  reviewer_tool, author_tool, review_round, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9500, '`+id+`', 'review', '`+
			v.BriefSHA256+`', '`+v.DesignSHA256+`', 'pass', 9501, 'codex', 'claude', 3, 'thread:crew-1')`), "needs the Owner's another_round")
	// The Owner's another_round on an earlier design does not count for a
	// later one; on the current one it lets round 3 be recorded, and its pass
	// takes.
	ok(t, iw.decide(t, id, "review_dispute", store.AnotherRound))
	ok(t, iw.design(t, id, "design four"))
	wantIs(t, iw.review(t, id, "通过", 3, "codex"), store.ErrState, "needs the Owner's another_round")
	ok(t, iw.decide(t, id, "review_dispute", store.AnotherRound))
	ok(t, iw.review(t, id, "通过", 3, "codex"))
	_, err = iw.take(t, id, fitting)
	ok(t, err)
}

// A8: superseding an ask changes when it was superseded and nothing else:
// not who answered, by which channel, or when she said it.
func TestIssueSupersedeChangesNothingElse(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "supersede")
	answered := iw.brief(t, id, "brief one")
	_, err := iw.answer(t, iw.dots, id, answered, "confirmed")
	ok(t, err)
	open := iw.brief(t, id, "brief two")
	supersede := func(ask int64, set string) error {
		return iw.issueEvent(t, id, "issue_state_changed", "thread:crew-1", map[string]any{"change": "supersede", "ask_ids": []any{ask}},
			`UPDATE agentrt.issue_ask SET superseded_at = now()`+set+` WHERE issue_ask_id = `+itoa(ask))
	}
	for _, set := range []string{", answered_by_actor_id = 'owner', answer_channel = 'console'", ", said_at = now() - interval '1 day'"} {
		wantErr(t, supersede(answered, set), "nothing else of it changes")
	}
	wantErr(t, supersede(open, ", said_at = now()"), "nothing else of it changes")
	ok(t, supersede(open, ""))
}

// A6, scope: an agent writes on an issue only while it sees it: its client
// not revoked, the issue's product among its products, past the store too,
// on a text and on a change of the issue.
func TestIssueAgentWritesOnlyWhatItSees(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	fin, err := store.SubmitIssue(ctx, iw.rt, iw.dotsOwner, iw.key(), store.IssueSubmission{Product: "finance", Kind: "request",
		Title: "his finance", OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "his words"}}})
	ok(t, err)
	f := fin["issue_id"].(string)
	added := func(w store.Writer, issue string, id int64) error {
		return iw.issueEvent(t, issue, "issue_added", w.Actor, map[string]any{"channel": "intake_local", "text_ids": []any{id}},
			textSQL(id, issue, "reading", "", "intake_local", "more", w.Actor)+";"+
				`UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = '`+issue+`'`)
	}
	ok(t, added(iw.dotsOwner, f, 9601))
	// The Owner narrows the client to screening: finance is out of its sight.
	_, err = store.SetIntakeClient(ctx, iw.rt, iw.owner, store.IntakeClientSetup{ClientID: iw.dotsOwner.Actor, OnBehalfOf: store.OwnerPerson,
		Products: []string{"screening"}})
	ok(t, err)
	wantErr(t, added(iw.dotsOwner, f, 9602), "writes only on issues it sees")
	wantErr(t, iw.issueEvent(t, f, "issue_added", iw.dotsOwner.Actor, map[string]any{"channel": "intake_local", "text_ids": []any{int64(9603)}},
		`UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = '`+f+`'`), "writes only on issues it sees")
	// Revoked: neither a text nor a change of its own person's issue.
	hers := iw.submit(t, "hers")
	ok(t, added(iw.dots, hers, 9604))
	ok(t, store.RevokeIntakeClient(ctx, iw.rt, iw.owner, iw.dots.Actor))
	wantErr(t, iw.issueEvent(t, hers, "issue_added", iw.dots.Actor, map[string]any{"channel": "intake_local", "text_ids": []any{int64(9605)}},
		`UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = '`+hers+`'`), "writes only on issues it sees")
	wantErr(t, added(iw.dots, hers, 9606), "writes only on issues it sees")
}
