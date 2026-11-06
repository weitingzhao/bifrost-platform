package landinggit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestFullCIFirstParentsListsMainsLandings(t *testing.T) {
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
	git("init", "--template=", "-b", "main", dir)
	git("commit", "--allow-empty", "-m", "Base")
	base := git("rev-parse", "HEAD")
	git("commit", "--allow-empty", "-m", "One\n\nbody")
	one := git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", "side")
	git("commit", "--allow-empty", "-m", "Side")
	git("checkout", "-q", "main")
	git("merge", "--no-ff", "-q", "-m", "Merge side", "side")
	p := git("rev-parse", "HEAD")
	w := &Workspace{git: &Git{repo: "bifrost-platform"}, dir: dir}
	got, err := w.FirstParents(context.Background(), base, p, 30)
	if err != nil || len(got) != 2 || got[0] != (Subject{p, "Merge side"}) || got[1] != (Subject{one, "One"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = w.FirstParents(context.Background(), base, p, 1); err != nil || len(got) != 1 || got[0].SHA != p {
		t.Fatalf("limit: %+v %v", got, err)
	}
	if got, err = w.FirstParents(context.Background(), p, p, 30); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
}
