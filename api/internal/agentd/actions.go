package agentd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
)

// actionTail turns what an executor writes to its log (log.txt, one JSON
// event per line) into actions for the heartbeat (W-31 2.3): files read and
// edited, commands run and their exit codes. A model's messages and reasoning
// are not actions and are dropped (Owner 2026-10-16). It reads the log from
// where it stopped each time, keeps a line only once it is whole, skips a line
// longer than maxBody, and holds at most maxPending actions, the newest.
// Paths and commands are written relative to the checkout, and any other
// local path and anything that looks like a credential is masked (scrub;
// review 265 A8). A target is cut to the platform's limit here, and a
// heartbeat's batch to maxBatchBytes, so a heartbeat is never refused for its
// size (review 265 A9).
//
// The log is read by one goroutine (collect), never by the lease keeper: the
// keeper only takes what is already found, so a log that cannot be read (a
// FIFO, a slow disk) never delays a heartbeat or the keeper's end (review 265
// A10). The last read after the executor exits is collect's too (flush), so a
// read that hangs holds that one goroutine and nothing more (review 266 B3).
// Only a regular file is read: a read of a regular file cannot be cut short,
// and agent-work is on the host's own disk.
type actionTail struct {
	io      sync.Mutex // reading and parsing the log
	mu      sync.Mutex // pending
	flushes chan chan struct{}
	gone    chan struct{} // closed when collect returns
	path    string
	dir     string
	parse   func(t *actionTail, ev map[string]any, now time.Time)
	off     int64
	partial []byte
	skip    bool // inside a line longer than maxBody
	pending []actions.Action
	cmds    map[string]string // a call or item id → its command, for its result
}

const (
	maxPending    = 200
	maxReadAtOne  = 4 << 20
	maxCmds       = 256      // cursor's started commands waiting for their result (review 265 B11)
	maxBatchBytes = 64 << 10 // the targets of one heartbeat's actions
	collectEvery  = 2 * time.Second
)

// newActionTail is the tail of an executor's log, or nil for an executor
// whose output is not one of the three vendors' streams.
func newActionTail(executor, logPath, checkout string) *actionTail {
	parse := map[string]func(*actionTail, map[string]any, time.Time){
		"exec:codex":  parseCodex,
		"exec:cursor": parseCursor,
		"exec:claude": parseClaude,
	}[executor]
	if parse == nil {
		return nil
	}
	return &actionTail{path: logPath, dir: strings.TrimRight(checkout, "/"), parse: parse, cmds: map[string]string{},
		flushes: make(chan chan struct{}), gone: make(chan struct{})}
}

// collect reads the log every collectEvery, and when flush asks, until ctx
// ends. It runs on its own: nothing waits for it but a flush, and a flush
// only until its own context ends.
func (t *actionTail) collect(ctx context.Context) {
	defer close(t.gone)
	tick := time.NewTicker(collectEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.readIf(ctx, time.Now())
		case done := <-t.flushes:
			t.readIf(ctx, time.Now())
			close(done)
		}
	}
}

// flush has collect read what the log gained and waits for that read until
// ctx ends; it reads nothing itself (review 266 B3). It says whether the read
// was done.
func (t *actionTail) flush(ctx context.Context) bool {
	done := make(chan struct{})
	select {
	case t.flushes <- done:
	case <-t.gone:
		return false
	case <-ctx.Done():
		return false
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// readIf reads what the log gained unless ctx ended, looked at again once the
// read is its to do (review 266 B3).
func (t *actionTail) readIf(ctx context.Context, now time.Time) {
	t.io.Lock()
	defer t.io.Unlock()
	if ctx.Err() != nil {
		return
	}
	t.read(now)
}

// readNow reads what the log gained (tests; collect reads through readIf).
func (t *actionTail) readNow(now time.Time) {
	if t == nil {
		return
	}
	t.readIf(context.Background(), now)
}

// take returns the newest max actions found so far, at most maxBatchBytes of
// targets, forgetting them; it never reads the log.
func (t *actionTail) take(max int, now time.Time) []actions.Action {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.pending)
	if n == 0 {
		return nil
	}
	if n > max {
		n = max
	}
	out := append([]actions.Action(nil), t.pending[len(t.pending)-n:]...)
	t.pending = t.pending[:0]
	size := 0
	for i := len(out) - 1; i >= 0; i-- { // the newest first
		size += len(out[i].Target)
		if size > maxBatchBytes {
			out = out[i+1:]
			break
		}
	}
	return out
}

func (t *actionTail) read(now time.Time) {
	if fi, err := os.Lstat(t.path); err != nil || !fi.Mode().IsRegular() {
		return // only a regular file: a FIFO or a link is not read
	}
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxReadAtOne))
	if err != nil || len(buf) == 0 {
		return
	}
	t.off += int64(len(buf))
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			if !t.skip {
				t.partial = append(t.partial, buf...)
				if len(t.partial) > maxBody {
					t.partial, t.skip = t.partial[:0], true
				}
			}
			return
		}
		line := buf[:i]
		buf = buf[i+1:]
		if t.skip {
			t.skip = false
			continue
		}
		if len(t.partial) > 0 {
			line = append(t.partial, line...)
			t.partial = t.partial[:0]
		}
		if len(line) > maxBody {
			continue
		}
		var ev map[string]any
		if json.Unmarshal(line, &ev) == nil {
			t.parse(t, ev, now)
		}
	}
}

