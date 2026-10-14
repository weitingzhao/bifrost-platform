package agentd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	ex := Executors("/usr/local/bin/agentd", []string{"PATH=/bin", "SOME_ADMIN_TOKEN=x"})
	sp, err := ex["exec:codex"](a)
	if err != nil || sp.Argv[0] != "codex" || !slices.Contains(sp.Argv, filepath.Join(out, "report.md")) ||
		!slices.Contains(sp.Argv, "gpt-5") || !strings.Contains(sp.Argv[len(sp.Argv)-1], "# do it") || slices.Contains(sp.Env, "SOME_ADMIN_TOKEN=x") {
		t.Fatalf("codex = %+v %v", sp, err)
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
