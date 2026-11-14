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
// of entries, the whole count, and truncated when the mirror cut it.
type treeMirror struct {
	trees     map[string][]giteaTreeEntry
	truncated map[string]bool
	shortBy   map[string]int // list this many fewer entries than the count says
	noCount   map[string]bool
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
		listed := entries[:len(entries)-m.shortBy[sha]]
		lo := min((page-1)*per, len(listed))
		hi := min(lo+per, len(listed))
		doc := map[string]any{"sha": sha, "tree": listed[lo:hi], "truncated": m.truncated[sha], "page": page}
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
	// 2,500 files over three pages a tree: 300 change, among them one in a
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
	for name, tc := range map[string]struct {
		mirror treeMirror
		want   string
	}{
		"the mirror truncated a tree":  {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: two}, truncated: map[string]bool{factSHA1: true}}, "truncated the tree"},
		"fewer entries than it counts": {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: two}, shortBy: map[string]int{factSHA2: 1}}, "has 2 entries and 1 were listed"},
		"no entry count":               {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: two}, noCount: map[string]bool{factSHA2: true}}, "without its entry count"},
		"a commit the mirror lacks":    {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two}}, "status 404"},
		"an entry without an id":       {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: {blob("a.go", "")}}}, "without a path, a kind or an id"},
		"a path listed twice":          {treeMirror{trees: map[string][]giteaTreeEntry{factSHA2: two, factSHA1: {blob("a.go", "1"), blob("a.go", "3")}}}, "twice"},
	} {
		s := tc.mirror.service(t)
		files, err := s.treeFilesBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: files %v, err %v; want an error with %q", name, files, err, tc.want)
		}
	}
}
