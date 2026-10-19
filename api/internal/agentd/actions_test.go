package agentd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	return newActionTail(executor, path, checkout, ""), write
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
	if tail := newActionTail("exec:deterministic", "/x/log.txt", checkout, ""); tail != nil {
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
		// review 266 A1: a JSON key, a quoted flag value, a whole private key
		`curl -d '{"token":"fixture-secret-value"}' https://example.invalid`:                                       `curl -d '{"token":"<secret>"}' https://example.invalid`,
		`curl -d "{\"api_key\": \"fixture-secret-value\"}" https://x`:                                              `curl -d "{\"api_key\": \"<secret>\"}" https://x`,
		`echo '{"Authorization": "Bearer fixture-secret-value"}'`:                                                  `echo '{"Authorization": "<secret>"}'`,
		`echo '{"password":"fixture \"secret\" value"}'`:                                                           `echo '{"password":"<secret>"}'`,
		"mysql --password 'fixture secret value' -e 1":                                                             "mysql --password <secret> -e 1",
		`mysql --password="fixture secret value" -e 1`:                                                             "mysql --password=<secret> -e 1",
		"mysql --password 'fixture secret":                                                                         "mysql --password <secret>",
		"PGPASSWORD='fixture secret value psql":                                                                    "PGPASSWORD=<secret>",
		"printf '-----BEGIN OPENSSH PRIVATE KEY-----\\nfixtureKeyBody\\n-----END OPENSSH PRIVATE KEY-----\\n' > k": "printf '<secret>\\n' > k",
		"cat <<EOF > k\n-----BEGIN RSA PRIVATE KEY-----\nfixtureKeyBody\n-----END RSA PRIVATE KEY-----\nEOF":       "cat <<EOF > k\n<secret>\nEOF",
		"echo '-----BEGIN EC PRIVATE KEY-----fixtureKeyBody":                                                       "echo '<secret>",
		// review 266 C8: a search pattern and a name that only contains a word
		`rg -n 'token:.*|password:.*|Authorization:.*' api`: `rg -n 'token:.*|password:.*|Authorization:.*' api`,
		"git log --author=Alice --oneline":                  "git log --author=Alice --oneline",
		"rg 'token:.*|x' && PGPASSWORD=fixture-secret psql": "rg 'token:.*|x' && PGPASSWORD=<secret> psql",
		`TOKEN="$GITEA_TOKEN" ./push.sh`:                    "TOKEN=<secret> ./push.sh",
		// review 267 A1: a value that starts like a pattern outside a search,
		// $'…', escaped quotes, concatenation; a value inside quotes
		"PASSWORD=+fixture-secret-value psql":            "PASSWORD=<secret> psql",
		`PASSWORD=$'fixture-secret-value' psql`:          "PASSWORD=<secret> psql",
		`mysql --password "fixture\" secret value" -e 1`: "mysql --password <secret> -e 1",
		`TOKEN="fixture\" secret value" ./push`:          "TOKEN=<secret> ./push",
		`TOKEN=fixture"secret value"rest ./push`:         "TOKEN=<secret> ./push",
		"rg x ; PASSWORD=+fixture-secret-value psql":     "rg x ; PASSWORD=<secret> psql",
		"echo 'TOKEN=fixture-secret-value' && ls":        "echo 'TOKEN=<secret>' && ls",
		"echo 'token: fixture secret value' > x.yaml":    "echo 'token: <secret>' > x.yaml",
		// review 268 A1: only the search pattern argument is kept; $(…) and
		// `…` are whole values; JSON escaped inside a shell argument
		"TOKEN=+fixture-secret-value rg needle api":                    "TOKEN=<secret> rg needle api",
		"rg --pre 'TOKEN=+fixture-secret-value ./filter' needle api":   "rg --pre 'TOKEN=<secret> ./filter' needle api",
		"PASSWORD=$(printf 'fixture-secret-value') psql":               "PASSWORD=<secret> psql",
		"PASSWORD=`printf 'fixture secret value'` psql":                "PASSWORD=<secret> psql",
		"PASSWORD=$(printf \"%s\" \"$(cat 'fixture)')\") psql":         "PASSWORD=<secret> psql",
		`curl -d "{\"token\":\"fixture\\\" secret value\"}" https://x`: `curl -d "{\"token\":\"<secret>\"}" https://x`,
		// and other shapes: curl -u, sshpass -p, mysql -p…, a header, a JWT
		"curl -u bot:fixture-secret-value https://x":                                   "curl -u <secret> https://x",
		"sshpass -p fixture-secret-value ssh h":                                        "sshpass -p <secret> ssh h",
		"mysql -pfixture-secret-value db":                                              "mysql -p<secret> db",
		`curl -H "X-Api-Key: fixture secret value" https://x`:                          `curl -H "X-Api-Key: <secret>" https://x`,
		"echo eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N": "echo <secret>",
		"sort -u a.txt && docker run -p 8080:80 img && mysql -P 3306 -h db":            "sort -u a.txt && docker run -p 8080:80 img && mysql -P 3306 -h db",
		// review 268 C3: git's own options before grep; a pattern with "token"
		`git -C api grep -n '"token":"[^"]+"'`: `git -C api grep -n '"token":"[^"]+"'`,
		"rg -n 'token validation failed' api":  "rg -n 'token validation failed' api",
		// review 267 C5: a JSON pattern in a search
		`rg -n '"token":"[^"]+"' api`: `rg -n '"token":"[^"]+"' api`,
		`Grep "password":\s*".*" src`: `Grep "password":\s*".*" src`,
	} {
		if got := scrub(in); got != want {
			t.Errorf("scrub(%q) = %q, want %q", in, got, want)
		}
	}
}

