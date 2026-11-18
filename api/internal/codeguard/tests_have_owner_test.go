package codeguard

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A test has an owner: its file sits in a package the ownership table gives
// to one domain, or the file declares itself a flow test across domains.
//
// Origin: Owner 2026-11-16. The code had been split into domains
// (config/domain-ownership.json) and the tests had not: of some 2,200 Go
// tests about 850 sat in the seven packages several domains share, so a red
// one had no domain to go to. Ruling: every test file sits in a package of
// ONE domain or declares itself a cross-domain flow test; a new test that
// does neither fails the landing; the ones that existed are a baseline per
// package that only shrinks.
//
// The declaration is a line before the package clause of the test file:
//
//	//bifrost:flow owner=<domain> contracts=<name>[,<name>...]
//
// owner is a domain id of the table and answers for the test; contracts
// names what of the other domains the flow goes through. The file is an
// external test package (package x_test) and imports, of this module, only
// packages of its owner and of the base domain.

// flowMark starts the declaration line.
const flowMark = "//bifrost:flow"

// maxFlowTestFiles caps the declared flow tests, so that the category does
// not become the next heap nobody owns. Raising it is a ruling, not a fix.
const maxFlowTestFiles = 40

// testFile is what the check reads of one *_test.go file.
type testFile struct {
	Path    string   // from the repository root, for messages
	Pkg     string   // directory from the repository root: "api/internal/x"
	PkgName string   // the package clause: "x" or "x_test"
	Tests   int      // top-level Test, Benchmark, Fuzz and Example functions
	Imports []string // imports of this module's packages, named like Pkg
	Flow    string   // the declaration line, "" when the file has none
	Owner   string   // the "//bifrost:owner <domain>" line, "" when the file has none
}

// ownerMark is how a test file in a package several domains share says which
// one of them answers for it, before its package clause:
//
//	//bifrost:owner <domain>
//
// The domain is one of those the package's row names. The file is then no
// longer without an owner, and it is counted apart (declared in a shared
// package) until it moves to a package of that domain. A new test in a
// shared package lands this way: the Owner's rule is that a test names its
// domain, not that nothing may be added before the shared packages are split.
const ownerMark = "//bifrost:owner"

// ownerRefusal says why an owner declaration does not hold, "" when it does.
func ownerRefusal(o ownership, f testFile) string {
	name := strings.TrimSpace(strings.TrimPrefix(f.Owner, ownerMark))
	row, listed := o.Packages[f.Pkg]
	switch {
	case o.domainOf(f.Pkg) != "mixed":
		return fmt.Sprintf("%s is declared for a file of %s, which config/domain-ownership.json already gives to %s: take the line out", ownerMark, f.Pkg, o.domainOf(f.Pkg))
	case name == "" || strings.ContainsAny(name, " \t"):
		return fmt.Sprintf("%q is not \"%s <domain>\"", f.Owner, ownerMark)
	case !listed:
		return fmt.Sprintf("%s has no row in config/domain-ownership.json", f.Pkg)
	}
	for _, d := range row.Domains {
		if d == name {
			return ""
		}
	}
	return fmt.Sprintf("%s names %q, which is not one of the domains %s is shared by (%s)", ownerMark, name, f.Pkg, strings.Join(row.Domains, ", "))
}

// baseOf is the default domain, which every domain stands on.
func baseOf(o ownership) string {
	if len(o.Default.Domains) == 1 {
		return o.Default.Domains[0]
	}
	return "mixed"
}

