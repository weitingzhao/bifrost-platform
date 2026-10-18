package agentd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
)

const checkout = "/home/bifrost-agentd-stg/agent-work/agentd/tasks/t_X/a1"

func tailOf(t *testing.T, executor string) (*actionTail, func(string)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	write := func(s string) {
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	return newActionTail(executor, path, checkout), write
}

// readTake reads what the log gained and takes it, as the collector and the
// keeper do between them.
func readTake(tail *actionTail) []actions.Action {
	tail.readNow(time.Now())
	return tail.take(50, time.Now())
}

func kinds(acts []actions.Action) string {
	var b []string
	for _, a := range acts {
		s := string(a.Kind) + " " + a.Target
		if a.Code != nil {
			s += " =" + string(rune('0'+*a.Code))
		}
		b = append(b, s)
	}
	return strings.Join(b, " | ")
}

// The shape measured on agent-06-stg (codex-cli 0.163, 2026-10-17): commands
// started and completed, file changes; messages are not actions.
func TestCodexActions(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	write(`{"type":"thread.started","thread_id":"x"}` + "\n")
	write(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I'll read the helper first."}}` + "\n")
	write(`{"type":"item.completed","item":{"id":"item_9","type":"reasoning","text":"thinking"}}` + "\n")
	write(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc \"cd ` + checkout + ` && go vet ./...\"","exit_code":null,"status":"in_progress"}}` + "\n")
	write(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc \"cd ` + checkout + ` && go vet ./...\"","aggregated_output":"ok","exit_code":1,"status":"failed"}}` + "\n")
	write(`{"type":"item.completed","item":{"id":"item_5","type":"file_change","changes":[{"path":"` + checkout + `/console/src/x.test.ts","kind":"add"}],"status":"completed"}}` + "\n")
	got := readTake(tail)
	want := `run /bin/bash -lc "cd . && go vet ./..." | result /bin/bash -lc "cd . && go vet ./..." =1 | edit console/src/x.test.ts`
	if kinds(got) != want {
		t.Fatalf("got  %s\nwant %s", kinds(got), want)
	}
	for _, a := range got {
		if strings.Contains(a.Target, "/home/") {
			t.Fatalf("a local path left: %q", a.Target)
		}
	}
	if again := readTake(tail); again != nil {
		t.Fatalf("taken twice: %v", again)
	}
	if _, _, err := actions.Normalize(got, time.Now()); err != nil {
		t.Fatalf("not in the platform's format: %v", err)
	}
}

func TestClaudeActions(t *testing.T) {
	tail, write := tailOf(t, "exec:claude")
	write(`{"type":"system","subtype":"init"}` + "\n")
	write(`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me look."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"` + checkout + `/api/a.go"}},{"type":"tool_use","id":"t2","name":"Grep","input":{"pattern":"Recover","path":"` + checkout + `/api"}}]}}` + "\n")
	write(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"package a"}]}}` + "\n")
	write(`{"type":"result","subtype":"success","result":"done"}` + "\n")
	if got, want := kinds(readTake(tail)), "read api/a.go | run Grep Recover ./api"; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestCursorActions(t *testing.T) {
	tail, write := tailOf(t, "exec:cursor")
	write(`{"type":"assistant","message":{"content":[{"type":"text","text":"reviewing"}]}}` + "\n")
	write(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"readToolCall":{"args":{"path":"` + checkout + `/api/a.go"}}}}` + "\n")
	write(`{"type":"tool_call","subtype":"started","call_id":"c2","tool_call":{"shellToolCall":{"args":{"command":"go test ./..."}}}}` + "\n")
	write(`{"type":"tool_call","subtype":"completed","call_id":"c2","tool_call":{"shellToolCall":{"args":{"command":"go test ./..."},"result":{"success":{"exitCode":0}}}}}` + "\n")
	write(`{"type":"tool_call","subtype":"started","call_id":"c3","tool_call":{"writeToolCall":{"args":{"path":"` + checkout + `/b.go","fileText":"x"}}}}` + "\n")
	write(`{"type":"tool_call","subtype":"started","call_id":"c4","tool_call":{"someNewToolCall":{"args":{"q":"x"}}}}` + "\n")
	if got, want := kinds(readTake(tail)), "read api/a.go | run go test ./... | result go test ./... =0 | edit b.go"; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// A line counts only once it is whole; a line over maxBody is skipped, the
// next one read; only the newest max are taken and at most maxPending kept.
func TestTailReadsWholeLinesOnly(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	line := `{"type":"item.started","item":{"id":"i","type":"command_execution","command":"ls"}}`
	write(line[:30])
	if got := readTake(tail); got != nil {
		t.Fatalf("half a line: %v", got)
	}
	write(line[30:] + "\n")
	if got := kinds(readTake(tail)); got != "run ls" {
		t.Fatalf("the whole line: %q", got)
	}
	write(`{"type":"item.started","item":{"type":"command_execution","command":"` + strings.Repeat("x", maxBody) + `"}}` + "\n")
	write(line + "\n")
	if got := kinds(readTake(tail)); got != "run ls" {
		t.Fatalf("after a long line: %q", got)
	}
	for i := 0; i < maxPending+30; i++ {
		write(`{"type":"item.started","item":{"type":"command_execution","command":"c` + strings.Repeat("y", i%5) + `"}}` + "\n")
	}
	got := readTake(tail)
	if len(got) != 50 || got[49].Target != "c"+strings.Repeat("y", (maxPending+29)%5) {
		t.Fatalf("took %d, the last %q", len(got), got[len(got)-1].Target)
	}
	if readTake(tail) != nil {
		t.Fatal("the older ones were kept after a take")
	}
}