// What is queued, a command's run and its result, holds no credential
// (review 266 A1).
func TestCredentialsAreNotQueued(t *testing.T) {
	for _, cmd := range []string{
		`curl -d '{"token":"fixture-secret-value"}' https://example.invalid`,
		"mysql --password 'fixture secret value' -e 1",
		"printf '-----BEGIN OPENSSH PRIVATE KEY-----\\nfixture-secret-value\\n-----END OPENSSH PRIVATE KEY-----\\n' > k",
		"PASSWORD=+fixture-secret-value psql",
		"TOKEN=+fixture-secret-value rg needle api",
		"PASSWORD=$(printf 'fixture-secret-value') psql",
		`curl -d "{\"token\":\"fixture\\\" secret value\"}" https://x`,
		`TOKEN="fixture\" secret value" ./push`,
	} {
		tail, write := tailOf(t, "exec:codex")
		ev, _ := json.Marshal(map[string]any{"type": "item.started", "item": map[string]any{"type": "command_execution", "command": cmd}})
		write(string(ev) + "\n")
		ev, _ = json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "command": cmd, "exit_code": 0}})
		write(string(ev) + "\n")
		got := readTake(tail)
		if len(got) != 2 || got[0].Kind != actions.Run || got[1].Kind != actions.Result {
			t.Fatalf("%q: %s", cmd, kinds(got))
		}
		for _, a := range got {
			if strings.Contains(a.Target, "fixture") || strings.Contains(a.Target, "secret value") {
				t.Errorf("%s queued as %q", a.Kind, a.Target)
			}
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
	tail := newActionTail("exec:codex", path, checkout, "")
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

// The last read is the collector's: a flush whose time is up while a read
// hangs leaves nothing behind, and a read whose turn comes after the end
// reads nothing (review 266 B3).
func TestFlushIsTheCollectorsRead(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tail.collect(ctx)
	tail.io.Lock() // a read that hangs
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		fctx, fcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if tail.flush(fctx) {
			t.Fatal("a flush done while the read hangs")
		}
		fcancel()
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatalf("%d goroutines after 20 flushes, %d before", n, before)
	}
	cancel()
	write(`{"type":"item.started","item":{"type":"command_execution","command":"ls"}}` + "\n")
	tail.io.Unlock()
	select {
	case <-tail.gone:
	case <-time.After(2 * time.Second):
		t.Fatal("the collector did not stop")
	}
	if got := tail.take(50, time.Now()); got != nil {
		t.Fatalf("read after the end: %s", kinds(got))
	}
	if tail.flush(context.Background()) {
		t.Fatal("a flush done after the collector stopped")
	}
}

