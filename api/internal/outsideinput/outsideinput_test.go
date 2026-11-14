package outsideinput

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// exports runs the real script from a copy inside a made-up workspace.
func exports(t *testing.T, platform string) map[string]string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(platform, "scripts", "ci", "outside_inputs.sh"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("outside_inputs.sh: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		kv, ok := strings.CutPrefix(line, "export ")
		k, v, ok2 := strings.Cut(kv, "=")
		if !ok || !ok2 || v == "" {
			t.Fatalf("not an export line: %q", line)
		}
		got[k] = v
	}
	return got
}

// 2026-11-13, CI feedback 18 (ci-platform-4glhs): a Console file two Go tests
// read was deleted and the cached results stayed green. The variable a package
// declares has to change when its input changes, and only then: a change under
// api/ or in another input must leave it alone, or the cache is lost for nothing.
func TestTheScriptNamesAnInputAsEnvDoesAndFollowsItsContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "ci", "outside_inputs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	platform, infra := filepath.Join(ws, "bifrost-platform"), filepath.Join(ws, "bifrost-trade-infra")
	write(t, filepath.Join(platform, "scripts", "ci", "outside_inputs.sh"), string(script))
	write(t, filepath.Join(platform, "api", "x.go"), "package x\n")
	write(t, filepath.Join(platform, "console", "src", "a.ts"), "a\n")
	write(t, filepath.Join(platform, "config", "c.yaml"), "c\n")
	write(t, filepath.Join(platform, "bifrost.units.yaml"), "u\n")
	write(t, filepath.Join(infra, "k8s", "cicd", "p.yaml"), "p\n")
	write(t, filepath.Join(infra, "scripts", "release", "r.sh"), "r\n")
	for _, dir := range []string{platform, infra} {
		git(t, dir, "init", "-q")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-q", "-m", "one")
	}
	names := []string{"console", "config", "bifrost.units.yaml", ".", "bifrost-trade-infra:", "bifrost-trade-infra:k8s/cicd", "bifrost-trade-infra:scripts/release", "bifrost-trade-infra:k8s"}
	first := exports(t, platform)
	for _, name := range names {
		if first[Env(name)] == "" {
			t.Errorf("input %s: the script exports no %s", name, Env(name))
		}
	}
	if _, has := first[Env("api")]; has {
		t.Errorf("api is inside the module: Go looks at it itself")
	}
	step := func(what string, change func(), moves ...string) {
		t.Helper()
		before := exports(t, platform)
		change()
		after := exports(t, platform)
		want := map[string]bool{}
		for _, name := range moves {
			want[name] = true
		}
		for _, name := range names {
			if moved := before[Env(name)] != after[Env(name)]; moved != want[name] {
				t.Errorf("%s: input %s moved=%v, want %v", what, name, moved, want[name])
			}
		}
	}
	step("a Go file under api/ changes", func() {
		write(t, filepath.Join(platform, "api", "x.go"), "package x // two\n")
		git(t, platform, "commit", "-q", "-am", "two")
	})
	step("a Console file is deleted", func() {
		git(t, platform, "rm", "-q", "console/src/a.ts")
		write(t, filepath.Join(platform, "console", "src", "b.ts"), "b\n")
		git(t, platform, "add", ".")
		git(t, platform, "commit", "-q", "-m", "three")
	}, "console", ".")
	step("a pipeline of the repository beside this one changes", func() {
		write(t, filepath.Join(infra, "k8s", "cicd", "p.yaml"), "p2\n")
		git(t, infra, "commit", "-q", "-am", "two")
	}, "bifrost-trade-infra:", "bifrost-trade-infra:k8s/cicd", "bifrost-trade-infra:k8s")
}

// A misspelt input reads a variable nobody sets: the package would be cached
// for ever again and nothing would say so. Every name a test declares is an
// entry of this repository, or a path of at most two elements in a repository
// beside it (checked when that checkout is there, as it is in CI).
func TestEveryDeclaredInputExists(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	found := 0
	err := filepath.WalkDir(filepath.Join(root, "api"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Declare" || len(call.Args) == 0 {
				return true
			}
			if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != "outsideinput" {
				return true
			}
			for _, arg := range call.Args[1:] {
				lit, ok := arg.(*ast.BasicLit)
				if !ok {
					t.Errorf("%s: an input is named by a string literal", fset.Position(arg.Pos()))
					continue
				}
				name, _ := strconv.Unquote(lit.Value)
				found++
				at := filepath.Join(root, name)
				if repo, path, sibling := strings.Cut(name, ":"); sibling {
					if _, err := os.Stat(filepath.Join(root, "..", repo)); err != nil {
						continue
					}
					if strings.Count(path, "/") > 1 {
						t.Errorf("%s: %s is deeper than two elements; the script exports no variable for it", fset.Position(lit.Pos()), name)
					}
					at = filepath.Join(root, "..", repo, path)
				} else if name != "." && (strings.Contains(name, "/") || name == "api") {
					t.Errorf("%s: %s is not a top-level entry outside api/", fset.Position(lit.Pos()), name)
				}
				if _, err := os.Stat(at); err != nil {
					t.Errorf("%s: input %s does not exist", fset.Position(lit.Pos()), name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("found no declared input: this test no longer sees the calls")
	}
}
