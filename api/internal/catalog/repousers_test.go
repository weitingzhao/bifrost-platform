package catalog

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

// 2026-11-25, Foundation (R0D, #719 s5): "who still uses this repository"
// was a script run on a local checkout. Each repository's scan keeps which
// dependency files name another repository, by kind; the cluster's scan
// keeps workloads carrying the name. A longer name is not a mention of a
// shorter one, a repository does not use itself, and a file of no dependency
// kind is not read. The control: a repository nobody names has no users.
func TestRepoUsersAreReadFromDependencyFiles(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	s := &Scanner{Kept: memKept{}}
	repos, _ := json.Marshal([]string{"bifrost-platform", "bifrost-platform-plugin", "bifrost-trade-api", "infra", "lonely", ClusterRepo})
	_ = s.kept().Write(keptRepos, repos)
	files := map[string]string{
		"k8s/cicd/pipeline-land.yaml": "git-url: http://gitea/bifrost/bifrost-platform.git\n",
		"k8s/overlays/stg/app.yaml":   "image: bifrost-trade-api\n",
		"scripts/release/release.sh":  "repo=bifrost-platform-plugin\n",
		"docs/README.md":              "bifrost-trade-api\n",
		"agent-config/x.json":         `{"self": "infra", "r": "bifrost-trade-api"}`,
		"console/Dockerfile":          "FROM x\nCOPY bifrost-platform-plugins/ .\n",
	}
	var list []string
	for f := range files {
		list = append(list, f)
	}
	s.keepRepoRefs("infra", Tree{Commit: "c", Files: list, Read: func(p string) ([]byte, bool, error) { return []byte(files[p]), true, nil }}, now)
	s.keepRepoRefs(ClusterRepo, Tree{Commit: "k", Files: []string{"bifrost-stg/trade-api", "data/postgres"}}, now)
	doc, ok := s.repoUsers("")
	if !ok || len(doc.Repos) != 5 {
		t.Fatalf("%+v", doc)
	}
	got := map[string]RepoUsers{}
	for _, r := range doc.Repos {
		got[r.Repo] = r
	}
	if k := got["bifrost-platform"].Kinds; k["clone"] != 1 || len(k) != 1 {
		t.Errorf("platform %+v", got["bifrost-platform"])
	}
	if k := got["bifrost-platform-plugin"].Kinds; k["governance"] != 1 || k["build"] != 0 {
		t.Errorf("plugin %+v (bifrost-platform-plugins is another name)", got["bifrost-platform-plugin"])
	}
	if k := got["bifrost-trade-api"].Kinds; k["build"] != 0 || k["governance"] != 1 || k["cluster-object"] != 1 || len(got["bifrost-trade-api"].Users) != 2 {
		t.Errorf("trade-api %+v (an overlay is not a dependency kind; README is not read)", got["bifrost-trade-api"])
	}
	if len(got["lonely"].Users) != 0 || len(got["infra"].Users) != 0 {
		t.Errorf("no users: %+v %+v", got["lonely"], got["infra"])
	}
	if _, ok := doc.Read["infra"]; !ok || len(doc.NotRead) != 4 {
		t.Errorf("read %v not read %v", doc.Read, doc.NotRead)
	}
	w := httptest.NewRecorder()
	RepoUsersRoute{Scanner: s}.ServeHTTP(w, httptest.NewRequest("GET", "/catalog/repo-users?repo=nowhere", nil))
	if w.Code != 404 {
		t.Errorf("unknown repo: %d", w.Code)
	}
}