// flowRefusal is why a file's flow declaration does not hold, "" when it
// does.
func flowRefusal(o ownership, f testFile) string {
	owner, contracts := "", ""
	for _, field := range strings.Fields(strings.TrimPrefix(f.Flow, flowMark)) {
		key, value, _ := strings.Cut(field, "=")
		switch key {
		case "owner":
			owner = value
		case "contracts":
			contracts = value
		default:
			return "unknown field " + strconv.Quote(field) + " (the line is: " + flowMark + " owner=<domain> contracts=<name>[,<name>...])"
		}
	}
	declared := false
	var ids []string
	for _, d := range o.Domains {
		ids = append(ids, d.ID)
		declared = declared || d.ID == owner
	}
	if !declared {
		return "owner " + strconv.Quote(owner) + " is not a domain of config/domain-ownership.json (" + strings.Join(ids, ", ") + ")"
	}
	if contracts == "" {
		return "contracts is empty: name what of the other domains the flow goes through"
	}
	for _, name := range strings.Split(contracts, ",") {
		if name == "" {
			return "contracts has an empty name: " + strconv.Quote(contracts)
		}
	}
	if !strings.HasSuffix(f.PkgName, "_test") {
		return "package " + f.PkgName + " is internal: a flow test is an external test package (package " + f.PkgName + "_test)"
	}
	base := baseOf(o)
	for _, dep := range f.Imports {
		if d := o.domainOf(dep); d != owner && d != base {
			return "imports " + dep + " (" + d + "): a flow test imports packages of its owner (" + owner + ") and of " + base + ", and reaches the rest through the contracts it names"
		}
	}
	return ""
}

// testOwners sorts the files: without is, per package, the number of test
// functions in files that have no owner; flows are the files with a
// declaration that holds; refused names each declaration that does not, and
// the cap.
func testOwners(o ownership, files []testFile) (without map[string]int, flows, refused []string) {
	without = map[string]int{}
	for _, f := range files {
		if f.Flow != "" {
			if why := flowRefusal(o, f); why != "" {
				refused = append(refused, f.Path+": "+why)
			} else {
				flows = append(flows, f.Path)
			}
			continue
		}
		if f.Owner != "" {
			if why := ownerRefusal(o, f); why != "" {
				refused = append(refused, f.Path+": "+why)
			}
			continue
		}
		if o.domainOf(f.Pkg) == "mixed" && f.Tests > 0 {
			without[f.Pkg] += f.Tests
		}
	}
	sort.Strings(flows)
	if len(flows) > maxFlowTestFiles {
		refused = append(refused, fmt.Sprintf("%d files declare a flow test and the cap is %d (maxFlowTestFiles): a flow test is the exception; put the test in the package of the domain that owns what it checks", len(flows), maxFlowTestFiles))
	}
	sort.Strings(refused)
	return without, flows, refused
}

// ownerBaselineErrors compares what the tree has with the baseline. pkgs are
// the packages that have test files at all.
func ownerBaselineErrors(without, baseline map[string]int, pkgs map[string]bool) []string {
	var out []string
	for pkg, n := range without {
		if held, ok := baseline[pkg]; !ok || n > held {
			out = append(out, fmt.Sprintf("a test with no owner was added to %s (%d test functions, baseline %d): several domains share that package, so nobody answers for the test. Three ways, best first: put it in a package config/domain-ownership.json gives to one domain; or, if it must stay in this package for now, put it in a file of its own that says before the package clause which of the package's domains answers for it: %s <domain>; or, if it checks a flow across domains, make the file package x_test, import only its owner's and the base's packages, and write before the package clause: %s owner=<domain> contracts=<name>[,<name>...]", pkg, n, baseline[pkg], ownerMark, flowMark))
		}
	}
	for pkg, held := range baseline {
		switch n := without[pkg]; {
		case !pkgs[pkg]:
			out = append(out, fmt.Sprintf("%s has no test files any more — take it off testsWithoutOwner (the list only shrinks)", pkg))
		case n == 0:
			out = append(out, fmt.Sprintf("%s has no test without an owner left — take it off testsWithoutOwner (the list only shrinks)", pkg))
		case n < held:
			out = append(out, fmt.Sprintf("%s is down to %d — lower testsWithoutOwner[%q] from %d to %d (the list only shrinks)", pkg, n, pkg, held, n))
		}
	}
	sort.Strings(out)
	return out
}

// testsWithoutOwner is the baseline: per package several domains share, the
// test functions in its files on 2026-11-16. It only shrinks: a test leaves
// by moving to a package of one domain (usually with the code it checks,
// when the package is split), or by its file becoming a declared flow test.
var testsWithoutOwner = map[string]int{
	"api/internal/agentrt/agentrtapi": 26,
	"api/internal/agentrt/controller": 17,
	"api/internal/agentrt/store":      61,
	"api/internal/cluster":            32,
	"api/internal/delivery":           12,
	"api/internal/server":             87,
}

