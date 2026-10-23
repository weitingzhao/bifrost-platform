package store_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 524 (W-31 BI-1 slice 2, round 1): A2, B1 and B2 in the store. A1 is
// the Console's (IssuePage.test.tsx); A2 on the real question store is
// server/agentrt_issuedecline_test.go.

// shown is an issue's state and whom it waits on as the crew's full view and
// its requester's list show them.
func (iw *issueWorld) shown(t *testing.T, id string) (string, string) {
	t.Helper()
	full, err := store.IssueFull(ctx, iw.rt, id)
	ok(t, err)
	row := full["issue"].(map[string]any)
	list, err := store.ListIssues(ctx, iw.rt, store.IssueListFilter{})
	ok(t, err)
	for _, r := range list["issues"].([]any) {
		if r := r.(map[string]any); r["issue_id"] == id && (r["state"] != row["state"] || r["waiting_on"] != row["waiting_on"]) {
			t.Fatalf("the list shows %v, the full view %v", r, row)
		}
	}
	return row["state"].(string), row["waiting_on"].(string)
}

// readied is p2's issue, its brief confirmed and classified.
func (iw *issueWorld) readied(t *testing.T, title string) string {
	t.Helper()
	id := iw.submit(t, title)
	ask := iw.brief(t, id, "brief of "+title)
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "improvement"))
	return id
}

// B1: an issue shows owner_deciding and waits on the Owner exactly while one
// of his cards on it is open; a card settled with no evidence (it expired, an
// answer that does not count) leaves it showing the step its evidence has
// reached, waiting on the crew; with two cards, settling one leaves it
// waiting on him for the other.
func TestIssueShowsTheOwnerOnlyWhileHisCardIsOpen(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	expire := func(q string) {
		e := d.questions[q]
		e.Status = "expired"
		d.questions[q] = e
	}
	id := iw.readied(t, "expires")
	q, err := iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	ok(t, err)
	if s, w := iw.shown(t, id); s != "owner_deciding" || w != "owner" {
		t.Fatalf("with the card open: %s %s", s, w)
	}
	expire(q)
	d.decide(t, iw)
	if s, w := iw.shown(t, id); s != "classified" || w != "crew" {
		t.Fatalf("after the only card expired: %s %s", s, w)
	}
	mine, err := store.MyIssues(ctx, iw.rt, iw.dots, store.MyIssuesFilter{State: "classified"})
	ok(t, err)
	if len(mine["issues"].([]any)) != 1 {
		t.Fatalf("her agent's list by the shown state: %v", mine)
	}

	ruled := iw.readied(t, "ruled")
	q, err = iw.requestOwner(t, d, ruled, store.OwnerCardPriority, "", 0)
	ok(t, err)
	d.answer(q, "b", "rule")
	d.decide(t, iw)
	if s, w := iw.shown(t, ruled); s != "classified" || w != "crew" {
		t.Fatalf("after an answer that does not count: %s %s", s, w)
	}

	// Two cards on the Owner's own issue: its brief's confirmation and its
	// priority; the first expires, the issue still waits on him for the
	// second; then it is answered.
	own := iw.ownerIssue(t)
	iw.brief(t, own, "BRIEF of his own")
	confirm, err := iw.requestOwner(t, d, own, store.OwnerCardConfirmBrief, "", 0)
	ok(t, err)
	priority, err := iw.requestOwner(t, d, own, store.OwnerCardPriority, "", 0)
	ok(t, err)
	expire(confirm)
	d.decide(t, iw)
	if s, w := iw.shown(t, own); s != "owner_deciding" || w != "owner" {
		t.Fatalf("one card of two settled: %s %s", s, w)
	}
	d.answer(priority, "b", "console") // later
	d.decide(t, iw)
	if s, w := iw.shown(t, own); s != "clarifying" || w != "user" {
		t.Fatalf("both settled, its brief's confirmation still asked of him: %s %s", s, w)
	}
	// A write answers with the state shown too.
	out, err := store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.ReplyPlain, Text: "a reply"})
	ok(t, err)
	if out["state"] != "classified" || out["waiting_on"] != "crew" {
		t.Fatalf("a write's answer: %v", out)
	}
}

