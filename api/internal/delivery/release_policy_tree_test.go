//bifrost:owner delivery

package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// treeMirror serves GET .../git/trees/<sha> the way Gitea 1.21 does: a page
// of entries, the whole count, and truncated on every page of a tree that
// does not fit one page (the last page too). It holds a page to 1,000 entries
// whatever is asked, as the mirror does.
type treeMirror struct {
	trees   map[string][]giteaTreeEntry
	movesAt map[string]int // from this page on the tree answers with another id
	shortBy map[string]int // list this many fewer entries than the count says
	noCount map[string]bool
}

func (m treeMirror) service(t *testing.T) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/repos/bifrost/repo-app/git/trees/"
		sha := strings.TrimPrefix(r.URL.Path, prefix)
		entries, ok := m.trees[sha]
		if !strings.HasPrefix(r.URL.Path, prefix) || !ok || r.URL.Query().Get("recursive") != "true" {
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		}
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if per <= 0 || per > 1000 {
			per = 1000
		}
		listed := entries[:len(entries)-m.shortBy[sha]]
		lo := min((page-1)*per, len(listed))
		hi := min(lo+per, len(listed))
		root := "root-of-" + sha
		if at := m.movesAt[sha]; at > 0 && page >= at {
			root += "-moved"
		}
		doc := map[string]any{"sha": root, "tree": listed[lo:hi], "truncated": len(entries) > per, "page": page}
		if !m.noCount[sha] {
			doc["total_count"] = len(entries)
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITEA_BASE", srv.URL)
	return NewService(&config.ClusterEntry{ID: "test"})
}

func blob(path, id string) giteaTreeEntry { return giteaTreeEntry{Path: path, Type: "blob", SHA: id} }

// The complete difference of two trees, whatever its size. Origin:
// 2026-11-14. Platform PROD releases of 100 files or more were refused as
// "may be truncated"; of 7 such in four days, the complete difference showed
// 5 touching a trust anchor and 2 touching nothing a rule names, refused for
// their size alone (6319a91e..393fe6a2, 177 files; 554ec329..8770cf06, 144).
func TestTreeFilesBetweenIsCompleteAtAnySize(t *testing.T) {
	var old, next []giteaTreeEntry
	// 2,500 files over three pages a tree, each page marked truncated as the
	// mirror marks them (2026-11-14: read as "cut short", that mark refused
	// 1c280b40..6bbcb657 and every tree of over 1,000 entries): 300 change, among them one in a
	// trust-anchor directory; one is removed, one added, one becomes a submodule.
	for i := 0; i < 2500; i++ {
		path := fmt.Sprintf("src/dir%02d/f%04d.go", i%50, i)
		old = append(old, blob(path, fmt.Sprintf("%040x", i)))
		id := fmt.Sprintf("%040x", i)
		if i < 300 {
			id = fmt.Sprintf("%040x", i+100000)
		}
		next = append(next, blob(path, id))
	}
	old = append(old, giteaTreeEntry{Path: "src", Type: "tree", SHA: "t1"}, blob("api/internal/approvals/service.go", "a1"), blob("gone.md", "g1"), blob("vendored", "v1"))
	next = append(next, giteaTreeEntry{Path: "src", Type: "tree", SHA: "t2"}, blob("api/internal/approvals/service.go", "a2"), blob("added.md", "n1"),
		giteaTreeEntry{Path: "vendored", Type: "commit", SHA: "v1"})
	s := treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: old, factSHA1: next}}.service(t)
	files, err := s.treeFilesBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range files {
		has[f] = true
	}
	if len(files) != 304 || !has["api/internal/approvals/service.go"] || !has["gone.md"] || !has["added.md"] || !has["vendored"] ||
		!has["src/dir00/f0000.go"] || !has["src/dir49/f0299.go"] || has["src/dir00/f0300.go"] || has["src"] {
		t.Fatalf("%d files; anchor %v gone %v added %v submodule %v dir %v", len(files), has["api/internal/approvals/service.go"], has["gone.md"], has["added.md"], has["vendored"], has["src"])
	}
	if same, err := s.treeFilesBetween(context.Background(), "repo-app", factSHA1, factSHA1, "", ""); err != nil || len(same) != 0 {
		t.Fatalf("the same commit: %v %v", same, err)
	}
}

// What cannot be established is an error, never a shorter list: a list
// that is silently short is the very thing this reading exists to rule out.
func TestTreeFilesBetweenFailsRatherThanGuess(t *testing.T) {
	two := []giteaTreeEntry{blob("a.go", "1"), blob("b.go", "2")}
	var many []giteaTreeEntry
	for i := 0; i < 1500; i++ {
		many = append(many, blob(fmt.Sprintf("m/f%04d.go", i), fmt.Sprintf("%040x", i)))
	}
	for name, tc := range map[string]struct {
		mirror treeMirror
		want   string
	}{
		"a tree that moves under the reading": {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: many, factSHA1: two}, movesAt: map[string]int{factSHA2: 2}}, "changed between pages"},
		"a large tree listed short":           {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: many, factSHA1: two}, shortBy: map[string]int{factSHA2: 1}}, "has 1500 entries and 1499 were listed"},
		"fewer entries than it counts":        {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: two}, shortBy: map[string]int{factSHA2: 1}}, "has 2 entries and 1 were listed"},
		"no entry count":                      {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: two}, noCount: map[string]bool{factSHA2: true}}, "without its entry count"},
		"a commit the mirror lacks":           {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two}}, "status 404"},
		"an entry without an id":              {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: {blob("a.go", "")}}}, "without a path, a kind or an id"},
		"a path listed twice":                 {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: {blob("a.go", "1"), blob("a.go", "3")}}}, "twice"},
	} {
		s := tc.mirror.service(t)
		files, err := s.treeFilesBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: files %v, err %v; want an error with %q", name, files, err, tc.want)
		}
	}
}
