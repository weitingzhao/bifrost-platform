package agentd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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

// stopSeen is a proxy in front of the platform that says when a heartbeat
// answer of stop has been written whole to agentd (review 341 B2: after the
// response, not before it is sent on).
func stopSeen(t *testing.T, base string) (string, *atomic.Bool) {
	t.Helper()
	u, err := url.Parse(base)
	must(t, err)
	var seen atomic.Bool
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: u.Scheme, Host: u.Host})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &teeWriter{ResponseWriter: w}
		rp.ServeHTTP(rec, r)
		if !strings.HasSuffix(r.URL.Path, "/heartbeat") {
			return
		}
		var out map[string]any
		if json.Unmarshal(rec.body.Bytes(), &out) == nil && out["stop"] == true {
			seen.Store(true)
		}
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL + u.Path, &seen
}

// teeWriter keeps a copy of what it writes on.
type teeWriter struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (w *teeWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func waitFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear", filepath.Base(path))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// T07 安全停止 (v5 §5.3): a stop that comes while the attempt's branch is
// being pushed leaves no half state. The remote's hook holds the push until
// the test lets it go; meanwhile the task is stopped and a heartbeat answer
// of stop is written to agentd (heartbeats go on through a hand-over; review
// 340 A3). That agentd marked it is not observed: a hand-over does not read
// it.
// Let go, the push is recorded; refused, nothing is pushed and nothing
// recorded: the branch on the remote and the commit the platform holds are
// the same, or both absent. A stop never cuts a hand-over.
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
			mark := func(f string) string { return filepath.Join(sig, f) }
			// The hook ends only when let go; if it never is, it says so and
			// refuses, and the test fails on the mark.
			hook := "#!/bin/sh\ntouch '" + mark("pushing") + "'\ni=0\n" +
				"while [ ! -f '" + mark("go") + "' ]; do\n" +
				"  i=$((i+1)); if [ $i -gt 1200 ]; then touch '" + mark("timedout") + "'; exit 1; fi\n" +
				"  sleep 0.05\ndone\n"
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
			viaProxy, seen := stopSeen(t, base)
			d := daemon(t, viaProxy, script(`echo 'package x' > x.go; printf '# done\n' > "$OUT/report.md"`), bare, env)
			cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
			if err != nil || cl == nil {
				t.Fatalf("claim = %+v %v", cl, err)
			}
			type ran struct {
				out agentd.Outcome
				err error
			}
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan ran, 1)
			go func() { out, err := d.Run(runCtx, cl); done <- ran{out, err} }()
			var r ran
			var finished atomic.Bool
			wait := func(within time.Duration) bool {
				var timeout <-chan time.Time // nil for a zero wait: it only looks
				if within > 0 {
					timeout = time.After(within)
				}
				select {
				case r = <-done:
					finished.Store(true)
					return true
				case <-timeout:
					return false
				default:
				}
				if timeout == nil {
					return false
				}
				select {
				case r = <-done:
					finished.Store(true)
					return true
				case <-timeout:
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
			must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				return store.SetControl(ctx, tx, "owner", store.NewControlID(), "task", task, "stop", "T07: a stop during the push")
			}))
			deadline := time.Now().Add(10 * time.Second)
			for !seen.Load() {
				if time.Now().After(deadline) {
					t.Fatal("agentd was not answered stop while the push was held")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if wait(0) {
				t.Fatalf("the run ended before the push was let go: %+v %v", r.out, r.err)
			}
			if _, err := os.Stat(mark("timedout")); err == nil {
				t.Fatal("the hook stopped holding the push on its own")
			}
			must(t, os.WriteFile(mark("go"), nil, 0o600))
			if !wait(30 * time.Second) {
				t.Fatal("the run did not end")
			}
			if _, err := os.Stat(mark("timedout")); err == nil {
				t.Fatal("the hook timed out")
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
