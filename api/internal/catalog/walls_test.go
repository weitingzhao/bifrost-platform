package catalog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 2026-11-26, the legislation line: walls were found by agents sent to look
// and drifted after; the scan finds the enforcement points by structure and
// the declaration beside each. One point of each kind: anchored, orphan,
// stale (its law not in force), dup and broken; a policy counted from its
// declaration. The controls: a file outside the index's roots is not read,
// and an unchanged commit is not read again.
func TestWallsAreFoundByStructureAndJudgedByTheirDeclaration(t *testing.T) {
	s := &Scanner{Kept: memKept{}}
	ix, _ := json.Marshal(Index{Classes: []Class{
		{Class: "wall", Sources: []string{"infra:scripts/", "infra:k8s/cicd/", "plat:api/"}, Check: CheckScanned},
		{Class: "policy", Sources: []string{"plat:api/"}, Check: CheckRegistry},
	}})
	_ = s.kept().Write(keptIndex, ix)
	repos, _ := json.Marshal([]string{"infra", "plat"})
	_ = s.kept().Write(keptRepos, repos)
	files := map[string]string{
		"scripts/agent-guard/preflight.js": "// wall: preflight.shared law: LAW-5 二 guards: x revoke: y\n" +
			"if (sw) return { deny: true, kind: 'shared-worktree', reason: sw }\n" +
			"if (pm) return { deny: true, kind: 'push-main', reason: pm }\n",
		"scripts/agent-guard/other.js":     "// wall: preflight.push-main dup-of: preflight.nowhere\n",
		"k8s/cicd/task.yaml":               "spec:\n  steps:\n    # wall: tekton.freeze law: LAW-99 一\n    - name: freeze-check\n      image: x\n    - name: build\n",
		"api/internal/codeguard/x_test.go": "// wall: codeguard.walls dup-of: preflight.shared\nfunc TestWalls(t *testing.T) {}\n// wall: codeguard.more dup-of: preflight.nowhere\nfunc TestMore(t *testing.T) {}\n",
		"api/internal/actions/catalog.go":  "\t{\n\t\tID: \"scale_deployment\", Tier: TierC,\n\t},\n\t{\n\t\tID: \"read_thing\", Tier: TierB,\n",
		"api/internal/lights/light.go":     "// policy: lights.bad-at-half law: LAW-8 二\nconst bad = 0.5\n",
		"docs/preflight.js":                "if (x) return { deny: true, kind: 'outside', reason: x }\n",
	}
	tree := func(repo string) Tree {
		var list []string
		for f := range files {
			if strings.HasPrefix(f, "api/") == (repo == "plat") {
				list = append(list, f)
			}
		}
		return Tree{Commit: "c-" + repo, Files: list, Read: func(p string) ([]byte, bool, error) { return []byte(files[p]), true, nil }}
	}
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	s.keepWalls("infra", tree("infra"), now)
	s.keepWalls("plat", tree("plat"), now)
	w, p := s.walls(map[string]bool{"LAW-5": true, "LAW-8": true})
	if !w.Measured || !w.LawsRead {
		t.Fatalf("%+v", w)
	}
	// preflight: shared (anchored), push-main (orphan: the dup elsewhere is not beside it);
	// tekton freeze-check (stale: LAW-99); codeguard TestWalls (dup), TestMore (broken);
	// action scale_deployment (orphan).
	want := WallCounts{Total: 6, Anchored: 1, Orphan: 2, Stale: 1, Dup: 1, Broken: 1}
	if w.WallCounts != want {
		t.Errorf("walls %+v, want %+v; orphans %v", w.WallCounts, want, w.Orphans)
	}
	if len(w.ByKind) != 4 || w.ByKind["preflight"].Total != 2 || w.ByKind["action"].Orphan != 1 {
		t.Errorf("by kind %+v", w.ByKind)
	}
	if p.Total != 1 || p.Anchored != 1 {
		t.Errorf("policies %+v", p.WallCounts)
	}
	{
		w2, _ := s.walls(nil)
		if w2.LawsRead || w2.Stale != 0 || w2.Anchored != 2 || len(w2.BrokenList) != 1 {
			t.Errorf("laws unread: no stale by law %+v", w2.WallCounts)
		}
	}
	files["scripts/agent-guard/preflight.js"] = "if (pm) return { deny: true, kind: 'push-main', reason: pm }\n"
	s.keepWalls("infra", tree("infra"), now)
	if w3, _ := s.walls(map[string]bool{"LAW-5": true}); w3.Total != 6 {
		t.Errorf("an unchanged commit is not read again: %+v", w3.WallCounts)
	}
}
