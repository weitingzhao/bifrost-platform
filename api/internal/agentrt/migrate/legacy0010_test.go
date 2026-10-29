package migrate_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const legacy0010Commit = "5a8edd550aaa49474a320eb15ed7c9c33f0e1bc5"

// Export the entire old API, not selected/reimplemented reader functions.
// Only a new test driver is added; every production file remains at the
// pinned commit. No checkout, migration embedding or shared worktree changes.
func buildLegacy0010(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "baseline.tar")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(cmd *exec.Cmd) {
		t.Helper()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("legacy build %s: %v\n%s", cmd.Args[0], err, out)
		}
	}
	root, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	must0010(t, err)
	run(exec.CommandContext(ctx, "git", "-C", strings.TrimSpace(string(root)), "archive", "--format=tar", "--output="+archive, legacy0010Commit, "api"))
	run(exec.CommandContext(ctx, "tar", "-xf", archive, "-C", dir))
	probe, err := os.ReadFile("testdata/legacy0010_probe_test.go")
	must0010(t, err)
	must0010(t, os.WriteFile(filepath.Join(dir, "api/internal/agentrt/store/upgrade0010_probe_test.go"), probe, 0o600))
	binary := filepath.Join(dir, "legacy.test")
	cmd := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, "./internal/agentrt/store")
	cmd.Dir = filepath.Join(dir, "api")
	run(cmd)
	t.Logf("compiled unmodified API %s plus T50 test driver", legacy0010Commit)
	return binary
}

func runLegacy0010(t *testing.T, binary, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestT50LegacyOnUpgradedDatabase$", "-test.v", "-test.count=1")
	cmd.Dir = filepath.Dir(binary)
	cmd.Env = append(os.Environ(), "AGENTRT_TEST_REQUIRED=1", "AGENTRT_T50_RUNTIME="+dsn)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy runtime %s: %v\n%s", legacy0010Commit, err, out)
	}
	t.Logf("legacy runtime %s:\n%s", legacy0010Commit, out)
}
