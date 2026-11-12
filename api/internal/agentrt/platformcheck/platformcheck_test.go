package platformcheck

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/testscope"
)

func TestRuleNames(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, imp, body, file string
		want                  bool
	}{
		{"parser", "go/parser", "parser.ParseFile(nil, name, nil, 0)", "rule_test.go", true},
		{"parserUnused", "go/parser", "println(1)", "rule_test.go", false},
		{"build", "go/build", "println(build.Default)", "rule_test.go", true},
		{"packages", "golang.org/x/tools/go/packages", "packages.Load(nil)", "rule_test.go", true},
		{"walk", "path/filepath", "filepath.WalkDir(path,nil)", "rule_test.go", true},
		{"walkOther", "path/filepath", "filepath.Join(path,path)", "rule_test.go", false},
		{"read", "os", "os.ReadFile(\"../b/x\")", "rule_test.go", true},
		{"localPath", "os", `path := "../b"; os.ReadDir(path)`, "rule_test.go", true},
		{"packagePath", "os", `os.Open(source); }; var source = "../b"; func unused(){`, "rule_test.go", true},
		{"readOther", "os", "os.ReadFile(\"local.txt\")", "rule_test.go", false},
		{"go", "os/exec", "exec.CommandContext(ctx,\"go\",\"test\")", "rule_test.go", true},
		{"otherProgram", "os/exec", "exec.Command(\"git\",\"show\")", "rule_test.go", false},
		{"vocab", "", "println(1)", "vocab_test.go", true},
		{"ordinary", "", "println(1)", "ordinary_test.go", false},
		{"helper", "os", "helper(); }; func helper(){ os.ReadFile(\"x.go\")", "rule_test.go", true},
		{"variable", "os", "println(helper); }; var helper = func(){ os.ReadDir(\"..\")", "rule_test.go", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "package p; "
			if c.imp != "" {
				src += "import " + strconvQuote(c.imp) + "; "
			}
			src += "func TestRule(t *testing.T){ " + c.body + " }"
			f, e := parser.ParseFile(token.NewFileSet(), c.file, src, 0)
			if e != nil {
				t.Fatal(e)
			}
			got := ruleNames(map[string]*ast.File{c.file: f})
			if slices.Contains(got, "TestRule") != c.want {
				t.Fatalf("names=%v, want match=%v", got, c.want)
			}
		})
	}
}
func strconvQuote(s string) string { return "\"" + s + "\"" }

func TestModuleRules(t *testing.T) {
	t.Parallel()
	dir, e := resolve("../../..")
	if e != nil {
		t.Fatal(e)
	}
	ps, e := load(context.Background(), dir, os.Environ())
	if e != nil {
		t.Fatal(e)
	}
	rs, e := rules(ps, dir)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string][]string{
		"internal/agentrt/store": {"TestTestsRunInParallel", "TestEventFieldValues"},
		"internal/questions":     {"TestServerNamesQuestionsOnlyInTheWireFile"},
		"internal/safego":        {"TestNoUnguardedGoroutines"},
		"internal/agentd":        {"TestNoNewTmpWrites"},
	}
	for p, ns := range testscope.ExplicitGuards {
		want[p] = append(want[p], ns...)
	}
	for suffix, ns := range want {
		for _, n := range ns {
			found := false
			for p, got := range rs {
				if strings.HasSuffix(p, "/"+suffix) && slices.Contains(got, n) {
					found = true
				}
			}
			if !found {
				t.Errorf("missing %s %s", suffix, n)
			}
		}
	}
}
