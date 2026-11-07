package agentdeploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testCommit = "0123456789abcdef0123456789abcdef01234567"
	testSHA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	oldCommit  = "fedcba9876543210fedcba9876543210fedcba98"
)

// world is the host, its runtime records and its ssh, in memory; steps is
// what the deploy did, in order.
type world struct {
	now     time.Time
	steps   []string
	running []int  // what each Running read answers; the last one stays
	build   string // what the host's claims report
	// afterUpdate is the build the host reports once agentd-update ran.
	afterUpdate string
	updateErr   error
	rollbackErr error
}

func (w *world) SetAside(_ context.Context, host, _ string) (string, error) {
	w.steps = append(w.steps, "aside "+host)
	return "c_1", nil
}

func (w *world) SetBack(_ context.Context, id string) error {
	w.steps = append(w.steps, "back "+id)
	return nil
}

func (w *world) Running(context.Context, string) (int, error) {
	n := w.running[0]
	if len(w.running) > 1 {
		w.running = w.running[1:]
	}
	w.steps = append(w.steps, fmt.Sprintf("running %d", n))
	return n, nil
}

func (w *world) Build(context.Context, string) (string, error) {
	w.steps = append(w.steps, "build "+w.build[:7])
	return w.build, nil
}

func (w *world) Run(_ context.Context, target string, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	w.steps = append(w.steps, "ssh "+target+" "+cmd)
	if strings.HasSuffix(cmd, "--rollback") {
		if w.rollbackErr == nil {
			w.build = oldCommit
		}
		return "rolled back", w.rollbackErr
	}
	if w.updateErr != nil {
		return "agentd-update: refused", w.updateErr
	}
	w.build = w.afterUpdate
	return "installed", nil
}

func (w *world) agentd(t *testing.T) *Agentd {
	t.Setenv("PLATFORM_AGENT_DEPLOY_LAST", filepath.Join(t.TempDir(), "last.json"))
	return &Agentd{
		Targets: func() (map[string]string, error) { return map[string]string{"mini-1": "deploy@10.0.0.5"}, nil },
		PackageSHA256: func(_ context.Context, commit string) (string, error) {
			w.steps = append(w.steps, "package "+commit[:7])
			return testSHA, nil
		},
		Hosts: w, SSH: w, Store: NewStore(),
		Now:   func() time.Time { return w.now },
		Sleep: func(_ context.Context, d time.Duration) error { w.now = w.now.Add(d); return nil },
	}
}

func cardParams() map[string]any {
	return map[string]any{"env": "prod", "host": "mini-1", "commit": testCommit, "sha256": testSHA}
}

func wantSteps(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The order: the package is checked, the host is set aside, the deploy
// waits until nothing runs on it, runs exactly agentd-update over ssh, waits
// until its claims report the build, and sets it back.
func TestAgentdDeployOrder(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{2, 1, 0}, build: oldCommit, afterUpdate: testCommit}
	d := w.agentd(t)
	out, err := d.Run(context.Background(), cardParams())
	if err != nil {
		t.Fatal(err)
	}
	wantSteps(t, w.steps,
		"package 0123456",
		"aside mini-1",
		"running 2", "running 1", "running 0",
		"ssh deploy@10.0.0.5 sudo -n agentd-update "+testCommit+" "+testSHA,
		"build 0123456",
		"back c_1")
	if out["control_id"] != "c_1" || out["host"] != "mini-1" {
		t.Fatalf("result %v", out)
	}
	last := d.Store.Last()
	if last == nil || last.Status != "done" || last.Role != "agentd" || last.Remote != "deploy@10.0.0.5" || !strings.Contains(last.Log, "set back") {
		t.Fatalf("job %+v", last)
	}
	if NewStore().Last() == nil {
		t.Fatal("the job is not kept on disk")
	}
}

// A host that never reports the build is rolled back and set back, and the
// deploy fails saying so.
func TestAgentdDeployRollsBackFailedCheck(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{0}, build: oldCommit, afterUpdate: oldCommit}
	d := w.agentd(t)
	_, err := d.Run(context.Background(), cardParams())
	if err == nil || !strings.Contains(err.Error(), "did not report agentd") || !strings.Contains(err.Error(), "--rollback restored") {
		t.Fatalf("err %v", err)
	}
	n := len(w.steps)
	if w.steps[n-2] != "ssh deploy@10.0.0.5 sudo -n agentd-update --rollback" || w.steps[n-1] != "back c_1" {
		t.Fatalf("steps %v", w.steps)
	}
	// Polled for five minutes, every ten seconds, before rolling back.
	if builds := strings.Count(strings.Join(w.steps, "\n"), "build "); builds != 31 {
		t.Fatalf("%d build reads", builds)
	}
	if last := d.Store.Last(); last == nil || last.Status != "failed" {
		t.Fatalf("job %+v", last)
	}
}

