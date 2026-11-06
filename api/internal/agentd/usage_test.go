package agentd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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

// PROD 11-02: a model's own weekly limit is not the account's weekly window.
// Its utilization reads no pool, however late it comes; the latest event of
// each of the account's own windows is that window's reading; and a model's
// refusal still refuses the weekly pool, until that model's limit resets.
func TestMeasureClaudeModelWeeklyLimits(t *testing.T) {
	event := func(kind, status string, used float64, resets int64) string {
		return `{"type":"rate_limit_event","rate_limit_info":{"status":"` + status + `","resetsAt":` + strconv.FormatInt(resets, 10) +
			`,"rateLimitType":"` + kind + `","utilization":` + strconv.FormatFloat(used, 'f', -1, 64) + `}}`
	}
	measured := func(lines ...string) Measured { return measureClaude(filepath.Join(writeLog(t, lines...), "log.txt")) }

	m := measured(event("seven_day", "allowed", 0.10, 1792900000), event("seven_day_opus", "allowed_warning", 0.99, 1792700000))
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || *m.Pools[0].UsedFraction != 0.10 ||
		m.Pools[0].ResetsAt.Unix() != 1792900000 || m.Pools[0].Limited {
		t.Fatalf("seven_day with seven_day_opus after it: %+v", m.Pools)
	}
	if m = measured(event("seven_day_opus", "allowed_warning", 0.99, 1792700000), event("seven_day_sonnet", "allowed", 0.5, 1792700000)); len(m.Pools) != 0 {
		t.Fatalf("models' limits alone read a pool: %+v", m.Pools)
	}
	m = measured(event("five_hour", "allowed", 0.3, 1792880000))
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:short" || *m.Pools[0].UsedFraction != 0.3 || m.Pools[0].Limited {
		t.Fatalf("five_hour: %+v", m.Pools)
	}
	m = measured(event("seven_day", "allowed", 0.2, 1792900000), event("seven_day", "allowed", 0.25, 1792900000))
	if len(m.Pools) != 1 || *m.Pools[0].UsedFraction != 0.25 {
		t.Fatalf("two seven_day events: %+v", m.Pools)
	}

	// A model's refusal refuses the weekly pool until that model's limit
	// resets, keeping the account's own reading, but only when the run was
	// refused: the vendor's words name it.
	failed := `{"type":"result","is_error":true,"result":"Opus weekly limit reached ∙ resets Nov 5","total_cost_usd":0}`
	m = measured(event("seven_day", "allowed", 0.4, 1792900000), event("seven_day_opus", "rejected", 1, 1792700000), failed)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || !m.Pools[0].Limited || *m.Pools[0].UsedFraction != 0.4 ||
		m.Pools[0].ResetsAt.Unix() != 1792700000 || !strings.Contains(m.Pools[0].Text, "Opus weekly limit reached") {
		t.Fatalf("a model refused: %+v", m.Pools)
	}
	// The same rejected model event in a run that went on (on another model)
	// does not cool the account: the weekly pool keeps the account's reading.
	m = measured(event("seven_day", "allowed", 0.4, 1792900000), event("seven_day_opus", "rejected", 1, 1792700000),
		`{"type":"result","is_error":false,"result":"done","total_cost_usd":0.01}`)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || m.Pools[0].Limited || *m.Pools[0].UsedFraction != 0.4 ||
		m.Pools[0].ResetsAt.Unix() != 1792900000 {
		t.Fatalf("a model's rejection in a run that went on: %+v", m.Pools)
	}
	// A rate_limit message on the way to a run that went on (claude fell
	// back to another model and the main thread's model answered) refuses
	// nothing, however the run then ended: neither the weekly pool for the
	// model's week nor a pool of its own (review of 823038ee). The run's last
	// words may quote the limit.
	refusedOnTheWay := `{"type":"assistant","error":"rate_limit","message":{"model":"<synthetic>","content":[{"type":"text","text":"You've hit your Opus limit · resets Nov 5"}]}}`
	answered := `{"type":"assistant","parent_tool_use_id":null,"message":{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"Reading usage.go"}]}}`
	done := `{"type":"result","is_error":false,"result":"done","total_cost_usd":0.01}`
	m = measured(event("seven_day", "allowed", 0.4, 1792900000), event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered, done)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || m.Pools[0].Limited || *m.Pools[0].UsedFraction != 0.4 ||
		m.Pools[0].ResetsAt.Unix() != 1792900000 {
		t.Fatalf("a model's refusal on the way to a run that went on: %+v", m.Pools)
	}
	for name, lines := range map[string][]string{
		"a model's refusal, alone":    {event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered, done},
		"a rate_limit message alone":  {refusedOnTheWay, answered, done},
		"its last words quote limits": {event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered, `{"type":"result","is_error":false,"result":"Fixed: a run that hit your weekly limit, usage limit reached, no longer cools the pool"}`},
		"killed after the answer":     {event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered},
		"failed otherwise":            {event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered, `{"type":"result","is_error":true,"result":"stopped at the hard limit"}`},
		"result with no is_error":     {event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered, `{"type":"result","subtype":"error_during_execution"}`},
	} {
		if m = measured(lines...); len(m.Pools) != 0 {
			t.Fatalf("%s, then the run went on: %+v", name, m.Pools)
		}
	}
	// A run that did not go on is refused, whatever its result's words: the
	// main thread's model did not answer after the message (nothing came, or
	// only a synthetic message, an error or a subagent's answer), the message
	// came after the result, or the result itself failed on the limit.
	for name, lines := range map[string][]string{
		"no answer, result not marked failed": {refusedOnTheWay, `{"type":"result","is_error":false,"result":"You've hit your Opus limit · resets Nov 5"}`},
		"no answer, result done":              {refusedOnTheWay, done},
		"no answer, killed":                   {refusedOnTheWay},
		"answered before the message only":    {answered, refusedOnTheWay, done},
		"a synthetic message after it":        {refusedOnTheWay, `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"No response requested."}]}}`, done},
		"an error after it":                   {refusedOnTheWay, `{"type":"assistant","error":"server_error","message":{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"API Error: 500"}]}}`, done},
		"a subagent's answer after it":        {refusedOnTheWay, `{"type":"assistant","parent_tool_use_id":"toolu_01","message":{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"found it"}]}}`, done},
		"a message after the result":          {answered, done, refusedOnTheWay},
	} {
		m = measured(append([]string{event("seven_day_opus", "rejected", 1, 1792700000)}, lines...)...)
		if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || !m.Pools[0].Limited || m.Pools[0].ResetsAt.Unix() != 1792700000 {
			t.Fatalf("%s: %+v", name, m.Pools)
		}
	}
	// A result that failed on a limit after the run went on past a model's
	// message is a refusal of its own, not that model's: it does not take the
	// model's reset (review of the round-2 fix).
	m = measured(event("seven_day_opus", "rejected", 1, 1792700000), refusedOnTheWay, answered,
		`{"type":"result","is_error":true,"result":"5-hour limit reached ∙ resets 3pm"}`)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:short" || !m.Pools[0].Limited || m.Pools[0].ResetsAt != nil {
		t.Fatalf("the result refused after the run went on: %+v", m.Pools)
	}
	// A result's refusal goes before the message's: its words choose the
	// pool and name the reset when no event does.
	for name, text := range map[string]string{"same window": "5-hour limit reached ∙ resets 3pm", "message says weekly": "Weekly limit reached ∙ resets Mon"} {
		m = measured(`{"type":"assistant","error":"rate_limit","message":{"model":"<synthetic>","content":[{"type":"text","text":"`+text+`"}]}}`,
			`{"type":"result","is_error":true,"result":"Claude AI usage limit reached|1792880000"}`)
		if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:short" || !m.Pools[0].Limited || m.Pools[0].ResetsAt == nil || m.Pools[0].ResetsAt.Unix() != 1792880000 {
			t.Fatalf("result refusal first, %s: %+v", name, m.Pools)
		}
	}
	m = measured(event("seven_day_opus", "rejected", 1, 1792700000),
		`{"type":"result","is_error":true,"result":"Opus weekly limit reached ∙ resets Nov 5","total_cost_usd":0}`)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "claude:owner:weekly" || !m.Pools[0].Limited || m.Pools[0].UsedFraction != nil ||
		m.Pools[0].ResetsAt.Unix() != 1792700000 || !strings.Contains(m.Pools[0].Text, "Opus weekly limit reached") {
		t.Fatalf("a model refused, alone: %+v", m.Pools)
	}
	// The account's own weekly refusal stands as it is.
	m = measured(event("seven_day", "rejected", 1, 1792900000), event("seven_day_opus", "rejected", 1, 1792700000))
	if len(m.Pools) != 1 || !m.Pools[0].Limited || m.Pools[0].ResetsAt.Unix() != 1792900000 || *m.Pools[0].UsedFraction != 1 {
		t.Fatalf("both refused: %+v", m.Pools)
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

// Review 274 A1: a reset in milliseconds is read as such; one that is neither
// seconds nor milliseconds is no reset; and a report that cannot be encoded
// is left out of computed, which is sent all the same.
func TestResetUnitsAndAnUnencodableReport(t *testing.T) {
	out := writeLog(t, `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1792880000000,"rateLimitType":"five_hour"}}`,
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":99999999999999999,"rateLimitType":"seven_day"}}`)
	m := measureClaude(filepath.Join(out, "log.txt"))
	if len(m.Pools) != 2 || m.Pools[0].ResetsAt == nil || m.Pools[0].ResetsAt.Unix() != 1792880000 || m.Pools[1].ResetsAt != nil {
		t.Fatalf("pools = %+v", m.Pools)
	}
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"computed":true}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd"}
	beyond := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	exit := 0
	refused, err := c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done",
		Measured{Pools: []PoolReading{{Pool: "claude:owner:short", Limited: true, ResetsAt: &beyond}}})
	if err != nil || !strings.HasPrefix(refused, "usage not sent") || len(bodies) != 1 || bodies[0]["pools"] != nil {
		t.Fatalf("refused %q, err %v, requests %v", refused, err, bodies)
	}
}

