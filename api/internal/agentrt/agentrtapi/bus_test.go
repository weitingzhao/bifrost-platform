package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// claimOne leases the world's task to agentd h1 and starts it; it returns the
// attempt id and a fence builder.
func claimOne(t *testing.T, s interface{ Close() }, call2 func(caller, method, path string, body any) (int, map[string]any)) (string, func(map[string]any) map[string]any) {
	t.Helper()
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	code, claim := call2("agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}})
	if code != http.StatusOK {
		t.Fatalf("claim: %d %v", code, claim)
	}
	attempt := claim["attempt_id"].(string)
	fence := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	if code, out := call2("agentd:h1", "POST", "attempts/"+attempt+"/start", fence(map[string]any{"model_provider": "cursor-mix"})); code != http.StatusOK {
		t.Fatalf("start: %d %v", code, out)
	}
	return attempt, fence
}

// A heartbeat carries actions (W-31 2.3); they never fail it: the lease is
// renewed and the answer says what was kept.
func TestHeartbeatKeepsActions(t *testing.T) {
	rdb, prefix := agentrttest.Redis(t)
	actionStore = &actions.Store{Redis: rdb, Prefix: prefix}
	t.Cleanup(func() { actionStore = nil })
	pool, _ := world(t)
	s := server(t, pool)
	c := func(caller, method, path string, body any) (int, map[string]any) {
		return call(t, s, caller, method, path, body)
	}
	attempt, fence := claimOne(t, s, c)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	acts := []any{
		map[string]any{"kind": "read", "target": "api/internal/safego/safego.go", "at": now},
		map[string]any{"kind": "result", "target": "go test ./...", "at": now, "code": 0},
	}
	code, out := c("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(map[string]any{"actions": acts}))
	if code != http.StatusOK || out["actions_kept"] != float64(2) || out["lease_expires_at"] == nil {
		t.Fatalf("heartbeat with actions: %d %v", code, out)
	}
	code, out = c("viewer", "GET", "attempts/"+attempt+"/actions", nil)
	if code != http.StatusOK || len(out["actions"].([]any)) != 2 || out["next"] == "" {
		t.Fatalf("read actions: %d %v", code, out)
	}
	code, out = c("viewer", "GET", "attempts/"+attempt+"/actions?last=1", nil)
	if got := out["actions"].([]any); code != http.StatusOK || len(got) != 1 || got[0].(map[string]any)["kind"] != "result" {
		t.Fatalf("last action: %d %v", code, out)
	}
	// An action outside the format: the heartbeat still renews, the answer says why nothing was kept.
	bad := []any{map[string]any{"kind": "think", "target": "x", "at": now}}
	code, out = c("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(map[string]any{"actions": bad}))
	if code != http.StatusOK || out["lease_expires_at"] == nil || !strings.Contains(out["actions_refused"].(string), "kind") {
		t.Fatalf("a heartbeat with a bad action: %d %v", code, out)
	}
	// Only a heartbeat carries actions.
	if code, out := c("agentd:h1", "POST", "attempts/"+attempt+"/checkpoint", fence(map[string]any{"checkpoint_seq": 1, "actions": acts})); code != http.StatusBadRequest {
		t.Fatalf("a checkpoint with actions: %d %v", code, out)
	}
	if code, out := c("viewer", "GET", "attempts/"+attempt+"/actions?limit=0", nil); code != http.StatusBadRequest {
		t.Fatalf("limit 0: %d %v", code, out)
	}
	if code, out := c("viewer", "GET", "attempts/not-an-attempt/actions", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("a bad attempt id: %d %v", code, out)
	}
}

// Without redis-agents the heartbeat keeps working and says no actions are kept.
func TestHeartbeatWithoutAnActionStore(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	c := func(caller, method, path string, body any) (int, map[string]any) {
		return call(t, s, caller, method, path, body)
	}
	attempt, fence := claimOne(t, s, c)
	acts := []any{map[string]any{"kind": "read", "target": "a.go", "at": time.Now().UTC().Format(time.RFC3339Nano)}}
	code, out := c("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(map[string]any{"actions": acts}))
	if code != http.StatusOK || !strings.Contains(out["actions_refused"].(string), "keeps no actions") {
		t.Fatalf("heartbeat: %d %v", code, out)
	}
	if code, _ := c("viewer", "GET", "attempts/"+attempt+"/actions", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("read without a store: %d", code)
	}
}

// The Owner asks the chief of staff; only the receiver reads and acknowledges it.
func TestMessages(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	c := func(caller, method, path string, body any, h ...string) (int, map[string]any) {
		return call(t, s, caller, method, path, body, h...)
	}
	code, out := c("admin", "POST", "messages", map[string]any{"to": "seat:chief_of_staff", "kind": "ask", "body": "今天进展如何？"})
	if code != http.StatusOK || out["from"] != "owner" {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["message_id"].(string)
	for name, body := range map[string]map[string]any{
		"an unknown receiver": {"to": "seat:nobody", "kind": "ask", "body": "x"},
		"the runtime's kind":  {"to": "seat:chief_of_staff", "kind": "wake", "body": "x"},
		"an empty body":       {"to": "seat:chief_of_staff", "kind": "ask", "body": ""},
		"a body too large":    {"to": "seat:chief_of_staff", "kind": "ask", "body": strings.Repeat("x", 8<<10+1)},
		"to oneself":          {"to": "owner", "kind": "note", "body": "x"},
	} {
		if code, out := c("admin", "POST", "messages", body); code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if code, _ := c("viewer", "POST", "messages", map[string]any{"to": "owner", "kind": "note", "body": "x"}); code != http.StatusUnauthorized {
		t.Errorf("a viewer sent a message: %d", code)
	}
	code, out = c("seat:chief_of_staff", "GET", "messages", nil)
	ms := out["messages"].([]any)
	if code != http.StatusOK || out["actor"] != "seat:chief_of_staff" || len(ms) != 1 || ms[0].(map[string]any)["body"] != "今天进展如何？" || ms[0].(map[string]any)["from"] != "owner" {
		t.Fatalf("the seat's inbox: %d %v", code, out)
	}
	if code, out := c("admin", "GET", "messages", nil); code != http.StatusOK || len(out["messages"].([]any)) != 0 {
		t.Fatalf("the Owner's inbox: %d %v", code, out)
	}
	if code, out := c("admin", "POST", "messages/ack", map[string]any{"message_ids": []string{id}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("the Owner acknowledged the seat's message: %d %v", code, out)
	}
	if code, out := c("seat:chief_of_staff", "POST", "messages/ack", map[string]any{"message_ids": []string{id}}); code != http.StatusOK || len(out["acknowledged"].([]any)) != 1 {
		t.Fatalf("ack: %d %v", code, out)
	}
	if code, out := c("seat:chief_of_staff", "POST", "messages/ack", map[string]any{"message_ids": []string{id}}); code != http.StatusOK || len(out["acknowledged"].([]any)) != 0 {
		t.Fatalf("ack again: %d %v", code, out)
	}
	// The answer goes back; the Owner's long poll returns when it arrives.
	got := make(chan map[string]any, 1)
	go func() {
		_, out := c("admin", "GET", "messages?wait=5", nil)
		got <- out
	}()
	time.Sleep(300 * time.Millisecond)
	if code, out := c("seat:chief_of_staff", "POST", "messages", map[string]any{"to": "owner", "kind": "answer", "body": "按计划。"}); code != http.StatusOK {
		t.Fatalf("answer: %d %v", code, out)
	}
	select {
	case out := <-got:
		if ms := out["messages"].([]any); len(ms) != 1 || ms[0].(map[string]any)["body"] != "按计划。" {
			t.Fatalf("the long poll: %v", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the long poll did not return")
	}
	start := time.Now()
	if code, out := c("seat:chief_of_staff", "GET", "messages?wait=1", nil); code != http.StatusOK || len(out["messages"].([]any)) != 0 || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("an empty long poll: %d %v after %v", code, out, time.Since(start))
	}
	for _, q := range []string{"wait=26", "wait=-1", "limit=0", "limit=101"} {
		if code, _ := c("admin", "GET", "messages?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, code)
		}
	}
	// A thread reads its own inbox by its session header.
	if code, out := c("reporter", "GET", "messages", nil, "X-Bifrost-Session", "local_fixture-1"); code != http.StatusOK || out["actor"] != "thread:local_fixture-1" {
		t.Fatalf("a thread's inbox: %d %v", code, out)
	}
}
