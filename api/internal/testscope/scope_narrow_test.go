package testscope

import (
	"fmt"
	"strings"
	"testing"
)

func TestScopeNarrowTestdataPackage(t *testing.T) {
	root := fixture(t)
	put(t, root, "p1/testdata/golden.txt", "x\n")
	put(t, root, "reader/r.go", "package reader\n")
	put(t, root, "reader/r_test.go", "package reader\nimport \"testing\"\nfunc TestRead(t *testing.T){_ = \"../p1/testdata/golden.txt\"}\n")
	put(t, root, "pathreader/r.go", "package pathreader\n")
	put(t, root, "pathreader/r_test.go", "package pathreader\nimport \"testing\"\nfunc TestRead(t *testing.T){_ = \"p1/testdata\"}\n")
	r := SelectChanges(root, strings.NewReader("M\tapi/p1/testdata/golden.txt"))
	if r.Scope != "incremental" || !strings.Contains(r.Why, "testdata") {
		t.Fatalf("%+v", r)
	}
	got := strings.Fields(r.Packages)
	want := []string{"example.org/api/p1", "example.org/api/reader"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("packages %v, want %v", got, want)
	}
	if !strings.Contains(r.Why, "example.org/api/reader") {
		t.Fatalf("why: %+v", r)
	}
	// A nested testdata directory belongs to the package above its first testdata element.
	r = SelectChanges(root, strings.NewReader("A\tapi/p1/testdata/sub/new.json"))
	if r.Scope != "incremental" || r.Packages != "example.org/api/p1" {
		t.Fatalf("nested: %+v", r)
	}
	// A testdata directory without an owning package is still a full run.
	r = SelectChanges(root, strings.NewReader("M\tapi/nowhere/testdata/x"))
	if r.Scope != "full" || !strings.Contains(r.Why, "unresolved") {
		t.Fatalf("unowned: %+v", r)
	}
}

func TestScopeNarrowTestdataStore(t *testing.T) {
	root := fixture(t)
	dir := "internal/agentrt/store/"
	put(t, root, dir+"alpha.go", "package store\nfunc Alpha(){}\n")
	put(t, root, dir+"testdata/named.json", "{}\n")
	put(t, root, dir+"testdata/unnamed.json", "{}\n")
	for name, body := range map[string]string{
		"named_test.go": "package store\nimport \"testing\"\nfunc TestNamed(t *testing.T){_ = \"testdata/named.json\"}",
		"a_test.go":     "package store\nimport \"testing\"\nfunc TestA(t *testing.T){}",
		"b_test.go":     "package store\nimport \"testing\"\nfunc TestB(t *testing.T){}",
		"c_test.go":     "package store\nimport \"testing\"\nfunc TestC(t *testing.T){}",
	} {
		put(t, root, dir+name, body)
	}
	r := SelectChanges(root, strings.NewReader("M\tapi/"+dir+"testdata/named.json"))
	if r.Scope != "incremental" || r.Packages != "example.org/api/internal/agentrt/store" || r.StoreRun != "^(TestNamed)$" {
		t.Fatalf("named: %+v", r)
	}
	r = SelectChanges(root, strings.NewReader("M\tapi/"+dir+"testdata/unnamed.json"))
	if r.Scope != "incremental" || r.Packages != "example.org/api/internal/agentrt/store" || r.StoreRun != "" {
		t.Fatalf("unnamed: %+v", r)
	}
	// A store test naming another package's testdata selects the store with that test only.
	put(t, root, "p2/testdata/shared.txt", "x\n")
	put(t, root, dir+"a_test.go", "package store\nimport \"testing\"\nfunc TestA(t *testing.T){_ = \"shared.txt\"}")
	r = SelectChanges(root, strings.NewReader("M\tapi/p2/testdata/shared.txt"))
	if r.Scope != "incremental" || r.Packages != "example.org/api/internal/agentrt/store example.org/api/p2" || r.StoreRun != "^(TestA)$" {
		t.Fatalf("store reader: %+v", r)
	}
}

