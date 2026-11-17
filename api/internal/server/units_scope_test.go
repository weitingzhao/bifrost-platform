//bifrost:owner verification

package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// scopeTrees serves commits by name from files by path, as unitplan reads them.
func scopeTrees(t *testing.T, commits map[string]map[string]string) unitTreesFunc {
	t.Helper()
	return func(_ context.Context, repo, commit string) (*unitplan.Tree, error) {
		files, ok := commits[commit]
		if !ok || repo != "r" {
			return nil, fmt.Errorf("gitea: no %s@%s", repo, commit)
		}
		blobs, dirs := map[string]string{}, map[string][]string{}
		var entries []unitplan.Entry
		for p, body := range files {
			blobs[gitBlobID(body)] = body
			entries = append(entries, unitplan.Entry{Path: p, ID: gitBlobID(body)})
			for d := p; strings.Contains(d, "/"); {
				d = d[:strings.LastIndex(d, "/")]
				dirs[d] = append(dirs[d], p+"="+body)
			}
			dirs[""] = append(dirs[""], p+"="+body)
		}
		dirID := func(d string) string { sort.Strings(dirs[d]); return gitBlobID("tree:" + strings.Join(dirs[d], "\n")) }
		for d := range dirs {
			if d != "" {
				entries = append(entries, unitplan.Entry{Path: d, Dir: true, ID: dirID(d)})
			}
		}
		return unitplan.NewTree(dirID(""), entries, func(id string) ([]byte, error) { return []byte(blobs[id]), nil })
	}
}

// What the scope route answers for each kind of commit. Origin: the three
// acceptance sentences of the declaration standard (Owner, 2026-11-13): no
// declaration, everything runs; a declaration, the blocks it names; and the
// fall-backs of its §4, each saying why. The manifest here names a module
// "web": no name the platform's own repository uses is needed for an answer.
func TestUnitScope(t *testing.T) {
	t.Parallel()
	const manifest = `version: 1
modules:
  - name: svc
    kind: go
    root: svc
    tasks: {test: {per: package}}
  - name: web
    root: web
    tasks: {check: {}}
`
	base := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
		"svc/a/a.go": "package a\n", "svc/a/a_test.go": "package a\n", "web/index.ts": "1"}
	with := func(change func(map[string]string)) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		change(out)
		return out
	}
	b, c := strings.Repeat("b", 40), strings.Repeat("c", 40)
	passed := func(context.Context, string, string) (string, string, error) { return "Succeeded", "ci-1", nil }
	line := func(s unitplan.Scope) string {
		var out []string
		for _, m := range s.Modules {
			out = append(out, fmt.Sprintf("%s=%v", m.Module, m.Run))
		}
		return fmt.Sprintf("declared=%v %s", s.Declared, strings.Join(out, " "))
	}
	for _, tc := range []struct {
		name       string
		head       map[string]string
		base       string
		want, note string
	}{
		{"the back end alone", with(func(f map[string]string) { f["svc/a/a.go"] = "package a\n\nconst A = 1\n" }), b, "declared=true svc=true web=false", ""},
		{"the front end alone", with(func(f map[string]string) { f["web/index.ts"] = "2" }), b, "declared=true svc=false web=true", ""},
		{"no declaration", with(func(f map[string]string) { delete(f, "bifrost.units.yaml") }), b, "declared=false ", "the project is one block"},
		{"a declaration that does not read", with(func(f map[string]string) { f["bifrost.units.yaml"] = "version: 7\n" }), b, "declared=true ", "the declaration does not read"},
		{"no base", with(func(map[string]string) {}), "", "declared=true svc=true web=true", "no base"},
		{"the commit as its own base", with(func(map[string]string) {}), c, "declared=true svc=true web=true", "the base is the commit itself"},
	} {
		trees := scopeTrees(t, map[string]map[string]string{b: base, c: tc.head})
		got, err := unitScope(context.Background(), trees, passed, "r", c, tc.base)
		if err != nil || line(got) != tc.want || !strings.Contains(got.Note, tc.note) || (tc.note == "") != (got.Note == "") {
			t.Errorf("%s: %s note %q err %v; want %s note %q", tc.name, line(got), got.Note, err, tc.want, tc.note)
		}
	}
	// A base without a declaration that reads: everything runs, and it says so.
	trees := scopeTrees(t, map[string]map[string]string{b: with(func(f map[string]string) { delete(f, "bifrost.units.yaml") }), c: base})
	if got, err := unitScope(context.Background(), trees, passed, "r", c, b); err != nil || line(got) != "declared=true svc=true web=true" || !strings.Contains(got.Note, "the base has no declaration") {
		t.Errorf("an undeclared base: %s note %q err %v", line(got), got.Note, err)
	}
	// A module that does not run is vouched for by the base's CI run. A base
	// that was red, or that the ledger has no full CI for, vouches for
	// nothing: everything runs. Origin: 2026-11-13, main was red for about 28
	// minutes (commit 5f1e5df2); a commit made on it that touched another
	// module would otherwise have been called green.
	head := with(func(f map[string]string) { f["svc/a/a.go"] = "package a\n\nconst A = 1\n" })
	trees = scopeTrees(t, map[string]map[string]string{b: base, c: head})
	if got, err := unitScope(context.Background(), trees, passed, "r", c, b); err != nil || got.BaseRun != "ci-1" || scopeText(got) != "declared yes\nrun svc\nsame web\nend\n" {
		t.Errorf("a green base: %+v %v %q", got, err, scopeText(got))
	}
	for _, status := range []string{"Failed", ""} {
		ci := func(context.Context, string, string) (string, string, error) { return status, "ci-2", nil }
		got, err := unitScope(context.Background(), trees, ci, "r", c, b)
		if err != nil || line(got) != "declared=true svc=true web=true" || got.BaseRun != "" || !strings.Contains(got.Note, "no passing full CI of the base") {
			t.Errorf("a base the ledger says %q of: %s note %q err %v", status, line(got), got.Note, err)
		}
	}
	// A commit Gitea does not have is an error, not "everything runs": the caller decides its own fall-back.
	if _, err := unitScope(context.Background(), trees, passed, "r", strings.Repeat("d", 40), b); err == nil {
		t.Error("a commit that is not there was given a scope")
	}
}

