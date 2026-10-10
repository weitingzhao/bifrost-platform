package approvals

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	// platformDeadline: a platform action still refused (transiently) this long
	// after approval expires.
	platformDeadline = time.Hour
	// handoffDeadline: owner and host actions must be claimed within a day,
	// the window owner-run.sh always allowed.
	handoffDeadline = 24 * time.Hour

	leaseFor       = 60 * time.Second
	heartbeatEvery = 20 * time.Second
	// unknownGrace: a running record whose lease lapsed this long ago is unknown.
	unknownGrace = 10 * time.Minute

	retryBase = 30 * time.Second
	retryMax  = 5 * time.Minute

	maxTail    = 2048
	maxErrText = 300
	maxWait    = 25 * time.Second
)

func deadlineFor(runner string) time.Duration {
	if runner == actions.RunnerPlatform {
		return platformDeadline
	}
	return handoffDeadline
}

func backoff(attempts int) time.Duration {
	d := retryBase
	for i := 1; i < attempts && d < retryMax; i++ {
		d *= 2
	}
	if d > retryMax {
		d = retryMax
	}
	return d
}

// claimLocked moves an approved record to running under a new lease. Callers
// run it inside store.update, so two claimers cannot both win.
func (s *Service) claimLocked(a *Approval, executor string, now time.Time) string {
	if a.Execution == nil {
		a.Execution = &Execution{Deadline: now.Add(deadlineFor(a.Runner))}
	}
	e := a.Execution
	lease := "lease_" + randomHex(12)
	e.Attempts++
	e.ExecutorID = executor
	e.LeaseID = lease
	e.LeaseExpiresAt = now.Add(leaseFor)
	e.ClaimedAt = now
	e.StartedAt = now
	e.NextAttemptAt = time.Time{}
	a.Status = StatusRunning
	return lease
}

// precheck refuses to start a record whose stored params no longer hash to
// params_hash, or that classifies as tier X now (D10). "" means go.
func precheck(ctx context.Context, a Approval) string {
	hash, err := actions.ParamsHash(a.Params)
	if err != nil || hash != a.ParamsHash {
		return "params hash mismatch at execution"
	}
	act, ok := actions.ByID(a.Action)
	if !ok {
		return "unknown action"
	}
	if act.TierOf(ctx, a.Params) == actions.TierX {
		return "action classifies as tier X at execution (D10); not run"
	}
	return ""
}

// runPlatform executes a record this process claimed and stores the outcome.
// The approve handler audits its own call; the retry loop records the events.
func (s *Service) runPlatform(ctx context.Context, rec Approval) (Approval, []event) {
	lease := rec.Execution.LeaseID
	started := s.clock()
	var result any
	var execErr error
	if msg := precheck(ctx, rec); msg != "" {
		execErr = fmt.Errorf("%s", msg)
	} else {
		stop := make(chan struct{})
		done := make(chan struct{})
		safego.Go("approvals.lease", func() {
			defer close(done)
			t := time.NewTicker(heartbeatEvery)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					_, _ = s.renew(rec.ID, lease)
				}
			}
		})
		result, execErr = actions.Execute(ctx, rec.Action, rec.Params)
		close(stop)
		<-done
	}
	return s.finishPlatform(rec.ID, lease, started, result, execErr)
}

func (s *Service) finishPlatform(id, lease string, started time.Time, result any, execErr error) (Approval, []event) {
	now := s.clock()
	var out Approval
	var evs []event
	err := s.store.update(func(doc *file) error {
		evs = nil
		a, ok := doc.find(id)
		if !ok {
			return errNoChange
		}
		out = a
		e := a.Execution
		if e == nil || e.LeaseID != lease {
			return errNoChange
		}
		switch a.Status {
		case StatusRunning:
			if execErr != nil && actions.IsTransient(execErr) {
				requeue(&a, oneLine(execErr.Error()), now)
				evs = append(evs, event{"approval.queue", a.ID, StatusApproved,
					fmt.Sprintf("attempt=%d refusal=%s", e.Attempts, e.LastRefusal)})
				break
			}
			finishWith(&a, started, now, result, execErr)
			evs = append(evs, event{"approval.execute", a.ID, a.Status, executeDetail(a)})
		case StatusUnknown:
			if e.LateResult {
				return errNoChange
			}
			e.LateResult = true
			finishWith(&a, started, now, result, execErr)
			evs = append(evs, event{"approval.late_result", a.ID, a.Status, executeDetail(a)})
		default:
			return errNoChange
		}
		doc.put(a)
		out = a
		return nil
	})
	if err != nil {
		out.Error = "store approvals: " + err.Error()
	}
	return out, evs
}

