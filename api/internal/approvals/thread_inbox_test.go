package approvals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	runtime "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func TestThreadInboxEveryTerminalStateRetriesInExistingLoops(t *testing.T) {
	t.Parallel()
	for _, status := range []string{StatusExecuted, StatusFailed, StatusRejected, StatusExpired, StatusUnknown} {
		for _, workers := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/workers=%t", status, workers), func(t *testing.T) {
				t.Parallel()
				s := inboxFixture(t, time.Now(), status, "local_approval")
				var attempts atomic.Int32
				mail := make(chan string, 4)
				s.SetThreadInbox(dedupeInboxSender(func(_ context.Context, key, to, body string) error {
					if attempts.Add(1) == 1 {
						return errors.New("temporary inbox outage")
					}
					mail <- body
					return nil
				}))
				transitionInboxFixture(t, s, status)
				s.RetryThreadInbox(context.Background())
				a, found, err := s.Lookup("appr_" + status)
				if err != nil || !found || a.Status != status || attempts.Load() != 1 {
					t.Fatalf("failed send changed decision: approval=%+v attempts=%d err=%v", a, attempts.Load(), err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				safego.Go("test-approval-thread-inbox", func() {
					defer close(done)
					if workers {
						s.RunRetries(ctx, time.Millisecond)
					} else {
						s.RunHeldFinishes(ctx, time.Millisecond)
					}
				})
				t.Cleanup(func() { cancel(); <-done })
				select {
				case body := <-mail:
					if !strings.Contains(body, "Conclusion: "+status) {
						t.Fatalf("wrong conclusion: %s", body)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("existing loop did not retry delivery")
				}
				cancel()
				<-done
				s.RetryThreadInbox(context.Background())
				if attempts.Load() != 2 || len(mail) != 0 {
					t.Fatal("conclusion was delivered again")
				}
			})
		}
	}
}

