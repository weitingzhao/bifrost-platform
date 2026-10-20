package agentd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// dieAt is a proxy in front of the platform that holds the first request
// matching at and says so: the test kills agentd while it is held. With
// taken, the platform has the request first and only its answer is lost
// with the process; without, the request never reaches the platform.
type dieAt struct {
	at    func(r *http.Request) bool
	taken bool
	held  chan struct{} // closed when the request is held
	dead  chan struct{} // closed by the test once agentd is dead
	once  sync.Once
}

func (d *dieAt) proxy(t *testing.T, base string) string {
	t.Helper()
	target, err := url.Parse(base)
	must(t, err)
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := false
		if d.at(r) {
			d.once.Do(func() { hit = true })
		}
		if !hit {
			rp.ServeHTTP(w, r)
			return
		}
		if d.taken {
			rp.ServeHTTP(httptest.NewRecorder(), r) // the platform takes it; the answer goes nowhere
		}
		close(d.held)
		<-d.dead
	}))
	t.Cleanup(func() {
		select {
		case <-d.dead:
		default:
			close(d.dead)
		}
		s.Close()
	})
	return s.URL + target.Path
}

// T34 case 4 (design v5 §5.2 Gate A, 上传中途重启 agentd): agentd, a process
// of its own, is killed (SIGKILL) in the middle of its hand-over: while the
// report is being uploaded, once the platform has taken the report but
// before its answer came back, and once the platform has recorded persist
// but before its answer came back. A new agentd on the same registry and
// workspace takes over: the result is handed over once (the artifacts, one
// artifact_put each; one attempt_persisted; one attempt_submitted; one
// commit, on the attempt's branch, the one the platform holds), and the task
// can be verified.
func TestRestartDuringTheHandOver(t *testing.T) {
	report := func(r *http.Request) bool {
		return r.Method == http.MethodPut && r.Header.Get("X-Artifact-Kind") == "report"
	}
	for _, c := range []struct {
		name  string
		at    func(*http.Request) bool
		taken bool
		// what the platform holds when agentd dies: reports, persisted
		reports, persisted int
	}{
		{"uploading the report", report, false, 0, 0},
		{"the report taken, its answer lost", report, true, 1, 0},
		{"persist recorded, its answer lost", func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/persist") }, true, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := branchPlatform(t)
			die := &dieAt{at: c.at, taken: c.taken, held: make(chan struct{}), dead: make(chan struct{})}
			root := t.TempDir()
			cfg := agentd.TestDaemon{Base: die.proxy(t, w.base), Host: "h1", Token: "fixture-agentd-h1", Root: root,
				Registry: filepath.Join(root, "registry.json"), Remote: w.bare, GitEnv: w.env,
				Script: `echo compiling; echo 'package x' > x.go; printf '# done\nall good\n' > "$OUT/report.md"`}
			raw, err := json.Marshal(cfg)
			must(t, err)
			path := filepath.Join(t.TempDir(), "daemon.json")
			must(t, os.WriteFile(path, raw, 0o600))

			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), agentd.TestDaemonEnv+"="+path)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			must(t, cmd.Start())
			select {
			case <-die.held:
			case <-time.After(60 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("agentd never reached the point:\n%s", stderr.String())
			}
			must(t, cmd.Process.Kill())
			_ = cmd.Wait()
			close(die.dead)
			t.Logf("the first agentd, killed:\n%s", stderr.String())

			var attempt string
			must(t, w.pool.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id = $1`, w.task).Scan(&attempt))
			count := func(q string, args ...any) int {
				t.Helper()
				var n int
				must(t, w.pool.QueryRow(ctx, q, args...).Scan(&n))
				return n
			}
			if state, _ := taskState(t, w.pool, w.task); state != "running" {
				t.Fatalf("before the takeover the task is %s", state)
			}
			reports := count(`SELECT count(*) FROM agentrt.artifact WHERE artifact_kind = 'report'`)
			persisted := count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'attempt_persisted' AND attempt_id = $1`, attempt)
			if reports != c.reports || persisted != c.persisted {
				t.Fatalf("agentd died with %d reports and %d persisted on the platform; want %d and %d", reports, persisted, c.reports, c.persisted)
			}
			reg, err := agentd.OpenRegistry(cfg.Registry)
			must(t, err)
			journal, err := reg.Load()
			must(t, err)
			if e := journal[attempt]; e.Phase != "computed" {
				t.Fatalf("the killed agentd's journal: %+v", e)
			}

			next := daemon(t, w.base, nil, w.bare, w.env)
			next.Registry, next.Root = reg, root
			must(t, next.Recover(ctx))

			state, rep := taskState(t, w.pool, w.task)
			if state != "submitted" || rep == "" {
				t.Fatalf("after the takeover: task %s, report %q", state, rep)
			}
			for _, typ := range []string{"attempt_persisted", "attempt_submitted"} {
				if n := count(`SELECT count(*) FROM agentrt.event WHERE event_type = $1 AND attempt_id = $2`, typ, attempt); n != 1 {
					t.Errorf("%d %s", n, typ)
				}
			}
			if n := count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'artifact_put' GROUP BY body_json->>'artifact_id' ORDER BY 1 DESC LIMIT 1`); n != 1 {
				t.Errorf("an artifact recorded %d times", n)
			}
			for _, kind := range []string{"report", "log_tail"} {
				if n := count(`SELECT count(*) FROM agentrt.artifact WHERE artifact_kind = $1`, kind); n != 1 {
					t.Errorf("%d artifacts of kind %s", n, kind)
				}
			}
			var pushed, ref string
			must(t, w.pool.QueryRow(ctx, `SELECT pushed_commit, pushed_ref FROM agentrt.attempt WHERE attempt_id = $1`, attempt).Scan(&pushed, &ref))
			head, _, _ := strings.Cut(sh(t, t.TempDir(), w.env, "git", "ls-remote", w.bare, ref), "\t")
			if head != pushed || ref != "refs/heads/agent/"+w.task+"/a1" {
				t.Fatalf("the branch %s is at %s, the platform holds %s", ref, head, pushed)
			}
			if n := strings.TrimSpace(sh(t, t.TempDir(), w.env, "git", "--git-dir", w.bare, "rev-list", "--count", w.baseCommit+".."+pushed)); n != "1" {
				t.Fatalf("%s commits on the branch", n)
			}
			if m, lerr := reg.Load(); lerr != nil || len(m) != 0 {
				t.Fatalf("registry after the takeover = %v %v", m, lerr)
			}
			check, err := store.StartVerification(ctx, w.pool, w.task)
			if err != nil || check == "" {
				t.Fatalf("the task cannot be verified: %q %v", check, err)
			}
			if state, _ := taskState(t, w.pool, w.task); state != "verifying" {
				t.Fatalf("after its verification was started the task is %s", state)
			}
		})
	}
}
