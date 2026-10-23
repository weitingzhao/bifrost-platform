package server

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 524 A2 on the real question store and a real database: a person's
// issue, her agent's confirmation, the Owner's priority card answered
// Decline on the Console, the runtime's evidence, the crew's close as
// declined; the issue closed declined, its open asks superseded, her agent
// reading the close among its updates. Without the Owner's decline on the
// current brief the crew may not decline it.
func TestOwnersDeclineOnARealCardCloses(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ask, read, ack, withdraw := rt.agentRuntimeAsk(), rt.agentRuntimeQuestion(), rt.agentRuntimeAck(), rt.agentRuntimeWithdraw()
	owner, crew := store.Writer{Actor: "owner", Token: "admin"}, store.Writer{Actor: "thread:bi1-2", Token: "operator"}
	dots := store.Writer{Actor: "agent:dots-p2", Token: "intake:dots-p2"}
	must := func(_ map[string]any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.PutPerson(ctx, conn, owner, "person:p2", "Person two"))
	must(store.SetIntakeClient(ctx, conn, owner, store.IntakeClientSetup{ClientID: dots.Actor, OnBehalfOf: "person:p2", Products: []string{"screening"}}))
	keys := 0
	key := func() string { keys++; return "k-" + string(rune('a'+keys%26)) + time.Now().Format("150405.000000") }
	version := func(id string) int {
		v, err := store.IssueState(ctx, conn, id)
		if err != nil {
			t.Fatal(err)
		}
		return v.Version
	}
	bring := func(title string) string {
		out, err := store.SubmitIssue(ctx, conn, dots, key(), store.IssueSubmission{Product: "screening", Kind: "request", Title: title,
			OnBehalfOf: "person:p2", Words: []store.IssueWords{{Text: "words of " + title}}})
		if err != nil {
			t.Fatal(err)
		}
		id := out["issue_id"].(string)
		out, err = store.DraftBrief(ctx, conn, crew, key(), store.BriefDraft{IssueID: id, ExpectedVersion: version(id), Text: "brief of " + title})
		if err != nil {
			t.Fatal(err)
		}
		must(store.AnswerAsk(ctx, conn, dots, key(), store.AskAnswer{IssueID: id, ExpectedVersion: version(id), AskID: int64(vnumber(out["ask_id"])),
			Words: "yes", Verdict: "confirmed"}))
		return id
	}
	decline := func(id string) string {
		out, err := store.RequestOwner(ctx, conn, crew, key(), store.OwnerCardRequest{IssueID: id, ExpectedVersion: version(id),
			Purpose: store.OwnerCardPriority}, ask)
		if err != nil {
			t.Fatal(err)
		}
		q := out["question_id"].(string)
		if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", q, []byte(`{"option":"c"}`)); res.Code != http.StatusOK {
			t.Fatalf("Decline: %d %v", res.Code, res.Body)
		}
		if _, err := store.DecideIssueCards(ctx, conn, read, ask, withdraw, ack); err != nil {
			t.Fatal(err)
		}
		return q
	}
	closeIt := func(id string) error {
		_, err := store.CloseIssue(ctx, conn, crew, key(), store.IssueClose{IssueID: id, ExpectedVersion: version(id), Conclusion: store.ConclusionDeclined})
		return err
	}

	other := bring("not declined")
	if err := closeIt(other); err == nil {
		t.Fatal("the crew declined a person's issue the Owner did not decline")
	}
	old := bring("declined on an older brief")
	decline(old)
	must(store.DraftBrief(ctx, conn, crew, key(), store.BriefDraft{IssueID: old, ExpectedVersion: version(old), Text: "a newer brief"}))
	if err := closeIt(old); err == nil {
		t.Fatal("a decline on an older brief closed the issue")
	}

	id := bring("declined")
	must(store.Reply(ctx, conn, crew, key(), store.IssueReply{IssueID: id, ExpectedVersion: version(id), Purpose: store.AskNeedsInfo, Text: "anything else?"}))
	q := decline(id)
	if err := closeIt(id); err != nil {
		t.Fatal(err)
	}
	v, err := store.IssueState(ctx, conn, id)
	if err != nil || v.State != "closed" || v.Conclusion != store.ConclusionDeclined {
		t.Fatalf("closed: %+v %v", v, err)
	}
	var open int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.issue_ask WHERE issue_id = $1 AND verdict IS NULL AND superseded_at IS NULL`, id).
		Scan(&open); err != nil || open != 0 {
		t.Fatalf("open asks after the close: %d %v", open, err)
	}
	var basis string
	if err := conn.QueryRow(ctx, `SELECT body_json->>'question_id' FROM agentrt.event WHERE event_stream_id = 'issue:' || $1
	  AND event_type = 'issue_state_changed' AND body_json->>'change' = 'close'`, id).Scan(&basis); err != nil || basis != q {
		t.Fatalf("the close names card %q: %v", basis, err)
	}
	// Her agent reads the close among its updates.
	closed := false
	var cursor *store.EventCursor
	for deadline := time.Now().Add(60 * time.Second); !closed && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		out, err := store.IntakeUpdates(ctx, conn, dots, store.UpdatesRequest{Cursor: cursor, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range out["updates"].([]any) {
			u := u.(map[string]any)
			closed = closed || (u["issue_id"] == id && u["type"] == "closed" && u["conclusion"] == store.ConclusionDeclined)
		}
		next, _ := store.ParseEventCursor(out["next_cursor"].(string))
		cursor = &next
	}
	if !closed {
		t.Fatal("her agent never read the close")
	}
}

// vnumber is a number of a store answer (json.Number, int, float64).
func vnumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case interface{ Float64() (float64, error) }:
		f, _ := n.Float64()
		return f
	}
	return 0
}
