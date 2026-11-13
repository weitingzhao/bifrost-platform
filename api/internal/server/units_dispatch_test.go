package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// fakeUnitCluster keeps what was created, as the cluster would return it.
type fakeUnitCluster struct {
	runs map[string]*unitexec.TaskRun
	seq  int
}

func (f *fakeUnitCluster) List(_ context.Context, plan string) ([]unitexec.TaskRun, error) {
	var out []unitexec.TaskRun
	for _, tr := range f.runs {
		if plan == "" || tr.Metadata.Labels[unitexec.LabelPlan] == plan {
			out = append(out, *tr)
		}
	}
	return out, nil
}

func (f *fakeUnitCluster) Create(_ context.Context, obj map[string]any) (bool, error) {
	raw, _ := json.Marshal(obj)
	var tr unitexec.TaskRun
	if err := json.Unmarshal(raw, &tr); err != nil {
		return false, err
	}
	if _, ok := f.runs[tr.Metadata.Name]; ok {
		return false, nil
	}
	f.seq++
	now := time.Now()
	tr.Metadata.UID, tr.Metadata.CreationTimestamp = "uid-"+tr.Metadata.Name, &now
	f.runs[tr.Metadata.Name] = &tr
	return true, nil
}

// finish ends a run as the cluster ends one: the condition, and for a run
// that judged its content the results the template writes.
func (f *fakeUnitCluster) finish(t *testing.T, name, status, reason string, results map[string]string) {
	t.Helper()
	tr := f.runs[name]
	if tr == nil {
		t.Fatalf("no run %s among %d", name, len(f.runs))
	}
	raw, _ := json.Marshal(map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status, "reason": reason, "message": reason}}})
	if err := json.Unmarshal(raw, &tr.Status); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tr.Status.CompletionTime = &now
	for name, value := range results {
		tr.Status.Results = append(tr.Status.Results, unitexec.NameValue{Name: name, Value: value})
	}
}

func unitTestTrees(t *testing.T, commit string) unitTreesFunc {
	t.Helper()
	entries, blobs := planRepo()
	// The api module as a dispatchable one: an image and a template. The
	// repository's own unit keeps none, as the real manifest's does today.
	withImage := strings.Replace(unitsManifest, "    root: api\n", "    root: api\n    image: docker.io/library/golang:1.25-bookworm\n    template: bifrost-unit-run\n", 1)
	if withImage == unitsManifest {
		t.Fatal("the test manifest has no api module to give an image")
	}
	for i, e := range entries {
		if e.Path == "bifrost.units.yaml" {
			id := gitBlobID(withImage)
			blobs[id] = withImage
			entries[i].SHA = id
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/bifrost/r/git/trees/"+commit:
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": commit, "page": 1, "total_count": len(entries), "truncated": false, "tree": entries})
		case strings.HasPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost/r/git/blobs/")
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(blobs[id]))})
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	g := &giteaTrees{access: func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL, Org: "bifrost"}, nil
	}, client: srv.Client()}
	return g.tree
}

