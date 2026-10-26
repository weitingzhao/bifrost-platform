package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// voidSet is a made-up exam set: one item of the chief's exam.
type voidSet struct{}

func (voidSet) Digest() string { return strings.Repeat("d4", 32) }
func (voidSet) Items(job string) []store.ExamItem {
	if job != "chief_of_staff" {
		return nil
	}
	return []store.ExamItem{{ID: "V1", Class: "exam-brief"}}
}
func (voidSet) Render(string, time.Time) (store.ExamSpec, error) {
	return store.ExamSpec{T0: time.Now().UTC().Truncate(time.Minute), Spec: []byte(`{"text": "# V1"}`)}, nil
}
func (voidSet) Grade(string, string, time.Time) (store.ExamVerdict, error) {
	return store.ExamVerdict{Passed: true}, nil
}

// TD-407: the Owner voids an unanswered exam paper through an approval of
// void_exam_paper, on the real path: the request, the Owner's approval on the
// Console, the platform's executor, the store. The void records the
// approval's id, and the executor is handed a new paper at once. The action
// has no direct route; a request for another environment's runtime, or for a
// paper with an answer, fails at execution and writes nothing.
func TestVoidExamPaperApprovalOnTheRealPath(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "prod")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the chief's job"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('chief_of_staff', 'dev', 'job:test', 1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterHost(ctx, conn, "owner", "agent-06-stg", "dev"); err != nil {
		t.Fatal(err)
	}
	srv.agentRuntime.api.DB = conn
	ask := store.ExamRequest{Host: "agent-06-stg", Token: "agentd:agent-06-stg", Job: "chief_of_staff", Executor: "exec:claude"}
	paper, err := store.IssueExam(ctx, conn, ask, voidSet{})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Router()
	do := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	// request creates the approval; approve is the Owner's, on the Console.
	request := func(env, run string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"action": "void_exam_paper", "reason": "TD-407: agentd refused the paper before any run",
			"params": map[string]any{"environment": env, "run_id": run, "reason": "agentd refused it before any run (short commits)"}})
		rec := do(http.MethodPost, "/api/v1/approvals", string(body), "operator-test-token")
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"tier":"D"`) {
			t.Fatalf("request = %d %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	approve := func(id string) string {
		t.Helper()
		rec := do(http.MethodGet, "/api/v1/approvals/"+id, "", "viewer-test-token")
		var row struct {
			ApprovalLine string `json:"approval_line"`
			ParamsHash   string `json:"params_hash"`
			Number       int    `json:"number"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil || row.ApprovalLine == "" {
			t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
		}
		body, _ := json.Marshal(map[string]any{"channel": "console", "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash,
			"confirm_number": row.Number})
		return do(http.MethodPost, "/api/v1/approvals/"+id+"/approve", string(body), "admin-test-token").Body.String()
	}
	notes := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`,
			store.ExamsStream).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// No direct path: an operator's call of the executor without an
	// approval is refused, as is a chat approval of tier D.
	if _, err = actions.Execute(ctx, "void_exam_paper", map[string]any{"environment": "prod", "run_id": paper.RunID, "reason": "x"}); err == nil {
		t.Fatal("the executor ran without an approval")
	}
	wrongBody, _ := json.Marshal(map[string]any{"action": "void_exam_paper", "params": map[string]any{"environment": "stg", "run_id": paper.RunID, "reason": "test"}, "reason": "test"})
	if rec := do(http.MethodPost, "/api/v1/approvals", string(wrongBody), "operator-test-token"); rec.Code != 422 || !strings.Contains(rec.Body.String(), "ops.bifrost.lan") {
		t.Fatalf("another environment's request: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := srv.voidExamPaper(actions.WithCreateAttempt(ctx, "appr_stale", 1), map[string]any{"environment": "stg", "run_id": paper.RunID, "reason": "test"}); err == nil {
		t.Fatal("cross-environment execution accepted")
	}
	id := request("prod", paper.RunID)
	chat := do(http.MethodPost, "/api/v1/approvals/"+id+"/approve", `{"channel":"chat"}`, "admin-test-token")
	if chat.Code != http.StatusForbidden || notes() != 0 {
		t.Fatalf("a chat approval of tier D = %d %s", chat.Code, chat.Body.String())
	}
	if out := approve(id); !strings.Contains(out, `"status":"executed"`) || !strings.Contains(out, `"voided_by":"`+id+`"`) {
		t.Fatalf("approve = %s", out)
	}
	var voided string
	if err = conn.QueryRow(ctx, `SELECT body_json->>'voided' FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`,
		store.ExamsStream).Scan(&voided); err != nil || voided != id {
		t.Fatalf("the void's approval: %q (%v), want %s", voided, err, id)
	}
	next, err := store.IssueExam(ctx, conn, ask, voidSet{})
	if err != nil || next.RunID == paper.RunID {
		t.Fatalf("a new paper after the void: %v", err)
	}
	// A paper with an answer is not voided: the approval fails, nothing is written.
	if _, err = store.RecordExamAnswers(ctx, conn, "agent-06-stg", "agentd:agent-06-stg", store.ExamSubmission{RunID: next.RunID,
		Executor: "exec:claude", Provider: "anthropic", Model: "opus", ToolVersion: "2.1.0 (Claude Code)", ExamSHA256: next.SetSHA256, PinsOK: true,
		Answers: []store.ExamAnswer{{Item: "V1", RunNo: 1, Round: 1, Text: "an answer", WallS: 30, Conclusion: "done"}}}, voidSet{}); err != nil {
		t.Fatal(err)
	}
	if out := approve(request("prod", next.RunID)); !strings.Contains(out, `"status":"failed"`) || notes() != 1 {
		t.Fatalf("a paper with an answer: %s (%d notes)", out, notes())
	}
	if _, err = store.IssueExam(ctx, conn, ask, voidSet{}); !errors.Is(err, store.ErrState) {
		t.Fatalf("the answered paper still holds exec:claude: %v", err)
	}
}
