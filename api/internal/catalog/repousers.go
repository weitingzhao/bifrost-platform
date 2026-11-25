package catalog

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Who uses a repository (2026-11-25, Foundation for structure elasticity
// R0D, #719 s5: "这个仓库还有谁在用"). The answer was a script run on a local
// checkout (infra scripts/repos/repo_users.sh) and could not be read once
// the checkout was stale. Each round, the scan of every repository reads the
// files that make a dependency and keeps which of them name another
// repository of the organization, by kind:
//   - clone: Tekton and workflow files (k8s/cicd/, tekton/, .gitea/workflows,
//     .github/workflows) — a pipeline that clones or builds it;
//   - build: Dockerfiles, go.mod, package.json, pyproject.toml,
//     requirements*.txt, units.yaml, kustomization.yaml;
//   - governance: scripts and governance files (agent-config/, scripts/)
//     that write the name down.
//
// The cluster's scan keeps the workloads whose name carries the repository's
// name. Any other mention is not read; Secrets are not read (the agent's
// identity cannot list them: Foundation's ruling, 11-24). Measures only.

const keptRepoRefsPrefix = "repo-refs-"

// RepoRefKinds in the order a file is judged.
var RepoRefKinds = []string{"clone", "build", "governance", "cluster-object"}

// RepoRefMaxBytes is the largest file read for names.
const RepoRefMaxBytes = 256 << 10

// RepoRef is one file of one repository naming another.
type RepoRef struct {
	To   string `json:"to"`
	File string `json:"file"`
	Kind string `json:"kind"`
}

type keptRepoRefs struct {
	Repo   string    `json:"repo"`
	Commit string    `json:"commit"`
	At     time.Time `json:"at"`
	Refs   []RepoRef `json:"refs"`
}

// refKind is the kind of dependency a file of a repository can make, "" for
// a file not read.
func refKind(file string) string {
	base := path.Base(file)
	ext := path.Ext(file)
	yaml := ext == ".yaml" || ext == ".yml"
	switch {
	case yaml && (strings.HasPrefix(file, "k8s/cicd/") || strings.Contains(file, "tekton/") ||
		strings.HasPrefix(file, ".gitea/workflows/") || strings.HasPrefix(file, ".github/workflows/")):
		return "clone"
	case strings.HasPrefix(base, "Dockerfile") || base == "go.mod" || base == "package.json" || base == "pyproject.toml" ||
		(strings.HasPrefix(base, "requirements") && ext == ".txt") || base == "units.yaml" || base == "kustomization.yaml":
		return "build"
	case strings.HasPrefix(file, "agent-config/") || strings.HasPrefix(file, "scripts/") || strings.Contains(file, "/scripts/"):
		switch ext {
		case ".sh", ".py", ".yaml", ".yml", ".json", ".js", ".mjs", ".ts", ".go", ".txt", ".awk":
			return "governance"
		}
	}
	return ""
}

// nameRe matches a repository's name standing alone (not part of a longer
// name: bifrost-platform does not match inside bifrost-platform-plugin).
func nameRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_-])`)
}

// repoNames are the organization's repositories the last round listed.
func (s *Scanner) repoNames() []string {
	var repos []string
	if raw, err := s.kept().Read(keptRepos); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &repos)
	}
	out := repos[:0:0]
	for _, r := range repos {
		if r != ClusterRepo && r != DatabaseRepo {
			out = append(out, r)
		}
	}
	return out
}

// retiredNames are the repository registry's rows of tier retired that the
// organization no longer lists (2026-11-25, Foundation: retiring is "nothing
// depends on it any more", so the retired name is looked for too; its own
// tree is not read).
func (s *Scanner) retiredNames(listed []string) []string {
	raw, err := s.kept().Read(keptRepoRegistry)
	if err != nil || raw == nil {
		return nil
	}
	var k keptRepoRows
	if json.Unmarshal(raw, &k) != nil {
		return nil
	}
	in := map[string]bool{}
	for _, n := range listed {
		in[n] = true
	}
	var out []string
	for _, r := range k.Rows {
		if r.Tier == RetiredTier && r.Name != "" && !in[r.Name] {
			in[r.Name] = true
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// keepRepoRefs keeps which files of repo's tree name another repository.
func (s *Scanner) keepRepoRefs(repo string, t Tree, now time.Time) {
	names := s.repoNames()
	if len(names) == 0 {
		return
	}
	names = append(names, s.retiredNames(names)...)
	k := keptRepoRefs{Repo: repo, Commit: t.Commit, At: now.UTC(), Refs: []RepoRef{}}
	if repo == ClusterRepo {
		for _, f := range t.Files {
			name := f[strings.LastIndex(f, "/")+1:]
			for _, n := range names {
				if name == n || (strings.Contains(strings.TrimPrefix(n, "bifrost-"), "-") && name == strings.TrimPrefix(n, "bifrost-")) {
					k.Refs = append(k.Refs, RepoRef{To: n, File: f, Kind: "cluster-object"})
				}
			}
		}
	} else {
		// The registry of repositories names every repository by design; it
		// is the list, not a use (STG 11-25 21:13Z: it was counted as a
		// clone of bifrost-analytics).
		regRepo, regFile, _ := s.classSource("repository")
		res := map[string]*regexp.Regexp{}
		for _, n := range names {
			if n != repo {
				res[n] = nameRe(n)
			}
		}
		for _, f := range t.Files {
			kind := refKind(f)
			if kind == "" || (repo == regRepo && f == regFile) {
				continue
			}
			raw, ok, err := t.Read(f)
			if err != nil || !ok || len(raw) > RepoRefMaxBytes {
				continue
			}
			text := string(raw)
			for n, re := range res {
				if strings.Contains(text, n) && re.MatchString(text) {
					k.Refs = append(k.Refs, RepoRef{To: n, File: f, Kind: kind})
				}
			}
		}
	}
	sort.Slice(k.Refs, func(i, j int) bool {
		if k.Refs[i].To != k.Refs[j].To {
			return k.Refs[i].To < k.Refs[j].To
		}
		return k.Refs[i].File < k.Refs[j].File
	})
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptRepoRefsPrefix+repo, out); err != nil {
			slog.Warn("catalog: keep repository references", "repo", repo, "err", err)
		}
	}
}

// RepoUser is one repository (or the cluster) using another, with its files.
type RepoUser struct {
	From   string         `json:"from"`
	Commit string         `json:"commit"`
	Kinds  map[string]int `json:"kinds"`
	Files  []RepoRef      `json:"files"`
}

// RepoUsers is one repository's users.
type RepoUsers struct {
	Repo string `json:"repo"`
	// Retired is a repository the registry keeps as retired and the
	// organization no longer lists: its users are what still names it.
	Retired bool           `json:"retired,omitempty"`
	Users   []RepoUser     `json:"users"`
	Kinds   map[string]int `json:"kinds"`
}

// RepoUsersDoc is GET /catalog/repo-users.
type RepoUsersDoc struct {
	Version int `json:"version"`
	// Read are the repositories (and the cluster) whose references a round
	// kept, with when; NotRead those the last round listed but kept none of.
	Read    map[string]time.Time `json:"read"`
	NotRead []string             `json:"not_read"`
	Repos   []RepoUsers          `json:"repos"`
	Note    string               `json:"note"`
}

const repoUsersNote = "read from each repository's main: Tekton and workflow files (clone), Dockerfiles, go.mod, package.json, pyproject.toml, requirements, units.yaml, kustomization.yaml (build), agent-config/ and scripts/ (governance); the cluster's workload names (cluster-object). Other mentions are not read; Secrets are not read (the agent's identity cannot list them)."

// repoUsers gathers what the last round kept: every repository's users, or
// one repository's (ok false when the listing does not have it).
func (s *Scanner) repoUsers(only string) (RepoUsersDoc, bool) {
	doc := RepoUsersDoc{Version: Version, Read: map[string]time.Time{}, NotRead: []string{}, Repos: []RepoUsers{}, Note: repoUsersNote}
	names := s.repoNames()
	by := map[string]map[string]*RepoUser{}
	for _, from := range append(append([]string{}, names...), ClusterRepo) {
		raw, err := s.kept().Read(keptRepoRefsPrefix + from)
		var k keptRepoRefs
		if err != nil || raw == nil || json.Unmarshal(raw, &k) != nil {
			doc.NotRead = append(doc.NotRead, from)
			continue
		}
		doc.Read[from] = k.At
		for _, r := range k.Refs {
			if by[r.To] == nil {
				by[r.To] = map[string]*RepoUser{}
			}
			u := by[r.To][from]
			if u == nil {
				u = &RepoUser{From: from, Commit: k.Commit, Kinds: map[string]int{}, Files: []RepoRef{}}
				by[r.To][from] = u
			}
			u.Kinds[r.Kind]++
			u.Files = append(u.Files, r)
		}
	}
	found := only == ""
	retired := map[string]bool{}
	for _, n := range s.retiredNames(names) {
		retired[n] = true
	}
	targets := append(append([]string{}, names...), s.retiredNames(names)...)
	for _, n := range targets {
		if only != "" && n != only {
			continue
		}
		found = true
		ru := RepoUsers{Repo: n, Retired: retired[n], Users: []RepoUser{}, Kinds: map[string]int{}}
		for _, u := range by[n] {
			ru.Users = append(ru.Users, *u)
			for kind, c := range u.Kinds {
				ru.Kinds[kind] += c
			}
		}
		sort.Slice(ru.Users, func(i, j int) bool { return ru.Users[i].From < ru.Users[j].From })
		doc.Repos = append(doc.Repos, ru)
	}
	return doc, found
}

// RepoUsersRoute is GET /catalog/repo-users[?repo=name].
type RepoUsersRoute struct{ Scanner *Scanner }

func (rt RepoUsersRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	doc, ok := rt.Scanner.repoUsers(repo)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no repository " + repo + " in the organization the last round listed"})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