// isTestFunc reports whether a top-level declaration is a function "go test"
// runs: TestXxx(*testing.T), BenchmarkXxx(*testing.B), FuzzXxx(*testing.F),
// ExampleXxx().
func isTestFunc(d ast.Decl) bool {
	fn, ok := d.(*ast.FuncDecl)
	if !ok || fn.Recv != nil || fn.Type.TypeParams != nil {
		return false
	}
	for prefix, arg := range map[string]string{"Test": "T", "Benchmark": "B", "Fuzz": "F", "Example": ""} {
		rest, ok := strings.CutPrefix(fn.Name.Name, prefix)
		if !ok || (rest != "" && rest[0] >= 'a' && rest[0] <= 'z') {
			continue
		}
		params := fn.Type.Params.List
		if arg == "" {
			return len(params) == 0 && fn.Type.Results == nil
		}
		if len(params) != 1 || len(params[0].Names) > 1 {
			return false
		}
		star, ok := params[0].Type.(*ast.StarExpr)
		if !ok {
			return false
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == arg
	}
	return false
}

// readTestFile parses one test file. root is the repository root.
func readTestFile(root, path string) (testFile, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		return testFile{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return testFile{}, err
	}
	rel = filepath.ToSlash(rel)
	f := testFile{Path: rel, Pkg: rel[:strings.LastIndex(rel, "/")], PkgName: parsed.Name.Name}
	for _, d := range parsed.Decls {
		if isTestFunc(d) {
			f.Tests++
		}
	}
	for _, imp := range parsed.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && strings.HasPrefix(p, module) {
			f.Imports = append(f.Imports, strings.TrimPrefix(p, module))
		}
	}
	for _, group := range parsed.Comments {
		for _, c := range group.List {
			if c.Pos() < parsed.Package && (c.Text == flowMark || strings.HasPrefix(c.Text, flowMark+" ")) && f.Flow == "" {
				f.Flow = c.Text
			}
			if c.Pos() < parsed.Package && (c.Text == ownerMark || strings.HasPrefix(c.Text, ownerMark+" ")) && f.Owner == "" {
				f.Owner = c.Text
			}
		}
	}
	return f, nil
}