func TestScopeNarrowLargeSelection(t *testing.T) {
	root := fixture(t)
	dir := "internal/agentrt/store/"
	put(t, root, dir+"alpha.go", "package store\nimport _ \"example.org/api/p0\"\nfunc Alpha(){}\n")
	put(t, root, dir+"beta.go", "package store\nfunc Beta(){}\n")
	put(t, root, dir+"alpha_test.go", "package store\nimport \"testing\"\nfunc TestAlpha(t *testing.T){Alpha()}")
	put(t, root, dir+"beta_test.go", "package store\nimport \"testing\"\nfunc TestBeta(t *testing.T){Beta()}")
	put(t, root, dir+"other_test.go", "package store\nimport \"testing\"\nfunc TestOther(t *testing.T){}")
	var lines []string
	for i := 0; i < 15; i++ {
		lines = append(lines, fmt.Sprintf("M\tapi/p%d/p.go", i))
	}
	r := SelectChanges(root, strings.NewReader(strings.Join(lines, "\n")))
	if r.Scope != "incremental" || strings.Contains(r.Why, "40%") || r.StoreRun != "^(TestAlpha)$" {
		t.Fatalf("%+v", r)
	}
	if n := len(strings.Fields(r.Packages)); n < 15 {
		t.Fatalf("selected %d packages: %+v", n, r)
	}
}

func TestScopeNarrowFullCauses(t *testing.T) {
	root := fixture(t)
	put(t, root, "helpers/testdata/x.txt", "x\n")
	for _, tc := range []struct{ line, why string }{
		{"M\tapi/go.mod", "module dependencies"},
		{"M\tapi/go.sum", "module dependencies"},
		{"A\tapi/internal/agentrt/migrations/x.sql", "protected path"},
		{"M\tapi/internal/testscope/testdata/x", "protected path"},
		{"M\tapi/cmd/testscope/main.go", "protected path"},
		{"M\tscripts/ci/test_api_scoped.sh", "selector script"},
		{"M\tapi/helpers/h.go", "shared test helper"},
		{"M\tapi/helpers/testdata/x.txt", "shared test helper"},
		{"D\tapi/gone/x.go", "unresolved Go package"},
		{"M\tapi/p0/testdata/x\nM\tapi/go.mod", "module dependencies"},
		{"bad", "change list error"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			r := SelectChanges(root, strings.NewReader(tc.line))
			if r.Scope != "full" || !strings.Contains(r.Why, tc.why) {
				t.Fatalf("%+v", r)
			}
		})
	}
	if r := SelectChanges(t.TempDir(), strings.NewReader("M\tapi/p0/testdata/x")); r.Scope != "full" || !strings.Contains(r.Why, "graph error") {
		t.Fatalf("read error: %+v", r)
	}
	if r := Select(root, "missing", "HEAD"); r.Scope != "full" {
		t.Fatalf("git error: %+v", r)
	}
}

func TestGuardOnlySelection(t *testing.T) {
	root := fixture(t)
	dir := "internal/agentrt/agentrtapi/"
	pkg := "example.org/api/internal/agentrt/agentrtapi"
	put(t, root, dir+"api.go", "package agentrtapi\n")
	put(t, root, dir+"walk_test.go", "package agentrtapi\nimport (\"path/filepath\";\"testing\";_ \"example.org/api/p0\")\nfunc TestWalk(t *testing.T){filepath.WalkDir(\".\",nil)}\nfunc TestAnother(t *testing.T){}\n")
	put(t, root, dir+"pool_test.go", "package agentrtapi\nimport \"testing\"\nfunc TestPools(t *testing.T){}\n")
	r := SelectChanges(root, strings.NewReader("M\tapi/p1/p.go"))
	if r.Scope != "incremental" || r.GuardPackages != pkg || r.GuardRun != "^(TestAnother|TestPools|TestWalk)$" || strings.Contains(r.Packages, pkg) {
		t.Fatalf("leaf: %+v", r)
	}
	for _, line := range []string{"M\tapi/p0/p.go", "M\tapi/" + dir + "api.go", "M\tapi/" + dir + "testdata/x"} {
		r = SelectChanges(root, strings.NewReader(line))
		if r.Scope != "incremental" || !strings.Contains(r.Packages, pkg) || r.GuardPackages != "" || r.GuardRun != "" {
			t.Fatalf("whole %s: %+v", line, r)
		}
	}
	put(t, root, dir+"helper_test.go", "package agentrtapi\nimport \"path/filepath\"\nfunc walkHelper(){filepath.Walk(\".\",nil)}\n")
	r = SelectChanges(root, strings.NewReader("M\tapi/p1/p.go"))
	if r.Scope != "incremental" || !strings.Contains(r.Packages, pkg) || r.GuardPackages != "" || r.GuardRun != "" {
		t.Fatalf("helper: %+v", r)
	}
}

func TestFullWritesEmptyGuards(t *testing.T) {
	var out strings.Builder
	Full("reason").Write(&out)
	if out.String() != "scope=full\npackages=\nstore_run=\nguard_packages=\nguard_run=\nwhy=reason\n" {
		t.Fatal(out.String())
	}
}
