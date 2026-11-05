package agentthreads

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// The Console's fixtures of P-c2's answers (R5:5.5-12 §7 mixed versions,
// S1-3's rendering test): GET /agent/threads from the real handler over
// deterministic beats, and the coverage.sessions it counts, each byte for
// byte. The runtime's works are the ones the plan view's fixture holds
// (store TestPlanViewCollectorWorkFixture: ew_111…, ew_333…, ew_444…), so
// the Console renders one consistent page from them.
// Update explicitly: UPDATE_AGENT_THREADS_FIXTURE=1 go test ./internal/agentthreads -run TestAgentThreadsResponseFixture
const (
	threadsFixture  = "../../../console/src/api/__fixtures__/agent-threads.generated.json"
	sessionsFixture = "../../../console/src/api/__fixtures__/agent-threads-sessions.generated.json"
)

var fixtureWorks = []Work{
	{ID: "ew_" + strings.Repeat("1", 64), Root: "R4", Node: "R4:4.1-1", State: "Running", Host: "h1", Vendor: "claude", Session: "0b8e2f4a-1111-2222-3333-444455556666", Epoch: 1},
	{ID: "ew_" + strings.Repeat("3", 64), Root: "R4", Node: "R4:4.2-1", State: "Submitted", Host: "h1", Vendor: "claude", Session: "7c1d9e3b-1111-2222-3333-444455556666", Epoch: 2},
	{ID: "ew_" + strings.Repeat("4", 64), Root: "R4", Node: "R4:4.3-1", State: "Running", Host: "h1", Vendor: "claude", Session: "9e5f0a1c-1111-2222-3333-444455556666", Epoch: 1},
}

func fixtureHandler(t *testing.T) *Handler {
	t.Helper()
	at := time.Date(2026, 10, 26, 21, 50, 0, 0, time.UTC)
	clk := &clock{at: at}
	cfg := DefaultConfig()
	cfg.ExpectedHosts = []string{"h1", "h2"}
	rec := NewRecorder(NewStore(""), cfg)
	rec.now = clk.now
	h := NewHandler(rec, cfg, nil, nil)
	h.now = clk.now
	h.UseWorks(func(context.Context) ([]Work, error) { return fixtureWorks, nil })
	keys := map[string]string{}
	post := func(p actuation.Principal, minutes int, fields map[string]any) {
		t.Helper()
		clk.at = at.Add(time.Duration(minutes) * time.Minute)
		body := map[string]any{"vendor": "claude", "host": "h1", "turn_id": "turn-1", "event": "turn_start"}
		for k, v := range fields {
			body[k] = v
		}
		id := body["vendor"].(string) + "/" + body["thread"].(string)
		if keys[id] != "" {
			body["thread_key"] = keys[id]
		}
		raw, _ := json.Marshal(body)
		w := as(h, p, "POST", "/api/v1/agent/threads/heartbeat", string(raw))
		var out struct {
			Key string `json:"thread_key"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusAccepted {
			t.Fatalf("%v: %d %s", fields, w.Code, w.Body)
		}
		if out.Key != "" {
			keys[id] = out.Key
		}
	}
	linked := fixtureWorks[0].Session
	post(collectorH1, 0, map[string]any{"thread": linked, "session": linked, "epoch": 1, "seq": 1, "event": "session_start", "reason": "startup",
		"sidebar": "local_0b8e2f4a-aaaa-bbbb-cccc-ddddeeeeffff", "host_id_source": "file", "client_bind": map[string]any{"state": "linked", "at": "2026-10-26T21:50:00Z"}})
	post(collectorH1, 1, map[string]any{"thread": linked, "session": linked, "epoch": 1, "seq": 2})
	post(collectorH1, 8, map[string]any{"thread": linked, "session": linked, "epoch": 1, "seq": 3, "event": "precompact", "reason": "auto"})
	post(collectorH1, 2, map[string]any{"thread": fixtureWorks[1].Session, "session": fixtureWorks[1].Session, "epoch": 2, "seq": 1, "event": "turn_end", "host_id_source": "file"})
	post(collectorH1, 3, map[string]any{"thread": fixtureWorks[2].Session, "session": fixtureWorks[2].Session, "epoch": 1, "seq": 1, "host_id_source": "file"})
	post(collectorH1, 4, map[string]any{"thread": "5a6b7c8d-1111-2222-3333-444455556666", "session": "5a6b7c8d-1111-2222-3333-444455556666", "epoch": 1, "seq": 1,
		"event": "session_start", "reason": "startup", "host_id_source": "env", "client_bind": map[string]any{"state": "unlinked", "reason": "no_context", "at": "2026-10-26T21:54:00Z"}})
	post(collectorH1, 5, map[string]any{"thread": "6c7d8e9f-1111-2222-3333-444455556666", "session": "6c7d8e9f-1111-2222-3333-444455556666", "epoch": 1, "seq": 1,
		"event": "before_tool", "tool": "Bash", "tool_timeout_s": 600, "host_id_source": "file",
		"client_bind": map[string]any{"state": "refused", "reason": "node_rejected", "at": "2026-10-26T21:55:00Z"}})
	post(reporterP, 6, map[string]any{"thread": "a3f0c2d4-1111-2222-3333-444455556666", "vendor": "codex", "host": "h2", "session": "a3f0c2d4-1111-2222-3333-444455556666", "epoch": 1, "seq": 1,
		"event": "after_tool", "tool": "shell"})
	post(reporterP, 7, map[string]any{"thread": "f3a1c2d4-0000-0000-0000-000000000000", "vendor": "cursor", "host": "h2", "seq": 1, "event": "turn_end"})
	for _, host := range []struct {
		p    actuation.Principal
		name string
	}{{collectorH1, "h1"}, {reporterP, "h2"}} {
		w := as(h, host.p, "POST", "/api/v1/agent/hosts/heartbeat", `{"host":"`+host.name+`","vendors":{"claude":{"wired":true,"token":true},"cursor":{"wired":true,"token":true},"codex":{"wired":true,"token":true}}}`)
		if w.Code != http.StatusAccepted {
			t.Fatalf("host %s: %d %s", host.name, w.Code, w.Body)
		}
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	clk.at = at.Add(10 * time.Minute)
	return h
}

func TestAgentThreadsResponseFixture(t *testing.T) {
	h := fixtureHandler(t)
	w := as(h, viewerP, "GET", "/api/v1/agent/threads", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	var list struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Threads) != 7 {
		t.Fatalf("list: %v %s", err, w.Body)
	}
	states := map[string]int{}
	for _, row := range list.Threads {
		states[row["binding"].(map[string]any)["state"].(string)]++
	}
	if states["linked"] != 3 || states["unlinked"] != 2 || states["refused"] != 1 || states["unsupported"] != 1 {
		t.Fatalf("the fixture's bindings: %v", states)
	}
	counts, err := h.SessionCoverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := json.MarshalIndent(counts, "", "  ")
	threads, _ := json.MarshalIndent(json.RawMessage(w.Body.Bytes()), "", "  ")
	for path, raw := range map[string][]byte{threadsFixture: append(threads, '\n'), sessionsFixture: append(sessions, '\n')} {
		if os.Getenv("UPDATE_AGENT_THREADS_FIXTURE") == "1" {
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, want) {
			t.Fatalf("%s drifted from Go's answer; regenerate and review it", path)
		}
	}
}
