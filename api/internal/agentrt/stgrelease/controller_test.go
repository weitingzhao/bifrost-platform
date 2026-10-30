package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/yaml"
)

const controlRoot = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"

func sha(s string) string { return strings.Repeat(s, 40) }
func dig(s string) string { return "sha256:" + strings.Repeat(s, 64) }

var controlMember = store.StgMemberKey{Root: controlRoot, Op: "land:one:1", R: sha("a")}

type memoryLedger struct {
	entries        []store.StgEntry
	clock          *clocktesting.FakeClock
	reads, commits int
	fail           bool
}

func (l *memoryLedger) Read(context.Context) (store.StgState, error) {
	l.reads++
	return store.StgFold(l.entries)
}
func (l *memoryLedger) Commit(_ context.Context, seq int64, rs []store.StgRecord) (bool, error) {
	l.commits++
	if l.fail {
		return false, errors.New("commit unavailable")
	}
	if seq != int64(len(l.entries)) {
		return false, nil
	}
	pending := append([]store.StgEntry{}, l.entries...)
	for _, r := range rs {
		parsed, err := store.ParseStgRelease(r)
		if err != nil {
			return false, err
		}
		pending = append(pending, store.StgEntry{Seq: int64(len(pending) + 1), At: l.clock.Now(), Event: store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "test", "stg_release": map[string]any(parsed)}}})
	}
	if _, err := store.StgFold(pending); err != nil {
		return false, err
	}
	l.entries = pending
	return true, nil
}
func (l *memoryLedger) add(t *testing.T, rs ...store.StgRecord) {
	t.Helper()
	if _, err := l.Commit(context.Background(), int64(len(l.entries)), rs); err != nil {
		t.Fatal(err)
	}
}
func (l *memoryLedger) state(t *testing.T) store.StgState {
	t.Helper()
	s, err := l.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (l *memoryLedger) effective(t *testing.T, m store.StgMemberKey, p string) {
	t.Helper()
	r := memberValue(m)
	for k, v := range map[string]any{"v": "2", "M0": sha("e"), "head": p, "has_r": true, "validity": "effective", "reverted_by": "", "verdict": "observed", "read_at": l.clock.Now().Format(time.RFC3339Nano)} {
		r[k] = v
	}
	l.entries = append(l.entries, store.StgEntry{Seq: int64(len(l.entries) + 1), At: l.clock.Now(), Event: store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", TaskID: m.Root, Body: map[string]any{"landing_main": r}}})
	_ = l.state(t)
}

type controlRuns struct {
	ledger                                                *memoryLedger
	obs                                                   delivery.STGRunObservation
	workloads                                             map[string]any
	health                                                delivery.STGHealth
	observeErr, workErr, pinErr, cleanErr                 error
	observations, cleans, creates, workReads, healthReads int
}

func (r *controlRuns) ReadNamedDeliverRun(context.Context, delivery.NamedDeliverInput) (*unstructured.Unstructured, error) {
	return r.obs.Run, r.observeErr
}
func (r *controlRuns) CreatePermittedNamedDeliverRun(context.Context, delivery.NamedDeliverInput, func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error) {
	r.creates++
	return cluster.ActuationResponse{}, nil, errors.New("unexpected Create")
}
func (r *controlRuns) ObserveRun(context.Context, delivery.STGRunIdentity) (delivery.STGRunObservation, error) {
	r.observations++
	return r.obs, r.observeErr
}
func (r *controlRuns) RemoveArchivedFinalizer(_ context.Context, _ delivery.NamedDeliverInput, uid types.UID) error {
	r.cleans++
	s, err := r.ledger.Read(context.Background())
	if err != nil {
		return err
	}
	found := false
	for _, b := range s.Batches {
		for _, tr := range b.Tries {
			if tr.RunEvidence.Record.String("uid") == string(uid) {
				found = true
			}
		}
	}
	if !found {
		return errors.New("finalizer removed before durable archive")
	}
	return r.cleanErr
}
func (r *controlRuns) ObserveWorkloads(context.Context) (map[string]any, error) {
	r.workReads++
	return r.workloads, r.workErr
}
func (r *controlRuns) ReadPin(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"ok": true}, r.pinErr
}
func (r *controlRuns) ReadHealth(context.Context) delivery.STGHealth {
	r.healthReads++
	return r.health
}
func workloads(api, console string) map[string]any {
	out := map[string]any{}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		d := api
		if name == "platform-console" {
			d = console
		}
		out[name] = map[string]any{"uid": name + "-uid", "generation": int64(1), "digest": d}
	}
	return out
}
func controlFixture(t *testing.T, status string) (*Controller, *memoryLedger, *controlRuns, *leaseWindows) {
	t.Helper()
	cfg, windows, policy := leaseFixture()
	p := *policy.Load()
	rule := *p.Rule
	landing := *rule.Landing
	landing.Tenant = "dev"
	rule.Landing = &landing
	rule.ID = "landing:dev"
	rule.Expires = cfg.Clock.Now().Add(7 * 24 * time.Hour)
	p.Rule = &rule
	policy.Store(&p)
	clock := cfg.Clock.(*clocktesting.FakeClock)
	l := &memoryLedger{clock: clock}
	now := clock.Now().Format(time.RFC3339Nano)
	base := map[string]any{"P": sha("e"), "api_digest": dig("a"), "console_digest": dig("c"), "evidence": "cluster/cicd/base-uid"}
	l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "evidence", "evidence": "cluster/cicd/base-uid", "source": "external", "run": "base-run", "P": sha("e"), "U": sha("b"), "infra": sha("c"), "content": map[string]any{"api_digest": dig("a"), "console_digest": dig("c")}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads(dig("a"), dig("c")), "at": now}, "health": map[string]any{"status": "ok"}})
	m := memberValue(controlMember)
	m["gen"] = 1
	l.add(t, batchRecord("opened", leaseBatch, 0, "members", []any{m}, "first_pending_at", map[string]any{controlMember.R: now}), batchRecord("p_selected", leaseBatch, 0, "P", sha("a"), "at", now))
	ci := map[string]any{"name": "ci-1", "uid": "ci-uid", "created_at": now, "read_at": now, "platform_commit": sha("a"), "ui_commit": sha("b"), "infra_commit": sha("c"), "pipeline_ref": "bifrost-ci-platform", "repo_url": "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"}
	in := map[string]any{"namespace": "cicd", "pipeline": delivery.STGReleasePipeline, "revision": sha("a"), "uiRevision": sha("b"), "infraRevision": sha("c"), "params_canonical": map[string]any{"revision": sha("a"), "uiRevision": sha("b"), "infraRevision": sha("c")}}
	l.add(t, batchRecord("ci_accepted", leaseBatch, 0, "P", sha("a"), "ci", ci), batchRecord("snapshot", leaseBatch, 1, "inputs", in, "base", base, "set", []any{memberValue(controlMember)}, "not_before", now))
	n := int64(len(l.entries) + 1)
	input := delivery.NamedDeliverInput{Batch: 1, Try: 1, IntentSeq: n, P: sha("a"), U: sha("b"), Infra: sha("c")}
	identity, err := input.Identity()
	if err != nil {
		t.Fatal(err)
	}
	convert := func(m map[string]string) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	l.add(t, batchRecord("started", leaseBatch, 1, "intent_seq", n, "run", identity.Name, "labels", convert(identity.Labels), "annotations", convert(identity.Annotations), "fingerprint", identity.Fingerprint, "spend", []any{map[string]any{"R": sha("a"), "gen": 1, "n": 1}}, "lease", map[string]any{"who": identity.Who, "gen": 1}, "deadline", clock.Now().Add(2*time.Minute).Format(time.RFC3339Nano)), batchRecord("bound", leaseBatch, 1, "uid", "run-uid"))
	obs := controlObservation(t, input, status)
	r := &controlRuns{ledger: l, obs: obs, workloads: workloads(dig("d"), dig("f")), health: delivery.STGHealth{Outcome: "passed", Evidence: map[string]any{"service": "bifrost-platform-api", "status": "ok"}}}
	c := &Controller{Ledger: l, Runs: r, Windows: windows, Policy: cfg.Policy, Clock: clock, Cluster: "cluster"}
	t.Cleanup(func() { _ = c.Close() })
	return c, l, r, windows
}
func controlObservation(t *testing.T, in delivery.NamedDeliverInput, status string) delivery.STGRunObservation {
	t.Helper()
	return controlObservationUID(t, in, status, "run-uid")
}
func controlObservationUID(t *testing.T, in delivery.NamedDeliverInput, status, uid string) delivery.STGRunObservation {
	t.Helper()
	data, err := os.ReadFile("../../delivery/testdata/stg_delivery_pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data, err = yaml.YAMLToJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := unstructured.UnstructuredJSONScheme.Decode(data, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pipe := decoded.(*unstructured.Unstructured)
	run, err := delivery.PrepareNamedDeliverRun(in, pipe)
	if err != nil {
		t.Fatal(err)
	}
	run.SetUID(types.UID(uid))
	run.SetResourceVersion("1")
	state := map[string]string{"Succeeded": "True", "Failed": "False", "Running": "Unknown"}[status]
	cond := []any{map[string]any{"type": "Succeeded", "status": state}}
	defs, _, _ := unstructured.NestedSlice(pipe.Object, "spec", "tasks")
	run.Object["status"] = map[string]any{"conditions": cond, "results": []any{}, "pipelineSpec": map[string]any{"tasks": defs}}
	refs := []any{}
	objects := []runtime.Object{run}
	ctl := true
	for _, v := range defs {
		def := v.(map[string]any)
		name := def["name"].(string)
		ref, ok := def["taskRef"].(map[string]any)
		if !ok {
			continue
		}
		child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "spec": map[string]any{"taskRef": ref}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}, "results": []any{}}}}
		child.SetName("task-" + name)
		child.SetNamespace("cicd")
		child.SetUID(types.UID("uid-" + name))
		child.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "tekton.dev/v1", Kind: "PipelineRun", Name: run.GetName(), UID: run.GetUID(), Controller: &ctl}})
		params, results := []any{}, []any{}
		commits := map[string]string{"clone-platform": in.P, "clone-ui": in.U, "clone-infra": in.Infra}
		repos := map[string]string{"clone-platform": "bifrost-platform", "clone-ui": "bifrost-ui", "clone-infra": "bifrost-trade-infra"}
		if commit := commits[name]; commit != "" {
			params = append(params, map[string]any{"name": "url", "value": "http://gitea.cicd.svc.cluster.local:3000/bifrost/" + repos[name] + ".git"})
			results = append(results, map[string]any{"name": "commit", "value": commit})
		}
		if name == "build-platform-api" || name == "build-platform-console" {
			d := dig("d")
			if name == "build-platform-console" {
				d = dig("f")
			}
			results = append(results, map[string]any{"name": "digest", "value": d})
		}
		if name == "gitops-sync" {
			for key, value := range map[string]string{"application": "bifrost-platform-stg", "apiDigest": dig("d"), "consoleDigest": dig("f"), "deliverRun": run.GetName()} {
				params = append(params, map[string]any{"name": key, "value": value})
			}
		}
		child.Object["spec"].(map[string]any)["params"] = params
		child.Object["status"].(map[string]any)["results"] = results
		refs = append(refs, map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "name": child.GetName(), "pipelineTaskName": name})
		objects = append(objects, child)
	}
	run.Object["status"].(map[string]any)["childReferences"] = refs
	client := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	adapter := &delivery.STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return client, nil }}
	obs, err := adapter.ObserveRun(context.Background(), delivery.STGRunIdentity{Namespace: "cicd", Name: run.GetName(), UID: run.GetUID()})
	if err != nil {
		t.Fatal(err)
	}
	return obs
}
func pass(t *testing.T, c *Controller) {
	t.Helper()
	if err := c.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func phase(t *testing.T, l *memoryLedger) string {
	t.Helper()
	return l.state(t).Batches[leaseBatch].Phase
}

func TestControlNoneDoesNoIO(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"enabled", "none"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, w := controlFixture(t, "Succeeded")
			if mode == "none" {
				p := c.Policy()
				rule := *p.Rule
				landing := *rule.Landing
				landing.Repos = append(landing.Repos[:0:0], landing.Repos...)
				landing.Repos[0].STGRelease = "none"
				rule.Landing = &landing
				p.Rule = &rule
				c.Policy = func() WindowPolicy { return p }
			}
			l.reads, l.commits = 0, 0
			for i := 0; i < 3; i++ {
				pass(t, c)
			}
			_, calls := w.snapshot()
			if mode == "none" {
				if l.reads != 0 || l.commits != 0 || r.observations != 0 || r.cleans != 0 || calls != [4]int{} {
					t.Fatalf("none performed I/O: ledger=%d/%d run=%d/%d window=%v", l.reads, l.commits, r.observations, r.cleans, calls)
				}
			} else if l.commits == 0 || r.observations != 1 {
				t.Fatal("enabled positive control did not progress")
			}
		})
	}
}
func TestControlTerminalArchiveBeforeFinalizerAndRestart(t *testing.T) {
	t.Parallel()
	c, l, r, w := controlFixture(t, "Succeeded")
	// Reuse the same live holder through terminal observation.
	h, err := HoldWindow(context.Background(), WindowConfig{Windows: w, Policy: c.Policy, Clock: c.Clock}, func() store.StgState {
		s := l.state(t)
		b := s.Batches[leaseBatch]
		b.Phase = "SNAP"
		b.Tries[1].Started = store.StgEvent{}
		return s
	}(), leaseBatch, 1)
	if err != nil {
		t.Fatal(err)
	}
	c.holder = h
	pass(t, c)
	if phase(t, l) != "TERMINAL" || r.cleans != 0 {
		t.Fatal("terminal was not archived before cleanup")
	}
	_, calls := w.snapshot()
	if calls[3] != 1 {
		t.Fatal("window not released before verification", calls)
	}
	r.cleanErr = errors.New("patch failed")
	// Durable raw is sufficient after restart, even with the original run gone.
	r.observeErr = errors.New("run deleted")
	restarted := &Controller{Ledger: l, Runs: r, Policy: c.Policy, Clock: c.Clock, Cluster: "cluster"}
	if err := restarted.Pass(context.Background()); err == nil {
		t.Fatal("cleanup error not reported")
	}
	if phase(t, l) != "VERIFIED" || r.observations != 1 {
		t.Fatal("cleanup failure prevented archive verification")
	}
	r.cleanErr = nil
	l.effective(t, controlMember, sha("a"))
	pass(t, restarted)
	s := l.state(t)
	if s.Batches[leaseBatch].Phase != "CLOSED" || s.Members[controlMember].To != "delivered" || r.cleans != 2 || r.creates != 0 {
		t.Fatal("restart did not finish exactly once", s.Members, r.cleans)
	}
	pass(t, restarted)
	if r.cleans != 2 {
		t.Fatal("successful cleanup repeated in the same process")
	}
}
func TestControlRunningAndUnknownDoNotReleaseExecution(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"running", "unreadable", "terminal"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Running")
			if mode == "unreadable" {
				r.observeErr = errors.New("forbidden")
			}
			if mode == "terminal" {
				in := input(leaseBatch, 1, l.state(t).Batches[leaseBatch].Tries[1].Started.Seq, object(l.state(t).Batches[leaseBatch].Tries[1].Snapshot.Record, "inputs"))
				r.obs = controlObservation(t, in, "Succeeded")
			}
			before := len(l.entries)
			err := c.Pass(context.Background())
			if mode == "unreadable" && err == nil {
				t.Fatal("read error lost")
			}
			if mode != "unreadable" && err != nil {
				t.Fatal(err)
			}
			tr := l.state(t).Batches[leaseBatch].Tries[1]
			if mode == "terminal" {
				if tr.Occupied() || tr.RunEvidence.Seq == 0 {
					t.Fatal("positive terminal control stuck")
				}
			} else if !tr.Occupied() || tr.Verify.Seq != 0 || len(l.entries) != before || r.creates != 0 {
				t.Fatal("unknown/running ended execution")
			}
		})
	}
}

func TestControlHealthGraceSurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"passed", "failed", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			pass(t, c)
			terminal := l.state(t).Batches[leaseBatch].Tries[1].RunEvidence.Record.Time("at")
			r.health.Outcome = outcome
			l.clock.Step(14 * time.Minute)
			pass(t, c)
			if outcome == "passed" {
				if phase(t, l) != "VERIFIED" {
					t.Fatal("healthy control not verified")
				}
				return
			}
			if l.state(t).Batches[leaseBatch].Tries[1].Verify.Seq != 0 {
				t.Fatal("early verification failure")
			}
			restarted := &Controller{Ledger: l, Runs: r, Policy: c.Policy, Clock: c.Clock, Cluster: "cluster"}
			l.clock.Step(time.Minute)
			pass(t, restarted)
			tr := l.state(t).Batches[leaseBatch].Tries[1]
			if !tr.RunEvidence.Record.Time("at").Equal(terminal) {
				t.Fatal("restart reset grace clock")
			}
			if outcome == "failed" {
				if tr.Verify.Record.String("why") != "health" {
					t.Fatal("15-minute definite failure did not end grace")
				}
			} else if tr.Verify.Seq != 0 || len(l.state(t).Batches[leaseBatch].Diagnostics) != 1 {
				t.Fatal("unknown became failure or repeated diagnostics")
			}
		})
	}
}

