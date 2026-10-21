package agentd_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-403: an attempt at bifrost-platform, whose tests need a database
// (store.Repos), runs with one of its own: the executor sees
// AGENTRT_TEST_REQUIRED=1 and AGENTRT_TEST_PG naming a socket in the
// attempt's directory, the socket there while it runs; once the attempt has
// ended no Postgres runs over that directory and the directory is gone. An
// attempt at a repository whose tests need none gets nothing of the kind.
func TestAPlatformAttemptHasATestDatabaseOfItsOwn(t *testing.T) {
	tools, err := agentd.FindTestDBTools(os.Getenv("AGENTD_PG_BIN"))
	if err != nil {
		t.Skipf("no test database programs here: %v", err)
	}
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
		t.Run(repo, func(t *testing.T) {
			env := gitEnvT(t.TempDir())
			bare, baseCommit := bareRemote(t, env)
			head := func(_ context.Context, _, ref string) (string, error) {
				out, err := exec.Command("git", "ls-remote", bare, ref).Output()
				first, _, _ := strings.Cut(string(out), "\t")
				return strings.TrimSpace(first), err
			}
			_, _, base := platformSpec(t, store.Task{Effect: "branch", Repo: repo, BaseCommit: baseCommit}, head, "application/json", []byte(`{"text":"t"}`))
			// The script runs with this test's environment, which may name a
			// database of its own and hold other names a real executor's
			// never does (AllowedEnv, keyless): they go, as they would, so
			// the environment checked behind the stage is an executor's
			// (review 489 A2).
			ex := script(`env | grep '^AGENTRT_TEST_' | sort > "$OUT/db-env.txt"; ` +
				`{ [ -z "$AGENTRT_TEST_PG" ] || test -S "$(echo "$AGENTRT_TEST_PG" | sed 's/^host=\([^ ]*\) .*/\1/')/.s.PGSQL.5432"; } && ` +
				`echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`)
			d := daemon(t, base, func(a *agentd.Attempt) (agentd.Spawn, error) {
				sp, err := ex(a)
				sp.Env = slices.DeleteFunc(sp.Env, func(kv string) bool { k, _, _ := strings.Cut(kv, "="); return agentd.BusinessKey(k) })
				return sp, err
			}, bare, env)
			// A root short enough for the sockets under it (a test's own
			// temporary directory, named after it, is not, on macOS).
			root, err := os.MkdirTemp("", "a")
			must(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			if len(root)+len("/s/")+24+len("/.s.PGSQL.5432") > 103 {
				t.Skipf("this machine's temporary directory %s is too long for the sockets under it", root)
			}
			d.Root, d.TestDB = root, tools
			cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
			if err != nil || cl == nil {
				t.Fatalf("claim = %+v %v", cl, err)
			}
			out, err := d.Run(ctx, cl)
			if err != nil || out.Conclusion != "done" || !out.Submitted {
				t.Fatalf("run = %+v %v", out, err)
			}
			attempt := agentd.WorkspaceDir(root, cl.TaskID, cl.AttemptNo) + ".out"
			db := filepath.Join(attempt, "tmp", "db")
			got, _ := os.ReadFile(filepath.Join(attempt, "db-env.txt"))
			if repo != "bifrost-platform" {
				if strings.TrimSpace(string(got)) != "" {
					t.Fatalf("%s's attempt saw %s", repo, got)
				}
				return
			}
			sock := filepath.Join(root, "d")
			for _, want := range []string{"AGENTRT_TEST_PG=host=" + sock + "/", " port=5432 ", "AGENTRT_TEST_REDIS=unix://" + sock + "/",
				"AGENTRT_TEST_REQUIRED=1\n"} {
				if !strings.Contains(string(got), want) {
					t.Fatalf("the executor's environment lacks %q:\n%s", want, got)
				}
			}
			if _, serr := os.Stat(db); !errors.Is(serr, os.ErrNotExist) {
				t.Fatalf("the test database's directory is still there: %v", serr)
			}
			if left, _ := os.ReadDir(sock); len(left) != 0 {
				t.Fatalf("the sockets' directory is still there: %v", left)
			}
			if b, perr := exec.Command("pgrep", "-f", db).Output(); perr == nil && strings.TrimSpace(string(b)) != "" {
				t.Fatalf("Postgres still runs over the attempt's directory: %s", b)
			}
		})
	}
}

// Review 489 A2: the environment the executor would have behind the stage is
// checked before anything starts: a name on the deny list there (a test
// executor's own environment is not keyless-checked, so it can hold one)
// ends the attempt before its executor, and no database is started.
func TestTheStageAddsNoBusinessKey(t *testing.T) {
	tools, err := agentd.FindTestDBTools(os.Getenv("AGENTD_PG_BIN"))
	if err != nil {
		t.Skipf("no test database programs here: %v", err)
	}
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	_, _, base := platformSpec(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head, "application/json", []byte(`{"text":"t"}`))
	ex := script(`touch "$OUT/ran"; echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`)
	d := daemon(t, base, func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := ex(a)
		sp.Env = slices.DeleteFunc(sp.Env, func(kv string) bool { k, _, _ := strings.Cut(kv, "="); return agentd.BusinessKey(k) })
		sp.Env = append(sp.Env, "KUBECONFIG=/home/agent/.kube/config")
		return sp, err
	}, bare, env)
	root, err := os.MkdirTemp("", "a")
	must(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	d.Root, d.TestDB = root, tools
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if out.EndHow != "prepare_failed" || err == nil || !strings.Contains(err.Error(), "KUBECONFIG") {
		t.Fatalf("run = %+v %v; want it ended before its executor, naming KUBECONFIG", out, err)
	}
	attempt := agentd.WorkspaceDir(root, cl.TaskID, cl.AttemptNo) + ".out"
	if _, serr := os.Stat(filepath.Join(attempt, "ran")); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("the executor ran: %v", serr)
	}
}
