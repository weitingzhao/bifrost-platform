package ownerexec

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Action is the only action this runner executes.
const Action = "owner_run_command"

// RunPath is the one route the Console calls. The code and the bearer travel
// in the body and a header, never in the path: every proxy in front logs paths.
const (
	RunPath    = "/api/owner-exec/run"
	HealthPath = "/api/owner-exec/health"
)

// Server is the runner's HTTP surface.
type Server struct {
	Platform Platform
	Verifier *Verifier
	// ExecutorID is recorded on the claim, e.g. "owner-exec:<pod>".
	ExecutorID string
	Timeout    time.Duration
	// Logf must never be handed a request body, a bearer, or a code.
	Logf func(format string, args ...any)
	// Run executes a command; tests replace it.
	Run func(ctx context.Context, command string, timeout time.Duration, secrets ...string) Outcome
	Now func() time.Time

	// egressOpen is set when the start-up probe reached an address the
	// NetworkPolicy must block. The runner then refuses every request.
	egressOpen atomic.Bool
	wg         sync.WaitGroup
}

type runRequest struct {
	ID            string          `json:"id"`
	CommandSHA256 string          `json:"command_sha256"`
	Code          string          `json:"code"`
	Approve       json.RawMessage `json:"approve"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+RunPath, s.handleRun)
	mux.HandleFunc("GET "+HealthPath, s.handleHealth)
	return mux
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if s.egressOpen.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "egress policy is not enforced"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRun is the whole protocol. Order matters:
//
//  1. admin bearer, checked with platform-api. A caller without one learns
//     nothing and cannot spend wrong-code attempts, so it can neither guess
//     the code nor lock the Owner out.
//  2. the card: an owner_run_command for runner owner, pending or approved,
//     whose command hashes to the fingerprint the page showed. Checked before
//     the code so a stale page does not burn one.
//  3. the one-time code. A wrong code stops here: nothing is approved.
//  4. approve (if still pending), with the body the page built.
//  5. claim. platform-api re-hashes the params and re-classifies; tier X is refused there.
//  6. the claimed command is hashed again. A mismatch hands the card back.
//  7. run, heartbeat, post the result.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if s.egressOpen.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "egress policy is not enforced; refusing to run"})
		return
	}
	bearer := bearerOf(r)
	if bearer == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "admin token required"})
		return
	}
	var req runRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		// The decode error can quote the body. It is not passed on or logged.
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body is not valid JSON"})
		return
	}
	id := strings.TrimSpace(req.ID)
	want := strings.ToLower(strings.TrimSpace(req.CommandSHA256))
	if id == "" || len(want) != 64 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id and command_sha256 are required"})
		return
	}
	ctx := r.Context()

	admin, err := s.Platform.IsAdmin(ctx, bearer)
	if err != nil {
		s.Logf("run %s: admin check failed: %v", id, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "platform-api did not answer the sign-in check"})
		return
	}
	if !admin {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "admin token required"})
		return
	}

	card, err := s.Platform.Card(ctx, bearer, id)
	if err != nil {
		s.relay(w, id, "read card", err)
		return
	}
	if msg := cardRefusal(card, want); msg != "" {
		s.Logf("run %s: refused: %s", id, msg)
		writeJSON(w, http.StatusConflict, map[string]any{"error": msg, "status": card.Status})
		return
	}

	if err = s.Verifier.Verify(req.Code, s.now()); err != nil {
		s.Logf("run %s: code refused: %v", id, err)
		status := http.StatusForbidden
		switch {
		case errors.Is(err, ErrLocked):
			status = http.StatusTooManyRequests
		case errors.Is(err, ErrWarmup):
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	if card.Status == "pending" {
		if len(req.Approve) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "approve body is required for a pending card"})
			return
		}
		if err = s.Platform.Approve(ctx, bearer, id, req.Approve); err != nil {
			s.relay(w, id, "approve", err)
			return
		}
		s.Logf("run %s: approved", id)
	}

	claim, err := s.Platform.Claim(ctx, bearer, id, s.ExecutorID)
	if err != nil {
		s.relay(w, id, "claim", err)
		return
	}
	command := claim.Approval.Command()
	if msg := cardRefusal(Card{Action: claim.Approval.Action, Runner: claim.Approval.Runner, Status: "approved", Params: claim.Approval.Params}, want); msg != "" {
		no := false
		_ = s.Platform.Result(ctx, bearer, id, Result{LeaseID: claim.LeaseID, Started: &no, Refusal: "owner-exec: " + msg})
		s.Logf("run %s: claimed card refused: %s", id, msg)
		writeJSON(w, http.StatusConflict, map[string]any{"error": msg})
		return
	}

	s.Logf("run %s: started (command sha256 %s)", id, want)
	every := time.Duration(claim.HeartbeatSeconds) * time.Second
	if every <= 0 {
		every = 20 * time.Second
	}
	s.wg.Add(1)
	lease, code := claim.LeaseID, req.Code
	safego.Go("ownerexec.execute", func() { s.execute(id, lease, command, bearer, code, every) })
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "running", "id": id})
}

// execute outlives the request. It holds the bearer only until the result is
// posted; the bearer is what lets it renew the lease and write the result.
func (s *Server) execute(id, lease, command, bearer, code string, every time.Duration) {
	defer s.wg.Done()
	ctx := context.Background()
	stop := make(chan struct{})
	done := make(chan struct{})
	safego.Go("ownerexec.heartbeat", func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := s.Platform.Heartbeat(ctx, bearer, id, lease); err != nil {
					s.Logf("run %s: heartbeat: %v", id, err)
				}
			}
		}
	})
	run := s.Run
	if run == nil {
		run = RunCommand
	}
	defer func() { close(stop); <-done }()
	out := run(ctx, command, s.Timeout, bearer, code)

	// No definitive result exists after a lost shell reply. Stop renewing and
	// let the existing approval lease transition to unknown; never requeue
	// or report a failure that could invite repeating an executed command.
	if out.Err == shellUnanswered || out.Err == shellUnreadable {
		s.Logf("run %s: shell outcome unknown; lease will expire", id)
		return
	}

	res := Result{
		LeaseID:      lease,
		ExitCode:     out.ExitCode,
		DurationMS:   out.Duration.Milliseconds(),
		OutputSHA256: out.OutputSHA,
		OutputTail:   out.OutputTail,
		Error:        out.Err,
	}
	// Keep the known result and the lease alive until delivery succeeds.
	// Retry only the result, never the command. Permanent refusals also keep
	// Wait pending: returning successfully would silently discard evidence.
	delay := 2 * time.Second
	for {
		if err := s.Platform.Result(ctx, bearer, id, res); resultDelivered(err) {
			break
		}
		s.Logf("run %s: result delivery pending; retrying in %s", id, delay)
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay += 2 * time.Second
		}
	}

	s.Logf("run %s: finished in %s", id, out.Duration.Round(time.Millisecond))
}

// Wait waits for execution and acknowledged result delivery. Failed delivery
// is retained in memory and retried, including during graceful shutdown.
// Forced process termination can still lose pending results. Unknown shell outcomes stop
// renewal instead of posting a definitive result, leaving lease expiry to
// mark the card unknown.
func (s *Server) Wait() { s.wg.Wait() }

// cardRefusal says why a card cannot run, or "".
func cardRefusal(c Card, wantSHA string) string {
	if c.Action != Action {
		return "this card is not an owner_run_command"
	}
	if c.Runner != "" && c.Runner != "owner" {
		return "this card is for runner " + c.Runner
	}
	if c.Status != "pending" && c.Status != "approved" {
		return "this card is " + c.Status + "; nothing to run"
	}
	command := c.Command()
	if command == "" {
		return "this card has no command"
	}
	sum := sha256.Sum256([]byte(command))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(wantSHA)) != 1 {
		return "the command on the card is not the one this page showed; reload and read it again"
	}
	return ""
}

// relay passes a platform-api refusal to the page as it came.
func (s *Server) relay(w http.ResponseWriter, id, step string, err error) {
	var se *StatusError
	if errors.As(err, &se) {
		s.Logf("run %s: %s refused by platform-api: %d", id, step, se.Status)
		body := se.Body
		if body == nil {
			body = map[string]any{}
		}
		if _, ok := body["error"]; !ok {
			body["error"] = step + " refused"
		}
		writeJSON(w, se.Status, body)
		return
	}
	s.Logf("run %s: %s failed: %v", id, step, err)
	writeJSON(w, http.StatusBadGateway, map[string]any{"error": "platform-api did not answer (" + step + ")"})
}

// ProbeEgress dials an address the NetworkPolicy must block. If the dial
// connects, the policy is not in force and the runner stops serving.
// kube-router needs about 15 seconds to program a new Pod, so the caller
// waits before probing.
func (s *Server) ProbeEgress(addr string, timeout time.Duration) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		s.Logf("egress probe: %s is blocked, as it must be", addr)
		return
	}
	_ = conn.Close()
	s.egressOpen.Store(true)
	s.Logf("egress probe: %s is reachable; the NetworkPolicy is not enforced; refusing to run", addr)
}

func bearerOf(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Only this conflict acknowledges a result whose first reply may have been lost.
func resultDelivered(err error) bool {
	var status *StatusError
	return err == nil || (errors.As(err, &status) && status.Status == http.StatusConflict && status.Body["error"] == "result already accepted")
}
