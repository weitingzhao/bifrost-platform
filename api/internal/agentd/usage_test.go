package agentd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "log.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// codex exec --json as measured 10-18 (codex 0.162), and its session file.
func TestMeasureCodex(t *testing.T) {
	const thread = "01a15031-53fc-7183-bce4-1bc9fbf576fb"
	out := writeLog(t,
		`{"type":"thread.started","thread_id":"`+thread+`"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"ok"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":16821,"cached_input_tokens":13184,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1000,"cached_input_tokens":0,"output_tokens":20}}`)
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "10", "18")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	session := `{"timestamp":"2026-10-18T17:51:25Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":4.0,"window_minutes":10080,"resets_at":1792874442},"secondary":null,"rate_limit_reached_type":null}}}` + "\n"
	if err := os.WriteFile(filepath.Join(day, "rollout-2026-10-18T12-51-25-"+thread+".jsonl"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	m := measureCodex(filepath.Join(out, "log.txt"), home)
	if m.Usage == nil || *m.Usage.Tokens != 16821-13184+5+1000+20 || m.Usage.Pool != "codex:owner:weekly" {
		t.Fatalf("usage = %+v", m.Usage)
	}
	if len(m.Pools) != 1 || m.Pools[0].Pool != "codex:owner:weekly" || *m.Pools[0].UsedFraction != 0.04 ||
		m.Pools[0].ResetsAt.Unix() != 1792874442 || m.Pools[0].Limited {
		t.Fatalf("pools = %+v", m.Pools)
	}

	// Refused with the window spent: that window is the one limited.
	spent := strings.Replace(strings.Replace(session, `"used_percent":4.0`, `"used_percent":100.0`, 1), `"rate_limit_reached_type":null`, `"rate_limit_reached_type":"primary"`, 1)
	if err := os.WriteFile(filepath.Join(day, "rollout-2026-10-18T12-51-25-"+thread+".jsonl"), []byte(spent), 0o600); err != nil {
		t.Fatal(err)
	}
	out = writeLog(t, `{"type":"thread.started","thread_id":"`+thread+`"}`,
		`{"type":"error","message":"You've hit your usage limit. Upgrade to Pro or try again in 4 days."}`,
		`{"type":"turn.failed","error":{"message":"You've hit your usage limit. Upgrade to Pro or try again in 4 days."}}`)
	m = measureCodex(filepath.Join(out, "log.txt"), home)
	if m.Usage != nil || len(m.Pools) != 1 || !m.Pools[0].Limited || !strings.Contains(m.Pools[0].Text, "usage limit") || m.Pools[0].ResetsAt == nil {
		t.Fatalf("refused: %+v %+v", m.Usage, m.Pools)
	}

	// No session file: the refusal alone, with no reset.
	m = measureCodex(filepath.Join(out, "log.txt"), t.TempDir())
	if len(m.Pools) != 1 || !m.Pools[0].Limited || m.Pools[0].ResetsAt != nil || m.Pools[0].Pool != "codex:owner:weekly" {
		t.Fatalf("refused without a session file: %+v", m.Pools)
	}
	// A thread id that is not one is never globbed.
	if r := codexLimits(home, "*"); r != nil {
		t.Fatalf("a glob as the thread read %+v", r)
	}
}

// cursor-agent's result (measured 10-12) and its refusal on stderr (10-18).
func TestMeasureCursor(t *testing.T) {
	out := writeLog(t, `{"type":"system","subtype":"init","model":"Auto"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"inputTokens":202728,"outputTokens":142730,"cacheReadTokens":3738368,"cacheWriteTokens":10}}`)
	m := measureCursor(filepath.Join(out, "log.txt"))
	if m.Usage == nil || *m.Usage.Tokens != 202728+142730+10 || m.Usage.Pool != "cursor:owner:monthly" || len(m.Pools) != 0 {
		t.Fatalf("measured %+v %+v", m.Usage, m.Pools)
	}
	out = writeLog(t, `{"type":"system","subtype":"init","model":"Auto"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Reply with the single word ok."}]}}`,
		`ActionRequiredError: You've hit your usage limit Get Cursor Pro for more Agent usage, unlimited Tab, and more.`)
	m = measureCursor(filepath.Join(out, "log.txt"))
	if m.Usage != nil || len(m.Pools) != 1 || !m.Pools[0].Limited || m.Pools[0].Pool != "cursor:owner:monthly" || m.Pools[0].ResetsAt != nil {
		t.Fatalf("refused: %+v %+v", m.Usage, m.Pools)
	}
	// A line that only mentions a limit cools nothing.
	out = writeLog(t, `warning: approaching the rate limit of the search tool`)
	if m = measureCursor(filepath.Join(out, "log.txt")); len(m.Pools) != 0 {
		t.Fatalf("a mention of a limit read as a refusal: %+v", m.Pools)
	}
}

// claude's result as measured 10-18 (2.1.302); its rate-limit event and
// refusal as documented, not yet seen.
func TestMeasureClaude(t *testing.T) {
	out := writeLog(t, `{"type":"system","subtype":"init","model":"claude-opus-5-5"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0.0421,"usage":{"input_tokens":12,"cache_creation_input_tokens":3000,"cache_read_input_tokens":90000,"output_tokens":400}}`)
	m := measureClaude(filepath.Join(out, "log.txt"))
	if m.Usage == nil || *m.Usage.Tokens != 3412 || *m.Usage.USD != 0.0421 || m.Usage.Pool != "claude:owner:weekly" || len(m.Pools) != 0 {
		t.Fatalf("measured %+v %+v", m.Usage, m.Pools)
	}
	out = writeLog(t, `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1792900000,"rateLimitType":"seven_day","utilization":0.91}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1792880000,"rateLimitType":"five_hour"}}`,
		`{"type":"assistant","error":"rate_limit","message":{"model":"<synthetic>","content":[{"type":"text","text":"5-hour limit reached ∙ resets 3pm"}]}}`,
		`{"type":"result","is_error":true,"result":"5-hour limit reached ∙ resets 3pm","total_cost_usd":0}`)
	m = measureClaude(filepath.Join(out, "log.txt"))
	if len(m.Pools) != 2 {
		t.Fatalf("pools = %+v", m.Pools)
	}
	short, weekly := m.Pools[0], m.Pools[1]
	if short.Pool != "claude:owner:short" || !short.Limited || short.ResetsAt.Unix() != 1792880000 || !strings.Contains(short.Text, "limit reached") {
		t.Fatalf("short = %+v", short)
	}
	if weekly.Pool != "claude:owner:weekly" || weekly.Limited || *weekly.UsedFraction != 0.91 {
		t.Fatalf("weekly = %+v", weekly)
	}
	// The older wording names its reset after a bar.
	out = writeLog(t, `{"type":"result","is_error":true,"result":"Claude AI usage limit reached|1792880000"}`)
	m = measureClaude(filepath.Join(out, "log.txt"))
	if len(m.Pools) != 1 || !m.Pools[0].Limited || m.Pools[0].ResetsAt == nil || m.Pools[0].ResetsAt.Unix() != 1792880000 {
		t.Fatalf("older refusal: %+v", m.Pools)
	}
	// A failed login is no refusal (measured 10-18).
	out = writeLog(t, `{"type":"assistant","error":"authentication_failed","message":{"content":[{"type":"text","text":"Failed to authenticate: OAuth session expired"}]}}`,
		`{"type":"result","is_error":true,"result":"Failed to authenticate: OAuth session expired and could not be refreshed","usage":{"input_tokens":0,"output_tokens":0}}`)
	if m = measureClaude(filepath.Join(out, "log.txt")); len(m.Pools) != 0 {
		t.Fatalf("a failed login read as a refusal: %+v", m.Pools)
	}
}

func TestRefusalTextIsScrubbedAndShort(t *testing.T) {
	out := writeLog(t, `ActionRequiredError: You've hit your usage limit for key=sk-live-0123456789abcdef0123456789abcdef `+strings.Repeat("x", 400))
	m := measureCursor(filepath.Join(out, "log.txt"))
	if len(m.Pools) != 1 || strings.Contains(m.Pools[0].Text, "0123456789abcdef") || len(m.Pools[0].Text) > 200 {
		t.Fatalf("text = %q", m.Pools[0].Text)
	}
}

func TestMeasureNothingKnown(t *testing.T) {
	if m := measure("exec:deterministic", writeLog(t, `{"type":"result","usage":{"input_tokens":5}}`)); !m.empty() {
		t.Fatalf("deterministic measured %+v", m)
	}
	if m := measure("exec:claude", t.TempDir()); !m.empty() {
		t.Fatalf("no log measured %+v", m)
	}
}

// A platform from before 2.4 refuses computed with usage (400, unknown
// field): agentd sends it again at once without it, and says so.
func TestComputedToAnOlderPlatform(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		for _, k := range []string{"usage", "pools"} {
			if _, has := body[k]; has {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"` + k + `\""}`))
				return
			}
		}
		_, _ = w.Write([]byte(`{"computed":true}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd"}
	exit, tokens := 0, int64(9)
	refused, err := c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done",
		Measured{Usage: &Usage{Pool: "codex:owner:weekly", Tokens: &tokens}})
	if err != nil || !strings.HasPrefix(refused, UsageUnsupported) {
		t.Fatalf("refused %q, err %v", refused, err)
	}
	if len(bodies) != 2 || bodies[1]["usage"] != nil || bodies[1]["lease_epoch"] != float64(7) || bodies[1]["end_how"] != "exit" {
		t.Fatalf("requests %v", bodies)
	}
	// Without usage there is one request, and nothing refused.
	bodies = nil
	if refused, err = c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done", Measured{}); err != nil || refused != "" || len(bodies) != 1 {
		t.Fatalf("computed without usage: refused %q, err %v, %d requests", refused, err, len(bodies))
	}
	// The platform's own refusal of the report is passed on.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"computed":true,"usage_refused":"store: invalid request: at most 4 pool readings"}`))
	}))
	t.Cleanup(srv2.Close)
	c.Base = srv2.URL
	if refused, err = c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done",
		Measured{Usage: &Usage{Tokens: &tokens}}); err != nil || !strings.Contains(refused, "pool readings") {
		t.Fatalf("platform refusal: %q %v", refused, err)
	}
}
