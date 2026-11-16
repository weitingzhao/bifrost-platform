package gatecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const ns = "cicd"

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("b", 40)
)

// memLedger is the event log in memory: one list of events per stream, each
// body checked against the declared vocabulary as Append would.
type memLedger struct {
	mu      sync.Mutex
	streams map[string][]memEvent
	errs    []error
}

type memEvent struct {
	typ  string
	body map[string]any
}

func (l *memLedger) Append(_ context.Context, stream, typ string, decide Decide) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.streams == nil {
		l.streams = map[string][]memEvent{}
	}
	var lastType string
	var lastBody map[string]any
	if evs := l.streams[stream]; len(evs) > 0 {
		lastType = evs[len(evs)-1].typ
		// Through JSON, as the database gives the body back.
		raw, _ := json.Marshal(evs[len(evs)-1].body)
		_ = json.Unmarshal(raw, &lastBody)
	}
	body, ok := decide(lastType, lastBody)
	if !ok {
		return false, nil
	}
	if err := eventlog.CheckBody(typ, body); err != nil {
		l.errs = append(l.errs, err)
		return false, err
	}
	l.streams[stream] = append(l.streams[stream], memEvent{typ, body})
	return true, nil
}

func (l *memLedger) events(stream string) []memEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]memEvent(nil), l.streams[stream]...)
}

func (l *memLedger) count(typ string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, evs := range l.streams {
		for _, e := range evs {
			if e.typ == typ {
				n++
			}
		}
	}
	return n
}

func run(name, pipeline, commit, status, reason string) *unstructured.Unstructured {
	r := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": name, "namespace": ns,
			"labels":        map[string]any{"tekton.dev/pipeline": pipeline},
			"managedFields": []any{map[string]any{"manager": "tekton", "fieldsV1": map[string]any{"f:status": map[string]any{}}}}},
		"spec": map[string]any{"params": []any{
			map[string]any{"name": "revision", "value": commit},
			map[string]any{"name": "uiRevision", "value": "main"}}},
		"status": map[string]any{
			"pipelineSpec":    map[string]any{"tasks": []any{map[string]any{"name": "build", "taskSpec": strings.Repeat("x", 4096)}}},
			"childReferences": []any{map[string]any{"name": name + "-build", "kind": "TaskRun"}},
		},
	}}
	if status != "" {
		_ = unstructured.SetNestedSlice(r.Object, []any{map[string]any{"type": "Succeeded", "status": status, "reason": reason,
			"message": strings.Repeat("m", 2048)}}, "status", "conditions")
	}
	return r
}

func windowCM(rec map[string]any) *unstructured.Unstructured {
	raw, _ := json.Marshal(rec)
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": WindowConfigMap, "namespace": ns},
		"data":     map[string]any{windowDataKey: string(raw)},
	}}
}

func fakeClient(objs ...runtime.Object) *dynfake.FakeDynamicClient {
	return dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		PipelineRunGVR: "PipelineRunList", ConfigMapGVR: "ConfigMapList"}, objs...)
}

type clock struct{ t atomic.Int64 }

func (c *clock) now() time.Time      { return time.Unix(c.t.Load(), 0).UTC() }
func (c *clock) set(t time.Time)     { c.t.Store(t.Unix()) }
func (c *clock) add(d time.Duration) { c.t.Add(int64(d / time.Second)) }

