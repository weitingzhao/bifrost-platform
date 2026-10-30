package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

func threadInboxServer(t *testing.T, now time.Time) (*Server, *lateDB, []string) {
	t.Helper()
	root := t.TempDir()
	paths := []string{filepath.Join(root, "approvals"), filepath.Join(root, "questions")}
	docs := []any{
		map[string]any{"approvals": []approvals.Approval{{ID: "appr_wiring", Number: 12,
			Status: approvals.StatusExecuted, Action: "grant_land", Env: "prod", RequesterThread: "local_wiring",
			Result: map[string]any{"grant_id": "grant_wiring", "token": "must-not-deliver"}}}},
		// The question store's own JSON shape: only questions_wire.go names
		// the questions package in server (questions TestServerNamesQuestionsOnlyInTheWireFile).
		map[string]any{"questions": []map[string]any{{"id": "q_aaaaaaaaaaaaaaaa", "number": 13,
			"status": "answered", "requester_thread": "local_wiring",
			"answer":  map[string]any{"option": "a", "by": "owner", "at": now},
			"options": []map[string]any{{"key": "a", "label": "Proceed"}}}}},
	}
	for i, doc := range docs {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[i], raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{approvals: approvals.New(paths[0], nil), questionRuntime: newQuestionRuntime(paths[1], nil)}
	s.approvals.SetClock(func() time.Time { return now })
	s.questionRuntime.svc.SetClock(func() time.Time { return now })
	late := &lateDB{}
	s.wireThreadInbox(late)
	return s, late, paths
}

func retryServerThreadInbox(s *Server) {
	s.approvals.RetryThreadInbox(context.Background())
	s.questionRuntime.svc.RetryThreadInbox(context.Background())
}

func TestThreadInboxWiringKeepsUnavailableRuntimePending(t *testing.T) {
	t.Parallel()
	s, _, paths := threadInboxServer(t, time.Now())
	retryServerThreadInbox(s)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc["thread_inbox"]) != 0 {
			t.Fatalf("disconnected runtime persisted a growing queue in %s", path)
		}
	}
	if a, found, err := s.approvals.Lookup("appr_wiring"); err != nil || !found || a.Status != approvals.StatusExecuted {
		t.Fatalf("approval changed: %+v %v", a, err)
	}
	if q, found, err := s.questionRuntime.svc.Lookup("q_aaaaaaaaaaaaaaaa"); err != nil || !found || q.Status != "answered" {
		t.Fatalf("answer changed: %+v %v", q, err)
	}
}

func TestThreadInboxWiringRecoversPGAndRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	w := store.Writer{Actor: "thread:local_wiring", Token: "fixture"}
	if _, _, err := store.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register wiring thread")); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	s, late, paths := threadInboxServer(t, now)
	retryServerThreadInbox(s) // the real sender returns errNotConnected
	late.open(pool, now)
	retryServerThreadInbox(s)
	mail, err := store.Unread(ctx, pool, w, 100)
	if err != nil || len(mail) != 2 {
		t.Fatalf("recovered inbox=%+v err=%v", mail, err)
	}
	var ids []string
	var bodies string
	for _, m := range mail {
		ids = append(ids, m.ID)
		bodies += m.Body
	}
	if !strings.Contains(bodies, "grant_id=grant_wiring") || !strings.Contains(bodies, "Answered by: owner") || strings.Contains(bodies, "must-not-deliver") {
		t.Fatalf("incomplete or unsafe bodies: %s", bodies)
	}
	if _, err := store.AckMessages(ctx, pool, w, ids); err != nil {
		t.Fatal(err)
	}
	restarted := &Server{approvals: approvals.New(paths[0], nil), questionRuntime: newQuestionRuntime(paths[1], nil)}
	restarted.wireThreadInbox(db.Pool(t, db.Runtime))
	retryServerThreadInbox(restarted)
	history, err := store.Thread(ctx, pool, w.Actor, "controller", 100)
	if err != nil || len(history) != 2 || history[0].AckedAt == nil || history[1].AckedAt == nil {
		t.Fatalf("restart changed acknowledged inbox: %+v %v", history, err)
	}
}
