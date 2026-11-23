package ownerexec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

type machineTicketPlatform struct {
	*fakePlatform
	host, mac string
	until     time.Time
}

func (f *machineTicketPlatform) AuthorizeHost(_ context.Context, _, _, host string, until time.Time, mac string) error {
	f.note("ticket")
	f.host, f.until, f.mac = host, until, mac
	return nil
}

// TD-578: the 2026-11-22 Station bootstrap needs the same code flow on
// macOS. Changing the displayed target must not consume the owner's code,
// and the verifier must never run the host-bound command itself.
func TestCodeAuthorizesOnlyReviewedMachine(t *testing.T) {
	h := newHarness(t, "pending")
	h.fake.card.Runner = "host"
	h.fake.card.Params["host"] = "work-mac"
	h.fake.card.ParamsHash = "fixture-config-fingerprint"
	h.srv.TicketKey = []byte(strings.Repeat("f", 32))
	p := &machineTicketPlatform{fakePlatform: h.fake}
	h.srv.Platform = p

	req := h.body(h.code())
	req["host"] = "other-mac"
	req["params_hash"] = h.fake.card.ParamsHash
	if rec := h.post(testBearer, req); rec.Code != http.StatusConflict {
		t.Fatalf("changed host: %d %s", rec.Code, rec.Body)
	}
	if h.fake.called("approve") || h.fake.called("ticket") {
		t.Fatal("a changed host was authorized")
	}
	req["host"] = "work-mac"
	req["params_hash"] = "different-fingerprint"
	if rec := h.post(testBearer, req); rec.Code != http.StatusConflict {
		t.Fatalf("changed parameters: %d %s", rec.Code, rec.Body)
	}
	req["params_hash"] = h.fake.card.ParamsHash
	if rec := h.post(testBearer, req); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "\"status\":\"queued\"") {
		t.Fatalf("correct host and same code: %d %s", rec.Code, rec.Body)
	}
	want := approvals.TicketMAC(h.srv.TicketKey, h.fake.card.ID, h.fake.card.ParamsHash, "work-mac", p.until)
	if !h.fake.called("approve") || p.host != "work-mac" || p.mac != want || !p.until.After(h.now) {
		t.Fatalf("the trusted verifier did not queue the correct ticket (host=%q)", p.host)
	}
	if h.fake.called("claim") || h.commandsRun() != 0 {
		t.Fatal("the verifier ran work intended for the remote machine")
	}
}

func TestWrongCodeDoesNotIssueMachineTicket(t *testing.T) {
	h := newHarness(t, "pending")
	h.fake.card.Runner = "host"
	h.fake.card.Params["host"] = "work-mac"
	h.fake.card.ParamsHash = "fixture-config-fingerprint"
	h.srv.TicketKey = []byte(strings.Repeat("f", 32))
	h.srv.Platform = &machineTicketPlatform{fakePlatform: h.fake}
	wrong := (h.code()[0]-'0'+1)%10 + '0'
	req := h.body(string(wrong) + h.code()[1:])
	req["host"], req["params_hash"] = "work-mac", h.fake.card.ParamsHash
	rec := h.post(testBearer, req)
	if rec.Code != http.StatusForbidden || h.fake.called("approve") || h.fake.called("ticket") {
		t.Fatalf("wrong code changed the card: %d %s", rec.Code, rec.Body)
	}
}

func TestMachineRunsOnlyItsClaim(t *testing.T) {
	h := newHarness(t, "approved")
	card := h.fake.card
	card.Status = "running"
	card.Runner = "host"
	card.Params["host"] = "work-mac"
	claim := Claim{Approval: card, LeaseID: "leased"}
	h.srv.runMachineClaim(context.Background(), claim, "another-mac", testBearer)
	if h.commandsRun() != 0 || h.fake.result == nil || h.fake.result.Started == nil || *h.fake.result.Started {
		t.Fatal("wrong machine did not refuse the claim")
	}
	h.srv.runMachineClaim(context.Background(), claim, "work-mac", testBearer)
	if h.commandsRun() != 1 || h.fake.result == nil || h.fake.result.LeaseID != "leased" {
		t.Fatal("matching machine did not execute and report its claim")
	}
}

