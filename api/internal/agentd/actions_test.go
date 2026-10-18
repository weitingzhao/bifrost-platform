package agentd

import (
	"os"
	"path/filepath"
	"strings"
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
	got := tail.take(50, time.Now())
	want := `run /bin/bash -lc "cd . && go vet ./..." | result /bin/bash -lc "cd . && go vet ./..." =1 | edit console/src/x.test.ts`
	if kinds(got) != want {
		t.Fatalf("got  %s\nwant %s", kinds(got), want)
	}
	for _, a := range got {
		if strings.Contains(a.Target, "/home/") {
			t.Fatalf("a local path left: %q", a.Target)
		}
	}
	if again := tail.take(50, time.Now()); again != nil {
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
	if got, want := kinds(tail.take(50, time.Now())), "read api/a.go | run Grep Recover ./api"; got != want {
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
	if got, want := kinds(tail.take(50, time.Now())), "read api/a.go | run go test ./... | result go test ./... =0 | edit b.go"; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// A line counts only once it is whole; a line over maxBody is skipped, the
// next one read; only the newest max are taken and at most maxPending kept.
func TestTailReadsWholeLinesOnly(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	line := `{"type":"item.started","item":{"id":"i","type":"command_execution","command":"ls"}}`
	write(line[:30])
	if got := tail.take(50, time.Now()); got != nil {
		t.Fatalf("half a line: %v", got)
	}
	write(line[30:] + "\n")
	if got := kinds(tail.take(50, time.Now())); got != "run ls" {
		t.Fatalf("the whole line: %q", got)
	}
	write(`{"type":"item.started","item":{"type":"command_execution","command":"` + strings.Repeat("x", maxBody) + `"}}` + "\n")
	write(line + "\n")
	if got := kinds(tail.take(50, time.Now())); got != "run ls" {
		t.Fatalf("after a long line: %q", got)
	}
	for i := 0; i < maxPending+30; i++ {
		write(`{"type":"item.started","item":{"type":"command_execution","command":"c` + strings.Repeat("y", i%5) + `"}}` + "\n")
	}
	got := tail.take(50, time.Now())
	if len(got) != 50 || got[49].Target != "c"+strings.Repeat("y", (maxPending+29)%5) {
		t.Fatalf("took %d, the last %q", len(got), got[len(got)-1].Target)
	}
	if tail.take(50, time.Now()) != nil {
		t.Fatal("the older ones were kept after a take")
	}
}

func TestNoTailForOtherExecutors(t *testing.T) {
	if tail := newActionTail("exec:deterministic", "/x/log.txt", checkout); tail != nil {
		t.Fatal("a deterministic run has a tail")
	}
	var none *actionTail
	if none.take(50, time.Now()) != nil {
		t.Fatal("a nil tail gave actions")
	}
}
