package codeguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Tests that reach into another domain are debt, counted apart and held to
// what there is (2026-11-17).
//
// Origin: LAW-10 (2026-11-16: domains speak only through contracts;
// judging and testing go only through the contract, black box) and the
// Owner's word of 2026-11-17 12:1x, through the coordinator: the standard
// for where a test sits and what it may touch is software engineering's, and
// CI follows it; what does not meet it is debt. The CI line's page
// (coordinator's shared notes duty/from-ci-w1-2026-11-17-test-standard.md)
// is the rule in words. A test is a unit test of its own domain, a contract
// test of the provider through what it offers, or a declared flow test
// (tests_have_owner_test.go). Two kinds are none of these:
//
//   - a test file in a package of one domain that imports a package of
//     another domain, or one several domains share, without declaring
//     itself a flow test (testsReachingAcross). The domain walls read
//     `go list {{.Imports}}`, production code only, so nothing held these.
//   - a test file in a package several domains share (testFilesInShared): it
//     reaches every domain's insides in that package. An owner line
//     (//bifrost:owner) says who answers for it and does not change what it
//     reaches; before today it was enough for a new one to land. The plan
//     tests left in agentrt/store (TD-582, 30 files) are of this kind.
//
// Both lists are what the tree had on 2026-11-17 and only shrink. A file
// leaves by moving to its domain's package as a unit test, becoming a black
// box test of the contract, or being declared a flow test.

// reaching sorts the test files: across is, by file, the packages of other
// domains (or shared) it imports; shared the files in shared packages. The
// assembly roots under api/cmd wire every domain and are left out, and so are
// the tests of the assembly domain (LAW-10, 2026-11-21): integration is its
// system test, and its tests reach every domain as its code does.
//
// A package another domain made public (contracts.public_packages, the E5
// gate's list) is its contract: a test that imports it is the black box
// test of what that domain offers, the kind LAW-10 asks for, and is
// not counted (2026-11-23, Foundation, LAW-5: once coordination and
// delivery named their public packages, 9 of the 13 held lines were tests
// using exactly those, and counting them as debt asked the testing domain
// to work around a contract the provider had just committed to).
func reaching(o ownership, public map[string]map[string]bool, files []testFile) (across map[string][]string, shared []string) {
	base := baseOf(o)
	across = map[string][]string{}
	for _, f := range files {
		if strings.HasPrefix(f.Pkg, "api/cmd/") || o.domainOf(f.Pkg) == assembly {
			continue
		}
		from := o.domainOf(f.Pkg)
		if from == "mixed" {
			shared = append(shared, f.Path)
			continue
		}
		if f.Flow != "" {
			continue
		}
		for _, dep := range f.Imports {
			if dep == f.Pkg {
				continue
			}
			if to := o.domainOf(dep); to != from && to != base && !public[to][dep] {
				across[f.Path] = append(across[f.Path], dep)
			}
		}
	}
	for p := range across {
		sort.Strings(across[p])
	}
	sort.Strings(shared)
	return across, shared
}

// reachErrors compares the tree with the two baselines.
func reachErrors(o ownership, across map[string][]string, shared []string, baseAcross map[string][]string, baseShared []string) []string {
	var out []string
	held := map[string]bool{}
	for f, deps := range baseAcross {
		for _, d := range deps {
			held[f+" -> "+d] = true
		}
	}
	now := map[string]bool{}
	for f, deps := range across {
		for _, d := range deps {
			now[f+" -> "+d] = true
			if !held[f+" -> "+d] {
				out = append(out, fmt.Sprintf("%s imports %s (%s), a package of another domain or one several domains share: a test reaches another domain only through its contract (LAW-10). Make it a unit test of %s, a black box test of what %s offers, or a flow test (%s owner=<domain> contracts=<name>...). The list testsReachingAcross only shrinks.", f, d, o.domainOf(d), o.domainOf(filepath.Dir(f)), o.domainOf(d), flowMark))
			}
		}
	}
	for pair := range held {
		if !now[pair] {
			out = append(out, fmt.Sprintf("%s is gone: take it off testsReachingAcross (the list only shrinks)", pair))
		}
	}
	inBase := map[string]bool{}
	for _, f := range baseShared {
		inBase[f] = true
	}
	inTree := map[string]bool{}
	for _, f := range shared {
		inTree[f] = true
		if !inBase[f] {
			out = append(out, fmt.Sprintf("%s is a new test file in %s, a package several domains share: it reaches every domain's insides there (LAW-10). Put it in the package of the domain it tests, make it a black box test of a contract, or a flow test (%s owner=<domain> contracts=<name>...); an owner line names who answers for it and is not enough. The list testFilesInShared only shrinks.", f, filepath.Dir(f), flowMark))
		}
	}
	for f := range inBase {
		if !inTree[f] {
			out = append(out, fmt.Sprintf("%s is no longer a test file of a shared package: take it off testFilesInShared (the list only shrinks)", f))
		}
	}
	sort.Strings(out)
	return out
}

