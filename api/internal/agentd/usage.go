package agentd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// What an attempt used and what its vendor said about its quota (W-31 2.4
// slice ①), read from the executor's output once it has ended and sent with
// computed. Pools are named tool:account:window; every vendor login on a host
// is the Owner's today, so the account is "owner" (Owner 2026-10-18
// 「账号写进池名」).
//
// Tokens leave out cache reads, which the vendors count apart and which cost
// little: codex input less cached input, plus output; cursor input, output
// and cache writes; claude input, cache writes and output. USD is claude's own
// estimate (total_cost_usd).

const account = "owner"

// Usage is what one attempt used.
type Usage struct {
	Pool   string   `json:"pool,omitempty"`
	Tokens *int64   `json:"tokens,omitempty"`
	USD    *float64 `json:"usd,omitempty"`
}

// PoolReading is what the vendor's output said about one quota window.
type PoolReading struct {
	Pool         string     `json:"pool"`
	UsedFraction *float64   `json:"used_fraction,omitempty"`
	ResetsAt     *time.Time `json:"resets_at,omitempty"`
	Limited      bool       `json:"limited,omitempty"`
	Text         string     `json:"text,omitempty"`
}

// Measured is an attempt's usage and pool readings.
type Measured struct {
	Usage *Usage
	Pools []PoolReading
}

func (m Measured) empty() bool { return m.Usage == nil && len(m.Pools) == 0 }

func pool(tool, window string) string { return tool + ":" + account + ":" + window }

// measure reads what the executor of this attempt used from its output in
// outDir. An executor it does not know, or output it cannot read, measures
// nothing: an ending never waits on it.
func measure(executor, outDir string) Measured {
	logPath := filepath.Join(outDir, "log.txt")
	switch executor {
	case "exec:codex":
		// The attempt's own codex home (TD-371); an attempt from before it
		// had one ran with the host's.
		home := codexHomeOf(outDir)
		if fi, err := os.Lstat(home); err != nil || !fi.IsDir() {
			home = codexHome()
		}
		return measureCodex(logPath, home)
	case "exec:cursor":
		return measureCursor(logPath)
	case "exec:claude":
		return measureClaude(logPath)
	}
	return Measured{}
}

// maxMeasured bounds what measure reads of one file; past it only the end is
// read (the last turns and the result). A variable so a test can lower it.
var maxMeasured int64 = 64 << 20

// maxHead bounds the start of a log read for what it says first (codex's
// thread id), when the log is past maxMeasured (review 275 B9).
const maxHead = 1 << 20

// openRegular opens a regular file for reading and nothing else: not through
// a symbolic link, and without waiting on a FIFO or a device the executor may
// have left in its place (review 274 A3). A file over max is read from its
// last max bytes on.
func openRegular(path string, max int64) (io.ReadCloser, error) { return openBounded(path, max, true) }

// openBounded is openRegular reading the first max bytes when tail is false.
func openBounded(path string, max int64, tail bool) (io.ReadCloser, error) {
	return openFrom(path, 0, max, tail)
}

// openRegularFrom is openRegular reading from byte from on, or from the
// last max bytes when they start later.
func openRegularFrom(path string, from, max int64) (io.ReadCloser, error) {
	return openFrom(path, from, max, true)
}

func openFrom(path string, from, max int64, tail bool) (io.ReadCloser, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("agentd: %s is not a regular file", path)
	}
	if tail && st.Size()-max > from {
		from = st.Size() - max
	}
	if from > 0 {
		if _, err = f.Seek(from, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(f, max), f}, nil
}

func eachJSON(logPath string, fn func(line []byte, ev map[string]any)) bool {
	f, err := openRegular(logPath, maxMeasured)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return eachLine(f, maxBody, func(line []byte) {
		var ev map[string]any
		if json.Unmarshal(line, &ev) != nil {
			ev = nil
		}
		fn(line, ev)
	}) == nil
}

func num(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok && n >= 0
}

func sumOf(m map[string]any, keys ...string) int64 {
	var n float64
	for _, k := range keys {
		if v, ok := num(m[k]); ok {
			n += v
		}
	}
	return int64(n)
}

