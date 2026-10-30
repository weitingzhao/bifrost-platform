package store_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The fixture redirects only the two fixed landing URLs to local bare repos.
// All history, diff, replay and branch operations run real Git. No listener,
// service credentials, production ref or candidate executable is involved.
type local704 struct {
	t                       *testing.T
	executable, root        string
	remotes, sources, bases map[string]string
}

func localGit704(t *testing.T) *local704 {
	t.Helper()
	git, err := exec.LookPath("git")
	ok(t, err)
	f := &local704{t: t, executable: git, root: t.TempDir(), remotes: map[string]string{}, sources: map[string]string{}, bases: map[string]string{}}
	for _, repo := range landing.LandingRepoNames() {
		f.remotes[repo] = filepath.Join(f.root, repo+".git")
		f.sources[repo] = filepath.Join(f.root, repo)
		f.run(f.root, "init", "--bare", f.remotes[repo])
		f.run(f.root, "init", "-b", "main", f.sources[repo])
		f.run(f.sources[repo], "commit", "--allow-empty", "-m", "base")
		f.bases[repo] = f.run(f.sources[repo], "rev-parse", "HEAD")
		f.run(f.sources[repo], "push", f.remotes[repo], "main")
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/bash\nset -e\na=()\nfor arg in \"$@\"; do\ncase \"$arg\" in\n"
	for _, repo := range landing.LandingRepoNames() {
		script += quote(landing.LandingRepos[repo].Remote) + ") arg=" + quote(f.remotes[repo]) + ";;\n"
	}
	script += "esac\na+=(\"$arg\")\ndone\nexport GIT_ALLOW_PROTOCOL=file\nexec " + quote(git) + " -c protocol.file.allow=always \"${a[@]}\"\n"
	bin := filepath.Join(f.root, "bin")
	ok(t, os.Mkdir(bin, 0700))
	ok(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for key, value := range map[string]string{landinggit.LanderUsernameFileEnv: "fixture-user", landinggit.LanderPasswordFileEnv: "fixture-password"} {
		path := filepath.Join(f.root, key)
		ok(t, os.WriteFile(path, []byte(value), 0600))
		t.Setenv(key, path)
	}
	return f
}
func (f *local704) run(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command(f.executable, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("local Git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func (f *local704) result(repo, task string, onMain bool) string {
	f.t.Helper()
	path := "console/src/components/Pair.tsx"
	if repo == infraRepo {
		path = infraPath
	}
	name := filepath.Join(f.sources[repo], path)
	ok(f.t, os.MkdirAll(filepath.Dir(name), 0700))
	ok(f.t, os.WriteFile(name, []byte("accepted fixture\n"), 0644))
	f.run(f.sources[repo], "add", "--", path)
	f.run(f.sources[repo], "commit", "-m", "accepted\n\nWork: W-31\nAgentrt-Task: "+task+"\nChange-Id: I"+strings.Repeat("a", 40))
	sha := f.run(f.sources[repo], "rev-parse", "HEAD")
	f.run(f.sources[repo], "push", f.remotes[repo], "HEAD:refs/heads/accepted")
	if onMain {
		f.run(f.sources[repo], "push", f.remotes[repo], "HEAD:refs/heads/main")
	}
	return sha
}
func (f *local704) adapters() map[string]controller.DrillGit {
	f.t.Helper()
	out := map[string]controller.DrillGit{}
	for _, repo := range landing.LandingRepoNames() {
		g, err := landinggit.LanderFromEnv("prod", repo)
		ok(f.t, err)
		out[repo] = g
	}
	return out
}

func passReal704(t *testing.T, w *world, root, sha string) {
	t.Helper()
	c := w.claim(t, "h1", store.LeaseTTL, cursor)
	if c == nil || c.TaskID != root {
		t.Fatal("wrong implementation", c)
	}
	f := fence(c, "h1")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "accepted"), Ref: c.PushedRef, Commit: sha}, heads(map[string]string{c.PushedRef: sha}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	v, err := store.StartVerification(ctx, w.rt, root)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "review" {
		t.Fatal(did)
	}
	review := w.checker(t, root, "review")
	w.run(t, "h3", codex, "done")
	did, err = store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "accepted" {
		t.Fatal(did)
	}
}

// Serial: event clock, signing anchor and subprocess PATH. Both dependency
// directions use real claim and branch preparation against real Git.
func TestReview704CrossRepoClaimAndPreparePG(t *testing.T) {
	for _, repo := range landing.LandingRepoNames() {
		t.Run(repo, func(t *testing.T) {
			w := newKernelWorld(t)
			sign := signer{t, rptest.NewKey(t)}
			_, err := sign.sign(w, ruleText("landing:dev", 1, "initial", since, until, infraRuleBody))
			ok(t, err)
			restore := store.FreezeEventClock(time.Now().Add(-time.Hour))
			for _, r := range landing.LandingRepoNames() {
				_, err = store.EnsureLandingMode(ctx, w.rt, r)
				ok(t, err)
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					_, e := store.ObserveLandingEnabled(ctx, tx, "prod", "dev", r, time.Now())
					return e
				}))
			}
			restore()
			w.goalMission(t, "M-3")
			f := localGit704(t)
			depRepo := "bifrost-platform"
			if repo == depRepo {
				depRepo = infraRepo
			}
			dep := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "cross repo dependency", Repo: depRepo, BaseCommit: f.bases[depRepo]})
			depSHA := f.result(depRepo, dep, true)
			passReal704(t, w, dep, depSHA)
			root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "cross repo downstream", Repo: repo, BaseCommit: f.bases[repo]})
			a := f.result(repo, root, false)
			passReal704(t, w, root, a)
			link704(t, w, root, dep)
			gits := f.adapters()
			src := controller.MultiRepoGitFacts(gits)
			h, err := src.History(ctx, depRepo, dep, depSHA, "")
			ok(t, err)
			if h.State != landing.Effective || h.Commit != depSHA {
				t.Fatal("dependency has no valid main evidence", h)
			}
			op, d, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, time.Now())
			ok(t, err)
			if op == "" || !d.Allowed {
				t.Fatal("claim", op, d)
			}
			before := f.run(f.remotes[depRepo], "show-ref")
			ctl := controller.Controller{DB: w.rt, Env: "prod", Live: true, LiveRepos: landing.LandingRepoNames(), DrillGit: gits, Drill: &controller.DrillSources{Facts: src}}
			ctl.Drill.InfraPlatformHead = func(ctx context.Context) (string, error) { return gits[platformRepo].Ref(ctx, "refs/heads/main") }
			seq := w.lastSeq(t, "landing:"+repo)
			ctl.Drill.Facts = controller.MultiRepoGitFacts(map[string]controller.DrillGit{repo: gits[repo], depRepo: gits[repo]})
			if err := ctl.LandingDrillBranch(ctx, gits[repo], op, time.Now(), repo); err == nil {
				t.Fatal("misbound dependency prepared")
			}
			if w.lastSeq(t, "landing:"+repo) != seq {
				t.Fatal("misbound dependency wrote a record")
			}
			ctl.Drill.Facts = src
			ok(t, ctl.LandingDrillBranch(ctx, gits[repo], op, time.Now(), repo))
			o := landingState(t, w, "landing:"+repo).Operations[op]
			if o.Phase != "branch_pushed" || o.R == "" {
				t.Fatal("not prepared", o)
			}
			tip, err := gits[repo].Ref(ctx, o.Ref)
			ok(t, err)
			if tip != o.R {
				t.Fatal("branch not pushed", tip, o.R)
			}
			if f.run(f.remotes[depRepo], "show-ref") != before {
				t.Fatal("dependency refs changed")
			}
			if f.run(f.remotes[repo], "rev-parse", "main") != f.bases[repo] {
				t.Fatal("preparation pushed main")
			}
		})
	}
}

