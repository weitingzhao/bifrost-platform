package agentd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// selfSpec is an implementation's spec as the platform writes it, with
// hand-in commands a shell line quotes and one it does not.
func selfSpec(t *testing.T) TaskSpec {
	t.Helper()
	repo := store.Repo{HandIn: [][]string{{"go", "-C", "api", "build", "./..."}, {"npm", "--prefix", "console", "run", "lint"}}}
	handIn := store.HandInChecks(repo, [][]string{{"go", "-C", "api", "test", "-run", "SelfCheck|X", "./internal/agentd/"}})
	body, err := store.ImplementSpec("title", "text", repo, [][]string{{"go", "test", "./..."}}, handIn)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSpec(body)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func codexCommand(write func(string), id, cmd string, code int) {
	started, _ := json.Marshal(map[string]any{"type": "item.started", "item": map[string]any{"id": id, "type": "command_execution", "command": cmd}})
	done, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"id": id, "type": "command_execution", "command": cmd, "exit_code": code}})
	write(string(started) + "\n" + string(done) + "\n")
}

func TestSelfCheckSpecListsHandIn(t *testing.T) {
	s := selfSpec(t)
	want := []string{"go -C api build ./...", "npm --prefix console run lint",
		"env AGENTRT_TEST_PG= AGENTRT_TEST_REDIS= AGENTRT_TEST_REQUIRED=0 go -C api test -run 'SelfCheck|X' ./internal/agentd/"}
	if fmt.Sprint(s.HandIn) != fmt.Sprint(want) {
		t.Fatalf("hand-in %q\nwant    %q", s.HandIn, want)
	}
	if m, _ := ParseSpec([]byte(`{"text":"no list here"}`)); m.HandIn != nil {
		t.Fatalf("a spec without the list has hand-in %q", m.HandIn)
	}
}

// Every result of a hand-in command counts, past what the tail keeps; the
// time from run to result is summed; a command wrapped in codex's shell, in
// the checkout, is the spec's.
func TestSelfCheckCounting(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	tail.self = newSelfCount(selfSpec(t).HandIn)
	if tail.selfCheck() != nil {
		t.Fatal("a count before any result")
	}
	start := time.Now()
	write(`{"type":"item.started","item":{"id":"b","type":"command_execution","command":"/bin/bash -lc 'go -C api build ./...'"}}` + "\n")
	tail.readNow(start)
	write(`{"type":"item.completed","item":{"id":"b","type":"command_execution","command":"/bin/bash -lc 'go -C api build ./...'","exit_code":0}}` + "\n")
	tail.readNow(start.Add(3 * time.Second))
	for i := range maxPending + 10 {
		codexCommand(write, fmt.Sprint("l", i), "/bin/bash -lc \"cd "+checkout+" && npm --prefix console run lint\"", 0)
	}
	codexCommand(write, "t", `/bin/bash -lc "env AGENTRT_TEST_PG= AGENTRT_TEST_REDIS= AGENTRT_TEST_REQUIRED=0 go -C api test -run 'SelfCheck|X' ./internal/agentd/"`, 0)
	tail.readNow(start.Add(4 * time.Second))
	got := tail.selfCheck()
	want := SelfCheck{Runs: maxPending + 12, LastGreen: true, Seconds: 3}
	if got == nil || *got != want {
		t.Fatalf("self-check %+v, want %+v", got, want)
	}
}

// A red command that later exits 0 is fixed; one still red at the end keeps
// last_green false, and a red that is fixed again counts again.
func TestSelfCheckRedThenGreenIsFixed(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	tail.self = newSelfCount(selfSpec(t).HandIn)
	codexCommand(write, "1", "go -C api build ./...", 2)
	codexCommand(write, "2", "go -C api build ./...", 0)
	codexCommand(write, "3", "npm --prefix console run lint", 1)
	tail.readNow(time.Now())
	if got, want := tail.selfCheck(), (SelfCheck{Runs: 3, Red: 2, Fixed: 1}); got == nil || *got != want {
		t.Fatalf("self-check %+v, want %+v", got, want)
	}
	codexCommand(write, "4", "npm --prefix console run lint", 1)
	codexCommand(write, "5", "npm --prefix console run lint", 0)
	tail.readNow(time.Now())
	if got, want := tail.selfCheck(), (SelfCheck{Runs: 5, Red: 3, Fixed: 2, LastGreen: true}); got == nil || *got != want {
		t.Fatalf("self-check %+v, want %+v", got, want)
	}
}

