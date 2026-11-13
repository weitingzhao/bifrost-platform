package ownerexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 rows, truncated to 6 digits.
func TestCodeAtMatchesRFC6238(t *testing.T) {
	seed := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got := CodeAt(seed, unix/30); got != want {
			t.Errorf("t=%d: got %s, want %s", unix, got, want)
		}
	}
}

func TestSeedRoundTrip(t *testing.T) {
	seed := []byte("12345678901234567890")
	got, err := DecodeSeed(strings.ToLower(EncodeSeed(seed)[:16]) + " " + EncodeSeed(seed)[16:] + "\n")
	if err != nil || !bytes.Equal(got, seed) {
		t.Fatalf("round trip: %v %q", err, got)
	}
	if _, err := DecodeSeed("MFRGG"); err == nil {
		t.Fatal("a short seed was accepted")
	}
}

var testSeed = []byte("12345678901234567890")

func verifierAt(now time.Time) *Verifier { return NewVerifier(testSeed, now.Add(-Warmup)) }

func TestVerifyAcceptsOnceThenRefusesTheSameCode(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := verifierAt(now)
	code := CodeAt(testSeed, stepOf(now))
	if err := v.Verify(code, now); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := v.Verify(code, now.Add(2*time.Second)); !errors.Is(err, ErrReplayed) {
		t.Fatalf("second use: got %v, want ErrReplayed", err)
	}
	// An older step never passes after a newer one, even inside the window.
	if err := v.Verify(CodeAt(testSeed, stepOf(now)-1), now); !errors.Is(err, ErrReplayed) {
		t.Fatalf("older step: got %v, want ErrReplayed", err)
	}
	// The next step's code is fresh.
	next := now.Add(Step)
	if err := v.Verify(CodeAt(testSeed, stepOf(next)), next); err != nil {
		t.Fatalf("next step: %v", err)
	}
}

func TestVerifyWindowIsOneStepEachSide(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for delta, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		err := verifierAt(now).Verify(CodeAt(testSeed, stepOf(now)+delta), now)
		if ok && err != nil || !ok && !errors.Is(err, ErrBadCode) {
			t.Errorf("delta %d: got %v, accepted should be %v", delta, err, ok)
		}
	}
}

func TestVerifyRefusesDuringWarmup(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := NewVerifier(testSeed, now.Add(-Warmup+time.Second))
	if err := v.Verify(CodeAt(testSeed, stepOf(now)), now); !errors.Is(err, ErrWarmup) {
		t.Fatalf("got %v, want ErrWarmup", err)
	}
	// Refused in warm-up means not consumed: the same code passes once warm.
	later := now.Add(2 * time.Second)
	if err := v.Verify(CodeAt(testSeed, stepOf(now)), later); err != nil {
		t.Fatalf("after warm-up: %v", err)
	}
}

func TestVerifyLocksAfterFiveWrongCodes(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := verifierAt(now)
	good := CodeAt(testSeed, stepOf(now))
	bad := "000000"
	if bad == good {
		bad = "000001"
	}
	for i := 0; i < maxFailures; i++ {
		if err := v.Verify(bad, now); !errors.Is(err, ErrBadCode) {
			t.Fatalf("wrong code %d: %v", i, err)
		}
	}
	if err := v.Verify(good, now); !errors.Is(err, ErrLocked) {
		t.Fatalf("right code while locked: got %v, want ErrLocked", err)
	}
	after := now.Add(lockFor)
	if err := v.Verify(CodeAt(testSeed, stepOf(after)), after); err != nil {
		t.Fatalf("after the lock: %v", err)
	}
}

func TestVerifyRefusesMalformedCodes(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, code := range []string{"", "12345", "1234567", "abcdef"} {
		if err := verifierAt(now).Verify(code, now); !errors.Is(err, ErrBadCode) {
			t.Errorf("%q: got %v", code, err)
		}
	}
}

