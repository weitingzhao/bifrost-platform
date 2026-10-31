package stgrelease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

var (
	sampleNow   = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	samplePRGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	sampleP     = strings.Repeat("a", 40)
	sampleU     = strings.Repeat("b", 40)
	sampleInfra = strings.Repeat("c", 40)
)

// sampleUIDOf is the UUID-shaped uid the fake apiserver gives the nth Create.
func sampleUIDOf(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

// sampleLedger is the sample stream in memory. Every write goes through
// store.StgSampleFold, the same transition check Append runs in PostgreSQL;
// the rule and B5 reads are switches. The PG suite covers the real ones.
type sampleLedger struct {
	mu        sync.Mutex
	entries   []store.StgSampleEntry
	nextEvent int64
	ruleOpen  []bool // successive RuleOpen answers; the last one repeats
	ruleReads int
	allocOpen bool // what Allocate and Sending read under the rule lock
	sendOpen  bool // the rule opened after the allocation: only Sending sees it
	lastBatch int64
	at        time.Time
}

func (l *sampleLedger) Read(context.Context) (store.StgSampleState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return store.StgSampleFold(l.entries)
}
func (l *sampleLedger) RuleOpen(context.Context) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ruleOpen) == 0 {
		return false, nil
	}
	i := min(l.ruleReads, len(l.ruleOpen)-1)
	l.ruleReads++
	return l.ruleOpen[i], nil
}
func (l *sampleLedger) append(record map[string]any) error {
	l.nextEvent += 7 // global event ids are not the stream's sequence
	at := l.at
	if at.IsZero() {
		at = sampleNow
	}
	e := store.StgSampleEntry{EventID: 5000 + l.nextEvent, Seq: int64(len(l.entries) + 1), At: at, Type: "note", Actor: "controller",
		Body: map[string]any{"text": "test", "stg_sample": canonical(record)}}
	if _, err := store.StgSampleFold(append(slices.Clone(l.entries), e)); err != nil {
		return err
	}
	l.entries = append(l.entries, e)
	return nil
}

