package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/ownerexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// The whole path of an owner_run_command approved with a one-time code, with
// nothing faked between the two programs: this platform's real approval
// routes over HTTP, the real runner, a real bash. Only the seed is a fixture.
type ownerExecFlow struct {
	t    *testing.T
	api  *httptest.Server
	run  *ownerexec.Server
	seed []byte
}

func newOwnerExecFlow(t *testing.T) *ownerExecFlow {
	t.Helper()
	srv := reachServer(t)
	// server.New installed the real lookup; with no cluster and no signed
	// policy it answers "locked, unknown". Give it the signed rule's shape.
	actions.SetD10Paths(func(context.Context) actions.D10Paths {
		return actions.D10Paths{Locked: true, Known: true, Match: releasepolicy.PathMatches,
			Patterns: []string{"scripts/run_daemon.py", "k8s/base/worker/**"}}
	})
	t.Cleanup(func() { actions.SetD10Paths(nil) })
	api := httptest.NewServer(srv.Router())
	t.Cleanup(api.Close)
	seed := []byte("12345678901234567890")
	f := &ownerExecFlow{t: t, api: api, seed: seed}
	f.run = &ownerexec.Server{
		Platform:   ownerexec.NewHTTPPlatform(api.URL),
		Verifier:   ownerexec.NewVerifier(seed, time.Now().Add(-ownerexec.Warmup)),
		ExecutorID: "owner-exec:test",
		Timeout:    time.Minute,
		Logf:       t.Logf,
	}
	return f
}

func (f *ownerExecFlow) call(method, path, principal string, body any) (int, map[string]any) {
	f.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, f.api.URL+path, bytes.NewReader(raw))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+reachTokens[principal])
	req.Header.Set("X-Bifrost-Session", "owner-exec-flow")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// card requests an owner_run_command and returns the record as the page reads it.
func (f *ownerExecFlow) card(command string) map[string]any {
	f.t.Helper()
	status, created := f.call(http.MethodPost, "/api/v1/approvals", "operator", map[string]any{
		"action": "owner_run_command",
		"params": map[string]any{"command": command, "reason": "owner-exec flow test"},
		"reason": "owner-exec flow test",
	})
	if status != http.StatusCreated && status != http.StatusOK && status != http.StatusAccepted {
		f.t.Fatalf("create card: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		f.t.Fatalf("create card: no id in %v", created)
	}
	return f.read(id)
}

func (f *ownerExecFlow) read(id string) map[string]any {
	f.t.Helper()
	status, rec := f.call(http.MethodGet, "/api/v1/approvals/"+id, "admin", nil)
	if status != http.StatusOK {
		f.t.Fatalf("read card %s: %d %v", id, status, rec)
	}
	return rec
}

func (f *ownerExecFlow) code() string {
	return ownerexec.CodeAt(f.seed, time.Now().Unix()/30)
}

// post is what the Console sends for card: its id, the fingerprint of the
// command it showed, the code, and the approve body it would have sent.
func (f *ownerExecFlow) post(principal string, card map[string]any, code string) *httptest.ResponseRecorder {
	f.t.Helper()
	params, _ := card["params"].(map[string]any)
	command, _ := params["command"].(string)
	sum := sha256.Sum256([]byte(command))
	raw, _ := json.Marshal(map[string]any{
		"id":             card["id"],
		"command_sha256": hex.EncodeToString(sum[:]),
		"code":           code,
		"approve": map[string]any{
			"channel": "console", "approval_line": card["approval_line"], "params_hash": card["params_hash"],
		},
	})
	req := httptest.NewRequest(http.MethodPost, ownerexec.RunPath, bytes.NewReader(raw))
	if principal != "" {
		req.Header.Set("Authorization", "Bearer "+reachTokens[principal])
	}
	rec := httptest.NewRecorder()
	f.run.Handler().ServeHTTP(rec, req)
	f.run.Wait()
	return rec
}

func TestOwnerExecFlowApprovesRunsAndWritesTheResultToTheCard(t *testing.T) {
	f := newOwnerExecFlow(t)
	card := f.card("echo owner-exec-ok; echo second-line")
	id := card["id"].(string)
	if card["status"] != "pending" || card["tier"] != "D" {
		t.Fatalf("new card: status %v tier %v", card["status"], card["tier"])
	}

	// A wrong code approves nothing.
	wrong := "000000"
	if wrong == f.code() {
		wrong = "000001"
	}
	if rec := f.post("admin", card, wrong); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
	}
	if got := f.read(id)["status"]; got != "pending" {
		t.Fatalf("after a wrong code the card is %v, want pending", got)
	}

	// An operator's token with the right code: refused before the code is looked at.
	code := f.code()
	if rec := f.post("operator", card, code); rec.Code != http.StatusUnauthorized {
		t.Fatalf("operator token: %d %s", rec.Code, rec.Body)
	}
	if got := f.read(id)["status"]; got != "pending" {
		t.Fatalf("after a non-admin call the card is %v, want pending", got)
	}

	// The Owner's token and the right code: approved, claimed, run, result stored.
	if rec := f.post("admin", card, code); rec.Code != http.StatusAccepted {
		t.Fatalf("right code: %d %s", rec.Code, rec.Body)
	}
	done := f.read(id)
	if done["status"] != "executed" {
		t.Fatalf("card after the run: status %v, error %v", done["status"], done["error"])
	}
	exec, _ := done["execution"].(map[string]any)
	tail, _ := exec["output_tail"].(string)
	if !strings.Contains(tail, "owner-exec-ok") || !strings.Contains(tail, "second-line") {
		t.Fatalf("output on the card: %q", tail)
	}
	if exec["executor_id"] != "owner-exec:test" {
		t.Fatalf("executor on the card: %v", exec["executor_id"])
	}
	if code, _ := exec["exit_code"].(float64); exec["exit_code"] == nil || code != 0 {
		t.Fatalf("exit code on the card: %v", exec["exit_code"])
	}

	// The same code on another card, seconds later: refused, card untouched.
	second := f.card("echo second-card")
	if rec := f.post("admin", second, code); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "already used") {
		t.Fatalf("replayed code: %d %s", rec.Code, rec.Body)
	}
	if got := f.read(second["id"].(string))["status"]; got != "pending" {
		t.Fatalf("after a replayed code the second card is %v, want pending", got)
	}
}

