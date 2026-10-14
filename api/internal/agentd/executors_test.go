package agentd

import (
	"encoding/json"
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
	if err != nil || sp.Argv[0] != "codex" || !slices.Contains(sp.Argv, "mcp_servers={}") || !slices.Contains(sp.Argv, filepath.Join(out, "report.md")) ||
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