// A target longer than maxScrub is cut at a blank, never inside a credential
// (review 267 A1).
func TestALongTargetIsCutAtABlank(t *testing.T) {
	tail := newActionTail("exec:codex", "", checkout, "")
	long := "cat /home/" + strings.Repeat("p", maxScrub-10-1-7) + " ghp_FIXTURESECRETVALUE1"
	got, cut := tail.clean(long)
	if !cut || strings.Contains(got, "ghp_") || strings.Contains(got, "FIXT") {
		t.Fatalf("cut %v, %q", cut, got)
	}
	if got, _ := tail.clean("TOKEN=" + strings.Repeat("a", maxScrub+1)); got != "" {
		t.Fatalf("a long word kept as %q", got)
	}
}

// A flush whose time ran out is not read later (review 267 B2).
func TestAnExpiredFlushIsNotRead(t *testing.T) {
	tail, write := tailOf(t, "exec:codex")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tail.collect(ctx)
	tail.io.Lock()
	fctx, fcancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer fcancel()
	if tail.flush(fctx) {
		t.Fatal("a flush done while the read hangs")
	}
	write(`{"type":"item.started","item":{"type":"command_execution","command":"ls"}}` + "\n")
	tail.io.Unlock()
	time.Sleep(200 * time.Millisecond)
	if got := tail.take(50, time.Now()); got != nil {
		t.Fatalf("the expired flush was read: %s", kinds(got))
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
			_, _ = w.Write([]byte(`{"error":"bad_request","message":"body: json: unknown field \"actions\""}`))
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

// A 400 for another reason is not taken for an older platform: it is the
// caller's, and actions are not given up (review 267 B6).
func TestHeartbeatRefusedForAnotherReason(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request","message":"lease_epoch is required"}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{Base: srv.URL, Host: "h1", Token: "fixture-agentd"}
	acts := []actions.Action{{Kind: actions.Read, Target: "a.go", At: time.Now().UTC()}}
	_, _, refused, err := c.Heartbeat(context.Background(), "a_01M56J5QPAAQQP53KV4MQYCW5Q", 7, acts)
	if err == nil || refused != "" || n != 1 {
		t.Fatalf("refused %q, err %v, %d requests", refused, err, n)
	}
}

// A lease with no keeper: only lastActions heartbeats.
func lastLease(t *testing.T) (*Daemon, *fakePlatform, *lease, func(string)) {
	t.Helper()
	f := newFake()
	d := fakeDaemon(t, f)
	l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}, done: make(chan struct{}), beat: make(chan struct{}, 1)}
	tail, write := tailOf(t, "exec:codex")
	l.tail.Store(tail)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go tail.collect(ctx)
	write(`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}` + "\n")
	return d, f, l, write
}

func lastActionsReturns(t *testing.T, ctx context.Context, d *Daemon, l *lease) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() { d.lastActions(ctx, "a1", 1, l); close(done) }()
	return done
}

// The last heartbeat gives up waiting for the keeper's turn when its context
// ends (review 266 B5).
func TestLastActionsGiveUpOnTheKeepersTurn(t *testing.T) {
	d, f, l, _ := lastLease(t)
	l.beat <- struct{}{} // the keeper's heartbeat in flight
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	select {
	case <-lastActionsReturns(t, ctx, d, l):
	case <-time.After(2 * time.Second):
		t.Fatal("lastActions waited for the keeper past its context")
	}
	if n := f.count("heartbeat"); n != 0 {
		t.Fatalf("%d heartbeats", n)
	}
}

