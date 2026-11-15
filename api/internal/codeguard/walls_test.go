package codeguard

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goListDeps is "go list -deps" of a package pattern, run in the module.
func goListDeps(t *testing.T, pkg string) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	return string(out)
}

// reaches says whether a dependency list names the package with that path
// suffix.
func reaches(deps, suffix string) bool {
	for _, line := range strings.Split(deps, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), suffix) {
			return true
		}
	}
	return false
}

// The walls between packages: who must not come to depend on whom, directly
// or through another package. Each was a test in the package it protects
// (questions' TestThosePackagesDoNotDependOnQuestions, agentprogress's
// TestReleasePackagesDoNotImportAgentProgress); a wall's answer changes when
// any package on the other side changes its imports, so the tests are here,
// with the guards that read the whole module, and the protected packages'
// own tests no longer need the whole module declared (2026-11-14).
func TestWallsBetweenPackages(t *testing.T) {
	for _, wall := range []struct {
		why      string
		behind   string
		outsides []string
	}{
		// A question is not an approval: nothing that decides or executes may
		// learn of questions (questions' own direction is its own test).
		{"the approval and release packages do not depend on questions", "/internal/questions",
			[]string{"./internal/actions/...", "./internal/approvals/...", "./internal/approvalnotify/...", "./internal/releasepolicy/..."}},
		// Packages on a release path must not import agentprogress. server
		// does, because it is the publish endpoint; it is not in this list.
		{"the release packages do not import agentprogress", "/internal/agentprogress",
			[]string{"./internal/approvals/...", "./internal/actions/...", "./internal/delivery/...", "./internal/releasepolicy/...",
				"./internal/gitops/...", "./internal/promote/..."}},
	} {
		for _, pkg := range wall.outsides {
			if reaches(goListDeps(t, pkg), wall.behind) {
				t.Errorf("%s: %s depends on %s", wall.why, pkg, wall.behind)
			}
		}
	}
}

// The positive control of the walls: the check sees a dependency that is
// there (server wires both packages in), and the predicate matches a path by
// its suffix only.
func TestWallCheckSeesADependency(t *testing.T) {
	server := goListDeps(t, "./internal/server/...")
	for _, behind := range []string{"/internal/questions", "/internal/agentprogress"} {
		if !reaches(server, behind) {
			t.Errorf("the check does not see that server depends on %s", behind)
		}
	}
	if !reaches("fmt\ngithub.com/weitingzhao/bifrost-platform/api/internal/agentprogress\n", "/internal/agentprogress") {
		t.Error("the predicate missed an import of agentprogress")
	}
	if reaches("fmt\ngithub.com/weitingzhao/bifrost-platform/api/internal/approvals\n", "/internal/agentprogress") {
		t.Error("the predicate matched a package that is not agentprogress")
	}
}