// Each persisted terminal state, including unknown, is delivered once by
// the real PG sender. Use the database's clock, not a fixed historical date.
func TestThreadInboxTerminalStatesPG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := runtime.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	w := runtime.Writer{Actor: "thread:local_approval", Token: "test"}
	if _, _, err := runtime.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register")); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{StatusExecuted, StatusFailed, StatusRejected, StatusExpired, StatusUnknown} {
		t.Run(status, func(t *testing.T) {
			// Serial: all subtests inspect the same thread mailbox.
			s := inboxFixture(t, now, status, "local_approval (TD-457)")
			transitionInboxFixture(t, s, status)
			s.SetThreadInbox(func(ctx context.Context, key, to, body string) error {
				return runtime.PostConclusion(ctx, pool, key, to, body)
			})
			s.RetryThreadInbox(ctx)
			s.RetryThreadInbox(ctx)
			// Restart, including the statefile service, after its first delivery.
			restarted := New(s.store.path, nil)
			restarted.SetThreadInbox(s.threadInbox)
			restarted.RetryThreadInbox(ctx)
			mail, err := runtime.Unread(ctx, pool, w, 100)
			if err != nil || len(mail) != 1 || !strings.Contains(mail[0].Body, "Conclusion: "+status) {
				t.Fatalf("mail=%+v err=%v", mail, err)
			}
			if _, err := runtime.AckMessages(ctx, pool, w, []string{mail[0].ID}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func inboxFixture(t *testing.T, now time.Time, state, thread string) *Service {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	s.SetClock(func() time.Time { return now })
	a := Approval{ID: "appr_" + state, Number: 7, Action: "cordon_node", Env: "stg", Status: StatusPending,
		RequesterThread: thread, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	if state == StatusExecuted || state == StatusFailed || state == StatusUnknown {
		a.Status = StatusRunning
		a.Execution = &Execution{LeaseID: "private-lease", StartedAt: now.Add(-time.Minute), LeaseExpiresAt: now.Add(time.Minute)}
	}
	if state == StatusExpired {
		a.ExpiresAt = now.Add(-time.Second)
	}
	if state == StatusUnknown {
		a.Execution.LeaseExpiresAt = now.Add(-unknownGrace - time.Second)
	}
	if err := s.store.update(func(doc *file) error { doc.put(a); return nil }); err != nil {
		t.Fatal(err)
	}
	return s
}

func transitionInboxFixture(t *testing.T, s *Service, state string) {
	t.Helper()
	switch state {
	case StatusExecuted, StatusFailed:
		exit := 0
		if state == StatusFailed {
			exit = 1
		}
		if r := s.result("appr_"+state, resultInput{LeaseID: "private-lease", ExitCode: &exit}); r.Status != 200 {
			t.Fatalf("result: %+v", r)
		}
	case StatusRejected:
		if r := s.reject("appr_"+state, "not now"); r.Status != 200 {
			t.Fatalf("reject: %+v", r)
		}
	case StatusExpired, StatusUnknown:
		if err := s.store.update(func(doc *file) error { sweep(doc, s.clock()); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreadInboxFailureRestartAndUnknownResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := inboxFixture(t, time.Now(), StatusUnknown, "thread:local_a")
	transitionInboxFixture(t, s, StatusUnknown)
	fail := func(context.Context, string, string, string) error { return errors.New("PG unavailable") }
	if err := s.retryThreadInbox(ctx, fail); err == nil {
		t.Fatal("failure not returned to background loop")
	}
	a, ok, err := s.Lookup("appr_unknown")
	if err != nil || !ok || a.Status != StatusUnknown {
		t.Fatalf("delivery failure changed result: %+v %v", a, err)
	}
	// Existing LateResult business evidence reconstructs unknown after restart.
	exit := 0
	if r := s.result(a.ID, resultInput{LeaseID: "private-lease", ExitCode: &exit}); r.Status != 200 {
		t.Fatalf("late result: %+v", r)
	}
	if r := s.result(a.ID, resultInput{LeaseID: "private-lease", ExitCode: &exit}); r.Status != 409 {
		t.Fatalf("repeated result: %+v", r)
	}
	restarted := New(s.store.path, nil)
	got := map[string]string{}
	var order []string
	send := dedupeInboxSender(func(_ context.Context, key, to, body string) error {
		if _, exists := got[key]; exists {
			t.Errorf("receipt failed to suppress %s", key)
		}
		got[key] = body
		order = append(order, key)
		return nil
	})
	for i := 0; i < 2; i++ {
		if err := restarted.retryThreadInbox(ctx, send); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || !strings.Contains(got["approval:appr_unknown:unknown"], "Conclusion: unknown") ||
		!strings.Contains(got["approval:appr_unknown:executed"], "Conclusion: executed") {
		t.Fatalf("conclusions=%v", got)
	}
	if order[0] != "approval:appr_unknown:unknown" {
		t.Fatalf("conclusions delivered out of order: %v", order)
	}
}

func TestThreadInboxNoRecipientAndSummary(t *testing.T) {
	t.Parallel()
	for _, thread := range []string{"", "a descriptive label", "local_a / local_b", "https://example.test/?next=local_a", "session-1 (do not notify thread:local_a)", "local_a"} {
		s := inboxFixture(t, time.Now(), StatusRejected, thread)
		transitionInboxFixture(t, s, StatusRejected)
		var calls int
		if err := s.retryThreadInbox(context.Background(), func(_ context.Context, key, to, body string) error {
			calls++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if (calls == 1) != (thread == "local_a") {
			t.Fatalf("thread=%q calls=%d", thread, calls)
		}
	}
	a := Approval{ID: "appr_safe", Number: 10, Action: "start_pipeline_run", Env: "stg", Status: StatusExecuted,
		Result: map[string]any{"grant_id": "grant_123", "run": map[string]any{"name": "ci-platform-123", "token": "never-copy"},
			"password": "never-copy", "output": "never-copy", "name": "password=never-copy"},
		Execution: &Execution{LeaseID: "never-copy", OutputTail: "never-copy"}}
	body := approvalConclusion(a)
	for _, want := range []string{"#10", "Action: start_pipeline_run", "Environment: stg", "grant_id=grant_123", "run.name=ci-platform-123", "#approvals?id=appr_safe"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "never-copy") {
		t.Fatal("unsafe result fields leaked")
	}
}

func TestThreadInboxResolutionReconstructsUnknown(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"succeeded", "failed", "not_run"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			s := inboxFixture(t, time.Now(), StatusUnknown, "local_review")
			transitionInboxFixture(t, s, StatusUnknown)
			if r := s.resolve("appr_unknown", outcome, "checked by owner"); r.Status != 200 {
				t.Fatal(r)
			}
			s = New(s.store.path, nil)
			mail := map[string]string{}
			send := dedupeInboxSender(func(_ context.Context, key, _, body string) error { mail[key] = body; return nil })
			for i := 0; i < 2; i++ {
				if err := s.retryThreadInbox(context.Background(), send); err != nil {
					t.Fatal(err)
				}
			}
			state := StatusFailed
			if outcome == "succeeded" {
				state = StatusExecuted
			}
			if len(mail) != 2 || !strings.Contains(mail["approval:appr_unknown:unknown"], "Conclusion: unknown") ||
				!strings.Contains(mail["approval:appr_unknown:"+state], "Conclusion: "+state) {
				t.Fatalf("business resolution lost conclusions: %v", mail)
			}
		})
	}
}

func TestThreadInboxPruningAndLegacyBackfill(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	// A legacy record has no queue. Its first background pass backfills it.
	raw := []byte(`{"approvals":[{"id":"appr_old","number":1,"status":"failed","requester_thread":"local_a"}]}`)
	if err := os.WriteFile(s.store.path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	send := func(context.Context, string, string, string) error { calls++; return nil }
	if err := s.retryThreadInbox(context.Background(), send); err != nil || calls != 1 {
		t.Fatalf("backfill calls=%d err=%v", calls, err)
	}
	// Queues for records that no longer exist are ignored. Business pruning
	// keeps exactly its original count and creates no auxiliary files.
	legacy := legacyInboxDocument{Inbox: map[string]legacyInboxEntry{"approval:gone:failed": {Record: "gone", To: "thread:local_a", Body: "failed"}, "done": {Record: "gone", Done: true}}}
	for i := 0; i < keepClosed+1; i++ {
		legacy.Approvals = append(legacy.Approvals, Approval{ID: fmt.Sprint(i), Status: StatusFailed})
	}
	b, err := legacy.marshalOnce()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.store.path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.store.update(func(*file) error { return nil }); err != nil {
		t.Fatal(err)
	}
	saved, err := s.store.read()
	if err != nil || len(saved.Approvals) != keepClosed {
		t.Fatalf("business pruning changed: %d %v", len(saved.Approvals), err)
	}
	if err := s.retryThreadInbox(context.Background(), send); err != nil || calls != 1 {
		t.Fatalf("pruned delivery calls=%d err=%v", calls, err)
	}
	b, err = os.ReadFile(s.store.path)
	if err != nil || strings.Contains(string(b), "thread_inbox") {
		t.Fatal("retired delivery fields survived")
	}
	files, err := os.ReadDir(filepath.Dir(s.store.path))
	if err != nil || len(files) != 2 || files[0].Name() != "approvals" || files[1].Name() != "approvals.lock" {
		t.Fatalf("auxiliary files created: %v %v", files, err)
	}
}