func TestControlFailureEffectsAndBackoff(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"unchanged", "this_try", "all_this_try", "other", "unknown"} {
		t.Run(class, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			pass(t, c)
			pass(t, c)
			r.workloads = workloads(dig("a"), dig("c"))
			switch class {
			case "this_try":
				r.workloads["platform-api"].(map[string]any)["digest"] = dig("d")
			case "all_this_try":
				r.workloads = workloads(dig("d"), dig("f"))
			case "other":
				r.workloads["platform-workers"].(map[string]any)["digest"] = dig("9")
			case "unknown":
				r.workErr = errors.New("timeout")
			}
			pass(t, c)
			pass(t, c)
			s := l.state(t)
			tr := s.Batches[leaseBatch].Tries[1]
			want := class
			if class == "all_this_try" {
				want = "this_try"
			}
			if tr.Effect.Record.String("class") != want {
				t.Fatal("wrong effect", tr.Effect.Record)
			}
			if class == "other" {
				if s.Batches[leaseBatch].Phase != "CLOSED" || len(s.Blocks) != 1 {
					t.Fatal("third party update was retried")
				}
				return
			}
			if class == "unknown" {
				if tr.Retry.Seq != 0 || tr.Occupied() {
					t.Fatal("unknown effect retried")
				}
				r.workErr = nil
				pass(t, c)
				pass(t, c)
			}
			tr = l.state(t).Batches[leaseBatch].Tries[1]
			due := tr.RunEvidence.Record.Time("at").Add(10 * time.Minute)
			if !tr.Retry.Record.Time("not_before").Equal(due) {
				t.Fatal("backoff not based on terminal time", tr.Retry.Record)
			}
			pass(t, c)
			if l.state(t).Batches[leaseBatch].Try != 1 {
				t.Fatal("snapshot before backoff")
			}
			l.clock.Step(10 * time.Minute)
			pass(t, c)
			next := l.state(t).Batches[leaseBatch].Tries[2]
			if next == nil || fmt.Sprint(next.Snapshot.Record["inputs"]) != fmt.Sprint(tr.Snapshot.Record["inputs"]) || object(next.Snapshot.Record, "base").String("P") != sha("e") {
				t.Fatal("retry changed frozen inputs or baseline")
			}
		})
	}
}