// readTestFiles is every *_test.go under api/.
func readTestFiles(t *testing.T) []testFile {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var files []testFile
	err = filepath.WalkDir(filepath.Join(root, "api"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := readTestFile(root, path)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestEveryTestHasAnOwner(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "domain-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o ownership
	if err = json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o.domainOf("api/internal/agentrt/eventlog") != "foundation" || len(o.Packages) < 50 || len(o.Domains) < 5 {
		t.Fatalf("the ownership table did not load as expected: %d rows, %d domains", len(o.Packages), len(o.Domains))
	}
	files := readTestFiles(t)
	pkgs := map[string]bool{}
	total := 0
	for _, f := range files {
		pkgs[f.Pkg] = true
		total += f.Tests
	}
	if len(files) < 200 || total < 1000 {
		t.Fatalf("the walk found %d test files with %d test functions", len(files), total)
	}
	without, flows, refused := testOwners(o, files)
	for _, msg := range refused {
		t.Error(msg)
	}
	for _, msg := range ownerBaselineErrors(without, testsWithoutOwner, pkgs) {
		t.Error(msg)
	}
	logTestOwnership(t, o, files, without, flows)
}

// logTestOwnership prints the readings the coordinator tracks; none fails.
func logTestOwnership(t *testing.T, o ownership, files []testFile, without map[string]int, flows []string) {
	base := baseOf(o)
	total, noOwner, noRow := 0, 0, 0
	noRowPkgs := map[string]bool{}
	all, black := map[string]int{}, map[string]int{}
	acrossMixed, acrossSingle := 0, 0
	for _, f := range files {
		from := o.domainOf(f.Pkg)
		total += f.Tests
		all[from] += f.Tests
		if strings.HasSuffix(f.PkgName, "_test") {
			black[from] += f.Tests
		}
		if _, ok := o.Packages[f.Pkg]; !ok && f.Tests > 0 {
			noRow += f.Tests
			noRowPkgs[f.Pkg] = true
		}
		for _, dep := range f.Imports {
			if to := o.domainOf(dep); dep != f.Pkg && to != base && (to != from || from == "mixed") {
				if from == "mixed" {
					acrossMixed++
				} else {
					acrossSingle++
				}
			}
		}
	}
	var pkgs []string
	for pkg, n := range without {
		noOwner += n
		pkgs = append(pkgs, pkg)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if without[pkgs[i]] != without[pkgs[j]] {
			return without[pkgs[i]] > without[pkgs[j]]
		}
		return pkgs[i] < pkgs[j]
	})
	t.Logf("test functions: %d in %d files", total, len(files))
	t.Logf("without an owner: %d in %d packages; declared flow test files: %d (cap %d)", noOwner, len(pkgs), len(flows), maxFlowTestFiles)
	for _, pkg := range pkgs {
		t.Logf("  %q: %d,", pkg, without[pkg])
	}
	t.Logf("in packages with no row (owned by %s by default): %d in %d packages", base, noRow, len(noRowPkgs))
	var domains []string
	for d := range all {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, d := range domains {
		if all[d] > 0 {
			t.Logf("black-box share %-13s %4d of %4d (%d%%)", d+":", black[d], all[d], 100*black[d]/all[d])
		}
	}
	t.Logf("cross-domain imports in tests: %d (in shared packages %d, in one domain's packages %d)", acrossMixed+acrossSingle, acrossMixed, acrossSingle)
}

func controlOwnership(t *testing.T) ownership {
	t.Helper()
	var o ownership
	if err := json.Unmarshal([]byte(`{"domains":[{"id":"foundation"},{"id":"plan"},{"id":"owner"}],
		"default":{"domains":["foundation"]},"packages":{
		"api/internal/plan":{"domains":["plan"]},
		"api/internal/owner":{"domains":["owner"]},
		"api/internal/both":{"domains":["plan","owner"]}}}`), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// The positive control of TestEveryTestHasAnOwner (2026-11-16, the ruling
// above): on a made-up tree a test added to a shared package beyond its
// baseline is named with what to do; tests in one domain's package, in a
// package with no row and in a file with a declaration that holds are not;
// a baseline that is too high, or of a package that is gone, is named.
func TestOwnerCheckSeesATestAddedToASharedPackage(t *testing.T) {
	o := controlOwnership(t)
	files := []testFile{
		{Path: "api/internal/both/a_test.go", Pkg: "api/internal/both", PkgName: "both", Tests: 3},
		{Path: "api/internal/both/b_test.go", Pkg: "api/internal/both", PkgName: "both_test", Tests: 2},
		{Path: "api/internal/both/helpers_test.go", Pkg: "api/internal/both", PkgName: "both"},
		{Path: "api/internal/plan/a_test.go", Pkg: "api/internal/plan", PkgName: "plan", Tests: 7, Imports: []string{"api/internal/owner"}},
		{Path: "api/internal/unlisted/a_test.go", Pkg: "api/internal/unlisted", PkgName: "unlisted", Tests: 4},
		{Path: "api/internal/plan/flow_test.go", Pkg: "api/internal/plan", PkgName: "plan_test", Tests: 1,
			Imports: []string{"api/internal/plan", "api/internal/unlisted"}, Flow: "//bifrost:flow owner=plan contracts=owner.card"},
	}
	pkgs := map[string]bool{"api/internal/both": true, "api/internal/plan": true, "api/internal/unlisted": true}
	without, flows, refused := testOwners(o, files)
	if len(without) != 1 || without["api/internal/both"] != 5 || len(refused) != 0 || strings.Join(flows, ",") != "api/internal/plan/flow_test.go" {
		t.Fatalf("without = %v, flows = %v, refused = %v", without, flows, refused)
	}
	if got := ownerBaselineErrors(without, map[string]int{"api/internal/both": 5}, pkgs); len(got) != 0 {
		t.Fatalf("at the baseline: %q", got)
	}
	got := ownerBaselineErrors(without, map[string]int{"api/internal/both": 4}, pkgs)
	if len(got) != 1 || !strings.Contains(got[0], "a test with no owner was added to api/internal/both (5 test functions, baseline 4)") || !strings.Contains(got[0], flowMark+" owner=<domain> contracts=") {
		t.Fatalf("one over the baseline: %q", got)
	}
	got = ownerBaselineErrors(without, map[string]int{}, pkgs)
	if len(got) != 1 || !strings.Contains(got[0], "added to api/internal/both") {
		t.Fatalf("a package not in the baseline: %q", got)
	}
	got = ownerBaselineErrors(without, map[string]int{"api/internal/both": 6, "api/internal/plan": 2, "api/internal/gone": 9}, pkgs)
	want := []string{
		"api/internal/both is down to 5 — lower testsWithoutOwner[\"api/internal/both\"] from 6 to 5 (the list only shrinks)",
		"api/internal/gone has no test files any more — take it off testsWithoutOwner (the list only shrinks)",
		"api/internal/plan has no test without an owner left — take it off testsWithoutOwner (the list only shrinks)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("a baseline that is too high = %q, want %q", got, want)
	}
}

// The positive control of the flow declaration (2026-11-16, the ruling
// above): each declaration that does not hold is refused with its reason,
// and one that holds in a shared package's directory cannot exist, because
// the file would import the shared package.
// Origin: 2026-11-16, the Owner's rule the same evening: a test that is added names its domain. A file in a shared
// package that says which of the package's domains answers for it is owned (the server's handler tests live in the
// shared server package until it is split, and every lane adds some); what it may not do is name a domain the package
// is not shared by, name none, or sit in a package that already has one domain. The first file is the control: the
// same file without the line is counted without an owner.
func TestATestInASharedPackageMayNameItsDomain(t *testing.T) {
	o := controlOwnership(t)
	file := func(name, pkg, owner string) testFile {
		return testFile{Path: pkg + "/" + name, Pkg: pkg, PkgName: "x", Tests: 2, Owner: owner}
	}
	without, _, refused := testOwners(o, []testFile{
		file("none_test.go", "api/internal/both", ""),
		file("mine_test.go", "api/internal/both", "//bifrost:owner plan"),
		file("other_test.go", "api/internal/both", "//bifrost:owner tenant"),
		file("empty_test.go", "api/internal/both", "//bifrost:owner"),
		file("single_test.go", "api/internal/plan", "//bifrost:owner plan"),
	})
	if len(without) != 1 || without["api/internal/both"] != 2 {
		t.Fatalf("without an owner: %v, want the two tests of none_test.go only", without)
	}
	want := []string{"empty_test.go", "is not", "other_test.go", "not one of the domains", "single_test.go", "already gives to plan"}
	if len(refused) != 3 {
		t.Fatalf("refused: %q", refused)
	}
	for i := 0; i < len(want); i += 2 {
		found := false
		for _, r := range refused {
			found = found || (strings.Contains(r, want[i]) && strings.Contains(r, want[i+1]))
		}
		if !found {
			t.Errorf("no refusal of %s saying %q in %q", want[i], want[i+1], refused)
		}
	}
}

func TestFlowDeclarationIsRefusedWithItsReason(t *testing.T) {
	o := controlOwnership(t)
	flow := func(name, pkgName, line string, imports ...string) testFile {
		return testFile{Path: "api/internal/plan/" + name, Pkg: "api/internal/plan", PkgName: pkgName, Tests: 1, Imports: imports, Flow: line}
	}
	without, flows, refused := testOwners(o, []testFile{
		flow("ok_test.go", "plan_test", "//bifrost:flow owner=plan contracts=owner.card,owner.answer", "api/internal/plan", "api/internal/unlisted"),
		flow("a_test.go", "plan_test", "//bifrost:flow owner=planning contracts=owner.card"),
		flow("b_test.go", "plan_test", "//bifrost:flow owner=plan"),
		flow("c_test.go", "plan_test", "//bifrost:flow owner=plan contracts="),
		flow("d_test.go", "plan", "//bifrost:flow owner=plan contracts=owner.card"),
		flow("e_test.go", "plan_test", "//bifrost:flow owner=plan contracts=owner.card", "api/internal/plan", "api/internal/owner"),
		flow("f_test.go", "plan_test", "//bifrost:flow owner=plan contracts=owner.card", "api/internal/both"),
		flow("g_test.go", "plan_test", "//bifrost:flow contracts=owner.card"),
		flow("h_test.go", "plan_test", "//bifrost:flow owner=plan contracts=owner.card reason=because"),
		flow("i_test.go", "plan_test", "//bifrost:flow owner=plan contracts=owner.card,"),
	})
	if len(without) != 0 || strings.Join(flows, ",") != "api/internal/plan/ok_test.go" {
		t.Fatalf("without = %v, flows = %v", without, flows)
	}
	want := []string{
		`a_test.go: owner "planning" is not a domain of`,
		`b_test.go: contracts is empty`,
		`c_test.go: contracts is empty`,
		`d_test.go: package plan is internal: a flow test is an external test package (package plan_test)`,
		`e_test.go: imports api/internal/owner (owner)`,
		`f_test.go: imports api/internal/both (mixed)`,
		`g_test.go: owner "" is not a domain of`,
		`h_test.go: unknown field "reason=because"`,
		`i_test.go: contracts has an empty name`,
	}
	if len(refused) != len(want) {
		t.Fatalf("refused = %q", refused)
	}
	for i, w := range want {
		if !strings.Contains(refused[i], "api/internal/plan/"+w) {
			t.Errorf("refused[%d] = %q, want it to contain %q", i, refused[i], w)
		}
	}
}

// The positive control of the cap (2026-11-16, the ruling above): one
// declared flow test file more than maxFlowTestFiles is refused, the cap
// itself is not.
func TestFlowTestsAreCapped(t *testing.T) {
	o := controlOwnership(t)
	var files []testFile
	for i := 0; i < maxFlowTestFiles; i++ {
		files = append(files, testFile{Path: fmt.Sprintf("api/internal/plan/f%02d_test.go", i), Pkg: "api/internal/plan", PkgName: "plan_test", Tests: 1, Flow: "//bifrost:flow owner=plan contracts=owner.card"})
	}
	if _, flows, refused := testOwners(o, files); len(flows) != maxFlowTestFiles || len(refused) != 0 {
		t.Fatalf("at the cap: %d flows, refused %q", len(flows), refused)
	}
	files = append(files, testFile{Path: "api/internal/plan/one_more_test.go", Pkg: "api/internal/plan", PkgName: "plan_test", Tests: 1, Flow: "//bifrost:flow owner=plan contracts=owner.card"})
	_, _, refused := testOwners(o, files)
	if len(refused) != 1 || !strings.Contains(refused[0], fmt.Sprintf("%d files declare a flow test and the cap is %d", maxFlowTestFiles+1, maxFlowTestFiles)) {
		t.Fatalf("one over the cap: %q", refused)
	}
}

// The positive control of the reader (2026-11-16, the ruling above): on a
// made-up file it counts the functions "go test" runs and no others, finds
// the declaration before the package clause and not one after it, and keeps
// this module's imports only.
func TestOwnerCheckReadsATestFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "api", "internal", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `// A flow.
//
//bifrost:flow owner=plan contracts=owner.card
package x_test

import (
	"testing"

	plan "` + module + `api/internal/plan"
)

//bifrost:flow owner=owner contracts=late
func TestOne(t *testing.T)        { _ = plan.X }
func TestMain(m *testing.M)       {}
func Testhelper(t *testing.T)     {}
func helper(t *testing.T)         {}
func BenchmarkTwo(b *testing.B)   {}
func FuzzThree(f *testing.F)      {}
func ExampleFour()                {}
func Example_five()               {}
func TestSix(t *testing.T, n int) {}
func (s suite) TestSeven(t *testing.T) {}

type suite struct{}
`
	path := filepath.Join(dir, "flow_test.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := readTestFile(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "api/internal/x/flow_test.go" || f.Pkg != "api/internal/x" || f.PkgName != "x_test" || f.Tests != 5 ||
		strings.Join(f.Imports, ",") != "api/internal/plan" || f.Flow != "//bifrost:flow owner=plan contracts=owner.card" {
		t.Fatalf("read %+v", f)
	}
}
