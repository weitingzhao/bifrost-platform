package approvals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	runtime "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestThreadInboxRetainedSourcesAndLateResultPG(t *testing.T) {
	t.Parallel()
	for _, state := range []string{StatusExecuted, StatusFailed} {
		for _, deliveredUnknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unknown_delivered=%t", state, deliveredUnknown), func(t *testing.T) {
				t.Parallel()
				retainedSourcesAndLateResultPG(t, state, deliveredUnknown)
			})
		}
	}
}

func retainedSourcesAndLateResultPG(t *testing.T, state string, deliveredUnknown bool) {
	t.Helper()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := runtime.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	w := runtime.Writer{Actor: "thread:local_approval", Token: "source-test"}
	if _, _, err := runtime.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register")); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	s := inboxFixture(t, now, StatusUnknown, "local_approval")
	transitionInboxFixture(t, s, StatusUnknown)
	if err := s.store.update(func(doc *file) error {
		for i := 0; i < 65; i++ {
			doc.put(Approval{ID: fmt.Sprintf("appr_source_%02d", i), Number: i + 10, Status: StatusFailed, RequesterThread: "local_approval"})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.retryThreadInbox(ctx, func(context.Context, string, string, string) error { return errors.New("PG offline") }); err == nil {
		t.Fatal("outage did not leave sources pending")
	}
	if deliveredUnknown {
		if err := s.retryThreadInbox(ctx, func(ctx context.Context, key, to, body string) error {
			return runtime.PostConclusion(ctx, pool, key, to, body)
		}); err != nil {
			t.Fatal(err)
		}
	}
	exit := 0
	if state == StatusFailed {
		exit = 1
	}
	if r := s.result("appr_unknown", resultInput{LeaseID: "private-lease", ExitCode: &exit}); r.Status != 200 {
		t.Fatalf("late result=%+v", r)
	}
	if err := s.retryThreadInbox(ctx, func(ctx context.Context, key, to, body string) error {
		if err := runtime.PostConclusion(ctx, pool, key, to, body); err != nil {
			return err
		}
		return errors.New("lost reply after real PG commit")
	}); err == nil {
		t.Fatal("lost commit reply did not exercise replay")
	}
	restarted := New(s.store.path, nil)
	send := func(ctx context.Context, key, to, body string) error {
		return runtime.PostConclusion(ctx, pool, key, to, body)
	}
	for i := 0; i < 2; i++ {
		if err := restarted.retryThreadInbox(ctx, send); err != nil {
			t.Fatal(err)
		}
	}
	mail, err := runtime.Unread(ctx, pool, w, 100)
	if err != nil || len(mail) != 67 {
		t.Fatalf("recovered mailbox=%d err=%v", len(mail), err)
	}
	var ids []string
	unknown, executed := -1, -1
	for i, m := range mail {
		ids = append(ids, m.ID)
		if strings.Contains(m.Body, "appr_unknown") {
			if strings.Contains(m.Body, "Conclusion: unknown") {
				unknown = i
			}
			if strings.Contains(m.Body, "Conclusion: "+state) {
				executed = i
			}
		}
	}
	if unknown < 0 || executed <= unknown {
		t.Fatalf("superseded conclusion missing/out of order: unknown=%d executed=%d", unknown, executed)
	}
	if _, err := runtime.AckMessages(ctx, pool, w, ids); err != nil {
		t.Fatal(err)
	}
	// The unchanged business record is always replayable; PG receipts suppress
	// acknowledged messages even after every service object is recreated.
	restarted = New(s.store.path, nil)
	if err := restarted.retryThreadInbox(ctx, send); err != nil {
		t.Fatal(err)
	}
	history, err := runtime.Thread(ctx, pool, w.Actor, "controller", 100)
	if err != nil || len(history) != 67 {
		t.Fatalf("history=%d err=%v", len(history), err)
	}
	for _, m := range history {
		if m.AckedAt == nil {
			t.Fatal("replay replaced an acknowledged message")
		}
	}
}
