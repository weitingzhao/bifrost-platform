package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	pruneTask  = "t_01M5B8BF2EM3SJQDKZ9E9TY2H8"
	pruneOther = "t_01M5B8BF2EM3SJQDKZ9E9TY2H9"
)

// pruneWorld is a bare repository standing in for Gitea, with three attempt
// branches, and a platform that hands out prunes and keeps the reports.
type pruneWorld struct {
	remote, commit, moved string
	mu                    sync.Mutex
	handed                []Prune
	reports               [][]PruneResult
	// unavailable makes that many reports fail with 503; refuse answers
	// every report 422.
	unavailable int
	refuse      bool
}

func newPruneWorld(t *testing.T) *pruneWorld {
	t.Helper()
	dir := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=a",
			"GIT_AUTHOR_EMAIL=a@example.invalid", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	w := &pruneWorld{remote: filepath.Join(dir, "bifrost-platform.git")}
	run(dir, "init", "-q", "--bare", w.remote)
	work := filepath.Join(dir, "work")
	run(dir, "init", "-q", work)
	run(work, "commit", "-q", "--allow-empty", "-m", "one")
	w.commit = run(work, "rev-parse", "HEAD")
	run(work, "commit", "-q", "--allow-empty", "-m", "two")
	w.moved = run(work, "rev-parse", "HEAD")
	for _, ref := range []string{"refs/heads/agent/" + pruneTask + "/a1", "refs/heads/agent/" + pruneTask + "/a2"} {
		run(work, "push", "-q", w.remote, w.commit+":"+ref)
	}
	run(work, "push", "-q", w.remote, w.moved+":refs/heads/agent/"+pruneOther+"/a1") // moved since the platform looked
	return w
}

func (w *pruneWorld) ref(task string, n string) string { return "refs/heads/agent/" + task + "/a" + n }

// protect makes the bare repository refuse every deletion, as Gitea refuses
// to delete a protected branch.
func (w *pruneWorld) protect(t *testing.T) {
	t.Helper()
	hook := filepath.Join(w.remote, "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do\n  case $new in 0000000000000000000000000000000000000000) echo \"Gitea: branch $ref is protected from deletion\" >&2; exit 1;; esac\ndone\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (w *pruneWorld) heads(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", w.remote, "for-each-ref", "--format=%(refname)", "refs/heads/agent").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (w *pruneWorld) daemon(t *testing.T) *Daemon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/prunes" && r.URL.Query().Get("host_id") == "h1":
			_ = json.NewEncoder(rw).Encode(map[string]any{"prunes": w.handed})
		case r.Method == http.MethodPost && r.URL.Path == "/prunes":
			var b struct {
				HostID  string        `json:"host_id"`
				Results []PruneResult `json:"results"`
			}
			if json.NewDecoder(r.Body).Decode(&b) != nil || b.HostID != "h1" {
				rw.WriteHeader(http.StatusBadRequest)
				return
			}
			if w.unavailable > 0 {
				w.unavailable--
				rw.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if w.refuse {
				rw.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(rw).Encode(map[string]any{"error": map[string]any{"code": "invalid", "message": "not pushed so"}})
				return
			}
			w.reports = append(w.reports, b.Results)
			_ = json.NewEncoder(rw).Encode(map[string]any{"recorded": len(b.Results)})
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &Daemon{Client: &Client{Base: srv.URL, Host: "h1", Token: "fixture"}, Root: t.TempDir(),
		Remote: func(string) string { return w.remote }, GitEnv: os.Environ()}
}

// TD-383: agentd deletes the attempt branches the platform hands it while
// they still name the commit given, reports a gone one as gone, leaves one
// that moved since unreported, and says what it did.
func TestPruneDeletesWhatItIsGiven(t *testing.T) {
	t.Parallel()
	w := newPruneWorld(t)
	w.handed = []Prune{
		{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "1"), Commit: w.commit},
		{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "3"), Commit: w.commit},  // gone already
		{Repo: "bifrost-platform", Ref: w.ref(pruneOther, "1"), Commit: w.commit}, // names another commit now
		{Repo: "bifrost-platform", Ref: "refs/heads/main", Commit: w.commit},      // never agentd's to delete
	}
	d := w.daemon(t)
	n, err := d.Prune(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("deleted %d (%v)", n, err)
	}
	if got := w.heads(t); got != w.ref(pruneTask, "2")+"\n"+w.ref(pruneOther, "1") {
		t.Fatalf("left on the remote:\n%s", got)
	}
	if len(w.reports) != 1 || len(w.reports[0]) != 2 || w.reports[0][0].Outcome != "deleted" || w.reports[0][1].Outcome != "gone" {
		t.Fatalf("reported %+v", w.reports)
	}
	// Nothing handed out: nothing done, nothing reported.
	w.handed = nil
	if n, err = d.Prune(context.Background()); err != nil || n != 0 || len(w.reports) != 1 {
		t.Fatalf("with nothing to do: %d (%v), %d reports", n, err, len(w.reports))
	}
}

