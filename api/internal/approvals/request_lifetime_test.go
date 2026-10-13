package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Run the hook after the decision is persisted, before Update returns to
// approveWith. This exercises disconnects at the commit/execution boundary.
type decisionHookBackend struct {
	*casBackend
	afterDecision func()
}

func (b *decisionHookBackend) Update(ctx context.Context, key string, mutate func([]byte) ([]byte, error)) error {
	if err := b.casBackend.Update(ctx, key, mutate); err != nil {
		return err
	}
	if key == "approvals" && b.afterDecision != nil {
		raw, err := b.Read(ctx, key)
		if err != nil {
			return err
		}
		doc, err := parse(raw)
		if err != nil {
			return err
		}
		for _, a := range doc.Approvals {
			if a.Status == StatusRunning {
				hook := b.afterDecision
				b.afterDecision = nil
				hook()
				break
			}
		}
	}
	return nil
}

func lifetimeService(t *testing.T) (*Service, *decisionHookBackend) {
	t.Helper()
	root := t.TempDir()
	b := &decisionHookBackend{casBackend: newCAS()}
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	s := New(filepath.Join(root, "approvals"), actuation.NewAuditLog(filepath.Join(root, "audit.json")))
	t.Cleanup(s.notices.Wait)
	return s, b
}

func lifetimeApproval(t *testing.T, s *Service) Approval {
	t.Helper()
	c := s.create(context.Background(), "request-lifetime", "trigger_cnpg_backup", "backup", "", nil)
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	return c.Approval
}

func assertLifetimeResult(t *testing.T, s *Service, a Approval, out decided, status string, calls int32) {
	t.Helper()
	stored, ok := s.find(a.ID)
	if !ok || out.Status != http.StatusOK || out.Body["status"] != status || stored.Status != status || calls != 1 || stored.Execution.Attempts != 1 {
		t.Fatalf("result = %d %v stored=%+v calls=%d", out.Status, out.Body, stored, calls)
	}
	// Disconnects and final outcomes must not leave work eligible for retries.
	s.retryDue(context.Background())
	if again := s.approve(context.Background(), a.ID, "phone"); again.Status != http.StatusConflict {
		t.Fatalf("second decision = %d %v", again.Status, again.Body)
	}
}

func TestApprovalDisconnectAfterDecision(t *testing.T) {
	s, b := lifetimeService(t)
	a := lifetimeApproval(t, s)
	type requestKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), requestKey{}, "kept"))
	defer cancel()
	b.afterDecision = cancel
	var calls atomic.Int32
	actions.RegisterExecutor(a.Action, func(runCtx context.Context, _ map[string]any) (any, error) {
		calls.Add(1)
		if ctx.Err() != context.Canceled {
			t.Error("request was not cancelled after commit")
		}
		if runCtx.Value(requestKey{}) != "kept" {
			t.Error("request value was lost")
		}
		stored, _ := s.find(a.ID)
		deadline, ok := runCtx.Deadline()
		if !ok || !deadline.Equal(stored.Execution.Deadline) {
			t.Errorf("deadline = %s (%v), stored = %s", deadline, ok, stored.Execution.Deadline)
		}
		attempt, ok := actions.CreateAttemptFrom(runCtx)
		hash, hashOK := actions.ParamsHashFrom(runCtx)
		if !ok || !hashOK || attempt.ApprovalID != a.ID || attempt.Attempt != 1 || hash != a.ParamsHash {
			t.Errorf("identity lost: attempt=%+v hash=%s", attempt, hash)
		}
		return map[string]any{"ok": true}, runCtx.Err()
	})
	t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
	out := s.approve(ctx, a.ID, "console")
	assertLifetimeResult(t, s, a, out, StatusExecuted, calls.Load())
	if calls.Load() != 1 {
		t.Fatal("retry executed the disconnected approval again")
	}
}