func TestTestSeedRefusedWithClusterIdentityOrSeed(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	seedFile := filepath.Join(dir, "seed")
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }

	// Nothing mounted: allowed, and the seed is printed for the tester.
	seed, err := LoadSeed(SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf)
	if err != nil || len(seed) != 20 {
		t.Fatalf("clean test mode: %v", err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "otpauth://totp/") || !strings.Contains(logged[0], EncodeSeed(seed)) {
		t.Fatalf("test seed was not logged as an otpauth link: %q", logged)
	}

	if err = os.WriteFile(token, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSeed(SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf); err == nil {
		t.Fatal("test mode started with a cluster identity mounted")
	}
	_ = os.Remove(token)

	if err = os.WriteFile(seedFile, []byte(EncodeSeed(testSeed)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadSeed(SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf); err == nil {
		t.Fatal("test mode started with a real seed mounted")
	}

	// Normal mode reads the file and logs nothing.
	logged = nil
	got, err := LoadSeed(SeedConfig{SeedFile: seedFile, TokenFile: token}, logf)
	if err != nil || !bytes.Equal(got, testSeed) || len(logged) != 0 {
		t.Fatalf("normal mode: %v, logged %q", err, logged)
	}
	if _, err = LoadSeed(SeedConfig{SeedFile: filepath.Join(dir, "absent")}, logf); err == nil {
		t.Fatal("a missing seed file was accepted")
	}
}

// fakePlatform is platform-api as the runner sees it.
type fakePlatform struct {
	mu          sync.Mutex
	adminBearer string
	card        Card
	claimCard   *Card // when set, what claim returns instead of card
	approveErr  error
	calls       []string
	result      *Result
	approveBody string
}

func (f *fakePlatform) note(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *fakePlatform) IsAdmin(_ context.Context, bearer string) (bool, error) {
	f.note("admin")
	return bearer == f.adminBearer, nil
}
func (f *fakePlatform) Card(context.Context, string, string) (Card, error) {
	f.note("card")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.card, nil
}
func (f *fakePlatform) Approve(_ context.Context, _, _ string, body json.RawMessage) error {
	f.note("approve")
	if f.approveErr != nil {
		return f.approveErr
	}
	f.mu.Lock()
	f.card.Status = "approved"
	f.approveBody = string(body)
	f.mu.Unlock()
	return nil
}
func (f *fakePlatform) Claim(context.Context, string, string, string) (Claim, error) {
	f.note("claim")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.card.Status != "approved" {
		return Claim{}, &StatusError{Status: 409, Body: map[string]any{"error": "not claimable"}}
	}
	c := f.card
	if f.claimCard != nil {
		c = *f.claimCard
	}
	f.card.Status = "running"
	return Claim{Approval: c, LeaseID: "lease_1", HeartbeatSeconds: 20}, nil
}
func (f *fakePlatform) Heartbeat(context.Context, string, string, string) error { return nil }
func (f *fakePlatform) Result(_ context.Context, _, _ string, res Result) error {
	f.note("result")
	f.mu.Lock()
	f.result = &res
	f.mu.Unlock()
	return nil
}

func (f *fakePlatform) called(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

const (
	testBearer  = "bearer-CANARY-7f3a9c21d4"
	testCommand = "echo owner-exec-ok"
)

func shaOf(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

type harness struct {
	t    *testing.T
	fake *fakePlatform
	srv  *Server
	now  time.Time
	log  *bytes.Buffer
	ran  []string
	mu   sync.Mutex
}

func newHarness(t *testing.T, status string) *harness {
	h := &harness{t: t, now: time.Unix(1700000000, 0), log: &bytes.Buffer{}}
	h.fake = &fakePlatform{adminBearer: testBearer, card: Card{
		ID: "apr_CARD-CANARY-1", Action: Action, Status: status, Runner: "owner",
		Params: map[string]any{"command": testCommand},
	}}
	var logMu sync.Mutex
	h.srv = &Server{
		Platform: h.fake, Verifier: verifierAt(h.now), ExecutorID: "owner-exec:test", Timeout: time.Minute,
		Now: func() time.Time { return h.now },
		Logf: func(f string, a ...any) {
			logMu.Lock()
			fmt.Fprintf(h.log, f+"\n", a...)
			logMu.Unlock()
		},
		Run: func(_ context.Context, command string, _ time.Duration, _ ...string) Outcome {
			h.mu.Lock()
			h.ran = append(h.ran, command)
			h.mu.Unlock()
			zero := 0
			return Outcome{ExitCode: &zero, OutputTail: "owner-exec-ok\n", OutputSHA: shaOf("owner-exec-ok\n")}
		},
	}
	return h
}

func (h *harness) code() string { return CodeAt(testSeed, stepOf(h.now)) }

func (h *harness) post(bearer string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, RunPath, bytes.NewReader(raw))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(rec, req)
	h.srv.Wait()
	return rec
}

func (h *harness) body(code string) map[string]any {
	return map[string]any{
		"id": "apr_CARD-CANARY-1", "command_sha256": shaOf(testCommand), "code": code,
		"approve": map[string]any{"channel": "console", "approval_line": "LINE", "params_hash": "HASH"},
	}
}

func (h *harness) commandsRun() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.ran) }

func TestRunApprovesClaimsRunsAndStoresTheResult(t *testing.T) {
	h := newHarness(t, "pending")
	rec := h.post(testBearer, h.body(h.code()))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if h.commandsRun() != 1 || h.ran[0] != testCommand {
		t.Fatalf("ran %q", h.ran)
	}
	if got := strings.Join(h.fake.calls, ","); got != "admin,card,approve,claim,result" {
		t.Fatalf("call order: %s", got)
	}
	if !strings.Contains(h.fake.approveBody, `"approval_line":"LINE"`) {
		t.Fatalf("the page's approve body was not passed on as it came: %s", h.fake.approveBody)
	}
	if h.fake.result == nil || h.fake.result.ExitCode == nil || *h.fake.result.ExitCode != 0 || h.fake.result.LeaseID != "lease_1" {
		t.Fatalf("result: %+v", h.fake.result)
	}
}

func TestRunAnApprovedCardSkipsApprove(t *testing.T) {
	h := newHarness(t, "approved")
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if h.fake.called("approve") || h.commandsRun() != 1 {
		t.Fatalf("calls %v, ran %d", h.fake.calls, h.commandsRun())
	}
}

// The three conditions: approved card, matching fingerprint, valid code.
// Each test below removes one and checks nothing was approved or run.

func TestWrongCodeApprovesNothing(t *testing.T) {
	h := newHarness(t, "pending")
	wrong := "000000"
	if wrong == h.code() {
		wrong = "000001"
	}
	rec := h.post(testBearer, h.body(wrong))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if h.fake.called("approve") || h.fake.called("claim") || h.commandsRun() != 0 || h.fake.card.Status != "pending" {
		t.Fatalf("a wrong code got past the check: calls %v", h.fake.calls)
	}
}

func TestSameCodeTwiceIsRefused(t *testing.T) {
	h := newHarness(t, "approved")
	code := h.code()
	if rec := h.post(testBearer, h.body(code)); rec.Code != http.StatusAccepted {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	// A second approved card, same code, seconds later.
	h.fake.mu.Lock()
	h.fake.card.Status = "approved"
	h.fake.calls = nil
	h.fake.mu.Unlock()
	rec := h.post(testBearer, h.body(code))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "already used") {
		t.Fatalf("second: %d %s", rec.Code, rec.Body)
	}
	if h.fake.called("claim") || h.commandsRun() != 1 {
		t.Fatalf("the replayed code ran something: calls %v", h.fake.calls)
	}
}

func TestFingerprintMismatchRunsNothingAndKeepsTheCode(t *testing.T) {
	h := newHarness(t, "pending")
	body := h.body(h.code())
	body["command_sha256"] = shaOf("echo something else")
	rec := h.post(testBearer, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if h.fake.called("approve") || h.commandsRun() != 0 {
		t.Fatalf("calls %v", h.fake.calls)
	}
	// The code was not consumed by the refused request.
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusAccepted {
		t.Fatalf("the code was burned by a stale page: %d %s", rec.Code, rec.Body)
	}
}

func TestCommandChangedBetweenReadAndClaimIsHandedBack(t *testing.T) {
	h := newHarness(t, "approved")
	swapped := h.fake.card
	swapped.Params = map[string]any{"command": "echo swapped"}
	h.fake.claimCard = &swapped
	rec := h.post(testBearer, h.body(h.code()))
	if rec.Code != http.StatusConflict || h.commandsRun() != 0 {
		t.Fatalf("status %d, ran %d", rec.Code, h.commandsRun())
	}
	if h.fake.result == nil || h.fake.result.Started == nil || *h.fake.result.Started || h.fake.result.Refusal == "" {
		t.Fatalf("the claim was not handed back as not started: %+v", h.fake.result)
	}
}

func TestNotApprovableCardRunsNothing(t *testing.T) {
	for _, status := range []string{"rejected", "expired", "executed", "running", "failed", "unknown"} {
		h := newHarness(t, status)
		if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
			t.Errorf("%s: status %d, ran %d", status, rec.Code, h.commandsRun())
		}
	}
	h := newHarness(t, "pending")
	h.fake.card.Action = "scale_deployment"
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
		t.Errorf("another action: status %d", rec.Code)
	}
	h = newHarness(t, "pending")
	h.fake.card.Runner = "host"
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
		t.Errorf("runner host: status %d", rec.Code)
	}
}

