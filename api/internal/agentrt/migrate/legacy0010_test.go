package migrate_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const legacy0010Commit = "5a8edd550aaa49474a320eb15ed7c9c33f0e1bc5"

// AGENTRT_T50_LEGACY_SRC, when set, must name api/ in a Git checkout whose
// HEAD is exactly legacy0010Commit (a depth=1 checkout is sufficient). Validate
// HEAD, then archive the committed tree, so dirty/untracked files cannot replace
// the old code. Without it, use the local repository's historical object. Never
// fetch here: CI supplies a separate checkout using its existing clone task.
func archiveLegacyAPI(ctx context.Context, source, archive, commit string) error {
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out)), nil
	}
	root, err := git(source, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if source != "" {
		actual, err := filepath.EvalSymlinks(source)
		if err != nil {
			return err
		}
		actual, err = filepath.Abs(actual)
		if err != nil {
			return err
		}
		if actual != filepath.Join(root, "api") {
			return fmt.Errorf("AGENTRT_T50_LEGACY_SRC must name the checkout's api directory, got %s", source)
		}
		head, err := git(root, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if head != commit {
			return fmt.Errorf("AGENTRT_T50_LEGACY_SRC HEAD is %s, want %s", head, commit)
		}
	}
	_, err = git(root, "archive", "--format=tar", "--output="+archive, commit, "api")
	return err
}

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
	if err := archiveLegacyAPI(ctx, os.Getenv("AGENTRT_T50_LEGACY_SRC"), archive, legacy0010Commit); err != nil {
		// Missing/invalid baseline is always a failure, including when
		// AGENTRT_TEST_REQUIRED=1; only the later database setup may skip.
		t.Fatalf("legacy API %s unavailable; set AGENTRT_T50_LEGACY_SRC to its api checkout: %v", legacy0010Commit, err)
	}
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
