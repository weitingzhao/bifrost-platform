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
// commits. Origin: ISSUE-22 (2026-11-13): CI knows nothing of how a
// project is divided (a changes task that spelled "console/" and "api/" was
// written and reverted that night, infra 562f9538), and a
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

// Which units of a module run, beside which modules: the same two plans, read
// one unit at a time. The manifest names a module "svc" and one "web": no
// name the platform's own repository uses is needed for the answer.
// Origin: main coordinator 2026-11-14: backend skip by package goes shadow
// first; a failing package the scope would skip is the miss this looks for.
func TestScopeByUnit(t *testing.T) {
	const manifest = `version: 1
modules:
  - name: svc
    kind: go
    root: svc
    tasks: {test: {per: package}}
    inputs:
      svc/d: ["docs/d.md"]
      svc/e: ["."]
  - name: web
    kind: node
    root: web
    tasks: {test: {}, build: {}}
`
	files := func() map[string]string {
		return map[string]string{ManifestPath: manifest, "svc/go.mod": goMod, "svc/go.sum": "sum-1\n",
			"svc/a/a.go": "package a\n\nimport _ \"example.com/m/b\"\n", "svc/a/a_test.go": "package a\n",
			"svc/b/b.go": "package b\n", "svc/b/b_test.go": "package b\n",
			"svc/c/c.go": "package c\n", "svc/c/c_test.go": "package c\n",
			"svc/d/d.go": "package d\n", "svc/d/d_test.go": "package d\n",
			"svc/e/e.go": "package e\n", "svc/e/e_test.go": "package e\n",
			"docs/d.md": "d", "docs/other.md": "o", "web/index.ts": "1"}
	}
	line := func(s Scope) string {
		var out []string
		for _, m := range s.Modules {
			out = append(out, fmt.Sprintf("%s run[%s] same[%s]", m.Module, strings.Join(m.RunUnits, " "), strings.Join(m.SameUnits, " ")))
		}
		return strings.Join(out, " | ")
	}
	base := scopePlan(t, strings.Repeat("b", 40), files(), nil)
	for _, tc := range []struct {
		name   string
		change func(f map[string]string)
		want   string
	}{
		// svc/e declares "." (the whole repository): it runs on every change, and only on a change.
		{"nothing", func(map[string]string) {}, "svc run[] same[svc/a svc/b svc/c svc/d svc/e] | web run[] same[web]"},
		{"a file of a package another imports", func(f map[string]string) { f["svc/b/b.go"] += "// x\n" },
			"svc run[svc/a svc/b svc/e] same[svc/c svc/d] | web run[] same[web]"},
		{"a file of a package nothing imports", func(f map[string]string) { f["svc/c/c.go"] += "// x\n" },
			"svc run[svc/c svc/e] same[svc/a svc/b svc/d] | web run[] same[web]"},
		{"a file a package lists in inputs", func(f map[string]string) { f["docs/d.md"] = "d2" },
			"svc run[svc/d svc/e] same[svc/a svc/b svc/c] | web run[] same[web]"},
		{"a file no package lists", func(f map[string]string) { f["docs/other.md"] = "o2" },
			"svc run[svc/e] same[svc/a svc/b svc/c svc/d] | web run[] same[web]"},
		{"go.mod", func(f map[string]string) { f["svc/go.mod"] = goMod + "// x\n" },
			"svc run[svc/a svc/b svc/c svc/d svc/e] same[] | web run[] same[web]"},
		// A unit with two tasks is named once.
		{"the other module", func(f map[string]string) { f["web/index.ts"] = "2" },
			"svc run[svc/e] same[svc/a svc/b svc/c svc/d] | web run[web] same[]"},
		{"a new package", func(f map[string]string) { f["svc/n/n.go"], f["svc/n/n_test.go"] = "package n\n", "package n\n" },
			"svc run[svc/e svc/n] same[svc/a svc/b svc/c svc/d] | web run[] same[web]"},
	} {
		f := files()
		tc.change(f)
		got := Compare(base, scopePlan(t, strings.Repeat("c", 40), f, nil))
		if line(got) != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, line(got), tc.want)
		}
		for _, m := range got.Modules {
			if m.Run != (len(m.RunUnits) > 0) || m.RunUnits == nil || m.SameUnits == nil {
				t.Errorf("%s: %s run=%v with units %v / %v", tc.name, m.Module, m.Run, m.RunUnits, m.SameUnits)
			}
		}
	}
	// What cannot be compared (an unreadable base, no base) runs whole by unit too, with the reason.
	whole := WholeProject("r", strings.Repeat("c", 40), "", "the base has no declaration that reads", true, base)
	if line(whole) != "svc run[svc/a svc/b svc/c svc/d svc/e] same[] | web run[web] same[]" || whole.Note == "" {
		t.Errorf("whole: %s note %q", line(whole), whole.Note)
	}
}
