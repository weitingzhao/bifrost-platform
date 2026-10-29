package landinggit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSTGLandedOperationTrailer(t *testing.T) {
	t.Parallel()
	good := "Change\n\nAgentrt-Task: " + testTask + "\nLanded-By: agentrt-prod/land:one:1\n"
	for _, bad := range []string{"Change\nLanded-By: agentrt-prod/land:one:1\n\nbody", good + "Landed-By: agentrt-prod/land:one:1\n", good + " continuation\n", strings.ReplaceAll(good, "agentrt-prod", "agentrt-stg"), "Change\n\n> Landed-By: agentrt-prod/land:one:1"} {
		if LandedOperation(good) != "land:one:1" {
			t.Fatal("positive trailer")
		}
		if LandedOperation(bad) != "" {
			t.Fatal("accepted malformed trailer", bad)
		}
	}
}

func TestSTGReleaseRangeReadsActualDiffAndManualCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(isolatedEnv(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--template=", dir)
	path := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(path, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "readme.txt")
	git("commit", "-m", "Base")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(path, []byte("base\nnew\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "readme.txt")
	git("commit", "-m", "Result\n\nAgentrt-Task: "+testTask+"\nChange-Id: I"+strings.Repeat("a", 40)+"\nLanded-By: agentrt-prod/land:one:1")
	landed := git("rev-parse", "HEAD")
	git("commit", "--allow-empty", "-m", "Manual revert")
	p := git("rev-parse", "HEAD")
	w := &Workspace{git: &Git{repo: "bifrost-platform"}, dir: dir}
	cs, err := w.ReleaseRange(context.Background(), base, p)
	if err != nil || len(cs) != 2 || cs[0].SHA != landed || cs[0].Parents[0] != base || len(cs[0].Changes) != 1 || cs[0].Changes[0].Added != 1 || cs[1].SHA != p || LandedOperation(cs[1].Message) != "" {
		t.Fatalf("range %q..%q: %+v %v", base, p, cs, err)
	}
	if cs, err = w.ReleaseRange(context.Background(), p, p); err != nil || len(cs) != 0 {
		t.Fatal("equal baseline", cs, err)
	}
}