func TestApproveRefusedByPlatformRunsNothing(t *testing.T) {
	h := newHarness(t, "pending")
	h.fake.approveErr = &StatusError{Status: 409, Body: map[string]any{"error": "approval_line does not match"}}
	rec := h.post(testBearer, h.body(h.code()))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "approval_line") || h.commandsRun() != 0 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

// A caller without the admin bearer gets 401 before the code is looked at,
// so it can neither guess codes nor lock the Owner out.
func TestNonAdminCannotSpendCodeAttempts(t *testing.T) {
	h := newHarness(t, "pending")
	for i := 0; i < maxFailures+3; i++ {
		for _, bearer := range []string{"", "some-operator-token"} {
			if rec := h.post(bearer, h.body("000000")); rec.Code != http.StatusUnauthorized {
				t.Fatalf("bearer %q: status %d", bearer, rec.Code)
			}
		}
	}
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusAccepted {
		t.Fatalf("the Owner was locked out by non-admin attempts: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminBearerAloneRunsNothing(t *testing.T) {
	h := newHarness(t, "approved")
	body := h.body("")
	if rec := h.post(testBearer, body); rec.Code != http.StatusForbidden || h.commandsRun() != 0 {
		t.Fatalf("status %d, ran %d", rec.Code, h.commandsRun())
	}
}

// The log scan, with its positive control: the card id is logged on purpose
// and must be found, which proves the scan reads what the runner logs. The
// bearer and the code must not be found, in any of the forms checked.
func TestLogsNeverHoldTheBearerOrTheCode(t *testing.T) {
	h := newHarness(t, "pending")
	wrong := "135790"
	if wrong == h.code() {
		wrong = "135791"
	}
	good := h.code()
	h.post(testBearer, h.body(wrong))                           // wrong code
	h.post(testBearer, map[string]any{"id": "x", "code": good}) // malformed
	h.post("not-admin-"+testBearer, h.body(good))               // not admin
	h.post(testBearer, h.body(good))                            // full run
	raw := httptest.NewRequest(http.MethodPost, RunPath, strings.NewReader(`{"code":"`+good+`", broken`))
	raw.Header.Set("Authorization", "Bearer "+testBearer)
	h.srv.Handler().ServeHTTP(httptest.NewRecorder(), raw) // bad JSON that quotes the code

	logs := h.log.String()
	if !strings.Contains(logs, "apr_CARD-CANARY-1") {
		t.Fatalf("positive control failed: the card id is not in the log, so this scan proves nothing:\n%s", logs)
	}
	for name, secret := range map[string]string{"bearer": testBearer, "code": good, "wrong code": wrong} {
		for form, v := range map[string]string{
			"plain": secret, "hex": hex.EncodeToString([]byte(secret)),
		} {
			if strings.Contains(logs, v) {
				t.Errorf("%s (%s) is in the log:\n%s", name, form, logs)
			}
		}
	}
}

func TestEgressProbeStopsTheRunnerWhenTheAddressIsReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	h := newHarness(t, "approved")
	h.srv.ProbeEgress(ln.Addr().String(), time.Second)
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusServiceUnavailable || h.commandsRun() != 0 {
		t.Fatalf("status %d, ran %d", rec.Code, h.commandsRun())
	}

	closed := ln.Addr().String()
	_ = ln.Close()
	h = newHarness(t, "approved")
	h.srv.ProbeEgress(closed, time.Second)
	if rec := h.post(testBearer, h.body(h.code())); rec.Code != http.StatusAccepted {
		t.Fatalf("a blocked address stopped the runner: %d", rec.Code)
	}
}

func TestRunCommandCapturesExitTailAndScrubsSecrets(t *testing.T) {
	t.Setenv("OWNER_EXEC_PARENT_ONLY", "parent-value")
	out := RunCommand(context.Background(), `echo "tok=secret-token-123"; echo "env=${OWNER_EXEC_PARENT_ONLY:-unset}"; pwd; exit 3`, time.Minute, "secret-token-123")
	if out.ExitCode == nil || *out.ExitCode != 3 {
		t.Fatalf("exit: %+v", out)
	}
	if strings.Contains(out.OutputTail, "secret-token-123") || !strings.Contains(out.OutputTail, "[removed]") {
		t.Fatalf("secret not scrubbed: %q", out.OutputTail)
	}
	if !strings.Contains(out.OutputTail, "env=unset") {
		t.Fatalf("the child saw the runner's environment: %q", out.OutputTail)
	}
	if len(out.OutputSHA) != 64 {
		t.Fatalf("sha: %q", out.OutputSHA)
	}
}

func TestRunCommandKillsOnTimeoutAndKeepsOnlyTheTail(t *testing.T) {
	out := RunCommand(context.Background(), `sleep 30`, 300*time.Millisecond)
	if out.ExitCode != nil || !strings.Contains(out.Err, "timed out") {
		t.Fatalf("timeout: %+v", out)
	}
	out = RunCommand(context.Background(), `head -c 100000 /dev/zero | tr '\0' 'a'; echo END`, time.Minute)
	if len(out.OutputTail) > tailSend || !strings.HasSuffix(out.OutputTail, "END\n") {
		t.Fatalf("tail: %d bytes", len(out.OutputTail))
	}
}

func TestHTTPPlatformSendsTheBearerAndReadsRefusals(t *testing.T) {
	var gotAuth, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		switch r.URL.Path {
		case "/api/v1/auth/capabilities":
			_, _ = w.Write([]byte(`{"authenticated":true,"can_admin":true}`))
		case "/api/v1/approvals/apr_1":
			_, _ = w.Write([]byte(`{"id":"apr_1","action":"owner_run_command","status":"pending","params":{"command":"echo hi"}}`))
		default:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"not claimable"}`))
		}
	}))
	defer ts.Close()
	p := NewHTTPPlatform(ts.URL + "/")
	ok, err := p.IsAdmin(context.Background(), "tok")
	if err != nil || !ok || gotAuth != "Bearer tok" {
		t.Fatalf("admin: %v %v %q", ok, err, gotAuth)
	}
	card, err := p.Card(context.Background(), "tok", "apr_1")
	if err != nil || card.Command() != "echo hi" || card.Status != "pending" {
		t.Fatalf("card: %+v %v", card, err)
	}
	_, err = p.Claim(context.Background(), "tok", "apr_1", "x")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 409 || gotPath != "/api/v1/approvals/claim" {
		t.Fatalf("claim: %v", err)
	}
	if strings.Contains(err.Error(), "tok") && !strings.Contains("not claimable", "tok") {
		t.Fatalf("the error text holds the bearer: %v", err)
	}
}
