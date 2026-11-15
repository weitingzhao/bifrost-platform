package mustrefuse_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/ownerexec"
)

// caseCount is how many cases the set has. Cases are added, with this number;
// a case is never dropped to make a change pass.
const caseCount = 10

// A made-up seed for the tests; it opens nothing.
var seed = []byte("test-seed-0123456789")

const (
	bearer  = "bearer-CANARY-7f3a9c21d4"
	command = "echo owner-exec-ok"
	cardID  = "apr_CARD-CANARY-1"
)

func shaOf(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

type fakePlatform struct {
	mu        sync.Mutex
	card      ownerexec.Card
	claimCard *ownerexec.Card
	approveEr error
	calls     []string
	result    *ownerexec.Result
}

func (f *fakePlatform) note(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakePlatform) called(n string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == n {
			return true
		}
	}
	return false
}
func (f *fakePlatform) IsAdmin(_ context.Context, b string) (bool, error) {
	f.note("admin")
	return b == bearer, nil
}
func (f *fakePlatform) Card(context.Context, string, string) (ownerexec.Card, error) {
	f.note("card")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.card, nil
}
func (f *fakePlatform) Approve(_ context.Context, _, _ string, _ json.RawMessage) error {
	f.note("approve")
	if f.approveEr != nil {
		return f.approveEr
	}
	f.mu.Lock()
	f.card.Status = "approved"
	f.mu.Unlock()
	return nil
}
func (f *fakePlatform) Claim(context.Context, string, string, string) (ownerexec.Claim, error) {
	f.note("claim")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.card.Status != "approved" {
		return ownerexec.Claim{}, &ownerexec.StatusError{Status: 409, Body: map[string]any{"error": "not claimable"}}
	}
	c := f.card
	if f.claimCard != nil {
		c = *f.claimCard
	}
	f.card.Status = "running"
	return ownerexec.Claim{Approval: c, LeaseID: "lease_1", HeartbeatSeconds: 20}, nil
}
func (f *fakePlatform) Heartbeat(context.Context, string, string, string) error { return nil }
func (f *fakePlatform) Result(_ context.Context, _, _ string, res ownerexec.Result) error {
	f.note("result")
	f.mu.Lock()
	f.result = &res
	f.mu.Unlock()
	return nil
}

type harness struct {
	fake *fakePlatform
	srv  *ownerexec.Server
	now  time.Time
	log  *bytes.Buffer
	mu   sync.Mutex
	ran  int
}

func newHarness(status string) *harness {
	h := &harness{now: time.Unix(1700000000, 0), log: &bytes.Buffer{}}
	h.fake = &fakePlatform{card: ownerexec.Card{ID: cardID, Action: ownerexec.Action, Status: status, Runner: "owner",
		Params: map[string]any{"command": command}}}
	var logMu sync.Mutex
	h.srv = &ownerexec.Server{
		Platform: h.fake, Verifier: ownerexec.NewVerifier(seed, h.now.Add(-ownerexec.Warmup)), ExecutorID: "owner-exec:test", Timeout: time.Minute,
		Now: func() time.Time { return h.now },
		Logf: func(f string, a ...any) {
			logMu.Lock()
			fmt.Fprintf(h.log, f+"\n", a...)
			logMu.Unlock()
		},
		Run: func(_ context.Context, _ string, _ time.Duration, _ ...string) ownerexec.Outcome {
			h.mu.Lock()
			h.ran++
			h.mu.Unlock()
			zero := 0
			return ownerexec.Outcome{ExitCode: &zero, OutputTail: "ok\n", OutputSHA: shaOf("ok\n")}
		},
	}
	return h
}

func (h *harness) code() string {
	return ownerexec.CodeAt(seed, h.now.Unix()/int64(ownerexec.Step/time.Second))
}

func (h *harness) commandsRun() int { h.mu.Lock(); defer h.mu.Unlock(); return h.ran }