func TestNoTailForOtherExecutors(t *testing.T) {
	if tail := newActionTail("exec:deterministic", "/x/log.txt", checkout); tail != nil {
		t.Fatal("a deterministic run has a tail")
	}
	var none *actionTail
	none.readNow(time.Now())
	if none.take(50, time.Now()) != nil {
		t.Fatal("a nil tail gave actions")
	}
}

// What must not leave the host is masked before it is queued (review 265 A8).
func TestTargetsAreScrubbed(t *testing.T) {
	for in, want := range map[string]string{
		"rg --files /home/bifrost-agentd-stg/agent-work/other":                "rg --files <local path>",
		"cat /Users/someone/.config/x":                                        "cat <local path>",
		`type C:\\Users\\x\\secret.txt`:                                       "type <local path>",
		"TOKEN=fixture-secret curl https://example.invalid":                   "TOKEN=<secret> curl https://example.invalid",
		"curl -H 'Authorization: Bearer abcdefghijklmnop' https://x":          "curl -H 'Authorization: <secret>' https://x",
		"git clone https://bot:fixture-pass@gitea.local/x.git":                "git clone https://<secret>@gitea.local/x.git",
		"echo github_pat_ABCDEFGHIJKLMNOPQRSTUV":                              "echo github_pat_<secret>",
		"mysql --password=hunter22 -e 1":                                      "mysql --password=<secret> -e 1",
		"export GITEA_ADMIN_TOKEN='0123456789abcdef0123456789abcdef01234567'": "export GITEA_ADMIN_TOKEN=<secret>",
		"go test ./internal/agentd/...":                                       "go test ./internal/agentd/...",
	} {
		if got := scrub(in); got != want {
			t.Errorf("scrub(%q) = %q, want %q", in, got, want)
		}
	}
}

// A target is cut to the platform's limit and a batch to maxBatchBytes, so a
// heartbeat is never refused for its size (review 265 A9).
func TestBatchesStaySmall(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	for i := 0; i < 2; i++ {
		write(`{"type":"item.started","item":{"type":"command_execution","command":"echo ` + strings.Repeat("x", 600000) + `"}}` + "\n")
	}
	for i := 0; i < 300; i++ {
		write(`{"type":"item.started","item":{"type":"command_execution","command":"` + strings.Repeat("é", 500) + `"}}` + "\n")
	}
	got := readTake(tail)
	size := 0
	for _, a := range got {
		size += len(a.Target)
		if len([]rune(a.Target)) > actions.MaxTarget || (len([]rune(a.Target)) == actions.MaxTarget && !a.Truncated) {
			t.Fatalf("a target of %d runes, truncated %v", len([]rune(a.Target)), a.Truncated)
		}
	}
	if size > maxBatchBytes || len(got) == 0 {
		t.Fatalf("%d actions, %d bytes of targets", len(got), size)
	}
}

// A log that is not a regular file is not read, and reading never blocks the
// keeper's take (review 265 A10).
func TestAFIFOIsNotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	tail := newActionTail("exec:codex", path, checkout)
	done := make(chan struct{})
	go func() { tail.readNow(time.Now()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
	if tail.take(50, time.Now()) != nil {
		t.Fatal("actions from a FIFO")
	}
}

// Cursor's table of started commands is bounded (review 265 B11).
func TestCursorCommandTableIsBounded(t *testing.T) {
	tail, write := tailOf(t, "exec:cursor")
	for i := 0; i < 1000; i++ {
		write(`{"type":"tool_call","subtype":"started","call_id":"c` + strings.Repeat("1", i%7) + string(rune('a'+i%26)) + strings.Repeat("z", i/26) + `","tool_call":{"shellToolCall":{"args":{"command":"ls"}}}}` + "\n")
	}
	write(`{"type":"tool_call","subtype":"started","call_id":"","tool_call":{"shellToolCall":{"args":{"command":"ls"}}}}` + "\n")
	tail.readNow(time.Now())
	if n := len(tail.cmds); n > maxCmds {
		t.Fatalf("%d commands waiting", n)
	}
	if _, ok := tail.cmds[""]; ok {
		t.Fatal("an empty call id was kept")
	}
}

// The collector stops with its context.
func TestCollectorStops(t *testing.T) {
	tail, _ := tailOf(t, "exec:codex")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tail.collect(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the collector did not stop")
	}
}

// A platform from before 2.3 refuses a heartbeat with actions (400, unknown
// field): agentd sends it again without them at once and says the platform
// takes none, so the lease never depends on actions.
func TestHeartbeatToAnOlderPlatform(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		if _, has := body["actions"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \\"actions\\""}`))
			return
		}
		_, _ = w.Write([]byte(`{"lease_expires_at":"2026-10-18T07:00:00Z","stop":false}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd"}
	acts := []actions.Action{{Kind: actions.Read, Target: "a.go", At: time.Now().UTC()}}
	stop, expires, refused, err := c.Heartbeat(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, acts)
	if err != nil || stop || expires.IsZero() || !strings.HasPrefix(refused, ActionsUnsupported) {
		t.Fatalf("stop %v, expires %v, refused %q, err %v", stop, expires, refused, err)
	}
	if len(bodies) != 2 || bodies[1]["actions"] != nil || bodies[1]["lease_epoch"] != float64(7) {
		t.Fatalf("requests %v", bodies)
	}
	bodies = nil
	if _, _, refused, err := c.Heartbeat(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, nil); err != nil || refused != "" || len(bodies) != 1 {
		t.Fatalf("a heartbeat without actions: refused %q, err %v, %d requests", refused, err, len(bodies))
	}
}