// A commit taken step by step: the task the ledger does not know is started
// once; the run the cluster lost is started once more; the retry's pass is
// recorded and becomes the ledger's verdict, after which nothing is left to
// run for that task. The task with no image is refused and said so.
func TestUnitsAdvanceAndReceive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	commit := strings.Repeat("c", 40)
	trees := unitTestTrees(t, commit)
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	cl := &fakeUnitCluster{runs: map[string]*unitexec.TaskRun{}}
	cfg := unitDispatchConfig{Mode: "shadow", MaxRunning: 12, GiteaBase: "http://gitea.cicd.svc.cluster.local:3000/bifrost", Namespace: "cicd"}

	off := cfg
	off.Mode = "off"
	if _, err := advanceUnits(ctx, pool, trees, cl, off, "r", commit, "branch"); err == nil || len(cl.runs) != 0 {
		t.Fatalf("dispatch off started %d runs: %v", len(cl.runs), err)
	}

	first, err := advanceUnits(ctx, pool, trees, cl, cfg, "r", commit, "branch")
	if err != nil {
		t.Fatal(err)
	}
	if first.Tasks != 2 || first.ToRun != 2 || len(first.Created) != 1 || len(first.Refused) != 1 || !strings.Contains(first.Refused[0], "repo check") {
		t.Fatalf("first step: %+v", first)
	}
	name := first.Created[0]
	if again, err := advanceUnits(ctx, pool, trees, cl, cfg, "r", commit, "branch"); err != nil || len(again.Created) != 0 || again.Present != 1 || len(cl.runs) != 1 {
		t.Fatalf("the same step again: %+v %v", again, err)
	}

	recv := &unitReceiver{}
	advance := func(ctx context.Context, repo, commit, tier string) error {
		_, err := advanceUnits(ctx, pool, trees, cl, cfg, repo, commit, tier)
		return err
	}
	if recorded, evidence, err := recv.receiveUnits(ctx, pool, trees, cl, advance, time.Now()); err != nil || recorded != 0 || evidence != 0 {
		t.Fatalf("a run still going was recorded: %d %d %v", recorded, evidence, err)
	}

	// The cluster loses the run; the receiving pass records it and has the
	// commit advanced: the one retry starts.
	cl.finish(t, name, "False", "TaskRunImagePullFailed", nil)
	recorded, evidence, err := recv.receiveUnits(ctx, pool, trees, cl, advance, time.Now())
	if err != nil || recorded != 1 || evidence != 0 {
		t.Fatalf("after the lost run: %d %d %v", recorded, evidence, err)
	}
	retry := name + "-r2"
	if cl.runs[retry] == nil || len(cl.runs) != 2 {
		t.Fatalf("the retry was not started: %d runs", len(cl.runs))
	}

	// The retry passes.
	cl.finish(t, retry, "True", "Succeeded", map[string]string{"exit-code": "0", "duration-seconds": "3", "fail-class": "", "failed-tests": "",
		"tests": "1 " + strings.Repeat("a", 64) + " 1 0"})
	recorded, evidence, err = recv.receiveUnits(ctx, pool, trees, cl, advance, time.Now())
	if err != nil || recorded != 1 || evidence != 1 {
		t.Fatalf("after the pass: %d %d %v", recorded, evidence, err)
	}
	plan, err := unitPlan(ctx, pool, trees, "r", commit, false)
	if err != nil || plan.Counts[store.EvidenceGreen] != 1 || plan.ToRun != 1 {
		t.Fatalf("the ledger after the pass: %+v %v", plan.Counts, err)
	}
	if recorded, evidence, err = recv.receiveUnits(ctx, pool, trees, cl, advance, time.Now()); err != nil || recorded != 0 || evidence != 0 {
		t.Fatalf("the same runs seen again wrote %d %d: %v", recorded, evidence, err)
	}
	// A receiver that starts anew (a restart) sees the same runs and writes nothing.
	if recorded, evidence, err = (&unitReceiver{}).receiveUnits(ctx, pool, trees, cl, advance, time.Now()); err != nil || recorded != 0 || evidence != 0 {
		t.Fatalf("after a restart the same runs wrote %d %d: %v", recorded, evidence, err)
	}
	last, err := advanceUnits(ctx, pool, trees, cl, cfg, "r", commit, "branch")
	if err != nil || last.ToRun != 1 || len(last.Created) != 0 || len(cl.runs) != 2 {
		t.Fatalf("after the verdict: %+v %v", last, err)
	}
}