func (t *actionTail) add(kind actions.Kind, target string, now time.Time, code *int) {
	target = strings.TrimSpace(scrub(t.local(target)))
	if target == "" {
		return
	}
	a := actions.Action{Kind: kind, Target: target, At: now.UTC(), Code: code}
	if r := []rune(a.Target); len(r) > actions.MaxTarget {
		a.Target, a.Truncated = string(r[:actions.MaxTarget]), true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = append(t.pending, a)
	if len(t.pending) > maxPending {
		t.pending = t.pending[len(t.pending)-maxPending:]
	}
}

// secretKey is a name that holds a credential: it ends in one of these words
// (GITHUB_TOKEN, PGPASSWORD, "api_key"); a name that only contains one
// (author, TOKEN_FILE) is not one (review 266 C8).
const secretKey = `[a-z0-9_]*(?:token|secret|passw(?:or)?d|pwd|api_?key|private_?key|access_?key|secret_?key|credentials?|auth|authorization)s?`

// Masks for what must not leave the host in a target (review 265 A8, 266 A1):
// a local path outside the checkout, and values that look like credentials. A
// quoted value is masked whole, and one whose quote never closes is masked to
// the end, as is a private key whose END line is not there.
type mask struct {
	re   *regexp.Regexp
	with string
}

// quoted are masked first: a private key, and a JSON value (raw, or inside a
// double-quoted shell argument), so that what follows does not cut them.
var quoted = []mask{
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), "<secret>"},
	{regexp.MustCompile(`(?i)("` + secretKey + `"\s*:\s*)(?:"(?:[^"\\]|\\.)*"|"(?s:.*)$|[^\s,}\]"]+)`), `${1}"<secret>"`},
	{regexp.MustCompile(`(?i)(\\"` + secretKey + `\\"\s*:\s*)(?:\\"(?:[^\\]|\\[^"])*?\\"|\\"(?s:.*)$|[^\s,}\]\\]+)`), `${1}\"<secret>\"`},
	{regexp.MustCompile(`(?i)\b(bearer|token|basic)\s+[a-z0-9._~+/=-]{8,}`), "${1} <secret>"},
}