func requeue(a *Approval, refusal string, now time.Time) {
	e := a.Execution
	e.LastRefusal = refusal
	e.NextAttemptAt = now.Add(backoff(e.Attempts))
	e.ExecutorID = ""
	e.LeaseID = ""
	e.LeaseExpiresAt = time.Time{}
	e.ClaimedAt = time.Time{}
	e.StartedAt = time.Time{}
	a.Status = StatusApproved
	a.Result = nil
	a.Error = ""
}

func finishWith(a *Approval, started, now time.Time, result any, execErr error) {
	e := a.Execution
	e.FinishedAt = now
	e.DurationMS = now.Sub(started).Milliseconds()
	e.LeaseExpiresAt = time.Time{}
	if execErr != nil {
		a.Status = StatusFailed
		a.Error = execErr.Error()
		a.Result = nil
		e.Error = oneLine(a.Error)
		return
	}
	a.Status = StatusExecuted
	a.Result = result
	a.Error = ""
}

func executeDetail(a Approval) string {
	if a.Status == StatusExecuted {
		return StatusExecuted
	}
	return a.Error
}

// renew extends a lease. ok is false when the record is no longer running
// under that lease.
func (s *Service) renew(id, lease string) (Approval, bool) {
	now := s.clock()
	var out Approval
	renewed := false
	_ = s.store.update(func(doc *file) error {
		renewed = false
		a, ok := doc.find(id)
		if !ok {
			return errNoChange
		}
		out = a
		if a.Status != StatusRunning || a.Execution == nil || a.Execution.LeaseID != lease {
			return errNoChange
		}
		a.Execution.LeaseExpiresAt = now.Add(leaseFor)
		doc.put(a)
		out = a
		renewed = true
		return nil
	})
	return out, renewed
}

// RunRetries retries platform approvals left approved by a transient refusal,
// and applies the time-based transitions, every interval until ctx ends.
func (s *Service) RunRetries(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.retryDue(ctx)
		}
	}
}

func (s *Service) retryDue(ctx context.Context) {
	doc, err := s.sweptDoc()
	if err != nil {
		return
	}
	now := s.clock()
	for _, a := range doc.Approvals {
		if !retryable(a, now) {
			continue
		}
		rec, ok := s.claimInternal(a.ID)
		if !ok {
			continue
		}
		_, evs := s.runPlatform(ctx, rec)
		s.record(nil, evs)
	}
}

func retryable(a Approval, now time.Time) bool {
	return a.Status == StatusApproved && a.Runner == actions.RunnerPlatform &&
		a.Execution != nil && !now.Before(a.Execution.NextAttemptAt)
}

func (s *Service) claimInternal(id string) (Approval, bool) {
	now := s.clock()
	var out Approval
	claimed := false
	err := s.store.update(func(doc *file) error {
		claimed = false
		a, ok := doc.find(id)
		if !ok || !retryable(a, now) {
			return errNoChange
		}
		if !a.Execution.Deadline.IsZero() && now.After(a.Execution.Deadline) {
			return errNoChange
		}
		s.claimLocked(&a, s.runnerID, now)
		doc.put(a)
		out = a
		claimed = true
		return nil
	})
	return out, err == nil && claimed
}

// claimInput is POST /approvals/claim.
type claimInput struct {
	ExecutorID  string   `json:"executor_id"`
	ID          string   `json:"id"`
	Runners     []string `json:"runners"`
	Actions     []string `json:"actions"`
	WaitSeconds int      `json:"wait_seconds"`
}

// claimRunners is what each role may claim: the executor token only host
// work; the Owner's admin token owner work, and host work as the fallback.
func claimRunners(role actuation.Role, asked []string) map[string]bool {
	allowed := map[string]bool{}
	switch role {
	case actuation.RoleExecutor:
		allowed[actions.RunnerHost] = true
	case actuation.RoleAdmin:
		allowed[actions.RunnerOwner] = true
		allowed[actions.RunnerHost] = true
	}
	if len(asked) == 0 {
		return allowed
	}
	out := map[string]bool{}
	for _, r := range asked {
		if allowed[strings.TrimSpace(r)] {
			out[strings.TrimSpace(r)] = true
		}
	}
	return out
}

func (s *Service) claim(ctx context.Context, role actuation.Role, in claimInput) decided {
	executor := strings.TrimSpace(in.ExecutorID)
	if executor == "" || len(executor) > 80 || strings.ContainsAny(executor, "\r\n") {
		return decided{Status: 400, Body: map[string]any{"error": "executor_id is required (one line, at most 80 bytes)"}}
	}
	runners := claimRunners(role, in.Runners)
	if len(runners) == 0 {
		return decided{Status: 403, Body: map[string]any{"error": "this token cannot claim those runners"}}
	}
	wantActions := map[string]bool{}
	for _, a := range in.Actions {
		wantActions[strings.TrimSpace(a)] = true
	}
	wait := time.Duration(in.WaitSeconds) * time.Second
	if wait > maxWait {
		wait = maxWait
	}
	until := time.Now().Add(wait)
	for {
		out := s.claimOnce(ctx, executor, strings.TrimSpace(in.ID), runners, wantActions)
		if out.Status != 204 || in.ID != "" || !time.Now().Before(until) {
			return out
		}
		select {
		case <-ctx.Done():
			return out
		case <-time.After(time.Second):
		}
	}
}

