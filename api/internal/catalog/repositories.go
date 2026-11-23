package catalog

import (
	"encoding/json"
	"log/slog"
	"sort"

	"gopkg.in/yaml.v3"
)

// Class repository is compared with its registry (2026-11-23, R0B
// structure elasticity, Foundation: "仓库名单只有一个出处"). Foundation keeps
// the registry, one row per repository with its tier, domain, visibility,
// whether it lands through ready/ and its purpose; the index names where it
// is ("<repo>:<path>"), never this code. Each round the map compares it with
// the Gitea organization's listing (the round's repositories) and keeps both
// differences. The map registers and answers; it judges nothing (Owner via
// the chief of staff, 2026-11-23).

const keptRepoRegistry = "repo-registry"

// RetiredTier is the tier of a repository kept in the registry after it is
// retired: it is not expected on the organization's live listing, and one
// still listed there is a problem.
const RetiredTier = "retired"

// repoRow is one row of the registry as far as the check reads it.
type repoRow struct {
	Name    string `json:"name" yaml:"name"`
	Tier    string `json:"tier,omitempty" yaml:"tier"`
	Domain  string `json:"domain,omitempty" yaml:"domain"`
	Public  *bool  `json:"public,omitempty" yaml:"public"`
	Ready   *bool  `json:"ready,omitempty" yaml:"ready"`
	Purpose string `json:"purpose,omitempty" yaml:"purpose"`
}

type keptRepoRows struct {
	Repo   string    `json:"repo"`
	Commit string    `json:"commit"`
	Path   string    `json:"path"`
	Rows   []repoRow `json:"rows"`
}

// keepRepoRegistry keeps the registry when repo is the one the index names
// for class repository.
func (s *Scanner) keepRepoRegistry(repo string, t Tree) {
	srcRepo, file, ok := s.classSource("repository")
	if !ok || srcRepo != repo {
		return
	}
	raw, found, err := t.Read(file)
	if err != nil || !found {
		return
	}
	var doc struct {
		Repositories []repoRow `yaml:"repositories"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		slog.Warn("catalog: repository registry", "err", err)
		return
	}
	k := keptRepoRows{Repo: repo, Commit: t.Commit, Path: file, Rows: doc.Repositories}
	if k.Rows == nil {
		k.Rows = []repoRow{}
	}
	if out, err := json.Marshal(k); err == nil {
		if err := s.kept().Write(keptRepoRegistry, out); err != nil {
			slog.Warn("catalog: keep repository registry", "err", err)
		}
	}
}

// repositoryCheck compares the registry with the organization's listing:
// listed but not registered, registered (and not retired) but not listed.
// Problems are a row named twice, a row with no name, a domain the table
// does not have, a retired repository still listed, and a domain that
// differs from the map's claim of that repository in its review file (the
// claim wins: the registry's domain column follows it, Foundation 2026-11-23).
func (s *Scanner) repositoryCheck(domains []contractDomain) ClassCheck {
	c := ClassCheck{Class: "repository", NotRegistered: []string{}, NotLive: []string{}, Problems: []string{}}
	raw, err := s.kept().Read(keptRepoRegistry)
	if err != nil || raw == nil {
		c.Note = "no scan has kept the repository registry yet (the file the index names for class repository)"
		return c
	}
	var k keptRepoRows
	if json.Unmarshal(raw, &k) != nil {
		c.Note = "the kept repository registry does not read"
		return c
	}
	var live []string
	if raw, err := s.kept().Read(keptRepos); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &live)
	}
	if len(live) == 0 {
		c.Note = "no round has kept the organization's listing yet"
		return c
	}
	claims := map[string]map[string]Review{}
	if raw, err := s.kept().Read(keptOthers); err == nil && raw != nil {
		_ = json.Unmarshal(raw, &claims)
	}
	known := map[string]bool{}
	for _, d := range domains {
		known[d.ID] = true
	}
	// The round also scans the cluster and the platform database as
	// repositories of the pool; they are not in the organization.
	orgs := live[:0:0]
	for _, r := range live {
		if r != ClusterRepo && r != DatabaseRepo {
			orgs = append(orgs, r)
		}
	}
	live = orgs
	listed := map[string]bool{}
	for _, r := range live {
		listed[r] = true
	}
	rows := map[string]int{}
	for _, r := range k.Rows {
		if r.Name == "" {
			c.Problems = append(c.Problems, "a row with no name")
			continue
		}
		rows[r.Name]++
		if rows[r.Name] == 2 {
			c.Problems = append(c.Problems, r.Name+": named by more than one row")
		}
		switch {
		case r.Tier == RetiredTier && listed[r.Name]:
			c.Problems = append(c.Problems, r.Name+": retired but still in the organization")
		case r.Tier != RetiredTier && !listed[r.Name] && rows[r.Name] == 1:
			c.NotLive = append(c.NotLive, r.Name)
		}
		if r.Domain != "" && len(known) > 0 && !known[r.Domain] {
			c.Problems = append(c.Problems, r.Name+": domain "+r.Domain+" is not a domain of the table")
		}
		if cl, ok := claims[r.Name]["."]; ok && cl.Verdict == VerdictClaim && cl.Domain != "" && r.Domain != cl.Domain {
			c.Problems = append(c.Problems, r.Name+": domain "+orNone(r.Domain)+" but the map's claim is "+cl.Domain)
		}
	}
	for _, r := range live {
		if rows[r] == 0 {
			c.NotRegistered = append(c.NotRegistered, r)
		}
	}
	sort.Strings(c.NotRegistered)
	sort.Strings(c.NotLive)
	sort.Strings(c.Problems)
	c.Measured, c.Registered, c.Live = true, len(rows), len(live)
	return c
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