// A rollback that fails leaves the host aside for the Owner.
func TestAgentdDeployFailedRollbackKeepsHostAside(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{0}, build: oldCommit, afterUpdate: oldCommit, rollbackErr: errors.New("exit status 1")}
	_, err := w.agentd(t).Run(context.Background(), cardParams())
	if err == nil || !strings.Contains(err.Error(), "stays set aside: lift control c_1") {
		t.Fatalf("err %v", err)
	}
	for _, s := range w.steps {
		if strings.HasPrefix(s, "back") {
			t.Fatalf("set back after a failed rollback: %v", w.steps)
		}
	}
}

// A sha256 that is not the package's is refused before anything is touched.
func TestAgentdDeployRefusesSHA256NotThePackages(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{0}, build: oldCommit, afterUpdate: testCommit}
	d := w.agentd(t)
	p := cardParams()
	p["sha256"] = strings.Repeat("b", 64)
	_, err := d.Run(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "not the card's") {
		t.Fatalf("err %v", err)
	}
	wantSteps(t, w.steps, "package 0123456")
	if d.Store.Last() != nil {
		t.Fatal("a refused card recorded a job")
	}
}

// A host still busy after thirty minutes is set back; nothing is installed.
func TestAgentdDeployGivesUpOnBusyHost(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{1}, build: oldCommit, afterUpdate: testCommit}
	_, err := w.agentd(t).Run(context.Background(), cardParams())
	if err == nil || !strings.Contains(err.Error(), "still run on host mini-1 after 30m0s") {
		t.Fatalf("err %v", err)
	}
	if w.steps[len(w.steps)-1] != "back c_1" || strings.Contains(strings.Join(w.steps, "\n"), "ssh ") {
		t.Fatalf("steps %v", w.steps)
	}
}

// A failed agentd-update swapped nothing: the host is set back, no rollback.
func TestAgentdDeployFailedUpdateSetsBack(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{0}, build: oldCommit, updateErr: errors.New("exit status 2")}
	_, err := w.agentd(t).Run(context.Background(), cardParams())
	if err == nil || !strings.Contains(err.Error(), "agentd-update on mini-1 failed") {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(strings.Join(w.steps, "\n"), "--rollback") || w.steps[len(w.steps)-1] != "back c_1" {
		t.Fatalf("steps %v", w.steps)
	}
}

func TestAgentdParamsForm(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"stg":         func(p map[string]any) { p["env"] = "stg" },
		"short":       func(p map[string]any) { p["commit"] = testCommit[:12] },
		"upper sha":   func(p map[string]any) { p["sha256"] = strings.ToUpper(testSHA) },
		"host flag":   func(p map[string]any) { p["host"] = "-oProxyCommand=x" },
		"extra param": func(p map[string]any) { p["target"] = "root@evil" },
	} {
		p := cardParams()
		mutate(p)
		if _, err := ParseAgentdParams(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseAgentdParams(cardParams()); err != nil {
		t.Fatal(err)
	}
}

// The card names a host; only configuration says where it is.
func TestAgentdDeployRefusesUnconfiguredHost(t *testing.T) {
	w := &world{now: time.Unix(1_800_000_000, 0), running: []int{0}, build: oldCommit}
	p := cardParams()
	p["host"] = "mini-9"
	if _, err := w.agentd(t).Run(context.Background(), p); err == nil || !strings.Contains(err.Error(), "no ssh target") {
		t.Fatalf("err %v", err)
	}
	if len(w.steps) != 0 {
		t.Fatalf("steps %v", w.steps)
	}
}

func TestAgentdTargets(t *testing.T) {
	got, err := ParseTargets(" mini-1=deploy@10.0.0.5 , mini-2=deploy@10.0.0.6 ")
	if err != nil || len(got) != 2 || got["mini-2"] != "deploy@10.0.0.6" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"mini-1", "mini-1=10.0.0.5", "mini-1=-oProxyCommand=x@y", "a=d@x,a=d@y"} {
		if _, err := ParseTargets(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAgentdPackageSHA256(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/"+testCommit+"/") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "%s  agentd\n", strings.ToUpper(testSHA))
	}))
	defer srv.Close()
	p := Package{URL: srv.URL + "/api/packages/bifrost/generic/agentd/{commit}/agentd.sha256"}
	if got, err := p.SHA256(context.Background(), testCommit); err != nil || got != testSHA {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := p.SHA256(context.Background(), oldCommit); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("err %v", err)
	}
}