func (s *Service) claimOnce(ctx context.Context, executor, ref string, runners, wantActions map[string]bool) decided {
	now := s.clock()
	var rec Approval
	var refusal *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		refusal, rec = nil, Approval{}
		evs = sweep(doc, now)
		changed := len(evs) > 0
		candidate := func(a Approval) bool {
			return a.Status == StatusApproved && runners[a.Runner] &&
				(len(wantActions) == 0 || wantActions[a.Action]) &&
				a.Execution != nil && !now.Before(a.Execution.NextAttemptAt)
		}
		var pick *Approval
		if ref != "" {
			found, ok := doc.find(ref)
			if !ok {
				refusal = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
				return keepSwept(evs)
			}
			if !candidate(found) {
				refusal = &decided{Status: 409, Body: map[string]any{"error": "not claimable", "status": found.Status, "runner": found.Runner}}
				return keepSwept(evs)
			}
			pick = &found
		} else {
			for i := range doc.Approvals {
				a := doc.Approvals[i]
				if !candidate(a) {
					continue
				}
				if msg := precheck(ctx, a); msg != "" {
					a.Status, a.Error = StatusFailed, msg
					doc.put(a)
					changed = true
					evs = append(evs, event{"approval.execute", a.ID, StatusFailed, msg})
					continue
				}
				if pick == nil || a.DecidedAt.Before(pick.DecidedAt) {
					c := a
					pick = &c
				}
			}
		}
		if pick == nil {
			refusal = &decided{Status: 204}
			if changed {
				return nil
			}
			return errNoChange
		}
		if msg := precheck(ctx, *pick); msg != "" {
			pick.Status, pick.Error = StatusFailed, msg
			doc.put(*pick)
			evs = append(evs, event{"approval.execute", pick.ID, StatusFailed, msg})
			refusal = &decided{Status: 409, Body: map[string]any{"error": msg, "status": StatusFailed}}
			return nil
		}
		s.claimLocked(pick, executor, now)
		doc.put(*pick)
		rec = *pick
		evs = append(evs, event{"approval.claim", rec.ID, StatusRunning,
			fmt.Sprintf("executor=%s runner=%s attempt=%d", executor, rec.Runner, rec.Execution.Attempts)})
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	if refusal != nil {
		refusal.events = evs
		return *refusal
	}
	return decided{Status: 200, events: evs, Body: map[string]any{
		"approval":          rec.public(),
		"lease_id":          rec.Execution.LeaseID,
		"lease_expires_at":  rec.Execution.LeaseExpiresAt,
		"lease_seconds":     int(leaseFor / time.Second),
		"heartbeat_seconds": int(heartbeatEvery / time.Second),
	}}
}

func (s *Service) heartbeat(ref, lease string) decided {
	lease = strings.TrimSpace(lease)
	if lease == "" {
		return decided{Status: 400, Body: map[string]any{"error": "lease_id is required"}}
	}
	doc, err := s.store.read()
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "load approvals: " + err.Error()}}
	}
	found, ok := doc.find(strings.TrimSpace(ref))
	if !ok {
		return decided{Status: 404, Body: map[string]any{"error": "not found"}}
	}
	rec, renewed := s.renew(found.ID, lease)
	if !renewed {
		msg := "lease mismatch"
		if rec.Status == StatusUnknown {
			msg = "lease lapsed; post the result"
		}
		return decided{Status: 409, Body: map[string]any{"error": msg, "status": rec.Status}}
	}
	return decided{Status: 200, Body: map[string]any{"status": rec.Status, "lease_expires_at": rec.Execution.LeaseExpiresAt}}
}

