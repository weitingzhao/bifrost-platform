package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// HookAndMcpWriteSameSession, its second half (R5:5.5-12 S1-2/S1-3, P-c2):
// on the whole platform, a session's hook writes through the collector
// token (its beats and its registration name the session's sidebar) and its
// MCP through its own token under the same sidebar (the session header).
// GET /agent/threads?sidebar= finds the hook's row, linked to the hook's
// work, and the MCP's writes are the thread of that same sidebar: one
// association by the sidebar, whichever wrote first. A Desktop gives the
// sidebar; a CLI session gets launch-<id> from the launcher (§2.2); two CLI
// sessions on one host are apart; a session that fell back to the host name
// (no sidebar at all) is associated with nothing by it.

// sessionServer is a platform with the walk's tokens and a migrated agent
// runtime with a dev plan R2 (step R2:1-1) to register on.
func sessionServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	db := agentrttest.Migrated(t)
	ctx := context.Background()
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	for _, n := range []store.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "step", StepKind: "agent", Root: "R2"},
	} {
		if err := store.InTx(ctx, pool, func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err }); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(reachIntakeEnv, reachTokens["intake:dots-p2"])
	cfg := newTestConfig(t)
	path := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(path, []byte(reachAuthFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = path
	srv := newTestServer(t, cfg)
	srv.agentRuntime.api.DB = pool
	return srv, srv.Router()
}

func call2(t *testing.T, router http.Handler, caller, method, target string, body any, header ...string) (int, []byte) {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		raw = b
	default:
		raw, _ = json.Marshal(b)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+reachTokens[caller])
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func describe(task string) []byte {
	return []byte(`{"v":1,"kind":"agentrt.external_work/v1","task":"` + task + `","executor":"claude","provider":"anthropic","repo":"platform","branch":"test","acceptance_refs":["PV1"],"estimate_minutes":[1,2]}`)
}

func artifactOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// hook is one session's hook: it beats as the collector of h1 and registers
// its work under its session identity and sidebar ("" when it has none).
func hook(t *testing.T, router http.Handler, session, sidebar string) string {
	t.Helper()
	beat := map[string]any{"thread": session, "session": session, "epoch": 1, "vendor": "claude", "host": "h1", "event": "turn_start", "turn_id": "turn-1", "seq": 1}
	if sidebar != "" {
		beat["sidebar"] = sidebar
	}
	if code, out := call2(t, router, "collector:h1", "POST", "/api/v1/agent/threads/heartbeat", beat); code != http.StatusAccepted {
		t.Fatalf("the hook's beat: %d %s", code, out)
	}
	spec := describe("hook " + session)
	if code, out := call2(t, router, "collector:h1", "PUT", "/api/v1/agentrt/artifacts/"+artifactOf(spec), spec, "X-Artifact-Kind", "other"); code != http.StatusOK {
		t.Fatalf("the hook's description: %d %s", code, out)
	}
	reg := map[string]any{"idempotency_key": "hook-" + session, "root": "R2", "plan_node_id": "R2:1-1", "artifact_id": artifactOf(spec), "vendor": "claude", "session": session, "epoch": 1}
	if sidebar != "" {
		reg["sidebar"] = sidebar
	}
	code, out := call2(t, router, "collector:h1", "POST", "/api/v1/agentrt/external-work", reg)
	var res struct {
		ID string `json:"external_work_id"`
	}
	if json.Unmarshal(out, &res); code != http.StatusOK || res.ID == "" {
		t.Fatalf("the hook's registration: %d %s", code, out)
	}
	return res.ID
}

// mcpWrites is the same session's MCP: its own token, its session header the
// sidebar (sessionId.ts: CLAUDE_CODE_HOST_SESSION_ID, or the host name).
// It registers work of its own, recorded as thread:<header>.
func mcpWrites(t *testing.T, router http.Handler, header string) string {
	t.Helper()
	spec := describe("mcp " + header)
	if code, out := call2(t, router, "reporter", "PUT", "/api/v1/agentrt/artifacts/"+artifactOf(spec), spec, "X-Artifact-Kind", "other", "X-Bifrost-Session", header); code != http.StatusOK {
		t.Fatalf("the MCP's description: %d %s", code, out)
	}
	reg := map[string]any{"idempotency_key": "mcp-" + header, "root": "R2", "plan_node_id": "R2:1-1", "source_run_key": "mcp-" + header, "artifact_id": artifactOf(spec)}
	code, out := call2(t, router, "reporter", "POST", "/api/v1/agentrt/external-work", reg, "X-Bifrost-Session", header)
	var res struct {
		ID string `json:"external_work_id"`
	}
	if json.Unmarshal(out, &res); code != http.StatusOK || res.ID == "" {
		t.Fatalf("the MCP's registration: %d %s", code, out)
	}
	return res.ID
}

type sessionRow struct {
	Thread  string `json:"thread"`
	Session string `json:"session"`
	Binding struct {
		State string `json:"state"`
		Work  string `json:"external_work_id"`
	} `json:"binding"`
}

func bySidebar(t *testing.T, router http.Handler, sidebar string) []sessionRow {
	t.Helper()
	code, out := call2(t, router, "viewer", "GET", "/api/v1/agent/threads?sidebar="+sidebar, nil)
	var body struct {
		Threads []sessionRow `json:"threads"`
	}
	if json.Unmarshal(out, &body); code != http.StatusOK || bytes.Contains(out, []byte(`"sidebar"`)) {
		t.Fatalf("?sidebar=%s: %d %s", sidebar, code, out)
	}
	return body.Threads
}

func registrantOf(t *testing.T, srv *Server, id string) string {
	t.Helper()
	var who string
	err := store.InTx(context.Background(), srv.agentRuntime.api.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT body_json->'external_work'->>'registrant' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq LIMIT 1`, "external-work:"+id).Scan(&who)
	})
	if err != nil {
		t.Fatal(err)
	}
	return who
}

func TestHookAndMcpWriteSameSessionBySidebar(t *testing.T) {
	for _, c := range []struct{ name, sidebar string }{
		{"a Desktop's sidebar", "local_0b8e2f4a-aaaa-bbbb-cccc-ddddeeeeffff"},
		{"a launcher's id for a CLI session", "launch-5a6b7c8d-1111-2222-3333-444455556666"},
	} {
		for _, hookFirst := range []bool{true, false} {
			order := map[bool]string{true: "hook first", false: "MCP first"}[hookFirst]
			t.Run(c.name+", "+order, func(t *testing.T) {
				srv, router := sessionServer(t)
				var hw, mw string
				if hookFirst {
					hw, mw = hook(t, router, "s-one", c.sidebar), mcpWrites(t, router, c.sidebar)
				} else {
					mw, hw = mcpWrites(t, router, c.sidebar), hook(t, router, "s-one", c.sidebar)
				}
				rows := bySidebar(t, router, c.sidebar)
				if len(rows) != 1 || rows[0].Session != "s-one" || rows[0].Binding.State != "linked" || rows[0].Binding.Work != hw {
					t.Fatalf("?sidebar=: %+v, the hook's work %s", rows, hw)
				}
				if got := registrantOf(t, srv, mw); got != "thread:"+c.sidebar {
					t.Fatalf("the MCP's work is %s's, not the sidebar's thread", got)
				}
				if got := registrantOf(t, srv, hw); got != "thread:collector@h1" {
					t.Fatalf("the hook's work is %s's", got)
				}
			})
		}
	}
	for _, hookFirst := range []bool{true, false} {
		t.Run("two CLI sessions on one host, "+map[bool]string{true: "hook first", false: "MCP first"}[hookFirst], func(t *testing.T) {
			twoSessions(t, hookFirst)
		})
	}
	t.Run("the host name is no sidebar", func(t *testing.T) {

		srv, router := sessionServer(t)
		hw := hook(t, router, "s-bare", "") // no sidebar: no Desktop, no launcher
		mw := mcpWrites(t, router, "h1")    // the MCP fell back to the host name
		if rows := bySidebar(t, router, "h1"); len(rows) != 0 {
			t.Fatalf("the host name found the session: %+v", rows)
		}
		code, out := call2(t, router, "viewer", "GET", "/api/v1/agent/threads?session=s-bare", nil)
		var body struct {
			Threads []sessionRow `json:"threads"`
		}
		if json.Unmarshal(out, &body); code != http.StatusOK || len(body.Threads) != 1 || body.Threads[0].Binding.Work != hw {
			t.Fatalf("?session=: %d %s", code, out)
		}
		if got := registrantOf(t, srv, mw); got != "thread:h1" {
			t.Fatalf("the MCP's work: %s", got)
		}
	})
}

// twoSessions: two CLI sessions on one host, each with its launcher's id, the
// hook or the MCP writing first; each sidebar finds its own session only.
func twoSessions(t *testing.T, hookFirst bool) {
	srv, router := sessionServer(t)
	works := map[string][2]string{}
	for _, s := range []string{"a", "b"} {
		sidebar := "launch-" + s
		if hookFirst {
			h := hook(t, router, "s-"+s, sidebar)
			works[s] = [2]string{h, mcpWrites(t, router, sidebar)}
		} else {
			m := mcpWrites(t, router, sidebar)
			works[s] = [2]string{hook(t, router, "s-"+s, sidebar), m}
		}
	}
	for _, s := range []string{"a", "b"} {
		rows := bySidebar(t, router, "launch-"+s)
		if len(rows) != 1 || rows[0].Session != "s-"+s || rows[0].Binding.Work != works[s][0] {
			t.Fatalf("launch-%s: %+v", s, rows)
		}
		if got := registrantOf(t, srv, works[s][1]); got != "thread:launch-"+s {
			t.Fatalf("launch-%s's MCP work is %s's", s, got)
		}
	}
}