// canonical gives the fake the JSON shapes PostgreSQL returns.
func canonical(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func (l *sampleLedger) Allocate(_ context.Context, req store.StgSampleAllocation) (store.StgSample, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := store.StgSampleFold(l.entries)
	if err != nil {
		return store.StgSample{}, err
	}
	// Same order as store.AllocateStgSample: a created sample is returned as
	// recorded; an uncreated or new one passes the gate first.
	x := s.Samples[req.Approval]
	if x != nil && x.Abandoned {
		return store.StgSample{}, &store.StgSampleError{Code: "conflict", Reason: "abandoned"}
	}
	if x != nil && x.UID != "" {
		return *x, nil
	}
	if err := l.gate(false); err != nil {
		return store.StgSample{}, err
	}
	if x != nil {
		return *x, nil
	}
	if a := req.Abandon; a != nil {
		if err := l.append(map[string]any{"v": 1, "kind": "abandoned", "approval": a.Approval, "by": req.Approval, "run": a.Run, "read_at": a.ReadAt.Format(time.RFC3339Nano)}); err != nil {
			return store.StgSample{}, err
		}
	}
	if err := l.append(map[string]any{"v": 1, "kind": "allocated", "approval": req.Approval, "P": req.P, "U": req.U, "infra": req.Infra}); err != nil {
		return store.StgSample{}, err
	}
	s, _ = store.StgSampleFold(l.entries)
	return *s.Samples[req.Approval], nil
}
func (l *sampleLedger) gate(send bool) error {
	if l.allocOpen || send && l.sendOpen {
		return &store.StgSampleError{Code: "rule_open", Reason: "rule open"}
	}
	if l.lastBatch > 0 {
		return &store.StgSampleError{Code: "not_first", Reason: "B5 opened"}
	}
	return nil
}

// Sending mirrors store.PermitStgSampleSend.
func (l *sampleLedger) Sending(_ context.Context, approval string) (store.StgSample, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.gate(true); err != nil {
		return store.StgSample{}, err
	}
	s, _ := store.StgSampleFold(l.entries)
	switch x := s.Samples[approval]; {
	case x == nil:
		return store.StgSample{}, &store.StgSampleError{Code: "missing"}
	case x.Abandoned:
		return store.StgSample{}, &store.StgSampleError{Code: "abandoned", Reason: "abandoned by " + x.AbandonedBy}
	case x.UID != "":
		return store.StgSample{}, &store.StgSampleError{Code: "conflict"}
	}
	if err := l.append(map[string]any{"v": 1, "kind": "sending", "approval": approval}); err != nil {
		return store.StgSample{}, err
	}
	s, _ = store.StgSampleFold(l.entries)
	return *s.Samples[approval], nil
}
func (l *sampleLedger) step(approval string, record map[string]any, done func(*store.StgSample) bool) (store.StgSample, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, _ := store.StgSampleFold(l.entries)
	x := s.Samples[approval]
	if x == nil {
		return store.StgSample{}, &store.StgSampleError{Code: "missing"}
	}
	if !done(x) {
		if err := l.append(record); err != nil {
			return store.StgSample{}, err
		}
	}
	s, _ = store.StgSampleFold(l.entries)
	return *s.Samples[approval], nil
}
func (l *sampleLedger) Created(_ context.Context, approval, uid string) (store.StgSample, error) {
	return l.step(approval, map[string]any{"v": 1, "kind": "created", "approval": approval, "uid": uid}, func(x *store.StgSample) bool { return x.UID == uid })
}
func (l *sampleLedger) Archive(_ context.Context, approval, by, uid, status string, raw map[string]any) (store.StgSample, error) {
	return l.step(approval, map[string]any{"v": 1, "kind": "archived", "approval": approval, "by": by, "uid": uid, "status": status, "raw": raw}, func(x *store.StgSample) bool { return x.Archived && x.UID == uid })
}
func (l *sampleLedger) Release(_ context.Context, approval, by, uid string) (store.StgSample, error) {
	return l.step(approval, map[string]any{"v": 1, "kind": "released", "approval": approval, "by": by, "uid": uid}, func(x *store.StgSample) bool { return x.Released && x.UID == uid })
}
func (l *sampleLedger) HasRunEvidence(ctx context.Context, id delivery.STGRunIdentity) (bool, error) {
	s, err := l.Read(ctx)
	return archivedIn(s, id), err
}

type sampleWorld struct {
	t       *testing.T
	ledger  *sampleLedger
	dyn     *dynfake.FakeDynamicClient
	core    *kfake.Clientset
	sampler *Sampler
	creates int
	patches int
	ci      landingci.ReleaseEvidence
}

func samplePipeline(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	f, err := os.Open("../../delivery/testdata/stg_delivery_pipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := kyaml.NewYAMLOrJSONDecoder(f, 4096)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err == io.EOF {
			t.Fatal("no Pipeline in fixture")
		} else if err != nil {
			t.Fatal(err)
		}
		if m["kind"] == "Pipeline" {
			return &unstructured.Unstructured{Object: m}
		}
	}
}

func newSampleWorld(t *testing.T, objects ...runtime.Object) *sampleWorld {
	t.Helper()
	w := &sampleWorld{t: t, ledger: &sampleLedger{}}
	objects = append(objects, samplePipeline(t))
	w.dyn = dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		samplePRGVR: "PipelineRunList",
		{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}:  "TaskRunList",
		{Group: "tekton.dev", Version: "v1", Resource: "pipelines"}: "PipelineList",
	}, objects...)
	// The tracker stamps neither UID nor resourceVersion; the apiserver does.
	w.dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
		w.creates++
		obj := action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID(types.UID(sampleUIDOf(w.creates)))
		obj.SetResourceVersion("1")
		return true, obj, w.dyn.Tracker().Create(samplePRGVR, obj, delivery.STGReleaseNamespace)
	})
	w.dyn.PrependReactor("patch", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
		w.patches++
		return false, nil, nil
	})
	w.core = kfake.NewSimpleClientset()
	adapter := &delivery.STGReleaseAdapter{Archive: w.ledger, Now: func() time.Time { return sampleNow },
		Dynamic: func() (dynamic.Interface, error) { return w.dyn, nil },
		Core:    func() (kubernetes.Interface, error) { return w.core, nil }}
	w.ci = landingci.ReleaseEvidence{Evidence: landingci.Evidence{CIRun: landing.CIRun{Run: "ci-platform-1", R: sampleP, Status: "Succeeded",
		CreatedAt: sampleNow.Add(-time.Hour), UID: "ci-uid", PlatformCommit: sampleP, UICommit: sampleU}, InfraCommit: sampleInfra},
		PipelineRef: "bifrost-ci-platform", RepoURL: landingci.PlatformRepoURL, ReadAt: sampleNow}
	w.sampler = &Sampler{Runs: adapter, Ledger: w.ledger, Now: func() time.Time { return sampleNow },
		CI: func(_ context.Context, p string) (landingci.ReleaseEvidence, error) {
			if p != w.ci.R {
				return landingci.ReleaseEvidence{}, errors.New("no CI")
			}
			return w.ci, nil
		}}
	return w
}

