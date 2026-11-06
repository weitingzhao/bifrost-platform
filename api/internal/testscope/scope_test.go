package testscope

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "go.mod", "module example.org/api\n\ngo 1.25.0\n")
	for i := 0; i < 20; i++ {
		put(t, root, fmt.Sprintf("p%d/p.go", i), fmt.Sprintf("package p%d\nconst X=1\n", i))
	}
	put(t, root, "consumer/c.go", "package consumer\nimport _ \"example.org/api/p0\"\n")
	put(t, root, "testconsumer/c.go", "package testconsumer\n")
	put(t, root, "testconsumer/c_test.go", "package testconsumer_test\nimport _ \"example.org/api/p0\"\n")
	put(t, root, "helpers/h.go", "package sharedtestutil\n")
	put(t, root, "othertest/h.go", "package sharedtest\n")
	return root
}
func TestFullTriggers(t *testing.T) {
	root := fixture(t)
	for _, tc := range []struct{ line, why string }{
		{"M\tapi/go.mod", "module dependencies"}, {"M\tapi/go.sum", "module dependencies"}, {"A\tapi/internal/agentrt/migrations/x.sql", "protected path"}, {"M\tapi/p0/testdata/x", "testdata"}, {"M\tapi/internal/testscope/x.go", "protected path"}, {"M\tapi/cmd/testscope/main.go", "protected path"}, {"M\tscripts/ci/test_api_scoped.sh", "selector script"}, {"M\tapi/helpers/h.go", "shared test helper"}, {"M\tapi/othertest/h.go", "shared test helper"}, {"M\tapi/helpers/readme.md", "shared test helper"}, {"D\tapi/gone/x.go", "unresolved"}, {"R100\tapi/gone/x.go\tapi/p0/new.go", "unresolved"}, {"bad", "change list error"}} {
		t.Run(tc.line, func(t *testing.T) {
			r := SelectChanges(root, strings.NewReader(tc.line))
			if r.Scope != "full" || !strings.Contains(r.Why, tc.why) {
				t.Fatalf("%+v", r)
			}
		})
	}
	if r := SelectChanges(filepath.Join(root, "absent"), strings.NewReader("")); r.Scope != "full" || !strings.Contains(r.Why, "graph error") {
		t.Fatalf("%+v", r)
	}
}
func TestDependentsGuardsAndCap(t *testing.T) {
	root := fixture(t)
	put(t, root, "guard/g.go", "package guard\n")
	put(t, root, "guard/g_test.go", "package guard\nimport (\"path/filepath\";\"testing\")\nfunc TestTree(t *testing.T){filepath.WalkDir(\".\",nil)}\n")
	r := SelectChanges(root, strings.NewReader("M\tapi/p0/p.go"))
	if r.Scope != "incremental" {
		t.Fatalf("%+v", r)
	}
	for _, p := range []string{"p0", "consumer", "testconsumer", "guard"} {
		if !strings.Contains(r.Packages, "example.org/api/"+p) {
			t.Fatalf("missing %s: %+v", p, r)
		}
	}
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, fmt.Sprintf("M\tapi/p%d/p.go", i))
	}
	r = SelectChanges(root, strings.NewReader(strings.Join(lines, "\n")))
	if r.Scope != "full" || !strings.Contains(r.Why, "40%") {
		t.Fatalf("%+v", r)
	}
	r = SelectChanges(root, strings.NewReader("D\tapi/p0/old.go"))
	if r.Scope != "incremental" {
		t.Fatalf("resolved deleted file: %+v", r)
	}
}
func TestStoreSelection(t *testing.T) {
	root := fixture(t)
	dir := "internal/agentrt/store/"
	put(t, root, dir+"alpha.go", "package store\nimport _ \"example.org/api/p0\"\nfunc Alpha(){}\n")
	put(t, root, dir+"beta.go", "package store\nfunc Beta(){}\n")
	for name, body := range map[string]string{
		"alpha_test.go":         "package store_test\nimport \"testing\"\nfunc TestName(t *testing.T){}",
		"reference_test.go":     "package store_test\nimport (\"testing\";s \"example.org/api/internal/agentrt/store\")\nfunc TestRef(t *testing.T){s.Alpha()}",
		"beta_internal_test.go": "package store\nimport \"testing\"\nfunc TestBeta(t *testing.T){Beta()}",
		"other_test.go":         "package store\nimport \"testing\"\nfunc TestOther(t *testing.T){}\nfunc TestOther2(t *testing.T){}",
		"export_test.go":        "package store\nvar Exported=Alpha",
	} {
		put(t, root, dir+name, body)
	}
	for _, tc := range []struct{ file, want string }{{"alpha.go", "^(TestName|TestRef)$"}, {"beta.go", "^(TestBeta)$"}, {"reference_test.go", "^(TestRef)$"}, {"export_test.go", ""}} {
		t.Run(tc.file, func(t *testing.T) {
			r := SelectChanges(root, strings.NewReader("M\tapi/"+dir+tc.file))
			if r.Scope != "incremental" || r.StoreRun != tc.want {
				t.Fatalf("%+v", r)
			}
		})
	}
	r := SelectChanges(root, strings.NewReader("M\tapi/p0/p.go"))
	if r.StoreRun != "^(TestName|TestRef)$" {
		t.Fatalf("dependency: %+v", r)
	}
	put(t, root, "middle/m.go", "package middle\nimport _ \"example.org/api/p0\"\n")
	put(t, root, dir+"alpha.go", "package store\nimport _ \"example.org/api/middle\"\nfunc Alpha(){}\n")
	r = SelectChanges(root, strings.NewReader("M\tapi/p0/p.go"))
	if r.StoreRun != "^(TestName|TestRef)$" {
		t.Fatalf("transitive dependency: %+v", r)
	}
	r = SelectChanges(root, strings.NewReader("M\tapi/"+dir+"alpha.go\nM\tapi/"+dir+"beta.go"))
	if r.StoreRun != "" {
		t.Fatalf("store cap: %+v", r)
	}
}