func (h *harness) body(code string) map[string]any {
	return map[string]any{"id": cardID, "command_sha256": shaOf(command), "code": code,
		"approve": map[string]any{"channel": "console", "approval_line": "LINE", "params_hash": "HASH"}}
}

func (h *harness) post(b string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, ownerexec.RunPath, bytes.NewReader(raw))
	if b != "" {
		req.Header.Set("Authorization", "Bearer "+b)
	}
	rec := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(rec, req)
	h.srv.Wait()
	return rec
}

type mustRefuse struct {
	name, origin string
	run          func() string // "" when the runner refused as it must
}

var cases = []mustRefuse{
	{"a wrong code approves and runs nothing", "execution card: the one-time code is the Owner's act", func() string {
		h := newHarness("pending")
		wrong := "000000"
		if wrong == h.code() {
			wrong = "000001"
		}
		rec := h.post(bearer, h.body(wrong))
		if rec.Code != http.StatusForbidden || h.fake.called("approve") || h.fake.called("claim") || h.commandsRun() != 0 || h.fake.card.Status != "pending" {
			return fmt.Sprintf("status %d calls %v", rec.Code, h.fake.calls)
		}
		return ""
	}},
	{"the same code twice is refused", "a code replayed seconds later must not run a second card", func() string {
		h := newHarness("approved")
		code := h.code()
		if rec := h.post(bearer, h.body(code)); rec.Code != http.StatusAccepted {
			return fmt.Sprintf("first run = %d", rec.Code)
		}
		h.fake.mu.Lock()
		h.fake.card.Status = "approved"
		h.fake.calls = nil
		h.fake.mu.Unlock()
		rec := h.post(bearer, h.body(code))
		if rec.Code != http.StatusForbidden || h.fake.called("claim") || h.commandsRun() != 1 {
			return fmt.Sprintf("replay = %d, ran %d", rec.Code, h.commandsRun())
		}
		return ""
	}},
	{"a card whose command is not the one shown runs nothing", "the Owner approves the text the page showed; its fingerprint must match", func() string {
		h := newHarness("pending")
		body := h.body(h.code())
		body["command_sha256"] = shaOf("echo something else")
		if rec := h.post(bearer, body); rec.Code != http.StatusConflict || h.fake.called("approve") || h.commandsRun() != 0 {
			return fmt.Sprintf("status %d, ran %d", rec.Code, h.commandsRun())
		}
		return ""
	}},
	{"a command changed between read and claim is handed back, not run", "the claimed text is hashed again", func() string {
		h := newHarness("approved")
		swapped := h.fake.card
		swapped.Params = map[string]any{"command": "echo swapped"}
		h.fake.claimCard = &swapped
		rec := h.post(bearer, h.body(h.code()))
		if rec.Code != http.StatusConflict || h.commandsRun() != 0 || h.fake.result == nil || h.fake.result.Started == nil || *h.fake.result.Started {
			return fmt.Sprintf("status %d, ran %d, result %+v", rec.Code, h.commandsRun(), h.fake.result)
		}
		return ""
	}},
	{"a card that is not approvable or not the runner's runs nothing", "only an owner_run_command for runner owner, pending or approved", func() string {
		for _, status := range []string{"rejected", "expired", "executed", "running", "failed", "unknown"} {
			h := newHarness(status)
			if rec := h.post(bearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
				return fmt.Sprintf("%s: status %d, ran %d", status, rec.Code, h.commandsRun())
			}
		}
		h := newHarness("pending")
		h.fake.card.Action = "scale_deployment"
		if rec := h.post(bearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
			return fmt.Sprintf("another action: status %d", rec.Code)
		}
		h = newHarness("pending")
		h.fake.card.Runner = "host"
		if rec := h.post(bearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
			return fmt.Sprintf("runner host: status %d", rec.Code)
		}
		return ""
	}},
	{"an approval the platform refuses runs nothing", "approve is the platform's decision, not the runner's", func() string {
		h := newHarness("pending")
		h.fake.approveEr = &ownerexec.StatusError{Status: 409, Body: map[string]any{"error": "approval_line does not match"}}
		if rec := h.post(bearer, h.body(h.code())); rec.Code != http.StatusConflict || h.commandsRun() != 0 {
			return fmt.Sprintf("status %d, ran %d", rec.Code, h.commandsRun())
		}
		return ""
	}},
	{"a caller without the admin bearer gets 401 and spends no code attempts", "a non-admin must neither guess codes nor lock the Owner out", func() string {
		h := newHarness("pending")
		for i := 0; i < 20; i++ {
			for _, b := range []string{"", "some-operator-token"} {
				if rec := h.post(b, h.body("000000")); rec.Code != http.StatusUnauthorized {
					return fmt.Sprintf("bearer %q: status %d", b, rec.Code)
				}
			}
		}
		if rec := h.post(bearer, h.body(h.code())); rec.Code != http.StatusAccepted {
			return fmt.Sprintf("the Owner was locked out: %d", rec.Code)
		}
		return ""
	}},
	{"the admin bearer without a code runs nothing", "the bearer alone is not the Owner's act", func() string {
		h := newHarness("approved")
		if rec := h.post(bearer, h.body("")); rec.Code != http.StatusForbidden || h.commandsRun() != 0 {
			return fmt.Sprintf("status %d, ran %d", rec.Code, h.commandsRun())
		}
		return ""
	}},
	{"the log never holds the bearer or the code", "the runner is handed secrets for one request and must not write them down", func() string {
		h := newHarness("approved")
		code := h.code()
		if rec := h.post(bearer, h.body(code)); rec.Code != http.StatusAccepted {
			return fmt.Sprintf("run = %d", rec.Code)
		}
		out := h.log.String()
		if !strings.Contains(out, cardID) { // positive control: the scan reads what is logged
			return "the log scan found nothing it should: " + out
		}
		if strings.Contains(out, bearer) || strings.Contains(out, code) {
			return "the log holds the bearer or the code"
		}
		return ""
	}},
	{"test mode does not start with a cluster identity or a real seed mounted", "a test seed must never appear where real approvals run", func() string {
		dir, err := os.MkdirTemp("", "mustrefuse")
		if err != nil {
			return err.Error()
		}
		defer os.RemoveAll(dir)
		token, seedFile := filepath.Join(dir, "token"), filepath.Join(dir, "seed")
		logf := func(string, ...any) {}
		if _, err = ownerexec.LoadSeed(ownerexec.SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf); err != nil {
			return "clean test mode refused: " + err.Error() // positive control
		}
		if err = os.WriteFile(token, []byte("x"), 0o600); err != nil {
			return err.Error()
		}
		if _, err = ownerexec.LoadSeed(ownerexec.SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf); err == nil {
			return "test mode started with a cluster identity mounted"
		}
		_ = os.Remove(token)
		if err = os.WriteFile(seedFile, []byte(ownerexec.EncodeSeed(seed)), 0o600); err != nil {
			return err.Error()
		}
		if _, err = ownerexec.LoadSeed(ownerexec.SeedConfig{TestMode: true, TokenFile: token, SeedFile: seedFile}, logf); err == nil {
			return "test mode started with a real seed mounted"
		}
		return ""
	}},
}

func TestTheRunnerStillRefuses(t *testing.T) {
	if len(cases) != caseCount {
		t.Fatalf("the set has %d cases and says %d: add cases with the number, never drop one to make a change pass", len(cases), caseCount)
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] || c.origin == "" {
			t.Fatalf("case %q is repeated or has no origin", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			if why := c.run(); why != "" {
				t.Fatalf("the runner no longer refuses (%s): %s", c.origin, why)
			}
		})
	}
}