// Only the spec's hand-in commands count: another command, one that only
// starts like one, one in another directory; an executor whose actions carry
// no exit codes (claude), or a spec with no list, counts nothing.
func TestSelfCheckOnlyHandInCommands(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	tail.self = newSelfCount(selfSpec(t).HandIn)
	codexCommand(write, "1", "go -C api build ./... && rm -rf x", 1)
	codexCommand(write, "2", "go -C api vet ./...", 1)
	codexCommand(write, "3", "cd /elsewhere && go -C api build ./...", 1)
	codexCommand(write, "4", "go -C api test -run 'SelfCheck|X' ./internal/agentd/", 1) // without the env the spec prints
	tail.readNow(time.Now())
	if got := tail.selfCheck(); got != nil {
		t.Fatalf("counted %+v", got)
	}
	codexCommand(write, "5", "  go -C api build ./...  ", 0)
	tail.readNow(time.Now())
	if got := tail.selfCheck(); got == nil || got.Runs != 1 {
		t.Fatalf("self-check %+v", got)
	}

	claude, cwrite := tailOf(t, "exec:claude")
	claude.self = newSelfCount(selfSpec(t).HandIn)
	cwrite(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go -C api build ./..."}}]}}` + "\n")
	claude.readNow(time.Now())
	if got := claude.selfCheck(); got != nil {
		t.Fatalf("claude counted %+v", got)
	}

	plain, pwrite := tailOf(t, "exec:codex")
	codexCommand(pwrite, "1", "go -C api build ./...", 0)
	plain.readNow(time.Now())
	if got := plain.selfCheck(); got != nil {
		t.Fatalf("no list, counted %+v", got)
	}
}

// Cursor's results come by call id, raw as its start wrote them.
func TestSelfCheckCursor(t *testing.T) {
	tail, write := tailOf(t, "exec:cursor")
	tail.self = newSelfCount(selfSpec(t).HandIn)
	write(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"shellToolCall":{"args":{"command":"npm --prefix console run lint"}}}}` + "\n")
	write(`{"type":"tool_call","subtype":"completed","call_id":"c1","tool_call":{"shellToolCall":{"args":{},"result":{"failure":{"exitCode":1}}}}}` + "\n")
	tail.readNow(time.Now())
	if got, want := tail.selfCheck(), (SelfCheck{Runs: 1, Red: 1}); got == nil || *got != want {
		t.Fatalf("self-check %+v, want %+v", got, want)
	}
}

// A platform from before R5:5.7 refuses computed with self_check (400,
// unknown field): agentd sends it again at once without it, usage kept, and
// says so; one from before 2.4 too refuses usage next, and that is dropped as
// well.
func TestSelfCheckToAnOlderPlatform(t *testing.T) {
	var bodies []map[string]any
	refuse := []string{"self_check"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		for _, k := range refuse {
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
	m := Measured{Usage: &Usage{Pool: "codex:owner:weekly", Tokens: &tokens}, SelfCheck: &SelfCheck{Runs: 2, Red: 1, Fixed: 1, LastGreen: true, Seconds: 40}}
	refused, err := c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done", m)
	if err != nil || !strings.HasPrefix(refused, SelfCheckUnsupported) {
		t.Fatalf("refused %q, err %v", refused, err)
	}
	if len(bodies) != 2 || bodies[1]["self_check"] != nil || bodies[1]["usage"] == nil || bodies[1]["lease_epoch"] != float64(7) {
		t.Fatalf("requests %v", bodies)
	}
	if sc := bodies[0]["self_check"]; fmt.Sprint(sc) != "map[fixed:1 last_green:true red:1 runs:2 seconds:40]" {
		t.Fatalf("self_check sent %v", sc)
	}

	bodies, refuse = nil, []string{"self_check", "usage"}
	refused, err = c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done", m)
	if err != nil || !strings.Contains(refused, SelfCheckUnsupported) || !strings.Contains(refused, UsageUnsupported) {
		t.Fatalf("refused %q, err %v", refused, err)
	}
	if len(bodies) != 3 || bodies[2]["self_check"] != nil || bodies[2]["usage"] != nil {
		t.Fatalf("requests %v", bodies)
	}

	// Without a self-check nothing is sent of it, and a platform that takes
	// it answers once.
	bodies, refuse = nil, nil
	if refused, err = c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done", Measured{}); err != nil || refused != "" ||
		len(bodies) != 1 || bodies[0]["self_check"] != nil {
		t.Fatalf("refused %q, err %v, requests %v", refused, err, bodies)
	}

	// A 400 for another reason is the caller's, sent once.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies = append(bodies, nil)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request","message":"end_how is missing"}`))
	}))
	t.Cleanup(srv2.Close)
	c.Base, bodies = srv2.URL, nil
	if _, err = c.Computed(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, &exit, "exit", "done", m); err == nil || len(bodies) != 1 {
		t.Fatalf("err %v, %d requests", err, len(bodies))
	}
}
