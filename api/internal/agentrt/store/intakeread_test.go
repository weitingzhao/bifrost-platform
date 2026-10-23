package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The read projections of business issues (W-31 BI-1 slice 2,
// issueread.go, intakeupdates.go; DESIGN-crew-intake §2.1, §4.3, §5.3, §8).

// internalMarkers are texts the crew keeps to itself: an intake client
// never reads them.
var internalMarkers = []string{"CLASSREASON-51c2", "COMMITMENT-51c2", "EXCERPT-51c2", "CLOSENOTE-51c2", "OTHERWORDS-51c2"}

func noneOf(t *testing.T, what string, v any, markers []string) {
	t.Helper()
	doc, err := json.Marshal(v)
	ok(t, err)
	for _, m := range markers {
		if strings.Contains(string(doc), m) {
			t.Fatalf("%s carries %s: %s", what, m, doc)
		}
	}
}

// workedIssue is p2's issue brought by her agent, its brief confirmed,
// classified with a reason, prioritized, taken with a commitment, replied
// to, checked with an excerpt: every internal text carries a marker.
func (iw *issueWorld) workedIssue(t *testing.T) (string, int64) {
	t.Helper()
	id := iw.submit(t, "worked")
	ask := iw.brief(t, id, "BRIEF-51c2")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	_, err = store.ClassifyIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueClassification{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		RiskClass: "small", Category: "improvement", SeatReport: iw.seat, Reason: "CLASSREASON-51c2"})
	ok(t, err)
	ok(t, iw.prioritize(t, id, "p2"))
	_, err = store.TakeIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueTake{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Commitment: "COMMITMENT-51c2", Mission: fitting})
	ok(t, err)
	_, err = store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.ReplyPlain, Text: "REPLY-51c2"})
	ok(t, err)
	_, err = store.RecordCloseCheck(ctx, iw.rt, iw.crew, iw.key(), store.IssueCloseCheck{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		AskID: ask, Result: "supports", SeatReport: iw.seat, Excerpt: "EXCERPT-51c2"})
	ok(t, err)
	return id, ask
}

// An intake client sees its person's issues of its products and its own
// proposals, nothing else; of each, its person's words and answers, its own
// reading, proposal and notes, what the crew wrote for it and the ask open
// now; never a reason, a commitment, a seat's excerpt, a design, a review.
func TestIntakeSeesItsOwnProjection(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id, _ := iw.workedIssue(t)
	_, err := store.SubmitIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueSubmission{Product: "screening", Kind: "request", Title: "the Owner's",
		OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "OTHERWORDS-51c2"}}})
	ok(t, err)
	mine, err := store.MyIssues(ctx, iw.rt, iw.dots, store.MyIssuesFilter{})
	ok(t, err)
	list := mine["issues"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["issue_id"] != id {
		t.Fatalf("my issues: %v", mine)
	}
	noneOf(t, "my issues", mine, append(internalMarkers, "BRIEF-51c2"))
	seen, err := store.IssueSeen(ctx, iw.rt, iw.dots, id)
	ok(t, err)
	noneOf(t, "the projection", seen, internalMarkers)
	doc, _ := json.Marshal(seen)
	for _, want := range []string{"words of worked", "the agent reads worked", "BRIEF-51c2", "REPLY-51c2", "yes, that is it"} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("the projection lacks %q: %s", want, doc)
		}
	}
	other := iw.count(t, `SELECT max(substr(issue_id, 4)::int) FROM agentrt.issue`)
	_, err = store.IssueSeen(ctx, iw.rt, iw.dots, fmt.Sprintf("BI-%d", other))
	wantIs(t, err, store.ErrInvalid, "there is no issue")
	// The other client sees the Owner's and not p2's.
	_, err = store.IssueSeen(ctx, iw.rt, iw.dotsOwner, id)
	wantIs(t, err, store.ErrInvalid, "there is no issue")
	// Revoked, it reads nothing, its replays included.
	ok(t, store.RevokeIntakeClient(ctx, iw.rt, iw.owner, iw.dots.Actor))
	_, err = store.MyIssues(ctx, iw.rt, iw.dots, store.MyIssuesFilter{})
	wantIs(t, err, store.ErrRevoked, "agent:dots-p2")
	_, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Limit: 5})
	wantIs(t, err, store.ErrRevoked, "agent:dots-p2")
	_, err = store.IntakeGuide(ctx, iw.rt, iw.dots, "")
	wantIs(t, err, store.ErrRevoked, "agent:dots-p2")
	// Another token on its name reads nothing.
	_, err = store.MyIssues(ctx, iw.rt, store.Writer{Actor: "agent:dots-owner", Token: "intake:dots-p2"}, store.MyIssuesFilter{})
	wantIs(t, err, store.ErrNotYours, "bound to token")
}