func TestControlWrongInputBlocksAcrossHundredScansAndGreenCIRepairs(t *testing.T) {
	t.Parallel()
	c, l, r, _ := controlFixture(t, "Succeeded")
	for _, value := range r.obs.Raw["child_refs"].([]any) {
		ref := value.(map[string]any)
		if ref["pipelineTaskName"] == "clone-ui" {
			ref["task_run"].(map[string]any)["status"].(map[string]any)["results"] = []any{map[string]any{"name": "commit", "value": sha("9")}}
		}
	}
	pass(t, c)
	pass(t, c)
	pass(t, c)
	s := l.state(t)
	if len(s.Blocks) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
		t.Fatal("wrong_input did not close behind a blocker")
	}
	var first store.StgBlock
	for _, b := range s.Blocks {
		first = b
	}
	seq := s.LastSeq
	// Re-reading the same successful CI cannot repair an executed wrong input.
	oldCI := object(s.Batches[leaseBatch].CI["ci-uid"].Record, "ci")
	c.Sources.CI = func(context.Context, string) (store.StgCIObservation, error) {
		return store.StgCIObservation{Status: "Succeeded", UID: "ci-uid", Accepted: map[string]any(oldCI), ReadAt: oldCI.Time("read_at")}, nil
	}
	l.clock.Step(time.Minute)
	for i := 0; i < 100; i++ {
		pass(t, c)
	}
	s = l.state(t)
	if s.LastSeq != seq || s.LastBatch != 1 || len(s.BlockHistory) != 1 || !s.Blocks[first.Opened.Record.String("block")].Opened.At.Equal(first.Opened.At) || r.creates != 0 {
		t.Fatal("block/trigger clock changed or wrong input retried")
	}
	// A new exact successful CI is the positive control through the same entry.
	ci := map[string]any{}
	for k, v := range oldCI {
		ci[k] = v
	}
	ci["uid"], ci["name"], ci["created_at"], ci["read_at"] = "new-ci", "new-ci", l.clock.Now().Format(time.RFC3339Nano), l.clock.Now().Format(time.RFC3339Nano)
	c.Sources.CI = func(context.Context, string) (store.StgCIObservation, error) {
		return store.StgCIObservation{Status: "Succeeded", UID: "new-ci", CreatedAt: l.clock.Now(), ReadAt: l.clock.Now(), Accepted: ci}, nil
	}
	pass(t, c)
	s = l.state(t)
	if s.Blocks[first.Opened.Record.String("block")].IsOpen() || s.LastSeq != seq+2 || s.Budget[store.StgBudgetKey{R: sha("a"), Gen: 1}] != 1 {
		t.Fatal("new successful CI did not repair without refund", s.Blocks)
	}
}

