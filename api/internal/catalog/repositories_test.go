package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// 2026-11-23, R0B (Foundation): the list of repositories was written in five
// places that disagreed (11 in two files, 14 in the Gitea organization) and
// class repository had no source, so a repository could appear or vanish
// with nothing to compare. The registry the index names is compared with the
// organization each round. The control: a registered, listed repository whose
// domain matches the map's claim raises nothing. STG 11-23 19:44Z the first
// reading listed cluster and platform-db as not registered: the round's list
// carries the pool's two non-Gitea repositories, which are left out.
func TestRepositoriesAreComparedWithTheOrganization(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	ix, _ := json.Marshal(Index{Classes: []Class{{Class: "repository", Sources: []string{"infra:k/repositories.yaml"}, Check: CheckScanned}}})
	_ = s.kept().Write(keptIndex, ix)
	live, _ := json.Marshal([]string{"ok", "new", "old", "wrong", "twice", ClusterRepo, DatabaseRepo})
	_ = s.kept().Write(keptRepos, live)
	_ = s.kept().Write(keptOthers, []byte(`{"ok":{".":{"verdict":"claim","domain":"payload","reason":"r"}},"wrong":{".":{"verdict":"claim","domain":"payload","reason":"r"}}}`))
	tree := Tree{Commit: "c", Files: []string{"k/repositories.yaml"}, Read: func(string) ([]byte, bool, error) {
		return []byte(`repositories:
  - {name: ok, tier: payload, domain: payload, public: true, ready: false, purpose: p}
  - {name: gone, tier: payload, domain: payload}
  - {name: analytics, tier: retired}
  - {name: old, tier: retired}
  - {name: wrong, tier: payload, domain: foundation}
  - {name: twice, domain: nowhere}
  - {name: twice}
`), true, nil
	}}
	s.keepRepoRegistry("other", tree)
	if c := s.repositoryCheck(nil); c.Measured {
		t.Fatalf("a repository the index does not name is not read: %+v", c)
	}
	s.keepRepoRegistry("infra", tree)
	c := s.repositoryCheck([]contractDomain{{ID: "payload"}, {ID: "foundation"}})
	if !c.Measured || c.Registered != 6 || c.Live != 5 {
		t.Fatalf("%+v", c)
	}
	if strings.Join(c.NotRegistered, ",") != "new" || strings.Join(c.NotLive, ",") != "gone" {
		t.Errorf("not registered %v, not live %v", c.NotRegistered, c.NotLive)
	}
	want := []string{
		"old: retired but still in the organization",
		"twice: domain nowhere is not a domain of the table",
		"twice: named by more than one row",
		"wrong: domain foundation but the map's claim is payload",
	}
	if strings.Join(c.Problems, "|") != strings.Join(want, "|") {
		t.Errorf("problems %q", c.Problems)
	}
}
