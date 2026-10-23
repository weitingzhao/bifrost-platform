package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// W-31 BI-1 slice 2 on the real question store and a real database
// (DESIGN-crew-intake §5.3 「测试」, the cards): an issue's texts each carry a
// marker; the runtime asks the Owner's cards of each purpose through the
// wire it asks every card by; viewer, reporter and trade-satellite list and
// read the cards and see no marker; none of them is assessed or listed for
// the rule; "none" with text is refused; the Owner's options become the
// issue's evidence, his card's confirmation the brief's.
func TestIssueCardsOnTheQuestionStore(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ask, read, ack, withdraw := rt.agentRuntimeAsk(), rt.agentRuntimeQuestion(), rt.agentRuntimeAck(), rt.agentRuntimeWithdraw()
	owner, crew := store.Writer{Actor: "owner", Token: "admin"}, store.Writer{Actor: "thread:bi1-2", Token: "operator"}
	if _, err := store.PutPerson(ctx, conn, owner, store.OwnerPerson, "Owner"); err != nil {
		t.Fatal(err)
	}
	markers := []string{"TITLE-c0de", "WORDS-c0de", "READING-c0de", "BRIEF-c0de", "DESIGN-c0de", "REVIEW-c0de"}
	out, err := store.SubmitIssue(ctx, conn, crew, "k-1", store.IssueSubmission{Product: "screening", Kind: "request", Title: "TITLE-c0de",
		OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "WORDS-c0de"}}, Reading: "READING-c0de"})
	if err != nil {
		t.Fatal(err)
	}
	id := out["issue_id"].(string)
	version := func() int {
		v, err := store.IssueState(ctx, conn, id)
		if err != nil {
			t.Fatal(err)
		}
		return v.Version
	}
	if _, err = store.DraftBrief(ctx, conn, crew, "k-2", store.BriefDraft{IssueID: id, ExpectedVersion: version(), Text: "BRIEF-c0de"}); err != nil {
		t.Fatal(err)
	}
	request := func(key, purpose, point string, letters int) string {
		t.Helper()
		out, err := store.RequestOwner(ctx, conn, crew, key, store.OwnerCardRequest{IssueID: id, ExpectedVersion: version(), Purpose: purpose,
			Decision: point, Letters: letters}, ask)
		if err != nil {
			t.Fatal(err)
		}
		return out["question_id"].(string)
	}
	decide := func() {
		t.Helper()
		if _, err := store.DecideIssueCards(ctx, conn, read, ask, withdraw, ack); err != nil {
			t.Fatal(err)
		}
	}
	answer := func(q, body string, want int) {
		t.Helper()
		if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", q, []byte(body)); res.Code != want {
			t.Fatalf("answer %s %s: %d %v", q, body, res.Code, res.Body)
		}
	}
	confirm := request("k-3", store.OwnerCardConfirmBrief, "", 0)
	answer(confirm, `{"option":"none","text":"WORDS-c0de"}`, http.StatusBadRequest)
	answer(confirm, `{"option":"a","text":"WORDS-c0de"}`, http.StatusBadRequest)
	answer(confirm, `{"option":"a"}`, http.StatusOK)
	decide()
	if v, _ := store.IssueState(ctx, conn, id); !v.BriefConfirmed {
		t.Fatalf("the Owner's card did not confirm the brief: %+v", v)
	}
	priority := request("k-4", store.OwnerCardPriority, "", 0)
	if _, err = store.PutDesign(ctx, conn, crew, "k-5", store.IssueDesign{IssueID: id, ExpectedVersion: version(), Text: "DESIGN-c0de",
		Decisions: []store.DesignDecision{{Key: "D1", Proceed: []string{"A"}}}}); err != nil {
		t.Fatal(err)
	}
	decision := request("k-6", store.OwnerCardDesignDecision, "D1", 2)
	for i, result := range []string{"先修", "先修"} {
		if _, err = store.RecordReview(ctx, conn, crew, "k-r"+string(rune('1'+i)), store.IssueReview{IssueID: id, ExpectedVersion: version(),
			Report: "REVIEW-c0de\n" + result, ReviewerTool: "codex", AuthorTool: "claude", Round: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	dispute := request("k-7", store.OwnerCardReviewDispute, "", 0)
	cards := []string{confirm, priority, decision, dispute}

	// The generic card reads, for viewer, reporter and trade-satellite.
	path := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(path, []byte(reachAuthFile), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { questions.Mount(r, auth, rt.svc) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	for _, caller := range []string{"viewer", "reporter", "trade-satellite"} {
		paths := []string{"/api/v1/questions", "/api/v1/questions?status=all"}
		for _, q := range cards {
			paths = append(paths, "/api/v1/questions/"+q)
		}
		for _, p := range paths {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+p, nil)
			req.Header.Set("Authorization", "Bearer "+reachTokens[caller])
			req.Header.Set("X-Bifrost-Session", "reader")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			doc, _ := json.Marshal(body)
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(doc), id) {
				t.Fatalf("%s GET %s: %d %s", caller, p, resp.StatusCode, doc)
			}
			for _, m := range markers {
				if strings.Contains(string(doc), m) {
					t.Fatalf("%s GET %s carries %s: %s", caller, p, m, doc)
				}
			}
		}
	}
	// Not assessed, not the rule's.
	items, err := rt.agentRuntimeOpenCards()(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ruled, err := rt.agentRuntimeCards()(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || len(ruled) != 0 {
		t.Fatalf("assessed %d, listed for the rule %d", len(items), len(ruled))
	}
	for _, q := range cards {
		got, ok, err := rt.svc.Lookup(q)
		if err != nil || !ok || got.WorkID != id || !questions.BusinessIssueCard(got) || got.Link != store.IssuePageLink(id) {
			t.Fatalf("card %s: %+v %v %v", q, got, ok, err)
		}
	}
	// The Owner's options become the evidence; the priority in two steps.
	answer(priority, `{"option":"a"}`, http.StatusOK)
	answer(decision, `{"option":"a"}`, http.StatusOK)
	answer(dispute, `{"option":"a"}`, http.StatusOK)
	decide()
	level := rt.svc.List(ctx, "owner-console", "admin", "admin", "open", false)
	if level.Code != http.StatusOK {
		t.Fatalf("open cards: %v", level)
	}
	open := level.Body.(map[string]any)["questions"].([]questions.Question)
	if len(open) != 1 || open[0].Title != id+" · priority, step 2" || open[0].WorkID != id {
		t.Fatalf("open after the first step: %+v", open)
	}
	answer(open[0].ID, `{"option":"c"}`, http.StatusOK)
	decide()
	var kinds string
	if err := conn.QueryRow(ctx, `SELECT string_agg(check_kind || ':' || result || ':' || answer_channel, ',' ORDER BY issue_check_id)
	  FROM agentrt.issue_check WHERE issue_id = $1 AND created_by_actor_id = 'controller'`, id).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	if kinds != "owner_decision:A:console,owner_decision:proceed:console,priority:p3:console" {
		t.Fatalf("the evidence from the cards: %s", kinds)
	}
	if v, _ := store.IssueState(ctx, conn, id); v.State == "owner_deciding" || v.WaitingOn != "crew" || v.Priority != "p3" {
		t.Fatalf("after the cards: %+v", v)
	}
}
