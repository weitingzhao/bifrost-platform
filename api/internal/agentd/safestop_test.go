package agentd_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T07 安全停止 (v5 §5.3): a stop that comes while the attempt's branch is
// being pushed leaves no half state. The push goes on (a hand-over is not
// cut by a stop) and is recorded, or, refused by the remote, nothing is
// pushed and nothing recorded: the branch on the remote and the commit the
// platform holds are the same, or both absent. The stop is seen meanwhile:
// heartbeats go on through the hand-over and are answered stop.
func TestAStopDuringThePush(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		name := "pushed"
		if refuse {
			name = "refused"
		}
		t.Run(name, func(t *testing.T) {
			env := gitEnvT(t.TempDir())
			bare, baseCommit := bareRemote(t, env)
			sig := t.TempDir()
			hook := "#!/bin/sh\ntouch '" + sig + "/pushing'\ni=0\nwhile [ ! -f '" + sig + "/go' ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done\n"
			if refuse {
				hook += "exit 1\n"
			}
			must(t, os.WriteFile(filepath.Join(bare, "hooks", "pre-receive"), []byte(hook), 0o755))
			head := func(_ context.Context, _, ref string) (string, error) {
				out, err := exec.Command("git", "ls-remote", bare, ref).Output()
				first, _, _ := strings.Cut(string(out), "\t")
				return strings.TrimSpace(first), err
			}
			pool, task, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
			d := daemon(t, base, script(`echo 'package x' > x.go; printf '# done\n' > "$OUT/report.md"`), bare, env)
			cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
			if err != nil || cl == nil {
				t.Fatalf("claim = %+v %v", cl, err)
			}
			type ran struct {
				out agentd.Outcome
				err error
			}
			done := make(chan ran, 1)
			go func() { out, err := d.Run(ctx, cl); done <- ran{out, err} }()
			deadline := time.Now().Add(20 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(sig, "pushing")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the push did not start")
				}
				time.Sleep(20 * time.Millisecond)
			}
			must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				return store.SetControl(ctx, tx, "owner", store.NewControlID(), "task", task, "stop", "T07: a stop during the push")
			}))
			var stopped time.Time
			must(t, pool.QueryRow(ctx, `SELECT now() FROM agentrt.attempt LIMIT 1`).Scan(&stopped))
			deadline = time.Now().Add(10 * time.Second)
			for {
				var beat time.Time
				must(t, pool.QueryRow(ctx, `SELECT heartbeat_at FROM agentrt.attempt WHERE attempt_id = $1`, cl.AttemptID).Scan(&beat))
				if beat.After(stopped) {
					break // a heartbeat after the stop, answered stop
				}
				if time.Now().After(deadline) {
					t.Fatal("no heartbeat came while the push was held")
				}
				time.Sleep(20 * time.Millisecond)
			}
			must(t, os.WriteFile(filepath.Join(sig, "go"), nil, 0o600))
			var r ran
			select {
			case r = <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("the run did not end")
			}

			remote, _, _ := strings.Cut(sh(t, t.TempDir(), env, "git", "ls-remote", bare, cl.PushedRef), "\t")
			var recorded, state string
			must(t, pool.QueryRow(ctx, `SELECT COALESCE(pushed_commit, ''), state FROM agentrt.attempt WHERE attempt_id = $1`, cl.AttemptID).Scan(&recorded, &state))
			if remote != recorded {
				t.Fatalf("half a push: the remote's branch is %q, the platform holds %q (run %+v %v)", remote, recorded, r.out, r.err)
			}
			switch {
			case !refuse && (recorded == "" || !r.out.Submitted || state != "submitted"):
				t.Fatalf("not pushed and recorded: commit %q, attempt %s, run %+v %v", recorded, state, r.out, r.err)
			case refuse && (recorded != "" || r.out.Submitted):
				t.Fatalf("a refused push left commit %q, run %+v", recorded, r.out)
			}
		})
	}
}