// Every walking test file is automatically guarded, even when the walk is in a helper.
func TestRepositoryWalkingTestsAreGuarded(t *testing.T) {
	g, e := load("../..")
	if e != nil {
		t.Fatal(e)
	}
	set, e := guards(g)
	if e != nil {
		t.Fatal(e)
	}
	for name, p := range g.packages {
		entries, e := os.ReadDir(p.Dir)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range entries {
			if !strings.HasSuffix(v.Name(), "_test.go") {
				continue
			}
			f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(p.Dir, v.Name()), nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			if walks(f) && !set[name] {
				t.Errorf("walking tests in %s are not guarded", v.Name())
			}
		}
	}
	for p, names := range ExplicitGuards {
		q := g.packages[g.prefix+"/"+p]
		if q == nil {
			t.Errorf("missing explicit package %s", p)
			continue
		}
		for _, n := range names {
			found := false
			entries, _ := os.ReadDir(q.Dir)
			for _, v := range entries {
				if strings.HasSuffix(v.Name(), "_test.go") {
					f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(q.Dir, v.Name()), nil, 0)
					if e != nil {
						t.Fatal(e)
					}
					for _, test := range testNames(f) {
						if test == n {
							found = true
						}
					}
				}
			}
			if !found {
				t.Errorf("missing explicit guard %s.%s", p, n)
			}
		}
	}
}

func TestHistoryFallbacks(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		b, e := command(root, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(string(b))
	}
	run("git", "init", "-q")
	commit := func() string {
		run("git", "-c", "user.name=Test", "-c", "user.email=test@example.org", "commit", "-q", "--allow-empty", "-m", "test")
		return run("git", "rev-parse", "HEAD")
	}
	base := commit()
	head := commit()
	if r := Select(root, "missing", head); r.Why != "unreadable base" {
		t.Fatalf("%+v", r)
	}
	if r := Select(root, head, base); r.Why != "base is not an ancestor of head" {
		t.Fatalf("%+v", r)
	}
}

func TestParseChanges(t *testing.T) {
	for _, tc := range []struct {
		in    string
		count int
		bad   bool
	}{{"M\tapi/a file.go\n", 1, false}, {"R100\tapi/old.go\tapi/new.go", 1, false}, {"", 0, false}, {"R100\tapi/one.go", 0, true}, {"M\t\"quoted.go\"", 0, true}} {
		v, e := ParseChanges(strings.NewReader(tc.in))
		if (e != nil) != tc.bad || (!tc.bad && len(v) != tc.count) {
			t.Fatalf("%q: %v %v", tc.in, v, e)
		}
	}
}

func TestTestNames(t *testing.T) {
	f, e := parser.ParseFile(token.NewFileSet(), "names_test.go", `package p
func Test(t *testing.T){}
func TestA(t *testing.T){}
func TestMain(m *testing.M){}
func Testhelper(){}
func (x T) TestMethod(){}
`, 0)
	if e != nil {
		t.Fatal(e)
	}
	if got := strings.Join(testNames(f), ","); got != "Test,TestA" {
		t.Fatal(got)
	}
}