// unixTime is a reset the vendor gave in Unix seconds or milliseconds, or nil
// when it is neither (review 274 A1: a millisecond value read as seconds is
// past year 9999, and cannot even be sent).
func unixTime(v any) *time.Time {
	n, ok := num(v)
	switch {
	case !ok:
		return nil
	case n >= 1e12 && n < 1e13:
		n /= 1000
	case n < 1e9 || n >= 1e10:
		return nil
	}
	t := time.Unix(int64(n), 0).UTC()
	return &t
}

// windowOf is the kind of a window by its length in minutes.
func windowOf(minutes float64) string {
	switch {
	case minutes > 0 && minutes <= 24*60:
		return "short"
	case minutes > 24*60 && minutes <= 10*24*60:
		return "weekly"
	case minutes > 10*24*60:
		return "monthly"
	}
	return ""
}

// The refusals, as each vendor words them: codex and cursor "You've hit your
// usage limit" (cursor's measured 10-18); claude "… limit reached" or "You've
// hit your limit". Narrow on purpose: a line that merely mentions a limit must
// not cool a pool for hours.
var (
	usageLimit  = regexp.MustCompile(`(?i)hit your usage limit|usage limit reached`)
	claudeLimit = regexp.MustCompile(`(?i)usage limit reached|limit reached\b|hit your (session |weekly |usage )?limit`)
)

// refusalText is a vendor's refusal as reported: on one line, scrubbed, short.
func refusalText(s string) string { return oneLine(scrub(s), 200) }

func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// measureCodex: codex exec --json says thread.started (its session id) and
// turn.completed with each turn's usage (measured 10-18, codex 0.162); a
// refusal is an error or turn.failed event. The session file it keeps under
// CODEX_HOME carries its rate limits: for each window the percent used, its
// length and its reset (rate_limits in token_count events, measured 10-18).
func measureCodex(logPath, home string) Measured {
	var thread, refusal string
	var tokens int64
	var turns bool
	if !eachJSON(logPath, func(_ []byte, ev map[string]any) {
		switch ev["type"] {
		case "thread.started":
			thread, _ = ev["thread_id"].(string)
		case "turn.completed":
			u := obj(ev["usage"])
			tokens += sumOf(u, "input_tokens", "output_tokens") - sumOf(u, "cached_input_tokens")
			turns = true
		case "error":
			if msg, _ := ev["message"].(string); usageLimit.MatchString(msg) {
				refusal = msg
			}
		case "turn.failed":
			if msg, _ := obj(ev["error"])["message"].(string); usageLimit.MatchString(msg) {
				refusal = msg
			}
		}
	}) {
		return Measured{}
	}
	if thread == "" {
		thread = codexThread(logPath)
	}
	m := Measured{Pools: codexLimits(home, thread)}
	main := pool("codex", "weekly")
	for _, r := range m.Pools {
		if strings.HasSuffix(r.Pool, ":short") {
			continue
		}
		main = r.Pool
	}
	if turns {
		if tokens < 0 {
			tokens = 0
		}
		m.Usage = &Usage{Pool: main, Tokens: &tokens}
	}
	if refusal != "" {
		limited := false
		for i := range m.Pools {
			if m.Pools[i].UsedFraction != nil && *m.Pools[i].UsedFraction >= 1 {
				m.Pools[i].Limited, m.Pools[i].Text, limited = true, refusalText(refusal), true
			}
		}
		if !limited {
			m.Pools = append(m.Pools, PoolReading{Pool: main, Limited: true, Text: refusalText(refusal)})
		}
	}
	return m
}

// codexThread is the thread id from the start of a log too long to be read
// whole: thread.started is its first event.
func codexThread(logPath string) string {
	f, err := openBounded(logPath, maxHead, false)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var thread string
	_ = eachLine(f, maxHead, func(line []byte) {
		var ev map[string]any
		if thread == "" && json.Unmarshal(line, &ev) == nil && ev["type"] == "thread.started" {
			thread, _ = ev["thread_id"].(string)
		}
	})
	return thread
}