func (w *sampleWorld) create(approval string) (SampleResult, error) {
	return w.sampler.Create(context.Background(), SampleCreateRequest{Approval: approval, Env: "stg", P: sampleP, U: sampleU, Infra: sampleInfra})
}
func (w *sampleWorld) finalize(approval, run, uid string) (SampleResult, error) {
	return w.sampler.Finalize(context.Background(), SampleFinalizeRequest{Approval: approval, Env: "stg", Run: run, UID: uid})
}
func (w *sampleWorld) run(name string) *unstructured.Unstructured {
	w.t.Helper()
	obj, err := w.dyn.Resource(samplePRGVR).Namespace(delivery.STGReleaseNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	return obj
}
func (w *sampleWorld) finish(name, status string, extraFinalizers ...string) {
	w.t.Helper()
	obj := w.run(name)
	cond := map[string]any{"True": "True", "False": "False"}
	value := cond["True"]
	if status == "Failed" {
		value = cond["False"]
	}
	if status != "Running" {
		obj.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": value}}}
	}
	obj.SetFinalizers(append(obj.GetFinalizers(), extraFinalizers...))
	obj.SetResourceVersion("2")
	if err := w.dyn.Tracker().Update(samplePRGVR, obj, delivery.STGReleaseNamespace); err != nil {
		w.t.Fatal(err)
	}
}
func (w *sampleWorld) window() (who, env string, expires time.Time) {
	w.t.Helper()
	cm, err := w.core.CoreV1().ConfigMaps(delivery.STGReleaseNamespace).Get(context.Background(), "bifrost-release-window", metav1.GetOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	var rec struct {
		Who       string `json:"who"`
		Env       string `json:"env"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(cm.Data["window.json"]), &rec); err != nil {
		w.t.Fatal(err, cm.Data)
	}
	expires, _ = time.Parse(time.RFC3339, rec.ExpiresAt)
	return rec.Who, rec.Env, expires
}
func (w *sampleWorld) holdWindow(who string) {
	w.t.Helper()
	raw, _ := json.Marshal(map[string]any{"who": who, "what": "bifrost-platform", "env": "stg", "started_at": sampleNow.Format(time.RFC3339), "expires_at": sampleNow.Add(5 * time.Minute).Format(time.RFC3339)})
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bifrost-release-window", Namespace: delivery.STGReleaseNamespace, ResourceVersion: "1"}, Data: map[string]string{"window.json": string(raw)}}
	if _, err := w.core.CoreV1().ConfigMaps(delivery.STGReleaseNamespace).Create(context.Background(), cm, metav1.CreateOptions{}); err != nil {
		w.t.Fatal(err)
	}
}

// Positive control: the whole sample life. Create allocates from the sample
// stream, holds the window as the deterministic who, creates the held run
// with the production identity; finalize archives first, removes only the B5
// hold with the guarded patch, records the release and ends its window.
func TestSampleCreateAndFinalizeSucceed(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	out, err := w.create("approval-create-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.UID != sampleUIDOf(1) || out.Try != 1 || out.IntentSeq != 1 || out.Batch < 5000 || !out.Created || w.creates != 1 {
		t.Fatalf("create result %+v creates=%d", out, w.creates)
	}
	wantRun := fmt.Sprintf("stgrel-%d-1-%s", out.Batch, sampleP[:8])
	if out.Run != wantRun || out.Who != fmt.Sprintf("stgrel-bifrost-platform-%d-1", out.Batch) {
		t.Fatalf("identity %+v", out)
	}
	obj := w.run(out.Run)
	if !slices.Contains(obj.GetFinalizers(), delivery.STGReleaseFinalizer) || obj.GetAnnotations()["bifrost.io/stg-release-intent"] != "1" ||
		obj.GetLabels()["bifrost.io/stg-release-n"] != fmt.Sprint(out.Batch) {
		t.Fatalf("held run %v %v %v", obj.GetFinalizers(), obj.GetLabels(), obj.GetAnnotations())
	}
	if err := delivery.MatchNamedDeliverRun(obj, SampleInput(store.StgSample{Allocation: out.Batch, IntentSeq: 1, P: sampleP, U: sampleU, Infra: sampleInfra}), true); err != nil {
		t.Fatal("run does not carry the production identity", err)
	}
	if who, env, expires := w.window(); who != out.Who || env != "stg" || !expires.Equal(sampleNow.Add(60*time.Minute)) {
		t.Fatal("window", who, env, expires)
	}
	again, err := w.create("approval-create-1")
	if err != nil || again.UID != out.UID || again.Run != out.Run || w.creates != 1 {
		t.Fatal("retry must return the recorded sample without a second Create", again, err, w.creates)
	}

	w.finish(out.Run, "Succeeded", "other.example/keep")
	done, err := w.finalize("approval-finalize-1", out.Run, out.UID)
	if err != nil {
		t.Fatal(err)
	}
	if !done.Archived || !done.Released || done.Status != "Succeeded" || !done.WindowEnded || w.patches != 1 {
		t.Fatalf("finalize %+v patches=%d", done, w.patches)
	}
	if got := w.run(out.Run).GetFinalizers(); !slices.Equal(got, []string{"other.example/keep"}) {
		t.Fatal("finalizers after release", got)
	}
	if _, _, expires := w.window(); !expires.Equal(sampleNow) {
		t.Fatal("own window not ended", expires)
	}
	s, _ := w.ledger.Read(context.Background())
	x := s.Samples["approval-create-1"]
	if x.ArchivedBy != "approval-finalize-1" || x.ReleasedBy != "approval-finalize-1" || x.ArchivedSeq >= x.ReleasedSeq || s.Open() != nil {
		t.Fatalf("archive must precede release %+v", x)
	}
	if again, err := w.finalize("approval-finalize-1", out.Run, out.UID); err != nil || !again.Released || w.patches != 1 {
		t.Fatal("finalize retry is idempotent", again, err)
	}
	// A released sample frees the stream for a new first-opening sample.
	if _, err := w.create("approval-create-2"); err != nil {
		t.Fatal("new sample after release", err)
	}
}

func TestSampleCreateRefusals(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		setup func(*sampleWorld)
		req   func(*SampleCreateRequest)
		want  string
	}{
		{name: "env is not stg", req: func(r *SampleCreateRequest) { r.Env = "prod" }, want: "env stg only"},
		{name: "short revision", req: func(r *SampleCreateRequest) { r.U = "main" }, want: "full 40-hex"},
		{name: "rule already open", setup: func(w *sampleWorld) { w.ledger.ruleOpen = []bool{true} }, want: "first opening only"},
		{name: "rule opens before the allocation commits", setup: func(w *sampleWorld) { w.ledger.allocOpen = true }, want: "rule_open"},
		{name: "B5 already opened a batch", setup: func(w *sampleWorld) { w.ledger.lastBatch = 3 }, want: "not_first"},
		{name: "CI of another ui revision", setup: func(w *sampleWorld) { w.ci.UICommit = strings.Repeat("d", 40) }, want: "no accepted release CI"},
		{name: "CI not succeeded", setup: func(w *sampleWorld) { w.ci.Status = "Failed" }, want: "no accepted release CI"},
		{name: "held run already in cicd", setup: func(w *sampleWorld) {
			o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun"}}
			o.SetName("stgrel-1-1-eeeeeeee")
			o.SetNamespace(delivery.STGReleaseNamespace)
			o.SetFinalizers([]string{delivery.STGReleaseFinalizer})
			if err := w.dyn.Tracker().Create(samplePRGVR, o, delivery.STGReleaseNamespace); err != nil {
				w.t.Fatal(err)
			}
		}, want: "held STG release run already exists"},
		{name: "open sample of another approval", setup: func(w *sampleWorld) {
			if _, err := w.create("approval-earlier"); err != nil {
				w.t.Fatal(err)
			}
		}, want: "already exists"},
		{name: "rule opens before the send", setup: func(w *sampleWorld) { w.ledger.sendOpen = true }, want: "sample not sent: stg_sample rule_open"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := newSampleWorld(t)
			if test.setup != nil {
				test.setup(w)
			}
			before := w.creates
			req := SampleCreateRequest{Approval: "approval-refused", Env: "stg", P: sampleP, U: sampleU, Infra: sampleInfra}
			if test.req != nil {
				test.req(&req)
			}
			_, err := w.sampler.Create(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want refusal %q, got %v", test.want, err)
			}
			if w.creates != before {
				t.Fatal("a refused request sent a Create")
			}
			if s, _ := w.ledger.Read(context.Background()); s.Samples["approval-refused"] != nil && s.Samples["approval-refused"].UID != "" {
				t.Fatal("refused request recorded a UID")
			}
		})
	}
}

// A window held by another release is transient: nothing is sent, the
// allocation stays, and the same approval later sends under the same name.
func TestSampleCreateWaitsForTheWindow(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.holdWindow("release-sh-other")
	_, err := w.create("approval-busy")
	if !errors.Is(err, ErrSampleBusy) || w.creates != 0 {
		t.Fatal("want busy without a Create", err, w.creates)
	}
	s, _ := w.ledger.Read(context.Background())
	first := s.Samples["approval-busy"]
	if first == nil || first.UID != "" {
		t.Fatal("allocation must survive the busy refusal", first)
	}
	if err := w.core.CoreV1().ConfigMaps(delivery.STGReleaseNamespace).Delete(context.Background(), "bifrost-release-window", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := w.create("approval-busy")
	if err != nil || out.Batch != first.Allocation || w.creates != 1 {
		t.Fatal("retry reuses the allocation", out, err)
	}
}

// An allocation whose Create never landed blocks the stream until another
// approval proves the run absent after the grace; inside the grace, or with
// the run present, a new sample is refused.
func TestSampleAbandonNeedsProvenAbsence(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.holdWindow("release-sh-other")
	if _, err := w.create("approval-stuck"); !errors.Is(err, ErrSampleBusy) {
		t.Fatal(err)
	}
	if err := w.core.CoreV1().ConfigMaps(delivery.STGReleaseNamespace).Delete(context.Background(), "bifrost-release-window", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.create("approval-next"); err == nil || !strings.Contains(err.Error(), "not proven absent") {
		t.Fatal("inside the grace", err)
	}
	later := sampleNow.Add(store.StgSampleAbandonGrace + time.Second)
	w.sampler.Now = func() time.Time { return later }
	w.ledger.at = later
	out, err := w.create("approval-next")
	if err != nil || out.UID == "" {
		t.Fatal("abandon after proven absence", err)
	}
	s, _ := w.ledger.Read(context.Background())
	if !s.Samples["approval-stuck"].Abandoned || s.Samples["approval-stuck"].AbandonedBy != "approval-next" {
		t.Fatalf("old allocation %+v", s.Samples["approval-stuck"])
	}
}

func TestSampleFinalizeRefusals(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		setup func(*sampleWorld, SampleResult) (run, uid string)
		want  string
	}{
		{name: "no sample of that run", setup: func(w *sampleWorld, out SampleResult) (string, string) {
			return "stgrel-1-1-" + sampleP[:8], out.UID
		}, want: "no sample named"},
		{name: "uid differs from the recorded one", setup: func(w *sampleWorld, out SampleResult) (string, string) {
			w.finish(out.Run, "Succeeded")
			return out.Run, sampleUIDOf(99) // a fresh GET's uid, not the recorded one
		}, want: "not the recorded sample uid"},
		{name: "run not terminal", setup: func(w *sampleWorld, out SampleResult) (string, string) {
			w.finish(out.Run, "Running")
			return out.Run, out.UID
		}, want: "not terminal"},
		{name: "finalized by another approval", setup: func(w *sampleWorld, out SampleResult) (string, string) {
			w.finish(out.Run, "Failed")
			if _, err := w.finalize("approval-finalize-first", out.Run, out.UID); err != nil {
				w.t.Fatal(err)
			}
			return out.Run, out.UID
		}, want: "already finalized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := newSampleWorld(t)
			out, err := w.create("approval-create")
			if err != nil {
				t.Fatal(err)
			}
			run, uid := test.setup(w, out)
			patches := w.patches
			_, err = w.finalize("approval-finalize", run, uid)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want refusal %q, got %v", test.want, err)
			}
			if w.patches != patches {
				t.Fatal("a refused finalize patched the run")
			}
		})
	}
	if _, err := newSampleWorld(t).finalize("approval-finalize", "x", ""); err == nil {
		t.Fatal("finalize without uid")
	}
	if _, err := newSampleWorld(t).sampler.Finalize(context.Background(), SampleFinalizeRequest{Approval: "a", Env: "prod", Run: "x", UID: "y"}); err == nil {
		t.Fatal("finalize outside stg")
	}
}

// The archive is written before the hold is touched. When the patch is
// refused (the platform ServiceAccount lacks patch today), the hold stays,
// the sample stays open, and a later approval resumes from the archive.
func TestSampleFinalizeArchivesBeforeThePatch(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	out, err := w.create("approval-create")
	if err != nil {
		t.Fatal(err)
	}
	w.finish(out.Run, "Succeeded")
	forbidden := true
	w.dyn.PrependReactor("patch", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
		if forbidden {
			return true, nil, apierrors.NewForbidden(samplePRGVR.GroupResource(), out.Run, errors.New("cannot patch"))
		}
		return false, nil, nil
	})
	if _, err := w.finalize("approval-finalize-1", out.Run, out.UID); err == nil || !apierrors.IsForbidden(errors.Unwrap(err)) && !strings.Contains(err.Error(), "forbidden") {
		t.Fatal("want forbidden", err)
	}
	s, _ := w.ledger.Read(context.Background())
	if x := s.Samples["approval-create"]; !x.Archived || x.Released || !slices.Contains(w.run(out.Run).GetFinalizers(), delivery.STGReleaseFinalizer) {
		t.Fatalf("archive first, hold kept: %+v", x)
	}
	forbidden = false
	done, err := w.finalize("approval-finalize-2", out.Run, out.UID)
	if err != nil || !done.Released || slices.Contains(w.run(out.Run).GetFinalizers(), delivery.STGReleaseFinalizer) {
		t.Fatal("resume after the permission is granted", done, err)
	}
	s, _ = w.ledger.Read(context.Background())
	if x := s.Samples["approval-create"]; x.ArchivedBy != "approval-finalize-1" || x.ReleasedBy != "approval-finalize-2" {
		t.Fatalf("one archive, later release %+v", x)
	}
}

// A Create whose response is lost and whose readback finds nothing is not
// proof of absence: the approval goes to unknown, never to a new name.
func TestSampleCreateUnknownOutcome(t *testing.T) {
	t.Parallel()
	w := newSampleWorld(t)
	w.dyn.PrependReactor("create", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("apiserver restarting")
	})
	_, err := w.create("approval-unknown")
	if !errors.Is(err, ErrSampleUncertain) || !strings.Contains(err.Error(), "stgrel-") {
		t.Fatal("want an uncertain outcome naming the run", err)
	}
	s, _ := w.ledger.Read(context.Background())
	if x := s.Samples["approval-unknown"]; x == nil || x.UID != "" || s.Open() != x {
		t.Fatal("the allocation stays open until its run is proven absent", x)
	}
}