// The configuration view's answer: what the declaration says, counted from
// the plan CI computes. Origin: the Owner's rule of 2026-11-13 that he accepts
// what he can see on the Ops page, with the data computed from the source CI
// reads; and acceptance sentence 1: a project with no declaration shows as
// one block that runs whole.
func TestUnitConfig(t *testing.T) {
	t.Parallel()
	const manifest = `version: 1
modules:
  - name: svc
    kind: go
    root: svc
    tasks: {test: {per: package}, vet: {per: module}}
    shards: {svc/a: 3}
    inputs:
      svc/a: ["."]
      svc/b: ["web/index.ts"]
  - name: web
    root: web
    needs: ["svc/a/a.go"]
    tasks: {check: {}}
`
	files := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
		"svc/a/a.go": "package a\n", "svc/a/a_test.go": "package a\n", "svc/b/b.go": "package b\n", "svc/b/b_test.go": "package b\n",
		"svc/c/c.go": "package c\n", "svc/c/c_test.go": "package c\n", "web/index.ts": "1"}
	c := strings.Repeat("c", 40)
	got, err := unitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c)
	if err != nil || !got.Declared || got.Note != "" || len(got.Modules) != 2 {
		t.Fatalf("declared: %+v %v", got, err)
	}
	svc, web := got.Modules[0], got.Modules[1]
	if svc.Name != "svc" || svc.Units != 4 || svc.DeclaredInputs != 2 || svc.WholeRepository != 1 ||
		fmt.Sprint(svc.Tasks) != "[{test 3 3} {vet 1 0}]" {
		t.Errorf("svc: %+v", svc)
	}
	if web.Name != "web" || web.Units != 1 || web.WholeRepository != 0 || fmt.Sprint(web.Needs) != "[svc/a/a.go]" || fmt.Sprint(web.Tasks) != "[{check 1 0}]" {
		t.Errorf("web: %+v", web)
	}
	delete(files, "bifrost.units.yaml")
	if got, err = unitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c); err != nil || got.Declared || len(got.Modules) != 0 || !strings.Contains(got.Note, "one block") {
		t.Errorf("no declaration: %+v %v", got, err)
	}
	files["bifrost.units.yaml"] = "version: 7\n"
	if got, err = unitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c); err != nil || !got.Declared || len(got.Modules) != 0 || !strings.Contains(got.Note, "does not read") {
		t.Errorf("a declaration that does not read: %+v %v", got, err)
	}
}