// Serial: Setenv for the local Git transport fixture.
func TestReview704LocalGitEvidenceControl(t *testing.T) {
	f := localGit704(t)
	task := "t_00000000000000000000000001"
	dep := "t_00000000000000000000000002"
	a := f.result("bifrost-platform", task, false)
	b := f.result(infraRepo, dep, true)
	gits := f.adapters()
	src := controller.MultiRepoGitFacts(gits)
	single, changes, err := src.Result(context.Background(), "bifrost-platform", task, "attempt", f.bases["bifrost-platform"], a)
	ok(t, err)
	if !single || len(changes) != 1 {
		t.Fatal(single, changes)
	}
	h, err := src.History(ctx, infraRepo, dep, b, "")
	ok(t, err)
	if h.State != landing.Effective {
		t.Fatal(h)
	}
	// Exercise the fixture's real replay and branch push without PG too.
	w, err := gits["bifrost-platform"].Open(ctx, a)
	ok(t, err)
	defer w.Close()
	r, err := w.Replay(ctx, landinggit.ReplayInput{A: a, Base: f.bases["bifrost-platform"], M0: f.bases["bifrost-platform"], Task: task, Attempt: "a_fixture", Mission: "M-3", Reviewers: "reviewer", Operation: "land:" + task + ":1", RuleVersion: 1, AcceptedAt: time.Now().Add(-time.Minute), CommitterDate: time.Now(), Env: "prod"})
	ok(t, err)
	ref := "refs/heads/land/prod/" + task + "/1"
	ok(t, w.Push(ctx, ref, r))
	tip, err := gits["bifrost-platform"].Ref(ctx, ref)
	ok(t, err)
	if tip != r {
		t.Fatal("replay branch mismatch", tip, r)
	}
	if f.run(f.remotes[infraRepo], "rev-parse", "main") != b {
		t.Fatal("dependency main changed")
	}
	for _, bad := range []map[string]controller.DrillGit{{infraRepo: gits["bifrost-platform"]}, {"unknown": gits[infraRepo]}} {
		repo := infraRepo
		if bad["unknown"] != nil {
			repo = "unknown"
		}
		_, err = controller.MultiRepoGitFacts(bad).History(ctx, repo, dep, b, "")
		if err == nil {
			t.Fatal("bad adapter admitted", fmt.Sprint(bad))
		}
	}
}