func TestControlCIDeclinedThenGreenRetainsTrigger(t *testing.T) {
	t.Parallel()
	for _, cause := range []string{"ci_red", "ci_results", "ci_missing"} {
		t.Run(cause, func(t *testing.T) {
			t.Parallel()
			c, l, _, _ := controlFixture(t, "Succeeded")
			// Keep the prepared OPEN prefix, before CI/snapshot/started.
			l.entries = l.entries[:3]
			s := l.state(t)
			b := s.Batches[leaseBatch]
			base, _ := store.StgTrustedBaseline(s)
			rejected := store.StgCIObservation{UID: "failed-ci", CreatedAt: l.clock.Now(), ReadAt: l.clock.Now()}
			if err := c.blockAndFinish(context.Background(), s, leaseBatch, b, cause, base, &rejected); err != nil {
				t.Fatal(err)
			}
			first := l.state(t)
			var block store.StgBlock
			for _, v := range first.Blocks {
				block = v
			}
			// B5-g has not bound a question. A 429 or restart cannot consume the
			// persistent trigger: B5-e never acknowledges, removes, or regenerates it.
			for i := 0; i < 100; i++ {
				pass(t, c)
			}
			if l.state(t).LastSeq != first.LastSeq || !l.state(t).Blocks[block.Opened.Record.String("block")].IsOpen() {
				t.Fatal("unbound trigger was lost")
			}
			l.clock.Step(time.Minute)
			at := l.clock.Now()
			ci := map[string]any{"uid": "green-ci", "name": "green-ci", "created_at": at.Format(time.RFC3339Nano), "read_at": at.Format(time.RFC3339Nano), "platform_commit": sha("a"), "ui_commit": sha("b"), "infra_commit": sha("c"), "pipeline_ref": "bifrost-ci-platform", "repo_url": "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"}
			c.Sources.CI = func(context.Context, string) (store.StgCIObservation, error) {
				return store.StgCIObservation{Status: "Succeeded", UID: "green-ci", ReadAt: at, CreatedAt: at, Accepted: ci}, nil
			}
			pass(t, c)
			if l.state(t).Blocks[block.Opened.Record.String("block")].IsOpen() {
				t.Fatal("green CI did not automatically unblock")
			}
		})
	}
}

func TestControlStaleCommitAndFailedArchivePreserveFinalizer(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "write_failed", "stale"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			s := l.state(t)
			l.fail = mode == "write_failed"
			if mode == "stale" {
				l.effective(t, controlMember, sha("a"))
			}
			err := c.observe(context.Background(), s, leaseBatch, s.Batches[leaseBatch], s.Batches[leaseBatch].Tries[1])
			if mode == "write_failed" && err == nil {
				t.Fatal("failed persistence accepted")
			}
			if mode != "write_failed" && err != nil {
				t.Fatal(err)
			}
			tr := l.state(t).Batches[leaseBatch].Tries[1]
			if mode == "success" {
				if tr.RunEvidence.Seq == 0 {
					t.Fatal("positive archive missing")
				}
			} else if tr.RunEvidence.Seq != 0 || r.cleans != 0 || !tr.Occupied() {
				t.Fatal("failed/stale archive released finalizer or execution")
			}
		})
	}
}