func TestTestsReachIntoOtherDomainsOnlyAsMuchAsTheyDid(t *testing.T) {
	raw := ownershipRaw(t)
	var o ownership
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	public, err := publicOf(raw)
	if err != nil {
		t.Fatal(err)
	}
	files := readTestFiles(t)
	across, shared := reaching(o, public, files)
	if os.Getenv("CODEGUARD_PRINT_REACH") != "" {
		printReachBaselines(t, across, shared)
	}
	n := 0
	for _, deps := range across {
		n += len(deps)
	}
	t.Logf("tests reaching across: %d files, %d imports; test files in shared packages: %d", len(across), n, len(shared))
	for _, msg := range reachErrors(o, across, shared, testsReachingAcross, testFilesInShared) {
		t.Error(msg)
	}
}

// printReachBaselines writes the two lists as Go, for the day a list is cut.
func printReachBaselines(t *testing.T, across map[string][]string, shared []string) {
	var b strings.Builder
	b.WriteString("var testsReachingAcross = map[string][]string{\n")
	var files []string
	for f := range across {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(&b, "\t%q: {", f)
		for i, d := range across[f] {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", d)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\nvar testFilesInShared = []string{\n")
	for _, f := range shared {
		fmt.Fprintf(&b, "\t%q,\n", f)
	}
	b.WriteString("}\n")
	t.Log("\n" + b.String())
}

// The positive control of TestTestsReachIntoOtherDomainsOnlyAsMuchAsTheyDid
// (2026-11-17, the ruling above), on a made-up tree. A test of one domain's
// package that imports another domain's or a shared package is counted, one
// that imports its own domain or the base is not, a declared flow test is
// not; every test file of a shared package is counted, an owner line or not.
// Over the baselines each is named with what to do; under them the entry to
// take off is named.
func TestReachingTestsAreHeldToTheBaselines(t *testing.T) {
	o := controlOwnership(t)
	o.Packages["api/internal/owner/api"] = struct{ Domains []string }{Domains: []string{"owner"}}
	files := []testFile{
		{Path: "api/internal/plan/a_test.go", Pkg: "api/internal/plan", Imports: []string{"api/internal/owner", "api/internal/unlisted"}},
		{Path: "api/internal/plan/b_test.go", Pkg: "api/internal/plan", Imports: []string{"api/internal/both", "api/internal/plan"}},
		{Path: "api/internal/plan/own_test.go", Pkg: "api/internal/plan", Imports: []string{"api/internal/plan", "api/internal/unlisted"}},
		{Path: "api/internal/plan/flow_test.go", Pkg: "api/internal/plan", Imports: []string{"api/internal/owner"}, Flow: "//bifrost:flow owner=plan contracts=owner.card"},
		{Path: "api/internal/both/x_test.go", Pkg: "api/internal/both"},
		{Path: "api/internal/both/y_test.go", Pkg: "api/internal/both", Owner: "//bifrost:owner plan"},
		{Path: "api/cmd/root/z_test.go", Pkg: "api/cmd/root", Imports: []string{"api/internal/owner"}},
		{Path: "api/internal/plan/contract_test.go", Pkg: "api/internal/plan", Imports: []string{"api/internal/owner/api"}},
	}
	across, shared := reaching(o, map[string]map[string]bool{"owner": {"api/internal/owner/api": true}}, files)
	if fmt.Sprint(across) != "map[api/internal/plan/a_test.go:[api/internal/owner] api/internal/plan/b_test.go:[api/internal/both]]" ||
		strings.Join(shared, ",") != "api/internal/both/x_test.go,api/internal/both/y_test.go" {
		t.Fatalf("across = %v, shared = %v", across, shared)
	}
	if got := reachErrors(o, across, shared, across, shared); len(got) != 0 {
		t.Fatalf("at the baselines: %q", got)
	}
	got := reachErrors(o, across, shared,
		map[string][]string{"api/internal/plan/a_test.go": {"api/internal/owner"}},
		[]string{"api/internal/both/x_test.go"})
	if len(got) != 2 || !strings.Contains(got[0], "api/internal/both/y_test.go is a new test file in api/internal/both") || !strings.Contains(got[0], "an owner line names who answers for it and is not enough") ||
		!strings.Contains(got[1], "api/internal/plan/b_test.go imports api/internal/both (mixed)") || !strings.Contains(got[1], flowMark+" owner=<domain>") {
		t.Fatalf("over the baselines: %q", got)
	}
	got = reachErrors(o, across, shared,
		map[string][]string{"api/internal/plan/a_test.go": {"api/internal/owner"}, "api/internal/plan/b_test.go": {"api/internal/both"}, "api/internal/plan/gone_test.go": {"api/internal/owner"}},
		[]string{"api/internal/both/x_test.go", "api/internal/both/y_test.go", "api/internal/both/gone_test.go"})
	want := []string{
		"api/internal/both/gone_test.go is no longer a test file of a shared package: take it off testFilesInShared (the list only shrinks)",
		"api/internal/plan/gone_test.go -> api/internal/owner is gone: take it off testsReachingAcross (the list only shrinks)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("under the baselines = %q, want %q", got, want)
	}
}
