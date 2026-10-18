package agentd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

func TestAllowedEnv(t *testing.T) {
	got := AllowedEnv([]string{"PATH=/bin", "HOME=/h", "AGENTD_TOKEN_FILE=/t", "SOME_ADMIN_TOKEN=x", "KUBECONFIG=/k",
		"OPENAI_API_KEY=k", "PGPASSWORD=p", "LANG=C"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/h,LANG=C" {
		t.Fatalf("env = %v", got)
	}
}

func TestAgentExecutors(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	a := &Attempt{Claim: &Claimed{Tool: Tool{Model: "gpt-5"}}, Spec: TaskSpec{Text: "# do it"}, Dir: dir, Out: out}
	ex := Executors("/usr/local/bin/agentd", []string{"PATH=/bin", "SOME_ADMIN_TOKEN=x"}, Secrets{})
	sp, err := ex["exec:codex"](a)
	if err != nil || sp.Argv[0] != "codex" || !slices.Contains(sp.Argv, filepath.Join(out, "report.md")) ||
		!slices.Contains(sp.Argv, "gpt-5") || !strings.Contains(sp.Argv[len(sp.Argv)-1], "# do it") || slices.Contains(sp.Env, "SOME_ADMIN_TOKEN=x") {
		t.Fatalf("codex = %+v %v", sp, err)
	}
	// What codex may write is pinned on its command line (TD-371).
	tmp := filepath.Join(out, "tmp")
	for _, pin := range []string{"sandbox_workspace_write.writable_roots=[" + strconv.Quote(tmp) + "]", "sandbox_workspace_write.exclude_slash_tmp=true",
		"sandbox_workspace_write.exclude_tmpdir_env_var=true", "notify=[]"} {
		if i := slices.Index(sp.Argv, pin); i < 1 || sp.Argv[i-1] != "-c" {
			t.Errorf("codex has no -c %s: %v", pin, sp.Argv)
		}
	}
	if !slices.Contains(sp.Env, "TMPDIR="+tmp) || slices.ContainsFunc(sp.Env, func(kv string) bool { return strings.HasPrefix(kv, "TMPDIR=") && kv != "TMPDIR="+tmp }) {
		t.Errorf("codex's TMPDIR: %v", sp.Env)
	}
	if fi, serr := os.Stat(tmp); serr != nil || !fi.IsDir() {
		t.Errorf("the attempt's temporary directory: %v", serr)
	}
	sp, err = ex["exec:cursor"](a)
	if err != nil || sp.Argv[0] != "cursor-agent" || !slices.Contains(sp.Argv, "--trust") || slices.Contains(sp.Argv, "--approve-mcps") || sp.After == nil {
		t.Fatalf("cursor = %+v %v", sp, err)
	}
	log := `{"type":"system","subtype":"init"}` + "\n" + `{"type":"assistant","message":"x"}` + "\n" + `{"type":"result","result":"# done\nall good"}` + "\n"
	if err = os.WriteFile(filepath.Join(out, "log.txt"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = sp.After(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "report.md")); string(b) != "# done\nall good" {
		t.Fatalf("cursor's report = %q", b)
	}
	if err = os.WriteFile(filepath.Join(out, "log.txt"), []byte(`{"type":"assistant"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if sp.After() == nil {
		t.Fatal("a cursor run with no result made a report")
	}
}

// The cursor key reaches cursor alone; the host's own CURSOR_API_KEY is not
// passed through to anyone.
// PROD M-3 (2026-10-17): cursor runs read-only (ask mode) for an attempt
// that pushes no branch, and with its tools for one that does.
func TestCursorReadOnlyWithoutABranch(t *testing.T) {
	for effect, want := range map[string]bool{"none": true, "": true, "branch": false} {
		a := &Attempt{Claim: &Claimed{Role: "review", EffectClass: effect}, Spec: TaskSpec{Text: "# review it"}, Dir: t.TempDir(), Out: t.TempDir()}
		sp, err := cursorExecutor(nil)(a)
		if err != nil {
			t.Fatal(err)
		}
		ask := slices.Contains(sp.Argv, "--mode") && sp.Argv[slices.Index(sp.Argv, "--mode")+1] == "ask"
		if ask != want {
			t.Errorf("effect %q: ask mode = %v, want %v (%q)", effect, ask, want, sp.Argv)
		}
		if p := Prompt(a); sp.Argv[len(sp.Argv)-1] != p {
			t.Errorf("effect %q: the prompt is not the last argument: %q", effect, sp.Argv)
		}
	}
}

func TestCursorKeyReachesCursorAlone(t *testing.T) {
	a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(), Out: t.TempDir()}
	ex := Executors("/usr/local/bin/agentd", []string{"PATH=/bin", "CURSOR_API_KEY=from-host", "AGENT_CLI_CREDENTIAL_STORE=file"},
		Secrets{CursorAPIKey: "k1"})
	for name, e := range ex {
		sp, err := e(a)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(sp.Env, "CURSOR_API_KEY=from-host") {
			t.Fatalf("%s got the host's CURSOR_API_KEY", name)
		}
		if got := slices.Contains(sp.Env, "CURSOR_API_KEY=k1"); got != (name == "exec:cursor") {
			t.Fatalf("%s: has the cursor key = %v", name, got)
		}
		// With the key, cursor keeps the login it buys in memory, not in a
		// keychain the daemon cannot write (10-15); the host's own store
		// reaches no one (review 202 B).
		var stores []string
		for _, kv := range sp.Env {
			if strings.HasPrefix(kv, "AGENT_CLI_CREDENTIAL_STORE=") {
				stores = append(stores, kv)
			}
		}
		want := []string{}
		if name == "exec:cursor" {
			want = []string{"AGENT_CLI_CREDENTIAL_STORE=memory"}
		}
		if !slices.Equal(append([]string{}, stores...), want) {
			t.Fatalf("%s: credential store %v, want %v", name, stores, want)
		}
		if slices.ContainsFunc(sp.Argv, func(s string) bool { return strings.Contains(s, "k1") }) {
			t.Fatalf("%s: the key is on the command line", name)
		}
	}
	sp, err := Executors("/usr/local/bin/agentd", []string{"PATH=/bin", "AGENT_CLI_CREDENTIAL_STORE=file"}, Secrets{})["exec:cursor"](a)
	if err != nil || slices.ContainsFunc(sp.Env, func(s string) bool {
		return strings.HasPrefix(s, "CURSOR_API_KEY=") || strings.HasPrefix(s, "AGENT_CLI_CREDENTIAL_STORE=")
	}) {
		t.Fatalf("no key, yet cursor's env = %v %v", sp.Env, err)
	}
}

// The deterministic executor through the launcher: the subject spec's
// verify commands run in the workspace and verify.json says how they ended.
func TestDeterministicExecutor(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		pass bool
	}{{"test -f README.md", true}, {"test -f MISSING", false}} {
		dir, out := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		subject := TaskSpec{Text: "# the work", Verify: [][]string{{"sh", "-c", c.cmd}}}
		a := &Attempt{Claim: &Claimed{Role: "verify"}, Spec: TaskSpec{Check: "verify"}, Subject: &subject, Dir: dir, Out: out}
		sp, err := deterministicExecutor(os.Args[0], append(os.Environ(), "AGENTD_TEST_LAUNCHER=1"))(a)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Launch(os.Args[0], sp, func(int, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		code := p.Wait()
		var v Verification
		b, _ := os.ReadFile(filepath.Join(out, "verify.json"))
		if jerr := json.Unmarshal(b, &v); jerr != nil || v.Pass != c.pass || (code == 0) != c.pass {
			t.Fatalf("%s: exit %d, verify.json %s", c.cmd, code, b)
		}
		r := a.results()
		if (a.conclusion("exit", code, r) == "done") != c.pass || r.reportKind != "verify" {
			t.Fatalf("%s: conclusion %s, kind %s", c.cmd, a.conclusion("exit", code, r), r.reportKind)
		}
	}
}

func TestRunVerifyUsage(t *testing.T) {
	if RunVerify(nil) != 2 || RunVerify([]string{"--spec", filepath.Join(t.TempDir(), "none"), "--out", "x"}) != 2 {
		t.Fatal("bad arguments did not exit 2")
	}
}

// STG walk M-9 (2026-10-17): ESLint printed the checkout's absolute path in
// its warnings, and the platform refused every Console check's verify.json
// (H8). What RunVerify writes names no path of this machine: the checkout is
// ".", the task's directory "..", the home "$HOME", the rest masked.
func TestRunVerifyHandsInNoLocalPath(t *testing.T) {
	task := filepath.Join(t.TempDir(), "tasks", "t_1")
	checkout := filepath.Join(task, "a1")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(checkout)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	print := strings.Join([]string{
		`echo "` + wd + `/console/src/a.ts: warning"`,
		`echo "` + filepath.Dir(wd) + `/bifrost-ui/src/b.ts"`,
		`echo "` + home + `/.npm/_logs/c.log"`,
		`echo /tmp/go-build1/b001`,
		`echo "` + wd + `2/not-the-checkout"`,
	}, "; ")
	spec := map[string]any{"text": "t", "verify": [][]string{{"sh", "-c", print}, {"ls", wd}}}
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	if err = os.WriteFile(specPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "verify.json")
	if rc := RunVerify([]string{"--spec", specPath, "--out", out}); rc != 0 {
		t.Fatalf("RunVerify exited %d", rc)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if p, found := localpath.Find(got); found {
		t.Fatalf("verify.json still holds a local path: %q\n%s", p, got)
	}
	var v Verification
	if err = json.Unmarshal(got, &v); err != nil {
		t.Fatal(err)
	}
	tail := v.Commands[0].Tail
	for _, want := range []string{"./console/src/a.ts: warning", "../bifrost-ui/src/b.ts", "$HOME/.npm/_logs/c.log", "<tmp>/go-build1/b001"} {
		if !strings.Contains(tail, want) {
			t.Errorf("the tail lacks %q:\n%s", want, tail)
		}
	}
	if strings.Contains(tail, "./2/not-the-checkout") || strings.Contains(tail, ".2/not-the-checkout") {
		t.Errorf("a sibling directory whose name starts like the checkout's was taken for it:\n%s", tail)
	}
	if v.Commands[1].Argv[1] != "." {
		t.Errorf("the checkout in a command's argv was not made relative: %q", v.Commands[1].Argv)
	}
}

// Review 237 A: handIn's bytes, the ones the platform scans, hold no local
// path even where the JSON encoder's escapes complete one, and stay valid.
func TestHandInMasksWhatEscapesComplete(t *testing.T) {
	v := Verification{Reason: "C:<", Commands: []CommandTrace{{Argv: []string{"x", "a:\n"}, Tail: "C:\xff and \x1a:< and C:\u2028"}}}
	out, err := handIn(v, &pathRewrite{})
	if err != nil {
		t.Fatal(err)
	}
	if p, found := localpath.Find(out); found {
		t.Fatalf("handIn still holds a local path: %q\n%s", p, out)
	}
	var back Verification
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("handIn wrote invalid JSON: %v\n%s", err, out)
	}
	if back.Reason != "C: <" || back.Commands[0].Argv[1] != "a: \n" {
		t.Errorf("handIn changed more than a space after the colon: %+v", back)
	}
}

// Review 237 B: only a whole path is the checkout, its parent or the home.
func TestPathRewriteTakesWholePathsOnly(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "tasks", "t_1", "a1")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Chdir(checkout)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(wd)
	p := newPathRewrite()
	for _, c := range []struct{ in, want string }{
		{wd + "/console/a.ts:3:1", "./console/a.ts:3:1"},
		{"at " + wd, "at ."},
		{wd + ":" + wd + "/b", ".:./b"},
		{"--dir=" + parent + "/bifrost-ui", "--dir=../bifrost-ui"},
		{wd + "2/x", "../a12/x"},
		{wd + "+backup/file", "../a1+backup/file"},
		{wd + "é/file", "../a1é/file"},
		{"/vendor" + wd + "/file", "/vendor" + wd + "/file"},
		{"https://example.test" + wd + "/file", "https://example.test" + wd + "/file"},
		{wd + parent + "/file", "." + parent + "/file"},
	} {
		if got := p.rewrite(c.in); got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Review 238 B: coloured output (ESLint --color, tsc) is aliased too,
	// its colour sequences dropped.
	v := withoutLocalPaths(Verification{Commands: []CommandTrace{{
		Tail: "\x1b[4m" + wd + "/console/a.ts\x1b[24m\n\x1b[96m" + wd + "/console/b.ts\x1b[0m:3:1\n\x1b[1m" + wd + "\x1b[22m",
	}}}, p)
	if want := "./console/a.ts\n./console/b.ts:3:1\n."; v.Commands[0].Tail != want {
		t.Errorf("coloured output: %q, want %q", v.Commands[0].Tail, want)
	}
}

// Review 163 A2: a review passes only on its verdict line.
func TestReviewVerdict(t *testing.T) {
	a := &Attempt{Claim: &Claimed{Role: "review"}, Spec: TaskSpec{Check: "review"}}
	for report, want := range map[string]string{
		"# looks right\n\n" + VerdictPass + "\n":                  "done",
		"# wrong\n" + VerdictFail:                                 "failed",
		"FAIL: changes are incorrect; request changes":            "none",
		"# ok\n" + VerdictPass + "\nbut one more thing":           "none",
		"all good, ship it":                                       "none",
		"# mixed\n" + VerdictFail + "\n" + VerdictPass + "  \n\n": "done",
	} {
		if got := a.conclusion("exit", 0, results{reportBody: []byte(report), reportKind: "review"}); got != want {
			t.Errorf("%q = %s, want %s", report, got, want)
		}
	}
	if !strings.Contains(Prompt(a), VerdictPass) || strings.Contains(Prompt(&Attempt{Claim: &Claimed{}, Spec: TaskSpec{}}), VerdictPass) {
		t.Fatal("only a review's prompt asks for the verdict line")
	}
}

// Review 163 A8: an agent executor is offered only where the user has no MCP
// server it would load, and never when that cannot be checked.
func TestIsolated(t *testing.T) {
	ex := map[string]Executor{"exec:deterministic": nil, "exec:codex": nil, "exec:cursor": nil}
	kept, why := Isolated(ex, map[string]MCPCheck{
		"exec:codex":  func() (int, error) { return 3, nil },
		"exec:cursor": func() (int, error) { return 0, errors.New("unreadable") },
	})
	if len(kept) != 1 || len(why) != 2 {
		t.Fatalf("kept %v, why %v", kept, why)
	}
	if _, ok := kept["exec:deterministic"]; !ok {
		t.Fatal("the deterministic executor needs no MCP check")
	}
	kept, _ = Isolated(ex, map[string]MCPCheck{"exec:codex": func() (int, error) { return 0, nil }, "exec:cursor": func() (int, error) { return 0, nil }})
	if len(kept) != 3 {
		t.Fatalf("with no MCP server: %v", kept)
	}
	t.Setenv("HOME", t.TempDir())
	if n, err := CursorMCP(); n != 0 || err != nil {
		t.Fatalf("no ~/.cursor/mcp.json = %d %v", n, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	home, _ := os.UserHomeDir()
	must(os.MkdirAll(filepath.Join(home, ".cursor"), 0o700))
	must(os.WriteFile(filepath.Join(home, ".cursor", "mcp.json"), []byte(`{"mcpServers":{"a":{},"b":{}}}`), 0o600))
	if n, err := CursorMCP(); n != 2 || err != nil {
		t.Fatalf("two servers = %d %v", n, err)
	}
}

// Review 164 B1: cursor's output is read a line at a time; a line over the
// limit is skipped, and the result after it is still found.
func TestCursorReportSkipsLongLines(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log.txt")
	long := `{"type":"assistant","message":"` + strings.Repeat("x", 2*maxBody) + `"}`
	body := `{"type":"result","result":"first"}` + "\n" + long + "\n" + `{"type":"result","result":"# last\nok"}` + "\n"
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := streamResultReport("cursor", log, filepath.Join(dir, "report.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "report.md")); string(b) != "# last\nok" {
		t.Fatalf("report = %q", b)
	}
	var lines []string
	if err := eachLine(strings.NewReader("a\n"+strings.Repeat("b", 50)+"\nc"), 10, func(l []byte) { lines = append(lines, string(l)) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "a,c" {
		t.Fatalf("lines = %q", lines)
	}
}