// TD-383: while agent/** is protected, Gitea refuses: agentd reports the
// first refusal with Gitea's words and stops the round, with no error.
func TestPruneStopsAtARefusal(t *testing.T) {
	t.Parallel()
	w := newPruneWorld(t)
	w.protect(t)
	w.handed = []Prune{
		{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "1"), Commit: w.commit},
		{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "2"), Commit: w.commit},
	}
	n, err := w.daemon(t).Prune(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("deleted %d (%v)", n, err)
	}
	if len(w.reports) != 1 || len(w.reports[0]) != 1 || w.reports[0][0].Outcome != "refused" ||
		!strings.Contains(w.reports[0][0].Detail, "protected from deletion") {
		t.Fatalf("reported %+v", w.reports)
	}
	if got := w.heads(t); !strings.Contains(got, w.ref(pruneTask, "1")) || !strings.Contains(got, w.ref(pruneTask, "2")) {
		t.Fatalf("left on the remote:\n%s", got)
	}
}

// Review 436 B1: what a round did reaches the platform though the platform
// did not take it at first: the results wait on disk and go first next
// round. A report the platform refuses as it is does not wait.
func TestPruneReportsLaterWhatThePlatformDidNotTake(t *testing.T) {
	t.Parallel()
	w := newPruneWorld(t)
	w.handed = []Prune{{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "1"), Commit: w.commit}}
	w.unavailable = 1
	d := w.daemon(t)
	pending := filepath.Join(d.Root, prunePending)
	if _, err := d.Prune(context.Background()); err == nil {
		t.Fatal("a report the platform did not take is no error")
	}
	if _, err := os.Stat(pending); err != nil || strings.Contains(w.heads(t), w.ref(pruneTask, "1")) {
		t.Fatalf("after the first round: pending %v, heads %s", err, w.heads(t))
	}
	w.handed = nil // deleted: the platform no longer hands it out
	if _, err := d.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.reports) != 1 || len(w.reports[0]) != 1 || w.reports[0][0].Outcome != "deleted" || w.reports[0][0].Ref != w.ref(pruneTask, "1") {
		t.Fatalf("reported %+v", w.reports)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending after the report was taken: %v", err)
	}
	w.handed, w.refuse = []Prune{{Repo: "bifrost-platform", Ref: w.ref(pruneTask, "2"), Commit: w.commit}}, true
	if _, err := d.Prune(context.Background()); !errors.Is(err, errReportRefused) {
		t.Fatalf("a refused report: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("a refused report waits: %v", err)
	}
}

// TD-383: what git says goes into an event: an address with a credential in
// it, or a local path, does not.
func TestGitWordsKeepNoCredential(t *testing.T) {
	t.Parallel()
	got := gitWords("To https://agentd:abcdef0123456789@gitea.example/bifrost/x.git\n"+
		"fatal: unable to access 'https://agentd:abcdef0123456789@gitea.example/bifrost/x.git/': Could not resolve host\n"+
		"fatal: unable to access 'https://fedcba9876543210@gitea.example/bifrost/x.git/': Could not resolve host\n"+
		"error: failed to push some refs to '/Users/someone/x.git'\n", nil)
	if strings.Contains(got, "abcdef0123456789") || strings.Contains(got, "fedcba9876543210") || strings.Contains(got, "/Users/someone") ||
		!strings.Contains(got, "Could not resolve host") {
		t.Fatalf("gitWords: %q", got)
	}
}
