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
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The landing grant executor's approval side (W-31 core C-5, coordinator v3
// §5.6 item 4, §12.2 item 5, T27): through the real approval service, the
// executor writes a grant only after reading its approval back — the
// action, tier D, running, its params_hash, the Console channel with the
// number confirmed, PROD. The store half is TestC5* (store).

type grantCall struct {
	a store.GrantApproval
	p store.GrantParams
}

// grantServer is a PROD server whose create check passes every request (the
// store half is tested apart) and whose grant writer is recorded.
func grantServer(t *testing.T) (*Server, func(method, path string, body any, role string) *httptest.ResponseRecorder, *[]grantCall,
	*func(ctx context.Context) error) {
	t.Helper()
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
	srv.agentRuntime.api.DB = &lateDB{} // a runtime; the recorded writer never uses it
	srv.approvals.SetCreateCheck(func(context.Context, string, map[string]any) error { return nil })
	var calls []grantCall
	during := func(context.Context) error { return nil }
	saved := writeLandingGrant
	writeLandingGrant = func(ctx context.Context, _ store.DB, _ store.GrantSources, a store.GrantApproval, p store.GrantParams) (store.LandingGrant, error) {
		calls = append(calls, grantCall{a, p})
		if err := during(ctx); err != nil {
			return store.LandingGrant{}, err
		}
		return store.LandingGrant{GrantID: "lg_" + a.ID, Op: p.Op, R: p.R, Waives: []string{"core"}}, nil
	}
	t.Cleanup(func() { writeLandingGrant = saved })
	h := srv.Router()
	do := func(method, path string, body any, role string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+role+"-test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	return srv, do, &calls, &during
}

func grantParams() map[string]any {
	return map[string]any{"env": "prod", "repo": "bifrost-platform", "task": "t_01JA0000000000000000000000", "op": "land:t_01JA0000000000000000000000:1",
		"R": strings.Repeat("b", 40), "material_digest": "sha256:" + strings.Repeat("c", 64), "stg_release": true}
}

type approvalRow struct {
	ID           string `json:"id"`
	Tier         string `json:"tier"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	ApprovalLine string `json:"approval_line"`
	ParamsHash   string `json:"params_hash"`
	Number       int    `json:"number"`
}

func requestGrant(t *testing.T, do func(string, string, any, string) *httptest.ResponseRecorder, action string, params map[string]any) approvalRow {
	t.Helper()
	rec := do(http.MethodPost, "/api/v1/approvals", map[string]any{"action": action, "params": params, "reason": "land the commit", "rollback": "revert"}, "operator")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created approvalRow
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	rec = do(http.MethodGet, "/api/v1/approvals/"+created.ID, nil, "viewer")
	var row approvalRow
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.Tier != "D" {
		t.Fatalf("tier %s", row.Tier)
	}
	return row
}

func decide(do func(string, string, any, string) *httptest.ResponseRecorder, row approvalRow, channel string, confirm bool) (int, approvalRow) {
	body := map[string]any{"channel": channel, "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash}
	if confirm {
		body["confirm_number"] = row.Number
	}
	rec := do(http.MethodPost, "/api/v1/approvals/"+row.ID+"/approve", body, "admin")
	var out approvalRow
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Serial: t.Setenv and the package's grant writer. The success, and each
// refusal one change from it.
func TestLandingGrantExecutorReadsItsApprovalBack(t *testing.T) {
	srv, do, calls, during := grantServer(t)
	ctx := context.Background()

	// No approval at all: a context any code in this process can build.
	if _, err := actions.Execute(actions.WithCreateAttempt(ctx, "appr_forged", 1), store.ActionCoreLanding, grantParams()); err == nil {
		t.Fatal("executed without an approval")
	}
	if len(*calls) != 0 {
		t.Fatal("the store was called")
	}

	// Without the confirmed number the Owner cannot approve it at all.
	row := requestGrant(t, do, store.ActionCoreLanding, grantParams())
	if code, out := decide(do, row, "console", false); code != http.StatusBadRequest || out.Status == "executed" {
		t.Fatalf("approved without its number: %d %+v", code, out)
	}
	// Chat is refused for tier D (the service's own check).
	if code, _ := decide(do, row, "chat", true); code != http.StatusForbidden {
		t.Fatalf("chat: %d", code)
	}

	// The success: Console, the number confirmed. While it runs, the record
	// read back is checked against each wrong expectation.
	var inner []string
	*during = func(ctx context.Context) error {
		p := grantParams()
		if _, err := srv.grantApproval(ctx, store.ActionGuardrailLanding, p); err == nil {
			inner = append(inner, "another action accepted")
		}
		p["stg_release"] = false
		if _, err := srv.grantApproval(ctx, store.ActionCoreLanding, p); err == nil {
			inner = append(inner, "other params accepted")
		}
		if _, err := srv.grantApproval(context.Background(), store.ActionCoreLanding, grantParams()); err == nil {
			inner = append(inner, "no execution context accepted")
		}
		t.Setenv("OPS_VIEWER_ENV", "stg")
		_, err := srv.grantApproval(ctx, store.ActionCoreLanding, grantParams())
		os.Setenv("OPS_VIEWER_ENV", "prod")
		if err == nil {
			inner = append(inner, "STG home accepted")
		}
		if _, err := srv.grantApproval(ctx, store.ActionCoreLanding, grantParams()); err != nil {
			inner = append(inner, "the record itself refused: "+err.Error())
		}
		return nil
	}
	code, out := decide(do, row, "console", true)
	if code != http.StatusOK || out.Status != "executed" {
		t.Fatalf("approve: %d %+v", code, out)
	}
	if len(inner) != 0 {
		t.Fatal(inner)
	}
	if len(*calls) != 1 || (*calls)[0].a.ID != row.ID || (*calls)[0].a.Number != int64(row.Number) || (*calls)[0].a.Action != store.ActionCoreLanding ||
		(*calls)[0].a.ParamsHash != row.ParamsHash || (*calls)[0].a.ApprovedAt.IsZero() || (*calls)[0].p.Op != grantParams()["op"] {
		t.Fatalf("%+v", *calls)
	}
	*during = func(context.Context) error { return nil }

	// Executed already: the same context again is not a running approval.
	if _, err := actions.Execute(actions.WithParamsHash(actions.WithCreateAttempt(ctx, row.ID, 2), row.ParamsHash), store.ActionCoreLanding, grantParams()); err == nil {
		t.Fatal("re-ran outside its approval's execution")
	}

	// Approved on the phone, number confirmed: not the Console, refused.
	other := grantParams()
	other["op"] = "land:t_01JA0000000000000000000000:2"
	row = requestGrant(t, do, store.ActionGuardrailLanding, other)
	code, out = decide(do, row, "phone", true)
	if code != http.StatusOK || out.Status != "failed" || !strings.Contains(out.Error, "Console") {
		t.Fatalf("phone: %d %+v", code, out)
	}
	if len(*calls) != 1 {
		t.Fatal("the store was called for a phone approval")
	}
}

// Closed parameters: an extra key, a missing key, STG, infra.
func TestLandingGrantParamsAreClosed(t *testing.T) {
	t.Parallel()
	good := grantParams()
	if _, err := store.DecodeGrantParams(good); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(map[string]any){
		"extra":   func(p map[string]any) { p["waives"] = []any{"core"} },
		"missing": func(p map[string]any) { delete(p, "stg_release") },
		"stg":     func(p map[string]any) { p["env"] = "stg" },
		"infra":   func(p map[string]any) { p["repo"] = "bifrost-trade-infra" },
		"op":      func(p map[string]any) { p["op"] = "land:t_01JA0000000000000000000001:1" },
	} {
		p := grantParams()
		edit(p)
		if _, err := store.DecodeGrantParams(p); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	p := grantParams()
	p["repo"] = "bifrost-trade-infra"
	if _, err := store.DecodeGrantParams(p); err == nil || !strings.Contains(err.Error(), "grant_unsupported") {
		t.Errorf("infra: %v", err)
	}
}

// The read-back record's check field by field (the inner layer: the approval
// service already refuses a landing grant approved on chat or without its
// number, so HTTP cannot reach these alone; a record from an older binary,
// or written by other code in the process, can).
func TestCheckGrantRecord(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	good := func() approvals.Approval {
		return approvals.Approval{ID: "appr_x", Number: 9, ConfirmedNumber: 9, Action: store.ActionCoreLanding, Tier: "D", Status: approvals.StatusRunning,
			ParamsHash: hash, Channel: "console", DecidedAt: time.Now(), Params: map[string]any{"env": "prod"}}
	}
	if err := checkGrantRecord(good(), store.ActionCoreLanding, hash, hash, true); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*approvals.Approval){
		"action":      func(a *approvals.Approval) { a.Action = store.ActionGuardrailLanding },
		"tier":        func(a *approvals.Approval) { a.Tier = "C" },
		"approved":    func(a *approvals.Approval) { a.Status = approvals.StatusApproved },
		"executed":    func(a *approvals.Approval) { a.Status = approvals.StatusExecuted },
		"hash":        func(a *approvals.Approval) { a.ParamsHash = strings.Repeat("b", 64) },
		"drill":       func(a *approvals.Approval) { a.Drill = true },
		"phone":       func(a *approvals.Approval) { a.Channel = "phone" },
		"chat":        func(a *approvals.Approval) { a.Channel = "chat" },
		"unconfirmed": func(a *approvals.Approval) { a.ConfirmedNumber = 0 },
		"other no.":   func(a *approvals.Approval) { a.ConfirmedNumber = 8 },
		"undecided":   func(a *approvals.Approval) { a.DecidedAt = time.Time{} },
		"stg params":  func(a *approvals.Approval) { a.Params["env"] = "stg" },
	} {
		a := good()
		edit(&a)
		if err := checkGrantRecord(a, store.ActionCoreLanding, hash, hash, true); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if checkGrantRecord(good(), store.ActionCoreLanding, hash, "", true) == nil || checkGrantRecord(good(), store.ActionCoreLanding, hash, hash, false) == nil {
		t.Error("no execution hash, or not PROD, accepted")
	}
}