func TestMachineChildUsesAccountHomeWithNoInheritedCredential(t *testing.T) {
	t.Setenv("OWNER_EXEC_HOST_TOKEN_FILE", "/secret-fixture")
	home := t.TempDir()
	out := runMachineCommand(context.Background(), "printf '%s/%s' \"$HOME\" \"${OWNER_EXEC_HOST_TOKEN_FILE-unset}\"", home, time.Second)
	if out.ExitCode == nil || *out.ExitCode != 0 || out.OutputTail != home+"/unset" {
		t.Fatalf("unexpected child result: %+v", out)
	}
}

// TD-578: run the same verifier, claim, command and result HTTP protocol an
// enrolled machine uses. The two credentials and every command are fixtures.
func TestMachineTicketHTTPRoundTrip(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "prod")
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.yaml")
	data := "tokens:\n" +
		"  - {name: admin, role: admin, token: fixture-admin-token}\n" +
		"  - {name: operator, role: operator, token: fixture-operator-token}\n" +
		"  - {name: host-work-mac, role: executor, token: fixture-work-mac-token}\n" +
		"  - {name: host-other, role: executor, token: fixture-other-token}\n"
	if err := os.WriteFile(authFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authFile)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("t", 32))
	api := approvals.New(filepath.Join(dir, "approvals"), nil)
	api.SetTicketKey(key)
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/capabilities", func(w http.ResponseWriter, req *http.Request) {
			admin := bearerOf(req) == "fixture-admin-token"
			writeJSON(w, 200, map[string]bool{"authenticated": admin, "can_admin": admin})
		})
		approvals.Mount(r, auth, api)
	})
	s := httptest.NewTLSServer(router)
	defer s.Close()
	p := NewHTTPPlatform(s.URL)
	p.Client = s.Client()
	ctx := context.Background()
	plan := map[string]any{"action": Action, "reason": "x\n改了什么：测试\n批了以后谁因此能做什么新事：没有",
		"params": map[string]any{"command": testCommand, "reason": "fixture", "host": "work-mac"}}
	var card Card
	if err := p.do(ctx, "fixture-operator-token", http.MethodPost, "/api/v1/approvals", plan, &card); err != nil {
		t.Fatal(err)
	}
	if got, err := p.claimHost(ctx, "fixture-work-mac-token", "work-mac"); err != nil || got.LeaseID != "" {
		t.Fatalf("a card without code was claimable: %v", err)
	}
	var shown struct {
		ApprovalLine string `json:"approval_line"`
	}
	if err := p.do(ctx, "fixture-admin-token", http.MethodGet, "/api/v1/approvals/"+card.ID, nil, &shown); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := newHarness(t, "pending")
	h.now = now
	h.srv.Verifier = verifierAt(now)
	h.srv.Platform, h.srv.TicketKey = p, key
	req := h.body(h.code())
	req["id"], req["host"], req["params_hash"] = card.ID, "work-mac", card.ParamsHash
	req["approve"] = map[string]any{"channel": "console", "approval_line": shown.ApprovalLine,
		"params_hash": card.ParamsHash, "confirm_number": card.Number}
	if rec := h.post("fixture-admin-token", req); rec.Code != http.StatusAccepted {
		t.Fatalf("code did not enqueue the ticket: %d %s", rec.Code, rec.Body)
	}
	if got, err := p.claimHost(ctx, "fixture-other-token", "other"); err != nil || got.LeaseID != "" {
		t.Fatalf("a different machine claimed the card: %v", err)
	}
	got, err := p.claimHost(ctx, "fixture-work-mac-token", "work-mac")
	if err != nil || got.Approval.ID != card.ID || got.LeaseID == "" {
		t.Fatalf("matching machine did not receive the ticket: %v", err)
	}
	// This time run the actual shell function; the server logs only status.
	h.srv.Run = func(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome {
		return runMachineCommand(ctx, command, dir, timeout, secrets...)
	}
	h.srv.runMachineClaim(ctx, got, "work-mac", "fixture-work-mac-token")
	finished, err := p.Card(ctx, "fixture-admin-token", card.ID)
	if err != nil || finished.Status != "executed" {
		t.Fatalf("the command did not complete: %q, %v", finished.Status, err)
	}
	got, err = p.claimHost(ctx, "fixture-work-mac-token", "work-mac")
	if err != nil || got.LeaseID != "" {
		t.Fatalf("machine reused the ticket: %v", err)
	}
}