func TestApprovalDisconnectDuringExecution(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		err          error
	}{
		{"success", StatusExecuted, nil},
		{"failure", StatusFailed, errors.New("executor failed")},
		{"uncertain", StatusUnknown, actions.Uncertain("create outcome unknown")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := lifetimeService(t)
			a := lifetimeApproval(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			var calls atomic.Int32
			actions.RegisterExecutor(a.Action, func(runCtx context.Context, _ map[string]any) (any, error) {
				calls.Add(1)
				entered <- runCtx
				<-release
				if err := runCtx.Err(); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true}, tc.err
			})
			t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
			finished := make(chan decided, 1)
			go func() { finished <- s.approve(ctx, a.ID, "console") }()
			runCtx := <-entered
			cancel()
			if runCtx.Err() != nil {
				t.Errorf("execution inherited disconnect: %v", runCtx.Err())
			}
			select {
			case <-finished:
				t.Error("approve returned before the executor finished")
			default:
			}
			close(release)
			out := <-finished
			assertLifetimeResult(t, s, a, out, tc.status, calls.Load())
			if tc.err != nil && out.Body["error"] != tc.err.Error() {
				t.Errorf("error classification changed: %v", out.Body)
			}
			if calls.Load() != 1 {
				t.Fatal("retry executed the disconnected approval again")
			}
		})
	}
}

func TestApprovalExecutionDeadlineEndsContext(t *testing.T) {
	s, _ := lifetimeService(t)
	a := lifetimeApproval(t, s)
	// The persisted approval deadline is still clock()+platformDeadline;
	// shift the service clock so that deadline occurs shortly in real time.
	deadline := time.Now().Add(150 * time.Millisecond)
	s.SetClock(func() time.Time { return deadline.Add(-platformDeadline) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	actions.RegisterExecutor(a.Action, func(runCtx context.Context, _ map[string]any) (any, error) {
		calls.Add(1)
		stored, _ := s.find(a.ID)
		got, ok := runCtx.Deadline()
		if !ok || !got.Equal(stored.Execution.Deadline) || !got.Equal(deadline) {
			t.Errorf("execution deadline = %s (%v), stored = %s", got, ok, stored.Execution.Deadline)
		}
		select {
		case <-runCtx.Done():
			if runCtx.Err() != context.DeadlineExceeded {
				t.Errorf("execution ended with %v", runCtx.Err())
			}
			return nil, runCtx.Err()
		case <-time.After(time.Second):
			return nil, errors.New("execution deadline did not cancel context")
		}
	})
	t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
	out := s.approve(ctx, a.ID, "console")
	assertLifetimeResult(t, s, a, out, StatusFailed, calls.Load())
	if out.Body["error"] != context.DeadlineExceeded.Error() {
		t.Fatalf("deadline outcome = %v", out.Body)
	}
}

func TestApprovalPrecheckSurvivesDisconnect(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "tier-X"}[deny], func(t *testing.T) {
			s, b := lifetimeService(t)
			actions.SetActuationPolicy(&actuationpolicy.Policy{Apply: actuationpolicy.Apply{Namespaces: map[string]string{"test": "C"}}})
			checking := false
			checks := 0
			actions.SetPlanLookup(func(ctx context.Context, _ string) (actions.PlanSummary, error) {
				if checking {
					checks++
					if ctx.Err() != nil {
						t.Errorf("precheck inherited disconnect: %v", ctx.Err())
						return actions.PlanSummary{}, ctx.Err()
					}
				}
				return actions.PlanSummary{Ready: true, Namespaces: []string{"test"}, Daemon: checking && deny}, nil
			})
			t.Cleanup(func() {
				actions.SetPlanLookup(nil)
				actions.SetActuationPolicy(nil)
				actions.RegisterExecutor("apply_manifest", nil)
			})
			c := s.create(context.Background(), "s", "apply_manifest", "apply", "", map[string]any{"plan_id": "test-plan"})
			if c.Status != http.StatusCreated {
				t.Fatalf("create = %d %v", c.Status, c.Body)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.afterDecision = func() { checking = true; cancel() }
			calls := 0
			actions.RegisterExecutor("apply_manifest", func(context.Context, map[string]any) (any, error) {
				calls++
				return nil, nil
			})
			out := s.approve(ctx, c.Approval.ID, "console")
			want, wantCalls := StatusExecuted, 1
			if deny {
				want, wantCalls = StatusFailed, 0
			}
			stored, _ := s.find(c.Approval.ID)
			if out.Body["status"] != want || stored.Status != want || calls != wantCalls || checks != 1 {
				t.Fatalf("precheck = %v stored=%s calls=%d checks=%d", out.Body, stored.Status, calls, checks)
			}
		})
	}
}