// Review 274 A3: what the executor may leave in place of a file it reads is
// never waited on, followed out, or read past its bound.
func TestMeasureOpensOnlyRegularFiles(t *testing.T) {
	const thread = "01a15031-53fc-7183-bce4-1bc9fbf576fb"
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "10", "18")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(day, "rollout-x-"+thread+".jsonl")
	if err := syscall.Mkfifo(session, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan []PoolReading, 1)
	go func() { done <- codexLimits(home, thread) }()
	select {
	case r := <-done:
		if r != nil {
			t.Fatalf("a FIFO read as %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO session file waits")
	}
	// a session file that links out of the sessions directory
	_ = os.Remove(session)
	outside := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if err := os.WriteFile(outside, []byte(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":50,"window_minutes":10080,"resets_at":1792874442}}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, session); err != nil {
		t.Fatal(err)
	}
	if r := codexLimits(home, thread); r != nil {
		t.Fatalf("a link out of the sessions directory read as %+v", r)
	}
	// the executor's log replaced by a FIFO, or by a link
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "log.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	mdone := make(chan Measured, 1)
	go func() { mdone <- measure("exec:cursor", dir) }()
	select {
	case m := <-mdone:
		if !m.empty() {
			t.Fatalf("a FIFO log measured %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("measuring a FIFO log waits")
	}
	if err := streamResultReport("cursor", "", dir); err == nil {
		t.Fatal("a FIFO log gave a report")
	}
	linked := t.TempDir()
	real := writeLog(t, `{"type":"result","is_error":false,"result":"ok","usage":{"inputTokens":5,"outputTokens":5}}`)
	if err := os.Symlink(filepath.Join(real, "log.txt"), filepath.Join(linked, "log.txt")); err != nil {
		t.Fatal(err)
	}
	if m := measure("exec:cursor", linked); !m.empty() {
		t.Fatalf("a linked log measured %+v", m)
	}
}

// Review 274 B8: cursor's refusal is its own line on a run that did not
// succeed; a line quoting the words, before a successful result, is not.
func TestCursorQuotingTheRefusalIsNoRefusal(t *testing.T) {
	out := writeLog(t, `warning: fixture example says You've hit your usage limit; continuing normally`,
		`ActionRequiredError: You've hit your usage limit Get Cursor Pro for more Agent usage, unlimited Tab, and more.`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"inputTokens":1,"outputTokens":1}}`)
	if m := measureCursor(filepath.Join(out, "log.txt")); len(m.Pools) != 0 {
		t.Fatalf("a successful run read as refused: %+v", m.Pools)
	}
	out = writeLog(t, `warning: fixture example says You've hit your usage limit; continuing normally`)
	if m := measureCursor(filepath.Join(out, "log.txt")); len(m.Pools) != 0 {
		t.Fatalf("a quoting line read as refused: %+v", m.Pools)
	}
}

// Review 275 B9: a log past the read bound keeps its thread id, read from its
// start, and the session file's spent window still cools.
func TestALongCodexLogKeepsItsThread(t *testing.T) {
	const thread = "01a15031-53fc-7183-bce4-1bc9fbf576fb"
	old := maxMeasured
	maxMeasured = 4 << 10
	t.Cleanup(func() { maxMeasured = old })
	out := writeLog(t, `{"type":"thread.started","thread_id":"`+thread+`"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"`+strings.Repeat("x", 8<<10)+`"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":2}}`)
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "10", "18")
	if err := os.MkdirAll(day, 0o700); err != nil {
		t.Fatal(err)
	}
	session := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":100.0,"window_minutes":300,"resets_at":1792874442},"rate_limit_reached_type":"primary"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(day, "rollout-x-"+thread+".jsonl"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	m := measureCodex(filepath.Join(out, "log.txt"), home)
	if len(m.Pools) != 1 || m.Pools[0].Pool != "codex:owner:short" || *m.Pools[0].UsedFraction != 1 || m.Usage == nil || *m.Usage.Tokens != 12 {
		t.Fatalf("measured %+v %+v", m.Usage, m.Pools)
	}
}

// A plan's follow-up is a second run in the same log: claude's and cursor's
// usage is that of every result, not the last's alone.
func TestMeasureAddsUpEveryResult(t *testing.T) {
	out := writeLog(t, `{"type":"result","is_error":false,"result":"plan","total_cost_usd":0.5,"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"output_tokens":1000}}`,
		"agentd: the plan's report had no block that reads",
		`{"type":"result","is_error":false,"result":"block","total_cost_usd":0.25,"usage":{"input_tokens":1,"cache_creation_input_tokens":2,"output_tokens":3}}`)
	m := measureClaude(filepath.Join(out, "log.txt"))
	if m.Usage == nil || *m.Usage.Tokens != 1116 || m.Usage.USD == nil || *m.Usage.USD != 0.75 {
		t.Fatalf("claude = %+v", m.Usage)
	}
	out = writeLog(t, `{"type":"result","is_error":false,"result":"plan","usage":{"inputTokens":10,"outputTokens":20,"cacheWriteTokens":30}}`,
		`{"type":"result","is_error":false,"result":"block","usage":{"inputTokens":1,"outputTokens":2,"cacheWriteTokens":3}}`)
	m = measureCursor(filepath.Join(out, "log.txt"))
	if m.Usage == nil || *m.Usage.Tokens != 66 {
		t.Fatalf("cursor = %+v", m.Usage)
	}
}