func TestOwnerExecFlowRecordsAFailingCommand(t *testing.T) {
	f := newOwnerExecFlow(t)
	card := f.card("echo about-to-fail; exit 7")
	if rec := f.post("admin", card, f.code()); rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body)
	}
	done := f.read(card["id"].(string))
	exec, _ := done["execution"].(map[string]any)
	if done["status"] != "failed" || exec["exit_code"] != float64(7) {
		t.Fatalf("status %v exit %v", done["status"], exec["exit_code"])
	}
}

// Approved in the Console without a code (the terminal path), then run later
// with one: no second approval is asked for.
func TestOwnerExecFlowRunsACardApprovedEarlier(t *testing.T) {
	f := newOwnerExecFlow(t)
	card := f.card("echo approved-earlier")
	id := card["id"].(string)
	status, body := f.call(http.MethodPost, "/api/v1/approvals/"+id+"/approve", "admin", map[string]any{
		"channel": "console", "approval_line": card["approval_line"], "params_hash": card["params_hash"],
	})
	if status != http.StatusAccepted {
		t.Fatalf("approve: %d %v", status, body)
	}
	approved := f.read(id)
	if approved["status"] != "approved" {
		t.Fatalf("after approve: %v", approved["status"])
	}
	if rec := f.post("admin", approved, f.code()); rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body)
	}
	if got := f.read(id)["status"]; got != "executed" {
		t.Fatalf("after the run: %v", got)
	}
}

// The trade-related refusal, tested here without touching any trade system:
// while D10 is locked a command naming a no_d10_paths file cannot become a card.
func TestOwnerRunCommandNamingAD10PathCannotBecomeACard(t *testing.T) {
	f := newOwnerExecFlow(t)
	status, body := f.call(http.MethodPost, "/api/v1/approvals", "operator", map[string]any{
		"action": "owner_run_command",
		"params": map[string]any{"command": "python scripts/run_daemon.py", "reason": "x"},
		"reason": "x",
	})
	if status != http.StatusForbidden || body["tier"] != "X" {
		t.Fatalf("got %d %v, want 403 tier X", status, body)
	}
}

// server.New installs the lookup, and with no signed policy to read it fails
// closed: every owner_run_command is tier X.
func TestServerInstallsTheD10LookupAndFailsClosed(t *testing.T) {
	_ = reachServer(t)
	t.Cleanup(func() { actions.SetD10Paths(nil) })
	act, _ := actions.ByID("owner_run_command")
	if got := act.TierOf(context.Background(), map[string]any{"command": "echo hello"}); got != actions.TierX {
		t.Fatalf("no readable policy: tier %s, want X", got)
	}
}
