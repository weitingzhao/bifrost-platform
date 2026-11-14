package unitplan

import (
	"fmt"
	"strings"
	"testing"
)

func scopePlan(t *testing.T, commit string, files, ui map[string]string) *Plan {
	t.Helper()
	plan, err := Build("r", commit, testTree(t, files), Source{Pinned: func(string, string) (*Tree, error) { return testTree(t, ui), nil }})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func scopeLine(s Scope) string {
	var out []string
	for _, m := range s.Modules {
		out = append(out, fmt.Sprintf("%s run=%v %d/%d", m.Module, m.Run, m.Changed, m.Tasks))
	}
	return strings.Join(out, " | ")
}

// Which modules run is read off the declaration, by comparing the keys of two
// commits. Origin: the Owner's 2026-11-13 rule that CI knows nothing of how a
// project is divided (a changes task that spelled "console/" and "api/" was
// written and reverted that night, infra 562f9538), and his example that a
// change to the back end alone should not wait for the console's tests.
func TestScopeFollowsTheDeclaration(t *testing.T) {
	baseFiles, baseUI := planFiles()
	base := scopePlan(t, strings.Repeat("b", 40), baseFiles, baseUI)
	for _, tc := range []struct {
		name   string
		change func(files, ui map[string]string)
		want   string
		why    map[string]string // module -> a reason it must give
	}{
		{"nothing", func(_, _ map[string]string) {}, "api run=false 0/4 | console run=false 0/2 | repo run=false 0/1", nil},
		// The console's key is its own tree and what it declares it needs: a Go file is neither.
		// The repository's own check has the whole tree as its root, so it runs on every change.
		{"a file of one Go package", func(f, _ map[string]string) { f["api/a/a.go"] += "// x\n" },
			"api run=true 3/4 | console run=false 0/2 | repo run=true 1/1", map[string]string{"api": "libs", "repo": "tree"}},
		// api/e declares "." (the whole repository), so the declaration says it runs; nothing here decides that for it.
		{"a console file", func(f, _ map[string]string) { f["console/src/app.ts"] = "app2" },
			"api run=true 1/4 | console run=true 2/2 | repo run=true 1/1", map[string]string{"console": "tree", "api": "needs ."}},
		{"a file of a pinned repository the console declares", func(_, u map[string]string) { u["lib/x.ts"] = "x2" },
			"api run=true 1/4 | console run=true 2/2 | repo run=false 0/1", map[string]string{"console": "needs ui:."}},
		{"a document the repository's check declares", func(f, _ map[string]string) { f["docs/readme.md"] = "bye" },
			"api run=true 1/4 | console run=false 0/2 | repo run=true 1/1", map[string]string{"repo": "needs docs"}},
	} {
		f, u := planFiles()
		tc.change(f, u)
		got := Compare(base, scopePlan(t, strings.Repeat("c", 40), f, u))
		if !got.Declared || got.Base != base.Commit || scopeLine(got) != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, scopeLine(got), tc.want)
			continue
		}
		for _, m := range got.Modules {
			if !m.Run && len(m.Why) != 0 {
				t.Errorf("%s: %s does not run and gives a reason: %v", tc.name, m.Module, m.Why)
			}
			if want := tc.why[m.Module]; want != "" && !strings.Contains(strings.Join(m.Why, "\n"), want+" (") {
				t.Errorf("%s: %s gives %v, want %q among them", tc.name, m.Module, m.Why, want)
			}
		}
	}
}

// A task the base did not have runs: a new package is not vouched for by the
// base's results. And a commit that cannot be split runs whole, saying why
// (declaration standard §4: no declaration means everything runs).
func TestScopeOfWhatCannotBeCompared(t *testing.T) {
	files, ui := planFiles()
	base := scopePlan(t, strings.Repeat("b", 40), files, ui)
	files["api/n/n.go"], files["api/n/n_test.go"] = "package n\n", "package n\n"
	head := scopePlan(t, strings.Repeat("c", 40), files, ui)
	got := Compare(base, head)
	if got.Modules[0].Module != "api" || !strings.Contains(strings.Join(got.Modules[0].Why, "\n"), "new: the base has no such task (1 of 5 tasks)") {
		t.Fatalf("a new package: %+v", got.Modules[0])
	}
	whole := WholeProject("r", head.Commit, "", "the base has no declaration", true, head)
	if scopeLine(whole) != "api run=true 5/5 | console run=true 2/2 | repo run=true 1/1" || whole.Modules[0].Why[0] != whole.Note {
		t.Fatalf("whole: %s %+v", scopeLine(whole), whole)
	}
	if none := WholeProject("r", head.Commit, "", "no bifrost.units.yaml", false, nil); none.Declared || len(none.Modules) != 0 || none.Note == "" {
		t.Fatalf("no declaration: %+v", none)
	}
}