// A red run is the ledger's red, with the test's name; a run lost twice is
// left; and no more runs are started than MaxRunning.
func TestUnitsRedLostAndCapacity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	cfg := unitDispatchConfig{Mode: "on", MaxRunning: 12, GiteaBase: "http://gitea.cicd.svc.cluster.local:3000/bifrost", Namespace: "cicd"}

	red := strings.Repeat("d", 40)
	trees := unitTestTrees(t, red)
	cl := &fakeUnitCluster{runs: map[string]*unitexec.TaskRun{}}
	step, err := advanceUnits(ctx, pool, trees, cl, cfg, "r", red, "branch")
	if err != nil || len(step.Created) != 1 {
		t.Fatalf("%+v %v", step, err)
	}
	cl.finish(t, step.Created[0], "False", "Failed", map[string]string{"exit-code": "1", "fail-class": "test", "failed-tests": "example.com/m/a.TestA",
		"tests": "1 " + strings.Repeat("a", 64) + " 1 0"})
	recv := &unitReceiver{}
	if recorded, evidence, err := recv.receiveUnits(ctx, pool, trees, cl, nil, time.Now()); err != nil || recorded != 1 || evidence != 1 {
		t.Fatalf("the red run: %d %d %v", recorded, evidence, err)
	}
	plan, err := unitPlan(ctx, pool, trees, "r", red, false)
	if err != nil || plan.Counts[store.EvidenceRed] != 1 {
		t.Fatalf("the ledger after the red run: %+v %v", plan.Counts, err)
	}
	var statement string
	if err = pool.QueryRow(ctx, `SELECT statement::text FROM agentrt.evidence WHERE commit_sha = $1`, red).Scan(&statement); err != nil ||
		!strings.Contains(statement, "example.com/m/a.TestA") || !strings.Contains(statement, `"fail_class": "test"`) {
		t.Fatalf("the red row's statement: %s %v", statement, err)
	}
	if again, err := advanceUnits(ctx, pool, trees, cl, cfg, "r", red, "branch"); err != nil || len(again.Created)+len(again.Retried) != 0 {
		t.Fatalf("red content was run again: %+v %v", again, err)
	}

	// Lost twice: nothing more is started, and it is said.
	lost := &fakeUnitCluster{runs: map[string]*unitexec.TaskRun{}}
	db2 := agentrttest.Migrated(t)
	pool2 := db2.Pool(t, db2.Runtime)
	step, _ = advanceUnits(ctx, pool2, trees, lost, cfg, "r", red, "branch")
	lost.finish(t, step.Created[0], "False", "TaskRunImagePullFailed", nil)
	step2, _ := advanceUnits(ctx, pool2, trees, lost, cfg, "r", red, "branch")
	if len(step2.Retried) != 1 {
		t.Fatalf("the retry: %+v", step2)
	}
	lost.finish(t, step2.Retried[0], "False", "TaskRunTimeout", nil)
	step3, err := advanceUnits(ctx, pool2, trees, lost, cfg, "r", red, "branch")
	if err != nil || len(step3.Lost) != 1 || len(lost.runs) != 2 {
		t.Fatalf("lost twice: %+v %v", step3, err)
	}
	if recorded, evidence, err := (&unitReceiver{}).receiveUnits(ctx, pool2, trees, lost, nil, time.Now()); err != nil || recorded != 2 || evidence != 0 {
		t.Fatalf("two lost runs: %d %d %v", recorded, evidence, err)
	}

	// Capacity: another commit's run is going and MaxRunning is 1.
	full := &fakeUnitCluster{runs: map[string]*unitexec.TaskRun{}}
	db3 := agentrttest.Migrated(t)
	pool3 := db3.Pool(t, db3.Runtime)
	busy := cfg
	busy.MaxRunning = 1
	now := time.Now()
	going := unitexec.TaskRun{}
	going.Metadata.Name, going.Metadata.CreationTimestamp = "unit-ffffffff-0", &now
	going.Metadata.Labels = map[string]string{unitexec.LabelPlan: strings.Repeat("f", 16)}
	full.runs[going.Metadata.Name] = &going
	held, err := advanceUnits(ctx, pool3, trees, full, busy, "r", red, "branch")
	if err != nil || held.Deferred != 1 || len(held.Created) != 0 || len(full.runs) != 1 {
		t.Fatalf("over capacity: %+v %v", held, err)
	}
}

func TestUnitDispatchConfigFromEnv(t *testing.T) {
	for _, tc := range []struct {
		mode, selector, max string
		wantMode            string
		wantSelector        int
		wantMax             int
	}{
		{"", "", "", "off", 0, 12},
		{"shadow", "bifrost.io/host-id=ubt-k3s-05", "20", "shadow", 1, 20},
		{" ON ", "", "", "on", 0, 12},
		{"yes", "a=b,c=d", "0", "off", 0, 12},
		{"on", "nokey", "many", "on", 0, 12},
	} {
		t.Setenv(unitDispatchEnv, tc.mode)
		t.Setenv(unitNodeSelectorEnv, tc.selector)
		t.Setenv(unitMaxRunningEnv, tc.max)
		got := unitDispatchConfigFromEnv("cicd")
		if got.Mode != tc.wantMode || len(got.NodeSelector) != tc.wantSelector || got.MaxRunning != tc.wantMax || got.Namespace != "cicd" || got.GiteaBase == "" {
			t.Errorf("%q %q %q: %+v", tc.mode, tc.selector, tc.max, got)
		}
	}
}

// The route takes a repository and a full commit and nothing else, and says
// so before it reads anything.
func TestUnitEvidenceRequestRefusesWhatIsNotACommit(t *testing.T) {
	for _, body := range []string{``, `{"repo":"bifrost-platform","commit":"main"}`, `{"repo":"../x","commit":"` + strings.Repeat("a", 40) + `"}`, `{"repo":"r.git","commit":"` + strings.Repeat("a", 40) + `"}`} {
		w := httptest.NewRecorder()
		(&Server{}).handleUnitEvidenceRequest(w, httptest.NewRequest(http.MethodPost, "/api/v1/units/evidence", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: %d", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	(&Server{}).handleUnitEvidenceRequest(w, httptest.NewRequest(http.MethodPost, "/api/v1/units/evidence", strings.NewReader(`{"repo":"r","commit":"`+strings.Repeat("a", 40)+`"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("a platform with no ledger answered %d", w.Code)
	}
}
