package agentd_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T22 case 4 (design v5 §5.2 Gate A, 推分支和过期同时发生): the attempt's
// branch is being pushed (the remote's hook holds the push) when its lease
// lapses past the grace; the lease loop takes the attempt as lost, the task
// goes back to ready and another host claims it. Let go, the push lands on
// the attempt's own branch and nowhere else; the platform refuses the
// hand-over (persist is fenced) and agentd salvages the pushed commit; the
// task and the other host's attempt are as they were before the push landed,
// and nothing of the lost attempt is persisted or submitted.
func TestPushAsTheLeaseLapses(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	sig := t.TempDir()
	mark := func(f string) string { return filepath.Join(sig, f) }
	hook := "#!/bin/sh\ntouch '" + mark("pushing") + "'\ni=0\n" +
		"while [ ! -f '" + mark("go") + "' ]; do\n" +
		"  i=$((i+1)); if [ $i -gt 1200 ]; then touch '" + mark("timedout") + "'; exit 1; fi\n" +
		"  sleep 0.05\ndone\n"
	must(t, os.WriteFile(filepath.Join(bare, "hooks", "pre-receive"), []byte(hook), 0o755))
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, task, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
	must(t, store.RegisterHost(ctx, pool, "owner", "h2", "dev"))
	d := daemon(t, base, script(`echo 'package x' > x.go; printf '# done\n' > "$OUT/report.md"`), bare, env)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil || cl.TaskID != task {
		t.Fatalf("claim = %+v %v", cl, err)
	}

	type ran struct {
		out agentd.Outcome
		err error
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan ran, 1)
	go func() { out, rerr := d.Run(runCtx, cl); done <- ran{out, rerr} }()
	var finished atomic.Bool
	var r ran
	wait := func(within time.Duration) bool {
		select {
		case r = <-done:
			finished.Store(true)
			return true
		case <-time.After(within):
			return false
		}
	}
	t.Cleanup(func() { // whatever failed: the hook let go, the run cancelled and waited for
		_ = os.WriteFile(mark("go"), nil, 0o600)
		cancel()
		if !finished.Load() && !wait(30*time.Second) {
			t.Errorf("the run did not end after it was cancelled")
		}
	})
	waitFile(t, mark("pushing"), 20*time.Second)

	// The lease lapses past the grace while the push is held: the fixture's
	// clock, the superuser moving the expiry back behind the triggers; then
	// the lease loop runs.
	var name string
	must(t, pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+name)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	_, err = admin.Exec(ctx, `SET session_replication_role = replica`)
	must(t, err)
	_, err = admin.Exec(ctx, `UPDATE agentrt.attempt SET lease_expires_at = clock_timestamp() - make_interval(secs => $2) WHERE attempt_id = $1`,
		cl.AttemptID, (store.Grace + time.Minute).Seconds())
	must(t, err)
	_, lost, err := store.SweepLeases(ctx, pool)
	if err != nil || lost != 1 {
		t.Fatalf("the sweep took %d lost (%v)", lost, err)
	}
	attemptState := func(id string) (state, pushed string, persisted bool) {
		t.Helper()
		must(t, pool.QueryRow(ctx, `SELECT state, COALESCE(pushed_commit, ''), persisted_at IS NOT NULL FROM agentrt.attempt WHERE attempt_id = $1`, id).
			Scan(&state, &pushed, &persisted))
		return
	}
	if st, _, _ := attemptState(cl.AttemptID); st != "lost" {
		t.Fatalf("the lapsed attempt is %s", st)
	}

	// Another host takes the task again, while the first push is still held.
	h2 := &agentd.Client{Base: base, Host: "h2", Token: "fixture-agentd-h2"}
	cl2, err := h2.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl2 == nil || cl2.TaskID != task || cl2.AttemptNo != 2 {
		t.Fatalf("the second claim = %+v %v", cl2, err)
	}
	type taskNow struct {
		state       string
		version     int
		moves       int
		otherState  string
		otherPushed string
	}
	now := func() taskNow {
		t.Helper()
		var n taskNow
		must(t, pool.QueryRow(ctx, `SELECT state, version FROM agentrt.task WHERE task_id = $1`, task).Scan(&n.state, &n.version))
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1`, task).Scan(&n.moves))
		n.otherState, n.otherPushed, _ = attemptState(cl2.AttemptID)
		return n
	}
	before := now()
	if before.state != "leased" || before.otherState != "leased" {
		t.Fatalf("before the push lands: %+v", before)
	}
	if wait(0) {
		t.Fatalf("the run ended before the push was let go: %+v %v", r.out, r.err)
	}

	must(t, os.WriteFile(mark("go"), nil, 0o600))
	if !wait(30 * time.Second) {
		t.Fatal("the run did not end")
	}
	if _, err := os.Stat(mark("timedout")); err == nil {
		t.Fatal("the hook stopped holding the push on its own")
	}
	if r.out.EndHow != "lease_lost" || r.out.Submitted || !r.out.Salvaged {
		t.Fatalf("run = %+v %v", r.out, r.err)
	}

	// The push landed on the lost attempt's own branch, and only there.
	refHead := func(ref string) string {
		h, _, _ := strings.Cut(sh(t, t.TempDir(), env, "git", "ls-remote", bare, ref), "\t")
		return h
	}
	st, pushed, persisted := attemptState(cl.AttemptID)
	if st != "lost" || !persisted || pushed == "" || refHead(cl.PushedRef) != pushed || cl.PushedRef != "refs/heads/agent/"+task+"/a1" {
		t.Fatalf("the lost attempt: %s, pushed %q (persisted %v); its branch %s is at %q", st, pushed, persisted, cl.PushedRef, refHead(cl.PushedRef))
	}
	if h := refHead(cl2.PushedRef); h != "" {
		t.Fatalf("the other attempt's branch %s is at %s", cl2.PushedRef, h)
	}
	if h := refHead("refs/heads/main"); h != baseCommit {
		t.Fatalf("main moved to %s", h)
	}
	if refs := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/agent/"+task+"/a1\nrefs/heads/main" {
		t.Fatalf("the remote's refs:\n%s", refs)
	}
	// The task and the other host's attempt did not move.
	if after := now(); after != before {
		t.Fatalf("the task moved when the push landed: before %+v, after %+v", before, after)
	}
	var persistedEv, submitted, salvaged int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type = 'attempt_persisted'), count(*) FILTER (WHERE event_type = 'attempt_submitted'),
	  count(*) FILTER (WHERE event_type = 'attempt_salvaged') FROM agentrt.event WHERE attempt_id = $1`, cl.AttemptID).Scan(&persistedEv, &submitted, &salvaged))
	if persistedEv != 0 || submitted != 0 || salvaged != 1 {
		t.Fatalf("the lost attempt: %d persisted, %d submitted, %d salvaged", persistedEv, submitted, salvaged)
	}
}