// start runs a gate over the client until the test ends, and waits until it watches.
func start(t *testing.T, client dynamic.Interface, c *clock) (*Gate, *memLedger) {
	t.Helper()
	l := &memLedger{}
	g := &Gate{Ledger: l, Namespace: ns, Dynamic: func() (dynamic.Interface, error) { return client, nil },
		Now: c.now, WindowEvery: 20 * time.Millisecond, Backoff: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-gate-cluster", func() { defer close(done); g.Run(ctx) })
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-g.Watching():
	case <-time.After(5 * time.Second):
		t.Fatal("the gate did not start watching")
	}
	return g, l
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not reached: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// settle gives the gate time to handle what it was sent (for "nothing more
// happened" checks; each is paired with a positive one before it).
func settle() { time.Sleep(150 * time.Millisecond) }

// 2026-11-16: bus step 2a (RULES.md §7, "no replay of history at startup"):
// runs the cluster already keeps when the gate starts, finished or not, say
// nothing; a run created after it watches says ci.run.started once.
func TestStartedOnlyForRunsCreatedAfterTheGateStarts(t *testing.T) {
	c := &clock{}
	c.set(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	client := fakeClient(run("old-done", "bifrost-ci-platform", shaA, "True", "Succeeded"), run("old-running", "bifrost-ci-platform", shaA, "Unknown", "Running"))
	_, l := start(t, client, c)
	if _, err := client.Resource(PipelineRunGVR).Namespace(ns).Create(context.Background(), run("ci-platform-new", "bifrost-ci-platform", shaB, "", ""), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "started for the new run", func() bool { return len(l.events(RunStream("ci-platform-new"))) == 1 })
	settle()
	if n := l.count(TypeRunStarted); n != 1 {
		t.Fatalf("%d ci.run.started, want 1 (the new run only)", n)
	}
	if n := l.count(TypeRunFinished); n != 0 {
		t.Fatalf("%d ci.run.finished for runs that finished before the gate started", n)
	}
	e := l.events(RunStream("ci-platform-new"))[0]
	if e.body["subject"] != "ci-platform-new@"+shaB || e.body["ref"] != "/api/v1/delivery/runs/ci-platform-new" || e.body["change"] != ChangeCreated || e.body["pipeline"] != "bifrost-ci-platform" {
		t.Errorf("body %v", e.body)
	}
	if len(l.errs) > 0 {
		t.Fatal(l.errs)
	}
}

// 2026-11-16: bus step 2a (register: ci.run.finished is durable, said once):
// a run whose Succeeded condition turns True or False says ci.run.finished
// once, whatever else rewrites the run after; a run running at startup that
// finishes later is said too (without a started: it began before the gate).
func TestFinishedOncePerRun(t *testing.T) {
	c := &clock{}
	c.set(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	client := fakeClient(run("old-running", "bifrost-deliver-platform", shaA, "Unknown", "Running"))
	_, l := start(t, client, c)
	res := client.Resource(PipelineRunGVR).Namespace(ns)
	bg := context.Background()
	if _, err := res.Create(bg, run("new-run", "bifrost-ci-platform", shaB, "Unknown", "Running"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "started", func() bool { return l.count(TypeRunStarted) == 1 })
	for i := 0; i < 3; i++ { // finished, then rewritten twice more (labels, results)
		for name, status := range map[string][2]string{"new-run": {"False", "Failed"}, "old-running": {"True", "Succeeded"}} {
			r := run(name, "bifrost-ci-platform", shaB, status[0], status[1])
			r.SetAnnotations(map[string]string{"rewrite": fmt.Sprint(i)})
			if _, err := res.Update(bg, r, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	eventually(t, "two finished", func() bool { return l.count(TypeRunFinished) == 2 })
	settle()
	if n := l.count(TypeRunFinished); n != 2 {
		t.Fatalf("%d ci.run.finished, want one per run", n)
	}
	evs := l.events(RunStream("new-run"))
	if len(evs) != 2 || evs[1].body["change"] != ChangeFailed || evs[1].body["status"] != "False" || evs[1].body["reason"] != "Failed" {
		t.Fatalf("new-run %v", evs)
	}
	if evs := l.events(RunStream("old-running")); len(evs) != 1 || evs[0].body["change"] != ChangeSucceeded {
		t.Fatalf("old-running %v", evs)
	}
	// The ledger's check holds across a restart: the stream's last event is
	// the finish, so a second gate saying it again writes nothing.
	g2 := &Gate{Ledger: l}
	g2.finished(bg, "new-run", runFacts{status: "False", reason: "Failed", commit: shaB})
	g2.started(bg, "new-run", runFacts{commit: shaB})
	if n := len(l.events(RunStream("new-run"))); n != 2 {
		t.Fatalf("a restarted gate wrote again: %d events", n)
	}
	if len(l.errs) > 0 {
		t.Fatal(l.errs)
	}
}

// 2026-11-16: bus step 2a (RULES.md §4.2, delivery's window events have no
// renewal): the window says held, released, queued and expired once per
// change of what a reader waits on; a renewal (only expires_at moves) and a
// rewrite of the same record say nothing.
func TestWindowChangeOncePerDataChange(t *testing.T) {
	c := &clock{}
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	c.set(t0)
	ts := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }
	held := map[string]any{"who": "seat-8.2", "what": "bifrost-platform", "started_at": ts(0), "expires_at": ts(5 * time.Minute)}
	client := fakeClient(windowCM(map[string]any{"who": "old", "what": "x", "started_at": ts(-time.Hour), "expires_at": ts(-50 * time.Minute), "ended_at": ts(-50 * time.Minute)}))
	_, l := start(t, client, c)
	settle()
	if n := len(l.events(WindowStream)); n != 0 {
		t.Fatalf("a window that ended before the gate looked said %d events", n)
	}
	cms := client.Resource(ConfigMapGVR).Namespace(ns)
	bg := context.Background()
	update := func(rec map[string]any) {
		t.Helper()
		if _, err := cms.Update(bg, windowCM(rec), metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	changes := func() []string {
		var out []string
		for _, e := range l.events(WindowStream) {
			out = append(out, e.body["change"].(string))
		}
		return out
	}
	update(held)
	eventually(t, "held", func() bool { return strings.Join(changes(), ",") == "held" })
	// A renewal and the same record again: nothing.
	c.add(time.Minute)
	renewed := map[string]any{"who": "seat-8.2", "what": "bifrost-platform", "started_at": ts(0), "expires_at": ts(6 * time.Minute), "renewals": 1}
	update(renewed)
	update(renewed)
	// Someone queues behind the holder.
	queued := map[string]any{"who": "seat-8.2", "what": "bifrost-platform", "started_at": ts(0), "expires_at": ts(6 * time.Minute), "renewals": 1,
		"queue": []any{map[string]any{"who": "seat-3.1", "what": "bifrost-ui", "since": ts(time.Minute), "until": ts(30 * time.Minute)}}}
	update(queued)
	eventually(t, "queued", func() bool { return strings.Join(changes(), ",") == "held,queued" })
	// Released: delivery ends it now.
	c.add(time.Minute)
	released := map[string]any{"who": "seat-8.2", "what": "bifrost-platform", "started_at": ts(0), "expires_at": ts(2 * time.Minute), "ended_at": ts(2 * time.Minute),
		"queue": queued["queue"]}
	update(released)
	eventually(t, "released", func() bool { return strings.Join(changes(), ",") == "held,queued,released" })
	update(released)
	// A new holder whose TTL passes without a write: expired, said by the tick.
	c.add(time.Minute)
	update(map[string]any{"who": "seat-3.1", "what": "bifrost-ui", "started_at": ts(3 * time.Minute), "expires_at": ts(8 * time.Minute)})
	eventually(t, "held again", func() bool { return strings.Join(changes(), ",") == "held,queued,released,held" })
	c.add(10 * time.Minute)
	eventually(t, "expired", func() bool { return strings.Join(changes(), ",") == "held,queued,released,held,expired" })
	settle()
	if got := strings.Join(changes(), ","); got != "held,queued,released,held,expired" {
		t.Fatalf("changes %s", got)
	}
	last := l.events(WindowStream)[4].body
	if last["who"] != "seat-3.1" || last["what"] != "bifrost-ui" || last["subject"] != "window" || last["key"] != WindowKey || last["ref"] != WindowRef {
		t.Errorf("body %v", last)
	}
	if len(l.errs) > 0 {
		t.Fatal(l.errs)
	}
}

// 2026-11-16: bus step 2a (workers OOMKilled at 512Mi listing 1182 runs,
// 2026-11-15): what the gate keeps of a run leaves status.pipelineSpec,
// status.childReferences, managedFields and the condition's message behind,
// and keeps what it reads.
func TestSlimDropsTheHeavyFields(t *testing.T) {
	full := run("ci-platform-x", "bifrost-ci-platform", shaA, "True", "Succeeded")
	full.SetLabels(map[string]string{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/revision": shaB, "other": "y"})
	full.SetAnnotations(map[string]string{"big": strings.Repeat("z", 4096)})
	s := Slim(full)
	for _, path := range [][]string{{"status", "pipelineSpec"}, {"status", "childReferences"}, {"metadata", "managedFields"}, {"metadata", "annotations"}} {
		if _, ok, _ := unstructured.NestedFieldNoCopy(s.Object, path...); ok {
			t.Errorf("%s kept", strings.Join(path, "."))
		}
	}
	raw, _ := json.Marshal(s.Object)
	fullRaw, _ := json.Marshal(full.Object)
	if len(raw) > 600 || strings.Contains(string(raw), "mmmm") || strings.Contains(string(raw), "uiRevision") || strings.Contains(string(raw), `"other"`) {
		t.Errorf("slim run is %d bytes (full %d): %s", len(raw), len(fullRaw), raw)
	}
	// The positive control: what is read survives.
	f := factsOf(full)
	if s.GetName() != "ci-platform-x" || f.pipeline != "bifrost-ci-platform" || f.commit != shaB || f.status != "True" || f.reason != "Succeeded" {
		t.Errorf("facts %+v of %s", f, raw)
	}
	// Without the label the revision param names the commit.
	if f := factsOf(run("r", "p", shaA, "", "")); f.commit != shaA || f.finished() {
		t.Errorf("facts %+v", f)
	}
}

// 2026-11-16: bus step 2a: the bodies the gate writes pass the declared
// vocabulary, and a body whose subject is not made of its run is refused.
func TestTheBodiesAreChecked(t *testing.T) {
	f := runFacts{pipeline: "bifrost-ci-platform", commit: shaA, status: "False", reason: "PipelineRunCancelled"}
	if err := eventlog.CheckBody(TypeRunStarted, runBody("ci-x", f, ChangeCreated)); err != nil {
		t.Fatal(err)
	}
	if err := eventlog.CheckBody(TypeRunStarted, runBody("ci-x", runFacts{}, ChangeCreated)); err != nil {
		t.Fatalf("a run naming no commit: %v", err)
	}
	if finishedChange(f) != ChangeCancelled || finishedChange(runFacts{status: "False", reason: "PipelineRunTimeout"}) != ChangeTimedOut {
		t.Error("finished changes")
	}
	bad := runBody("ci-x", f, ChangeCreated)
	bad["subject"] = "ci-y@" + shaA
	if err := eventlog.CheckBody(TypeRunStarted, bad); err == nil {
		t.Error("a subject not made of the run passes")
	}
	b, ok := windowChange(windowRecord{Who: "a", What: "bifrost-platform", StartedAt: "2026-11-16T12:00:00Z", ExpiresAt: "2026-11-16T12:05:00Z"},
		time.Date(2026, 11, 16, 12, 1, 0, 0, time.UTC), nil)
	if !ok || b["change"] != ChangeHeld {
		t.Fatalf("window %v %v", b, ok)
	}
	if err := eventlog.CheckBody(TypeWindowChanged, b); err != nil {
		t.Fatal(err)
	}
}