func TestApprovalDisconnectKeepsLeaseDuringSynchronousExecution(t *testing.T) {
	s, _ := lifetimeService(t)
	other := New(s.store.path, nil)
	a := lifetimeApproval(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	actions.RegisterExecutor(a.Action, func(runCtx context.Context, _ map[string]any) (any, error) {
		calls.Add(1)
		cancel()
		stored, _ := s.find(a.ID)
		lease := stored.Execution.LeaseID
		// Advance beyond the original lease, renew as the unchanged heartbeat
		// does, then let a second worker try to claim and run due work.
		now := stored.Execution.LeaseExpiresAt.Add(time.Second)
		s.SetClock(func() time.Time { return now })
		other.SetClock(func() time.Time { return now })
		if !s.backgroundRenew(a.ID, lease) {
			t.Error("running lease did not renew")
		}
		renewed, _ := s.find(a.ID)
		if renewed.Status != StatusRunning || !renewed.Execution.LeaseExpiresAt.Equal(now.Add(leaseFor)) {
			t.Errorf("renewed lease = %+v", renewed)
		}
		other.retryDue(context.Background())
		if _, ok := other.claimInternal(a.ID); ok {
			t.Error("second worker claimed a synchronous execution")
		}
		return nil, runCtx.Err()
	})
	t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
	out := s.approve(ctx, a.ID, "console")
	assertLifetimeResult(t, s, a, out, StatusExecuted, calls.Load())
	if calls.Load() != 1 {
		t.Fatal("synchronous execution was run twice")
	}
}

type disconnectedWriter struct{ *httptest.ResponseRecorder }

func (w disconnectedWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestApprovalHandlerDisconnectStillAuditsExecution(t *testing.T) {
	for _, gone := range []bool{false, true} {
		t.Run(map[bool]string{false: "connected", true: "disconnected"}[gone], func(t *testing.T) {
			s, _ := lifetimeService(t)
			a := lifetimeApproval(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			actions.RegisterExecutor(a.Action, func(runCtx context.Context, _ map[string]any) (any, error) {
				calls.Add(1)
				if gone {
					cancel()
				}
				return nil, runCtx.Err()
			})
			t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
			body, err := json.Marshal(map[string]any{"channel": "console", "approval_line": CanonicalApprovalLine(a), "params_hash": a.ParamsHash})
			if err != nil {
				t.Fatal(err)
			}
			r := chi.NewRouter()
			r.Post("/approvals/{id}/approve", s.HandleApprove)
			req := httptest.NewRequest(http.MethodPost, "/approvals/"+a.ID+"/approve", strings.NewReader(string(body))).WithContext(ctx)
			response := httptest.NewRecorder()
			var writer http.ResponseWriter = response
			if gone {
				writer = disconnectedWriter{response}
			}
			r.ServeHTTP(writer, req)
			stored, _ := s.find(a.ID)
			if stored.Status != StatusExecuted || calls.Load() != 1 || response.Code != http.StatusOK {
				t.Fatalf("handler = %d stored=%s calls=%d", response.Code, stored.Status, calls.Load())
			}
			if !gone && !strings.Contains(response.Body.String(), `"status":"executed"`) {
				t.Fatalf("synchronous response = %s", response.Body.String())
			}
			audit := httptest.NewRecorder()
			s.audit.HandleList(audit, req)
			if !strings.Contains(audit.Body.String(), `"action":"approval.execute"`) || !strings.Contains(audit.Body.String(), `"status":"executed"`) {
				t.Fatalf("execution audit = %s", audit.Body.String())
			}
		})
	}
}