var threadID = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// codexLimits reads the last rate limits in the session file of thread.
func codexLimits(home, thread string) []PoolReading {
	if home == "" || !threadID.MatchString(thread) {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+thread+".jsonl"))
	if len(files) != 1 {
		return nil
	}
	// inside the sessions directory, whatever links lead there (review 274 A3)
	root, err := filepath.EvalSymlinks(filepath.Join(home, "sessions"))
	if err != nil {
		return nil
	}
	if real, err := filepath.EvalSymlinks(files[0]); err != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return nil
	}
	var last map[string]any
	eachJSON(files[0], func(_ []byte, ev map[string]any) {
		if rl := obj(obj(ev["payload"])["rate_limits"]); rl != nil {
			last = rl
		}
	})
	if last == nil {
		return nil
	}
	reached := last["rate_limit_reached_type"] != nil
	var out []PoolReading
	for _, k := range []string{"primary", "secondary"} {
		w := obj(last[k])
		minutes, _ := num(w["window_minutes"])
		window := windowOf(minutes)
		if window == "" {
			continue
		}
		r := PoolReading{Pool: pool("codex", window)}
		if pct, ok := num(w["used_percent"]); ok {
			f := pct / 100
			if f > 1 {
				f = 1
			}
			r.UsedFraction = &f
			r.Limited = reached && pct >= 100
		}
		r.ResetsAt = unixTime(w["resets_at"])
		out = append(out, r)
	}
	return out
}

// measureCursor: cursor-agent's result event carries usage (inputTokens,
// outputTokens, cacheReadTokens, cacheWriteTokens; measured 10-12), added
// up over every result of the log (a plan's follow-up is a second turn,
// planfix.go). Out of
// quota it writes "ActionRequiredError: You've hit your usage limit …" to
// stderr, names no reset and exits 1 with no result (measured 10-18). Only
// that line is a refusal, and not when a result says the run succeeded: a
// line that quotes the words is not (review 274 B8).
var cursorRefusal = regexp.MustCompile(`^ActionRequiredError: You've hit your usage limit`)

func measureCursor(logPath string) Measured {
	var m Measured
	var refusal string
	succeeded := false
	if !eachJSON(logPath, func(line []byte, ev map[string]any) {
		if ev == nil {
			if s := strings.TrimSpace(string(line)); cursorRefusal.MatchString(s) {
				refusal = s
			}
			return
		}
		if ev["type"] == "result" {
			if failed, _ := ev["is_error"].(bool); !failed {
				succeeded = true
			}
			if u := obj(ev["usage"]); u != nil {
				if m.Usage == nil {
					m.Usage = &Usage{Pool: pool("cursor", "monthly"), Tokens: new(int64)}
				}
				*m.Usage.Tokens += sumOf(u, "inputTokens", "outputTokens", "cacheWriteTokens")
			}
		}
	}) {
		return Measured{}
	}
	if refusal != "" && !succeeded {
		m.Pools = []PoolReading{{Pool: pool("cursor", "monthly"), Limited: true, Text: refusalText(refusal)}}
	}
	return m
}

// claudeEpoch: an older claude says when the limit resets as
// "Claude AI usage limit reached|<unix seconds>".
var claudeEpoch = regexp.MustCompile(`limit reached\|(\d{9,11})`)