// A2: the Owner's Decline on a person's issue's priority card is the crew's
// to close as declined, and only that: on the current brief, from his card
// answered on the Console; never on an older brief, a rule's answer, a
// decline he took back with a later level, or for a thread with none.
func TestOwnersDeclineOnHisCardLetsTheCrewDecline(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	decline := func(id, channel string) {
		t.Helper()
		q, err := iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
		ok(t, err)
		d.answer(q, "c", channel)
		d.decide(t, iw)
	}
	closeIt := func(id string) error {
		t.Helper()
		_, err := store.CloseIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueClose{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
			Conclusion: store.ConclusionDeclined})
		return err
	}

	none := iw.readied(t, "nothing declined it")
	wantIs(t, closeIt(none), store.ErrState, "declined by the Owner")

	ruled := iw.readied(t, "a rule's decline")
	decline(ruled, "rule")
	wantIs(t, closeIt(ruled), store.ErrState, "declined by the Owner")

	old := iw.readied(t, "declined on an older brief")
	decline(old, "console")
	iw.brief(t, old, "a newer brief")
	wantIs(t, closeIt(old), store.ErrState, "declined by the Owner")

	overruled := iw.readied(t, "declined, then p2")
	decline(overruled, "console")
	ok(t, iw.prioritize(t, overruled, "p2"))
	wantIs(t, closeIt(overruled), store.ErrState, "declined by the Owner")

	id := iw.readied(t, "declined")
	_, err := store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.AskNeedsInfo, Text: "one more thing?"})
	ok(t, err)
	decline(id, "console")
	q := d.ids[len(d.ids)-1]
	full, err := store.IssueFull(ctx, iw.rt, id)
	ok(t, err)
	next, _ := full["issue"].(map[string]any)["next"].(map[string]any)
	if next["by"] != "crew" || !strings.Contains(next["what"].(string), "close it declined") || !strings.Contains(next["what"].(string), q) {
		t.Fatalf("what the issue shows next: %v", full["issue"])
	}
	ok(t, closeIt(id))
	v := iw.state(t, id)
	if v.State != "closed" || v.Conclusion != store.ConclusionDeclined {
		t.Fatalf("closed: %+v", v)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.issue_ask WHERE issue_id = '`+id+`' AND verdict IS NULL AND superseded_at IS NULL`); n != 0 {
		t.Fatalf("%d asks still open", n)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'issue:`+id+`' AND event_type = 'issue_state_changed'
	  AND body_json->>'change' = 'close' AND body_json->>'question_id' = '`+q+`'`); n != 1 {
		t.Fatalf("the close names the Owner's card: %d", n)
	}
	// A proposal stays the crew's to decline with a reason, as before; a
	// thread may not decline another person's issue it has no decline for.
	wantIs(t, closeIt(none), store.ErrState, "declined by the Owner")
}

// B2: a cursor past how far the client was served is refused and changes
// nothing (not served, not acknowledged), and the client's updates are
// still read on from where they were.
func TestAFutureCursorIsRefused(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "cursors")
	far := store.EventCursor{TxID: math.MaxInt64}
	_, err := store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Cursor: &store.EventCursor{TxID: 5, EventID: 1}, Limit: 5})
	wantIs(t, err, store.ErrBadCursor, "served (nothing)")
	out, err := store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Limit: 50})
	ok(t, err)
	served := out["next_cursor"].(string)
	for _, req := range []store.UpdatesRequest{{Cursor: &far, Limit: 5}, {Cursor: &far, Ack: &far, Limit: 0}, {Cursor: &far, Limit: 0}} {
		_, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, req)
		wantIs(t, err, store.ErrBadCursor, "past how far")
	}
	_, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Ack: &far})
	wantIs(t, err, store.ErrInvalid, "acknowledges no further")
	var gotServed, gotAcked string
	ok(t, iw.rt.QueryRow(ctx, `SELECT COALESCE(served_txid || '.' || served_event, ''), COALESCE(acked_txid || '.' || acked_event, '')
	  FROM agentrt.intake_client WHERE intake_client_id = $1`, iw.dots.Actor).Scan(&gotServed, &gotAcked))
	if gotServed != served || gotAcked != "" {
		t.Fatalf("served %q (read to %s), acked %q", gotServed, served, gotAcked)
	}
	// A reply after it is still delivered, read on from the cursor it was
	// handed.
	_, err = store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.ReplyPlain, Text: "REPLY-524"})
	ok(t, err)
	cur, _ := store.ParseEventCursor(served)
	found := false
	for i := 0; i < 600 && !found; i++ {
		out, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Cursor: &cur, Limit: 50})
		ok(t, err)
		for _, u := range out["updates"].([]any) {
			found = found || u.(map[string]any)["text"] == "REPLY-524"
		}
		cur, _ = store.ParseEventCursor(out["next_cursor"].(string))
		if !found {
			sleep100ms()
		}
	}
	if !found {
		t.Fatal("the reply after the refused cursor was not delivered")
	}
}

func sleep100ms() { time.Sleep(100 * time.Millisecond) }