// A lease lost while the last heartbeat waits for its turn is not
// heartbeated; a fenced answer marks the lease lost (review 266 B7).
func TestLastActionsAfterTheLeaseIsLost(t *testing.T) {
	d, f, l, _ := lastLease(t)
	l.beat <- struct{}{}
	done := lastActionsReturns(t, context.Background(), d, l)
	time.Sleep(200 * time.Millisecond) // read, taken, waiting for the turn
	l.markLost()
	<-l.beat
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lastActions did not return")
	}
	if n := f.count("heartbeat"); n != 0 {
		t.Fatalf("%d heartbeats after the lease was lost", n)
	}

	d, f, l, _ = lastLease(t)
	f.set("heartbeat", always(http.StatusConflict, "fenced"))
	<-lastActionsReturns(t, context.Background(), d, l)
	if f.count("heartbeat") != 1 || !l.isLost() {
		t.Fatalf("%d heartbeats, lost %v", f.count("heartbeat"), l.isLost())
	}

	d, f, l, _ = lastLease(t)
	<-lastActionsReturns(t, context.Background(), d, l)
	if f.count("heartbeat") != 1 || l.isLost() {
		t.Fatalf("%d heartbeats, lost %v", f.count("heartbeat"), l.isLost())
	}
}

// The last heartbeat to an older platform records that it takes no actions,
// as the keeper's does, before the turn is given back (review 267 B7).
func TestLastActionsToAnOlderPlatform(t *testing.T) {
	d, f, l, _ := lastLease(t)
	f.set("heartbeat", func(n int) (int, string) {
		if n == 0 { // the first, with actions
			return http.StatusBadRequest, `{"error":"bad_request","message":"body: json: unknown field \"actions\""}`
		}
		return http.StatusOK, `{"lease_expires_at":"2026-10-18T07:00:00Z"}`
	})
	<-lastActionsReturns(t, context.Background(), d, l)
	if f.count("heartbeat") != 2 || !l.noActions.Load() {
		t.Fatalf("%d heartbeats, no actions %v", f.count("heartbeat"), l.noActions.Load())
	}
}

// Masking stays linear on inputs built to make it walk far (review 268 6):
// nested $( that never close, many commands, many names in quotes.
func TestScrubIsBounded(t *testing.T) {
	for _, s := range []string{strings.Repeat("$(", 30000), strings.Repeat("a;", 30000), strings.Repeat("'TOKEN=$('", 6000),
		strings.Repeat("TOKEN=a;", 8000), strings.Repeat(`\"token\":\"`, 5000), strings.Repeat("rg 'token:.*|", 5000), strings.Repeat("\\", 60000)} {
		done := make(chan struct{})
		go func() { scrub(s); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("scrub of %d bytes %q… did not end in 5 s", len(s), s[:20])
		}
	}
}

// STG walk 3 (2026-10-19): the attempt's own directories are named in a
// target as a report and a log tail name them (handText): ".", "..",
// "$HOME", "$TMPDIR", each only as a whole path; "a1.out" is not the
// checkout "a1" with ".out" after it. Any other local path is still masked
// whole.
func TestTargetsNameTheAttemptsDirectories(t *testing.T) {
	task := "/home/bifrost-agentd-stg/agent-work/agentd/tasks/t_01M5B6KKPMM15HZB4BC0ZWVJ5R"
	tail := newActionTail("exec:codex", "", task+"/a1", task+"/a1.out")
	for in, want := range map[string]string{
		"mktemp -d " + task + "/a1.out/tmp/localpath-checks.XXXXXX":                               "mktemp -d $TMPDIR/localpath-checks.XXXXXX",
		"cat " + task + "/a1/api/internal/localpath/localpath.go":                                 "cat ./api/internal/localpath/localpath.go",
		task + "/a1/api/x_test.go":                                                                "api/x_test.go",
		"ls " + task + "/a1.out/home/.config":                                                     "ls $HOME/.config",
		"diff " + task + "/a2/x " + task + "/a1/x":                                                "diff ../a2/x ./x",
		"ls /home/bifrost-agentd-stg/agent-work/other":                                            "ls <local path>",
		"cat /home/bifrost-agentd-stg/agent-work/agentd/tasks/t_01M5B6KKPMM15HZB4BC0ZWVJ5Rx/a1/y": "cat <local path>",
	} {
		if got, _ := tail.clean(in); got != want {
			t.Errorf("clean(%q) = %q; want %q", in, got, want)
		}
	}
}
