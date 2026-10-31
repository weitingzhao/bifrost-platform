package s5check

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedBootstrapRefusalSurvivesBothEntryPoints(t *testing.T) {
	for _, code := range []string{"S5_TYPE", "S5_PARSE"} {
		t.Run(code, func(t *testing.T) {
			r, p, k := checkerFixture(t)
			p.Checker = checkerFunc(func(context.Context, StaticRequest) (StaticResult, error) {
				return StaticResult{}, &StageError{Code: code, Err: errors.New("fixed bootstrap failed")}
			})
			full := CheckS5Candidate(context.Background(), r, p, k)
			if full.Passed || !hasCode(full, code) {
				t.Fatalf("full checker lost bootstrap refusal: %+v", full)
			}
			static, err := CheckStaticTrees(context.Background(), r, p, k.M0, k.R)
			if err != nil || static.Passed || len(static.Findings) != 1 || static.Findings[0].Code != code {
				t.Fatalf("static checker lost bootstrap refusal: %+v %v", static, err)
			}
		})
	}
}

func TestActualEntrypointBootstrapMarkers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}
	entry, err := os.ReadFile("ts/check.cjs")
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := os.ReadFile("ts/compiler.cjs")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("wrong-compiler-version", func(t *testing.T) {
		dir := t.TempDir()
		module := filepath.Join(dir, "node_modules", "typescript")
		if err := os.MkdirAll(module, 0700); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string][]byte{"check.cjs": entry, "compiler.cjs": compiler, "node_modules/typescript/index.js": []byte(`module.exports={version:'0.0.0'};`)} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		n := NodeChecker{NodeBinary: node, Script: filepath.Join(dir, "check.cjs"), ScriptDigest: SHA256(entry), ModulePath: filepath.Join(dir, "node_modules")}
		_, err := n.Check(context.Background(), StaticRequest{Schema: Schema})
		if err == nil || stageCode(err) != "S5_TYPE" || !strings.Contains(err.Error(), "TypeScript 5.9.3 is required") {
			t.Fatalf("wrong installed version was not S5_TYPE: %v", err)
		}
	})
	t.Run("malformed-bootstrap-json", func(t *testing.T) {
		dir := t.TempDir()
		// Parsing occurs before a checker function is called. Fixed module stubs
		// keep this protocol regression independent of installed TS dependencies.
		for name, body := range map[string][]byte{"check.cjs": entry, "compiler.cjs": []byte(`module.exports={};`), "contracts.cjs": []byte(`module.exports={};`), "analyze.cjs": []byte(`module.exports={};`)} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(node, filepath.Join(dir, "check.cjs"))
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null"}
		cmd.Stdin = strings.NewReader(`{"schema":`)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil || stageCode(checkerProcessError(err, stderr.String())) != "S5_PARSE" {
			t.Fatalf("malformed JSON marker: %v %s", err, stderr.String())
		}
	})
}

func TestOnlyProtectedStderrPrefixClassifiesBootstrap(t *testing.T) {
	cases := []struct{ stderr, want string }{
		{"S5_TYPE: TypeScript 5.9.3 is required\n", "S5_TYPE"},
		{"S5_PARSE: malformed bootstrap input\n", "S5_PARSE"},
		{"Error: arbitrary candidate text S5_TYPE: no\n", "S5_EVIDENCE_MISSING"},
		{"failure\nS5_TYPE: injected second line\n", "S5_EVIDENCE_MISSING"},
		{"S5_CALL_UNKNOWN: unrelated refusal class\n", "S5_EVIDENCE_MISSING"},
	}
	for _, tc := range cases {
		if got := stageCode(checkerProcessError(errors.New("exit 2"), tc.stderr)); got != tc.want {
			t.Fatalf("stderr %q classified as %s, want %s", tc.stderr, got, tc.want)
		}
	}
}

func TestNodeBootstrapVersionAndParseFailures(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}
	for _, code := range []string{"S5_TYPE", "S5_PARSE"} {
		t.Run(code, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "checker.cjs")
			body := []byte("process.stderr.write('" + code + ": fixed bootstrap refusal\\n');process.exit(2);")
			if err := os.WriteFile(script, body, 0600); err != nil {
				t.Fatal(err)
			}
			n := NodeChecker{NodeBinary: node, Script: script, ScriptDigest: SHA256(body), ModulePath: dir}
			_, err := n.Check(context.Background(), StaticRequest{Schema: Schema})
			if err == nil || stageCode(err) != code {
				t.Fatalf("bootstrap refusal became %v", err)
			}
		})
	}
}