// resultInput is POST /approvals/{id}/result. Refusal with started=false is a
// transient refusal before anything ran: the record goes back to approved.
type resultInput struct {
	LeaseID      string `json:"lease_id"`
	Started      *bool  `json:"started"`
	Refusal      string `json:"refusal"`
	ExitCode     *int   `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	OutputSHA256 string `json:"output_sha256"`
	OutputTail   string `json:"output_tail"`
	Error        string `json:"error"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Service) result(ref string, in resultInput) decided {
	lease := strings.TrimSpace(in.LeaseID)
	if lease == "" {
		return decided{Status: 400, Body: map[string]any{"error": "lease_id is required"}}
	}
	started := in.Started == nil || *in.Started
	refusal := oneLine(in.Refusal)
	if started && in.ExitCode == nil && strings.TrimSpace(in.Error) == "" {
		return decided{Status: 400, Body: map[string]any{"error": "exit_code or error is required"}}
	}
	if !started && refusal == "" {
		return decided{Status: 400, Body: map[string]any{"error": "refusal is required when started is false"}}
	}
	sum := strings.ToLower(strings.TrimSpace(in.OutputSHA256))
	if sum != "" && !sha256Hex.MatchString(sum) {
		return decided{Status: 400, Body: map[string]any{"error": "output_sha256 must be 64 hex characters"}}
	}
	if in.DurationMS < 0 {
		return decided{Status: 400, Body: map[string]any{"error": "duration_ms must not be negative"}}
	}
	now := s.clock()
	var rec Approval
	var bad *decided
	var evs []event
	err := s.store.update(func(doc *file) error {
		bad, evs = nil, nil
		a, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			bad = &decided{Status: 404, Body: map[string]any{"error": "not found"}}
			return errNoChange
		}
		e := a.Execution
		if e == nil || e.LeaseID != lease {
			bad = &decided{Status: 409, Body: map[string]any{"error": "lease mismatch", "status": a.Status}}
			return errNoChange
		}
		late := false
		switch a.Status {
		case StatusRunning:
		case StatusUnknown:
			if e.LateResult {
				bad = &decided{Status: 409, Body: map[string]any{"error": "result already accepted", "status": a.Status}}
				return errNoChange
			}
			late = true
		default:
			bad = &decided{Status: 409, Body: map[string]any{"error": "result already accepted", "status": a.Status}}
			return errNoChange
		}
		if !started {
			if late {
				bad = &decided{Status: 409, Body: map[string]any{"error": "lease lapsed; a refusal cannot requeue an unknown run", "status": a.Status}}
				return errNoChange
			}
			requeue(&a, refusal, now)
			evs = append(evs, event{"approval.queue", a.ID, StatusApproved, fmt.Sprintf("attempt=%d refusal=%s", e.Attempts, refusal)})
			doc.put(a)
			rec = a
			return nil
		}
		e.FinishedAt = now
		e.DurationMS = in.DurationMS
		if e.DurationMS == 0 && !e.StartedAt.IsZero() {
			e.DurationMS = now.Sub(e.StartedAt).Milliseconds()
		}
		e.ExitCode = in.ExitCode
		e.OutputSHA256 = sum
		e.OutputTail = Redact(tail(in.OutputTail, maxTail))
		e.Error = oneLine(in.Error)
		e.LeaseExpiresAt = time.Time{}
		e.LateResult = late
		if in.ExitCode != nil && *in.ExitCode == 0 && e.Error == "" {
			a.Status = StatusExecuted
			a.Error = ""
		} else {
			a.Status = StatusFailed
			a.Error = e.Error
			if a.Error == "" {
				a.Error = "exit " + strconv.Itoa(*in.ExitCode)
			}
		}
		name := "approval.run.finish"
		if late {
			name = "approval.late_result"
		}
		evs = append(evs, event{name, a.ID, a.Status, finishDetail(a)})
		doc.put(a)
		rec = a
		return nil
	})
	if err != nil {
		return decided{Status: 500, Body: map[string]any{"error": "store approvals: " + err.Error()}}
	}
	if bad != nil {
		return *bad
	}
	return decided{Status: 200, events: evs, Body: map[string]any{"id": rec.ID, "status": rec.Status, "late_result": rec.Execution.LateResult}}
}

func finishDetail(a Approval) string {
	e := a.Execution
	exit := "none"
	if e.ExitCode != nil {
		exit = strconv.Itoa(*e.ExitCode)
	}
	d := fmt.Sprintf("exit=%s duration_ms=%d executor=%s", exit, e.DurationMS, e.ExecutorID)
	if e.OutputSHA256 != "" {
		d += " output_sha256=" + e.OutputSHA256
	}
	if e.Error != "" {
		d += " error=" + e.Error
	}
	return d
}

// tail keeps the last n bytes on a UTF-8 boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && (s[i]&0xC0) == 0x80 {
		i++
	}
	return s[i:]
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > maxErrText {
		s = s[:maxErrText]
	}
	return s
}

var redactions = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`), "[redacted private key]"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 [redacted]"},
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)[A-Za-z0-9_.-]*)(["']?\s*[:=]\s*["']?)[^\s"',;]+`), "$1$2[redacted]"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[redacted jwt]"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`), "[redacted aws key]"},
	{regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,})`), "[redacted token]"},
	{regexp.MustCompile(`://([^/\s:@]+):([^@\s/]+)@`), "://$1:[redacted]@"},
}

// Redact masks common secret shapes in executor output. It is a backstop:
// the command itself should not print secrets.
func Redact(s string) string {
	for _, r := range redactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
