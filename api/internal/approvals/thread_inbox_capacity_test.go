package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThreadInboxAllowedResultValueIsRedacted(t *testing.T) {
	t.Parallel()
	fake := "ghp_" + strings.Repeat("A", 24)
	if !resultIdentifier.MatchString(fake) {
		t.Fatal("fixture must reach the value redactor, not fail the shape check")
	}
	for _, key := range []string{"grant_id", "run_name", "name", "namespace", "status", "revision"} {
		if got := inboxResult(map[string]any{key: fake}); got != "" {
			t.Errorf("secret-shaped value entered allowed field %s", key)
		}
		if got := inboxResult(map[string]any{key: "ci-platform-123"}); got != key+"=ci-platform-123" {
			t.Errorf("ordinary identifier control for %s: %q", key, got)
		}
	}
	if got := inboxResult(map[string]any{"run": map[string]any{"name": fake, "namespace": "cicd"}}); got != "run.namespace=cicd" {
		t.Fatal("nested allowlist did not redact its value or removed the safe control")
	}
}

func TestThreadInboxScanCannotPruneBusiness(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	doc := file{Approvals: []Approval{{ID: "appr_receipt", Number: 1, Status: StatusRejected, RequesterThread: "local_review"}}, extra: map[string]json.RawMessage{"padding": json.RawMessage(`""`)}}
	raw, err := doc.marshalOnce()
	if err != nil {
		t.Fatal(err)
	}
	doc.extra["padding"], err = json.Marshal(strings.Repeat("x", maxDocBytes-len(raw)-1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = doc.marshalOnce()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.store.path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var body string
		if err := s.retryThreadInbox(context.Background(), func(_ context.Context, _, _ string, text string) error { body = text; return nil }); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "Conclusion: rejected") {
			t.Fatal("scan did not reconstruct the retained source")
		}
		saved, found, err := s.Lookup("appr_receipt")
		if err != nil || !found || saved.Status != StatusRejected {
			t.Fatal("scan pruned the business record")
		}
		stored, err := os.ReadFile(s.store.path)
		if err != nil || len(stored) > maxDocBytes {
			t.Fatalf("budget exceeded: %d %v", len(stored), err)
		}
	}
}

func TestThreadInboxBacklogDoesNotPruneDecision(t *testing.T) {
	t.Parallel()
	for _, state := range []string{StatusRejected, StatusExecuted, StatusFailed} {
		for _, backlog := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/backlog=%t", state, backlog), func(t *testing.T) {
				t.Parallel()
				now := time.Now().UTC()
				a := Approval{ID: "appr_review", Number: 1, Action: "cordon_node", Env: "stg", Status: StatusPending,
					RequesterThread: "local_review", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
				doc := legacyInboxDocument{file: file{Approvals: []Approval{a}}, Inbox: map[string]legacyInboxEntry{}}
				if backlog {
					for i := 0; i < 3100; i++ {
						old := a
						old.ID = fmt.Sprintf("appr_%016x", i)
						old.Number = i + 2
						old.Status = StatusFailed
						doc.Inbox["approval:"+old.ID+":failed"] = legacyInboxEntry{Record: old.ID, Order: uint64(i + 1), To: "thread:local_review", Body: approvalConclusion(old)}
					}
				}
				if state != StatusRejected {
					doc.Approvals[0].Status = StatusRunning
					doc.Approvals[0].Execution = &Execution{LeaseID: "test-lease", LeaseExpiresAt: now.Add(time.Hour)}
				}
				raw, err := doc.marshalOnce()
				if err != nil {
					t.Fatal(err)
				}
				if backlog && (len(raw) <= maxDocBytes || len(raw) > 900*1024) {
					t.Fatalf("fixture size %d", len(raw))
				}
				path := filepath.Join(t.TempDir(), "approvals")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				svc := New(path, nil)
				var res decided
				if state == StatusRejected {
					res = svc.reject(a.ID, "not now")
				} else {
					exit := 0
					if state == StatusFailed {
						exit = 1
					}
					res = svc.result(a.ID, resultInput{LeaseID: "test-lease", ExitCode: &exit})
				}
				saved, found, err := svc.Lookup(a.ID)
				t.Logf("backlog=%t queued=%d inputBytes=%d result=%d found=%t status=%s err=%v", backlog, len(doc.Inbox), len(raw), res.Status, found, saved.Status, err)
				if res.Status != 200 || err != nil {
					t.Fatal("rejection failed")
				}
				if !found || saved.Status != state {
					t.Fatal("delivery backlog removed the newly rejected approval")
				}
				for i := 0; i < 3; i++ {
					if err := svc.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error { return errors.New("PG unavailable") }); err == nil {
						t.Fatal("outage was not retryable")
					}
				}
				restarted := New(path, nil)
				mail := map[string]string{}
				send := dedupeInboxSender(func(_ context.Context, key, to, body string) error {
					if to != "thread:local_review" {
						t.Errorf("recipient=%q", to)
					}
					if _, ok := mail[key]; ok {
						t.Errorf("duplicate after restart: %s", key)
					}
					mail[key] = body
					return nil
				})
				for i := 0; i < 2; i++ {
					if err := restarted.retryThreadInbox(context.Background(), send); err != nil {
						t.Fatal(err)
					}
				}
				if len(mail) != 1 {
					t.Fatalf("recovered=%d want=1 retained business record", len(mail))
				}
				if !strings.Contains(mail["approval:"+a.ID+":"+state], "Conclusion: "+state) {
					t.Fatal("new conclusion missing")
				}

			})
		}
	}
}

func TestThreadInboxRebuildsLegacyRecipientFromSource(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"https://example.test/?next=local_victim", "session-1 (do not notify thread:local_victim)", "local_requester (TD-457)"} {
		s := New(filepath.Join(t.TempDir(), "approvals"), nil)
		doc := legacyInboxDocument{file: file{Approvals: []Approval{{ID: "appr_legacy", Status: StatusExecuted, RequesterThread: label}}}, Inbox: map[string]legacyInboxEntry{"approval:appr_legacy:executed": {Record: "appr_legacy", To: "thread:local_victim", Body: "stale"}}}
		raw, err := doc.marshalOnce()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.store.path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var calls int
		if err := s.retryThreadInbox(context.Background(), func(_ context.Context, key, to, body string) error {
			calls++
			if to != "thread:local_requester" || body == "stale" {
				t.Errorf("legacy misroute survived: to=%q body=%q", to, body)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if (calls == 1) != (label == "local_requester (TD-457)") {
			t.Fatalf("label=%q calls=%d", label, calls)
		}
	}
}
