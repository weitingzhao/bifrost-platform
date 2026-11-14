package landinggit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Serial: constructors read process environment; no remote or credential I/O.
func TestS6ConstructorsBindClosedRepository(t *testing.T) {
	for _, key := range []string{UsernameFileEnv, PasswordFileEnv, LanderUsernameFileEnv, LanderPasswordFileEnv} {
		t.Setenv(key, "/not-read/credential")
	}
	for _, repo := range landing.LandingRepoNames() {
		for _, env := range []string{"stg", "prod"} {
			var g *Git
			var err error
			if env == "stg" {
				g, err = FromEnv(env, repo)
			} else {
				g, err = LanderFromEnv(env, repo)
			}
			if err != nil || g.remote != landing.LandingRepos[repo].Remote || g.Repository() != repo {
				t.Fatal(repo, env, g, err)
			}
		}
	}
	for _, repo := range []string{"", "bifrost-research", "http://attacker/repo"} {
		if _, err := LanderFromEnv("prod", repo); err == nil {
			t.Fatal("accepted", repo)
		}
	}
}

func TestS6InfraDiffUsesSeparateRenameEndpoints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitTest(t, dir, "init", "-b", "main")
	old := "agent-config/work/multi-agent/REPORT-a.md"
	newPath := "agent-config/work/multi-agent/REPORT-b.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, old)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, old), []byte("document\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", old)
	gitTest(t, dir, "commit", "-m", "base")
	base := gitTest(t, dir, "rev-parse", "HEAD")
	gitTest(t, dir, "mv", old, newPath)
	gitTest(t, dir, "commit", "-m", "rename\n\nAgentrt-Task: "+testTask+"\nChange-Id: I"+strings.Repeat("a", 40))
	a := gitTest(t, dir, "rev-parse", "HEAD")
	w := &Workspace{git: &Git{repo: "bifrost-trade-infra"}, dir: dir}
	good, changes, err := w.Result(context.Background(), testTask, base, a)
	if err != nil || !good || len(changes) != 2 {
		t.Fatalf("%v %+v %v", good, changes, err)
	}
	var deleted, added bool
	for _, c := range changes {
		deleted = deleted || c.ChangeKind() == "D" && c.OldPath == old && c.NewPath == ""
		added = added || c.ChangeKind() == "A" && c.OldPath == "" && c.NewPath == newPath
	}
	if !deleted || !added {
		t.Fatalf("rename was collapsed: %+v", changes)
	}
	w.git.repo = "bifrost-platform"
	good, changes, err = w.Result(context.Background(), testTask, base, a)
	if err != nil || !good || len(changes) != 1 || changes[0].ChangeKind() != "R" {
		t.Fatalf("platform rename changed: %+v %v", changes, err)
	}
}