// get_updates: the update events of the issues the client sees, in commit
// order, with the texts the crew wrote for it and whether an ask is open;
// internal events and texts never; how far it was served moves without an
// event, its acknowledgement with one, never past what it was served.
func TestIntakeUpdatesAreTheRequesterProjection(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id, _ := iw.workedIssue(t)
	_, err := store.SubmitIssue(ctx, iw.rt, iw.dotsOwner, iw.key(), store.IssueSubmission{Product: "screening", Kind: "complaint",
		Title: "not p2's", OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "OTHERWORDS-51c2"}}})
	ok(t, err)
	_, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Ack: &store.EventCursor{TxID: 1}, Limit: 0})
	wantIs(t, err, store.ErrInvalid, "was served up to nothing")

	// A snapshot's horizon is the cluster's (other tests' transactions hold
	// it a moment): read on, three at a time, until the five are in.
	var updates []map[string]any
	var cursor *store.EventCursor
	for deadline := time.Now().Add(60 * time.Second); len(updates) < 5 && time.Now().Before(deadline); {
		out, err := store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Cursor: cursor, Limit: 3})
		ok(t, err)
		got := out["updates"].([]any)
		for _, u := range got {
			updates = append(updates, u.(map[string]any))
		}
		next, okc := store.ParseEventCursor(out["next_cursor"].(string))
		if !okc {
			t.Fatalf("next %v", out["next_cursor"])
		}
		cursor = &next
		if len(got) < 3 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	noneOf(t, "the updates", updates, internalMarkers)
	var types []string
	for _, u := range updates {
		if u["issue_id"] != id {
			t.Fatalf("an update of another issue: %v", u)
		}
		types = append(types, u["type"].(string))
	}
	if got := strings.Join(types, ","); got != "received,confirm_brief,classified,taken,reply" {
		t.Fatalf("updates %s", got)
	}
	if updates[1]["text"] != "BRIEF-51c2" || updates[1]["ask_user"] != false || updates[4]["text"] != "REPLY-51c2" {
		t.Fatalf("the brief's and the reply's updates: %v %v", updates[1], updates[4])
	}
	var served, acked string
	ok(t, iw.rt.QueryRow(ctx, `SELECT served_txid || '.' || served_event, COALESCE(acked_txid || '.' || acked_event, '') FROM agentrt.intake_client
	  WHERE intake_client_id = $1`, iw.dots.Actor).Scan(&served, &acked))
	if served != cursor.String() || acked != "" {
		t.Fatalf("served %s acked %s, read to %s", served, acked, cursor)
	}
	before := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'intake_acked'`)
	out, err := store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Ack: cursor, Limit: 0})
	ok(t, err)
	if out["acked"] != cursor.String() || len(out["updates"].([]any)) != 0 || iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'intake_acked'`) != before+1 {
		t.Fatalf("acknowledged: %v", out)
	}
	past := store.EventCursor{TxID: cursor.TxID + 1000}
	_, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Ack: &past})
	wantIs(t, err, store.ErrInvalid, "acknowledges no further")
	// A new ask opens: read on from the acknowledgement (no cursor), with
	// ask_user while it is open.
	_, err = store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.AskNeedsInfo, Text: "NEEDS-51c2"})
	ok(t, err)
	var got []any
	for deadline := time.Now().Add(60 * time.Second); len(got) == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		out, err = store.IntakeUpdates(ctx, iw.rt, iw.dots, store.UpdatesRequest{Limit: 10})
		ok(t, err)
		got = out["updates"].([]any)
	}
	if len(got) != 1 || got[0].(map[string]any)["type"] != "needs_info" || got[0].(map[string]any)["ask_user"] != true ||
		got[0].(map[string]any)["text"] != "NEEDS-51c2" {
		t.Fatalf("after the acknowledgement: %v", out)
	}
	// The Owner's own client reads none of p2's.
	out, err = store.IntakeUpdates(ctx, iw.rt, iw.dotsOwner, store.UpdatesRequest{Limit: 50})
	ok(t, err)
	for _, u := range out["updates"].([]any) {
		if u.(map[string]any)["issue_id"] == id {
			t.Fatalf("agent:dots-owner read %v", u)
		}
	}
	// The guide's change is an update to every client.
	ok(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.IntakeGuideStream, Type: "note", Actor: "owner", Body: map[string]any{"text": "v2"}})
		return err
	}))
	next, _ := store.ParseEventCursor(out["next_cursor"].(string))
	for deadline := time.Now().Add(60 * time.Second); ; {
		out, err = store.IntakeUpdates(ctx, iw.rt, iw.dotsOwner, store.UpdatesRequest{Cursor: &next, Limit: 50})
		ok(t, err)
		if len(out["updates"].([]any)) > 0 || time.Now().After(deadline) {
			break
		}
		next, _ = store.ParseEventCursor(out["next_cursor"].(string))
		time.Sleep(100 * time.Millisecond)
	}
	if g := out["updates"].([]any); len(g) != 1 || g[0].(map[string]any)["type"] != "guide_changed" {
		t.Fatalf("the guide's change: %v", out)
	}
}