// measureClaude: claude's result event carries usage and total_cost_usd
// (measured 10-18, claude 2.1.302), added up over every result of the log
// (a plan's follow-up resumes the session in a second run, planfix.go). Its rate-limit events
// (rate_limit_event: status, resetsAt, rateLimitType, utilization) and its
// refusal text (an assistant message with error rate_limit) were not seen
// yet: read as documented, to be checked on the first real one.
//
// Only five_hour (the short pool) and seven_day (the weekly pool) are the
// account's own windows; the latest event of each is its reading. A model's
// own weekly limit (seven_day_opus, seven_day_sonnet) is not the account's
// weekly window: its utilization reads no pool, or one model near its limit
// would hold back all claude work (PROD 11-02: the weekly pool read 99% 45
// minutes after its reset). Its refusal is a refusal all the same, of the
// weekly pool until that model's limit resets (modelRefusal).
func measureClaude(logPath string) Measured {
	var m Measured
	var refusal, message string
	var weekly, wentOn bool
	readings := map[string]PoolReading{}
	models := map[string]PoolReading{} // the latest event of each model's own weekly limit
	if !eachJSON(logPath, func(_ []byte, ev map[string]any) {
		switch ev["type"] {
		case "result":
			u := obj(ev["usage"])
			if u != nil {
				if m.Usage == nil {
					m.Usage = &Usage{Pool: pool("claude", "weekly"), Tokens: new(int64)}
				}
				*m.Usage.Tokens += sumOf(u, "input_tokens", "cache_creation_input_tokens", "output_tokens")
				if usd, ok := num(ev["total_cost_usd"]); ok && usd > 0 {
					if m.Usage.USD == nil {
						m.Usage.USD = new(float64)
					}
					*m.Usage.USD += usd
				}
			}
			if failed, _ := ev["is_error"].(bool); failed {
				if text, _ := ev["result"].(string); claudeLimit.MatchString(text) {
					refusal = text
				}
			}
		case "assistant":
			if ev["error"] == "rate_limit" {
				message, wentOn = "rate_limit", false
				for _, c := range asList(obj(ev["message"])["content"]) {
					if text, _ := obj(c)["text"].(string); text != "" {
						message = text
					}
				}
			} else if ev["error"] == nil && ev["parent_tool_use_id"] == nil && obj(ev["message"])["model"] != "<synthetic>" {
				wentOn = true
			}
		case "rate_limit_event":
			info := obj(ev["rate_limit_info"])
			kind, _ := info["rateLimitType"].(string)
			r := PoolReading{ResetsAt: unixTime(info["resetsAt"]), Limited: info["status"] == "rejected"}
			window := ""
			switch {
			case kind == "five_hour":
				window = "short"
			case kind == "seven_day":
				window, weekly = "weekly", true
			case strings.HasPrefix(kind, "seven_day_"):
				r.Pool, r.Text = pool("claude", "weekly"), refusalText(kind+" limit")
				models[kind] = r
				return
			default:
				return
			}
			r.Pool = pool("claude", window)
			if u, ok := num(info["utilization"]); ok && u <= 1 {
				r.UsedFraction = &u
			}
			readings[window] = r
		}
	}) {
		return Measured{}
	}
	// A rate_limit message refuses the run unless the run went on past it:
	// the main thread's model answered after the message (claude fell back
	// to another model), however the run then ended (a killed run has no
	// result). Otherwise one model's limit would cool all claude work until
	// that model's reset (review of 823038ee). A subagent's answer, a
	// synthetic message or an error is no answer. The result's words do not
	// decide it: a refused run's result repeats the message in words the
	// pattern may not know, and a run that went on may quote them. A result
	// that failed with the limit's words is a refusal of its own and stands.
	if refusal == "" && message != "" && !wentOn {
		refusal = message
	}
	overcame := message != "" && wentOn
	// A model's refusal holds the weekly pool until that model's limit
	// resets, keeping the account's own weekly reading of the same run; a
	// refusal of the account's own weekly window stands as it is. Only a run
	// that was refused counts: a run that went on (on another model) must not
	// cool the whole account for that model's week, and a later refusal in a
	// run that went on past the model's message is not that model's.
	if r, ok := modelRefusal(models); ok && refusal != "" && !overcame {
		if w, read := readings["weekly"]; !read || !w.Limited {
			r.UsedFraction = w.UsedFraction
			readings["weekly"] = r
		}
	}
	for _, w := range []string{"short", "weekly"} {
		if r, ok := readings[w]; ok {
			m.Pools = append(m.Pools, r)
		}
	}
	if refusal != "" {
		limited := false
		for i := range m.Pools {
			if m.Pools[i].Limited {
				m.Pools[i].Text, limited = refusalText(refusal), true
			}
		}
		if !limited {
			r := PoolReading{Pool: pool("claude", "short"), Limited: true, Text: refusalText(refusal)}
			if weekly || strings.Contains(strings.ToLower(refusal), "weekly") {
				r.Pool = pool("claude", "weekly")
			}
			if s := claudeEpoch.FindStringSubmatch(refusal); s != nil {
				if n, err := strconv.ParseFloat(s[1], 64); err == nil {
					r.ResetsAt = unixTime(n)
				}
			}
			m.Pools = append(m.Pools, r)
		}
	}
	return m
}

// modelRefusal is the refusal among the latest events of the models' own
// weekly limits: of several, the one that resets last (a known reset over
// none).
func modelRefusal(models map[string]PoolReading) (PoolReading, bool) {
	var out PoolReading
	for _, kind := range slices.Sorted(maps.Keys(models)) {
		r := models[kind]
		if r.Limited && (!out.Limited || out.ResetsAt == nil || r.ResetsAt != nil && r.ResetsAt.After(*out.ResetsAt)) {
			out = r
		}
	}
	return out, out.Limited
}

func asList(v any) []any { l, _ := v.([]any); return l }