// masks follow the assignments: flags, known token prefixes, credentials in a
// URL, local paths.
var masks = []mask{
	{regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api-key|key)(?:=|\s+))(?:"[^"]*"|'[^']*'|["'](?s:.*)$|\S+)`), "${1}<secret>"},
	{regexp.MustCompile(`\b(github_pat_|ghp_|gho_|ghs_|ghr_|glpat-|xox[abprs]-|sk-|AKIA)[A-Za-z0-9_\-]{8,}`), "${1}<secret>"},
	{regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`), "${1}<secret>@"},
	{regexp.MustCompile(`(?:/home|/Users|/root|/var/folders|/private/var|/private/tmp|/tmp)/[^\s"'<>|;&)]*`), "<local path>"},
	{regexp.MustCompile(`\b[A-Za-z]:\\[^\s"'<>|;&)]*`), "<local path>"},
}

// assignments are NAME=value, NAME: value and an Authorization header; their
// value is masked unless it is a search pattern (rg 'token:.*'), which is kept
// up to its next alternative and read on from there (review 266 C8).
var assignments = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*)((?:[a-z]+\s+)?[^\s"']+)`),
	regexp.MustCompile(`(?i)\b(` + secretKey + `\s*[=:]\s*)("[^"]*"|'[^']*'|["'](?s:.*)$|[^\s"'<]+)`),
}

func scrub(s string) string {
	for _, m := range quoted {
		s = m.re.ReplaceAllString(s, m.with)
	}
	for _, re := range assignments {
		s = maskValues(re, s)
	}
	for _, m := range masks {
		s = m.re.ReplaceAllString(s, m.with)
	}
	return s
}

// maskValues masks the value (the second group) of each match of re in s.
func maskValues(re *regexp.Regexp, s string) string {
	var b strings.Builder
	for {
		m := re.FindStringSubmatchIndex(s)
		if m == nil {
			b.WriteString(s)
			return b.String()
		}
		name, val := s[m[2]:m[3]], s[m[4]:m[5]]
		b.WriteString(s[:m[2]])
		if strings.ContainsAny(val[:1], `.*+?\([^$|`) {
			// a pattern or a variable, not a credential: kept to its next
			// alternative, the rest is read again
			head, _, alt := strings.Cut(val, "|")
			b.WriteString(name + head)
			if alt {
				b.WriteString("|")
				s = s[m[4]+len(head)+1:]
			} else {
				s = s[m[5]:]
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), "authorization:") {
			b.WriteString(name + "<secret>") // a header keeps its form
		} else {
			b.WriteString(strings.TrimRight(name, " \t=:") + "=<secret>")
		}
		s = s[m[1]:]
	}
}

// local writes the checkout's path as relative: a path inside it loses the
// prefix, the checkout itself and any other mention of it become ".".
func (t *actionTail) local(s string) string {
	if t.dir == "" {
		return s
	}
	if strings.HasPrefix(s, t.dir+"/") {
		s = s[len(t.dir)+1:]
	}
	return strings.ReplaceAll(s, t.dir, ".")
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func intp(v any) *int {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int(f)
	return &n
}

// parseCodex reads `codex exec --json` (measured on agent-06-stg 2026-10-17,
// codex-cli 0.163): item.started / item.completed of a command_execution
// (command; exit_code once completed) and item.completed of a file_change
// (changes[].path).
func parseCodex(t *actionTail, ev map[string]any, now time.Time) {
	item := obj(ev["item"])
	switch typ := str(ev["type"]); {
	case str(item["type"]) == "command_execution" && typ == "item.started":
		t.add(actions.Run, str(item["command"]), now, nil)
	case str(item["type"]) == "command_execution" && typ == "item.completed":
		if code := intp(item["exit_code"]); code != nil {
			t.add(actions.Result, str(item["command"]), now, code)
		}
	case str(item["type"]) == "file_change" && typ == "item.completed":
		changes, _ := item["changes"].([]any)
		for _, c := range changes {
			t.add(actions.Edit, str(obj(c)["path"]), now, nil)
		}
	}
}

// parseClaude reads `claude -p --output-format stream-json`: the assistant's
// tool_use blocks. The executor allows Read, Grep and Glob only; Edit, Write
// and Bash are read too in case the list grows. A tool_result has no exit
// code, so Claude's commands have no result action.
func parseClaude(t *actionTail, ev map[string]any, now time.Time) {
	if str(ev["type"]) != "assistant" {
		return
	}
	content, _ := obj(ev["message"])["content"].([]any)
	for _, c := range content {
		b := obj(c)
		if str(b["type"]) != "tool_use" {
			continue
		}
		in := obj(b["input"])
		switch str(b["name"]) {
		case "Read":
			t.add(actions.Read, str(in["file_path"]), now, nil)
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			t.add(actions.Edit, firstOf(in, "file_path", "notebook_path"), now, nil)
		case "Bash":
			t.add(actions.Run, str(in["command"]), now, nil)
		case "Grep", "Glob":
			t.add(actions.Run, strings.TrimSpace(str(b["name"])+" "+str(in["pattern"])+" "+str(in["path"])), now, nil)
		}
	}
}

// parseCursor reads `cursor-agent -p --output-format stream-json`: tool_call
// events, started and completed, whose one key names the tool (readToolCall,
// writeToolCall, editToolCall, deleteToolCall, shellToolCall, …) with its args.
// Not yet measured on agent-06; a tool it does not know is skipped.
func parseCursor(t *actionTail, ev map[string]any, now time.Time) {
	if str(ev["type"]) != "tool_call" {
		return
	}
	call, sub, id := obj(ev["tool_call"]), str(ev["subtype"]), str(ev["call_id"])
	for name, v := range call {
		body := obj(v)
		args := obj(body["args"])
		tool := strings.ToLower(strings.TrimSuffix(name, "ToolCall"))
		switch {
		case sub == "started" && (tool == "read"):
			t.add(actions.Read, str(args["path"]), now, nil)
		case sub == "started" && (tool == "write" || tool == "edit" || tool == "delete" || tool == "strreplace"):
			t.add(actions.Edit, str(args["path"]), now, nil)
		case sub == "started" && (tool == "shell" || tool == "terminal" || tool == "runterminalcommand"):
			cmd := firstOf(args, "command", "commandLine")
			if id != "" { // bounded: a start whose end never comes cannot grow the table (review 265 B11)
				if len(t.cmds) >= maxCmds {
					t.cmds = map[string]string{}
				}
				if r := []rune(cmd); len(r) > actions.MaxTarget {
					cmd = string(r[:actions.MaxTarget])
				}
				t.cmds[id] = cmd
			}
			t.add(actions.Run, cmd, now, nil)
		case sub == "completed" && t.cmds[id] != "":
			res := obj(body["result"])
			code := intp(obj(res["success"])["exitCode"])
			if code == nil {
				code = intp(obj(res["failure"])["exitCode"])
			}
			if code != nil {
				t.add(actions.Result, t.cmds[id], now, code)
			}
			delete(t.cmds, id)
		}
	}
}

func firstOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}
