// Moved from server (units_scope_test.go) on 2026-11-23, with UnitConfig.

package assembly

import (
	"context"
	gitsha1 "crypto/sha1"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// scopeTrees serves commits by name from files by path, as unitplan reads them.
func scopeTrees(t *testing.T, commits map[string]map[string]string) unitexec.TreesFunc {
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
		dirID := func(d string) string {
			sort.Strings(dirs[d])
			return gitBlobID("tree:" + strings.Join(dirs[d], "\n"))
		}
		for d := range dirs {
			if d != "" {
				entries = append(entries, unitplan.Entry{Path: d, Dir: true, ID: dirID(d)})
			}
		}
		return unitplan.NewTree(dirID(""), entries, func(id string) ([]byte, error) { return []byte(blobs[id]), nil })
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
	got, err := UnitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c)
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
	if got, err = UnitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c); err != nil || got.Declared || len(got.Modules) != 0 || !strings.Contains(got.Note, "one block") {
		t.Errorf("no declaration: %+v %v", got, err)
	}
	files["bifrost.units.yaml"] = "version: 7\n"
	if got, err = UnitConfig(context.Background(), scopeTrees(t, map[string]map[string]string{c: files}), "r", c); err != nil || !got.Declared || len(got.Modules) != 0 || !strings.Contains(got.Note, "does not read") {
		t.Errorf("a declaration that does not read: %+v %v", got, err)
	}
}

// gitBlobID is the git object id of a file's content, as Gitea names blobs.
func gitBlobID(content string) string {
	return fmt.Sprintf("%x", gitsha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