// The updates read on by transaction (§4.3): an update whose transaction
// took a smaller event id and committed later is read once it commits,
// none skipped and none twice, filtered in the same statement.
func TestIntakeUpdatesKeepLateCommits(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	first, second := iw.submit(t, "late"), iw.submit(t, "early")
	pool := iw.db.Pool(t, iw.db.Runtime)
	readAll := func(from *store.EventCursor) ([]int64, *store.EventCursor) {
		var ids []int64
		cur := from
		for {
			out, err := store.IntakeUpdates(ctx, pool, iw.dots, store.UpdatesRequest{Cursor: cur, Limit: 2})
			ok(t, err)
			page := out["updates"].([]any)
			for _, u := range page {
				ids = append(ids, int64(u.(map[string]any)["update_id"].(int64)))
			}
			next, _ := store.ParseEventCursor(out["next_cursor"].(string))
			cur = &next
			if len(page) < 2 {
				return ids, cur
			}
		}
	}
	_, cur := readAll(nil)
	// Two issues' streams: a stream's lock would serialize them.
	reply := func(tx pgx.Tx, id string) int64 {
		ev, err := store.Append(ctx, tx, store.Event{Stream: store.IssueStream(id), Type: "issue_replied", Actor: "owner",
			Body: map[string]any{"issue_id": id, "text_id": 0}})
		ok(t, err)
		return ev.EventID
	}
	late, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	ok(t, err)
	defer func() { _ = late.Rollback(ctx) }()
	small := reply(late, first)
	var big int64
	ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { big = reply(tx, second); return nil }))
	seen, mid := readAll(cur)
	for _, id := range seen {
		if id == big || id == small {
			t.Fatalf("read %v while the older transaction ran", seen)
		}
	}
	ok(t, late.Commit(ctx))
	var got []int64
	cur = mid
	for deadline := time.Now().Add(60 * time.Second); len(got) < 2 && time.Now().Before(deadline); {
		ids, next := readAll(cur)
		for _, id := range ids {
			if id == small || id == big {
				got = append(got, id)
			}
		}
		cur = next
		if len(got) < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if len(got) != 2 || got[0] != small || got[1] != big {
		t.Fatalf("read on %v, want %d then %d", got, small, big)
	}
}

// The crew's list and full view (§8): the row's shape, the views, the full
// view's texts, asks, checks, work, timeline and cards.
func TestCrewReadsIssuesInFull(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id, _ := iw.workedIssue(t)
	out, err := store.ListIssues(ctx, iw.rt, store.IssueListFilter{View: "active"})
	ok(t, err)
	rows := out["issues"].([]any)
	if len(rows) != 1 {
		t.Fatalf("active: %v", out)
	}
	row := rows[0].(map[string]any)
	if row["issue_id"] != id || row["served_person"].(map[string]any)["name"] != "Person two" ||
		row["via"].(map[string]any)["actor"] != iw.dots.Actor || row["via"].(map[string]any)["channel"] != store.ChannelIntake ||
		row["priority"] != "p2" || row["raised_times"] != float64(1) || len(row["work"].([]any)) != 1 || row["receipt"].(map[string]any)["pending"] != float64(5) {
		t.Fatalf("the row: %v", row)
	}
	for view, n := range map[string]int{"new": 0, "triage": 0, "closed": 0} {
		out, err := store.ListIssues(ctx, iw.rt, store.IssueListFilter{View: view})
		ok(t, err)
		if len(out["issues"].([]any)) != n {
			t.Fatalf("%s: %v", view, out)
		}
	}
	_, err = store.ListIssues(ctx, iw.rt, store.IssueListFilter{View: "new", State: "active"})
	wantIs(t, err, store.ErrInvalid, "not both")
	full, err := store.IssueFull(ctx, iw.rt, id)
	ok(t, err)
	doc, _ := json.Marshal(full)
	for _, want := range []string{"CLASSREASON-51c2", "COMMITMENT-51c2", "EXCERPT-51c2", "BRIEF-51c2", "words of worked", `"timeline"`, `"cards"`} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("the full view lacks %q", want)
		}
	}
	if b := full["issue"].(map[string]any)["brief"].(map[string]any); b["sha256"] != iw.state(t, id).BriefSHA256 || b["confirmed"] != true {
		t.Fatalf("the brief: %v", b)
	}
}
