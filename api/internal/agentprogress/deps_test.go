package agentprogress

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Packages on a release path must not import this one. server does, because
// it is the publish endpoint; it is not in this list.
func TestReleasePackagesDoNotImportAgentProgress(t *testing.T) {
	packages := []string{
		"./internal/approvals/...",
		"./internal/actions/...",
		"./internal/delivery/...",
		"./internal/releasepolicy/...",
		"./internal/gitops/...",
		"./internal/promote/...",
	}
	for _, pkg := range packages {
		out := goListDeps(t, pkg)
		if importsAgentProgress(out) {
			t.Fatalf("%s imports agentprogress", pkg)
		}
	}
}

// The predicate has to fail when a line names this package. A copy of the
// check that ignored the suffix would let this pass.
func TestImportPredicateCatchesAgentProgress(t *testing.T) {
	sample := "fmt\ngithub.com/weitingzhao/bifrost-platform/api/internal/agentprogress\n"
	if !importsAgentProgress(sample) {
		t.Fatal("predicate missed an import of agentprogress")
	}
	if importsAgentProgress("fmt\ngithub.com/weitingzhao/bifrost-platform/api/internal/approvals\n") {
		t.Fatal("predicate matched a package that is not agentprogress")
	}
}

func importsAgentProgress(deps string) bool {
	for _, line := range strings.Split(deps, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "/internal/agentprogress") {
			return true
		}
	}
	return false
}

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
