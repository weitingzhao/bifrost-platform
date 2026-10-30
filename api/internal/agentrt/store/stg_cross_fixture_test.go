package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
)

// The only fakes are external transports. All landing and B5 records come
// from production controllers/stores; no fixture appends record JSON.
type b5hWorld struct {
	w                 *world
	git               *local704
	root, op, p, base string
	runs              *b5hRuns
	windows           *b5hWindows
	policy            atomic.Pointer[stgrelease.WindowPolicy]
	clock             *clocktesting.FakeClock
	c                 *stgrelease.Controller
	commits           []landing.ReleaseCommit
}

func b5hFixture(t *testing.T) *b5hWorld {
	t.Helper()
	w, at := b5fWorld(t, "enabled") // SELECT now(), truncated to seconds.
	f := localGit704(t)
	base := f.bases[platformRepo]
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "B5 cross-block fixture", Repo: platformRepo, BaseCommit: base})
	a := f.result(platformRepo, root, false)
	passReal704(t, w, root, a)
	gits := f.adapters()
	src := controller.MultiRepoGitFacts(gits)
	op, decision, err := store.ClaimLanding(ctx, w.rt, src, "prod", root, store.DecisionAt())
	ok(t, err)
	if !decision.Allowed {
		t.Fatal("landing claim", decision)
	}
	lc := &controller.Controller{DB: w.rt, Env: "prod", Live: true, DrillGit: gits, Drill: &controller.DrillSources{Facts: src}}
	ok(t, lc.LandingDrillBranch(ctx, gits[platformRepo], op, store.DecisionAt()))
	prepared := landingState(t, w, landingRepoStream).Operations[op].PreparedAt
	store.FreezeEventClock(prepared.Add(2 * time.Minute))
	_, err = lc.LandingDrillCI(ctx, controller.DrillSources{Facts: src,
		Main:          func(context.Context, string) (string, error) { return base, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, r string, _ time.Time) (landing.CIRun, error) {
			return landing.CIRun{Run: "landing-green", R: r, Status: "Succeeded", CreatedAt: prepared.Add(time.Minute)}, nil
		},
	}, op, store.DecisionAt())
	ok(t, err)
	g := gits[platformRepo].(*landinggit.Git)
	p := f.run(f.remotes[platformRepo], "rev-parse", "main")
	_, err = store.ReconcileMain(ctx, w.rt, platformRepo, func(ctx context.Context, _, task, accepted, r string) (store.MainEvidence, error) {
		head, has, history, err := g.MainEvidence(ctx, r, task, accepted)
		return store.MainEvidence{Head: head, HasR: has, Validity: string(history.State), RevertedBy: history.RevertedBy}, err
	})
	ok(t, err)
	if !landingState(t, w, landingRepoStream).Operations[op].EverLanded || p == base {
		t.Fatal("production landing did not reach local main")
	}
	workspace, err := g.Open(ctx, p)
	ok(t, err)
	commits, err := workspace.ReleaseRange(ctx, base, p)
	ok(t, err)
	ok(t, workspace.Close())
	x := &b5hWorld{w: w, git: f, root: root, op: op, p: p, base: base, commits: commits, clock: clocktesting.NewFakeClock(store.DecisionAt())}
	rule, err := store.StgControlRule(ctx, w.rt, "dev")
	ok(t, err)
	x.policy.Store(&stgrelease.WindowPolicy{Environment: "prod", Live: true, Rule: rule})
	x.windows = &b5hWindows{now: x.clock.Now}
	x.runs = &b5hRuns{pipe: b5RunAdapter(t).pipe, observations: map[types.UID]delivery.STGRunObservation{}, work: b5hWork("a", "c"), health: delivery.STGHealth{Outcome: "passed", Evidence: map[string]any{"service": "bifrost-platform-api", "status": "ok"}}}
	x.c = x.controller(w.rt)
	t.Cleanup(func() { ok(t, x.c.Close()) })
	// Baseline evidence is acquired through ExternalPass, not synthesized.
	baseObs := b5hObservation(t, delivery.NamedDeliverInput{Batch: 900, Try: 1, IntentSeq: 1, P: base, U: commitA, Infra: commitA}, "Succeeded", "baseline", "a", "c")
	x.runs.observations[baseObs.Identity.UID] = baseObs
	x.runs.external = []*unstructured.Unstructured{baseObs.Run}
	ok(t, x.c.ExternalPass(ctx))
	x.runs.external = nil
	for range 8 {
		ok(t, x.c.Pass(ctx))
		s := x.state(t)
		if b := s.Batches[b5Batch]; b != nil && b.Phase == "SNAP" {
			return x
		}
	}
	t.Fatalf("fixture did not reach SNAP from production paths (PG anchor %s): %+v", at, x.state(t))
	return nil
}

func (x *b5hWorld) controller(db store.DB) *stgrelease.Controller {
	return &stgrelease.Controller{DB: db, Tenant: "dev", Cluster: "cluster", Runs: x.runs, Windows: x.windows, Clock: x.clock, FinalizerGuaranteed: true,
		Policy: func() stgrelease.WindowPolicy { return *x.policy.Load() }, Sources: stgrelease.Sources{
			Head: func(context.Context) (string, error) { return x.p, nil },
			CI: func(context.Context, string) (store.StgCIObservation, error) {
				e := landingci.ReleaseEvidence{Evidence: landingci.Evidence{CIRun: landing.CIRun{Run: "ci-exact", UID: "ci-exact-uid", R: x.p, Status: "Succeeded", CreatedAt: x.clock.Now().Add(-time.Minute), PlatformCommit: x.p, UICommit: commitA}, InfraCommit: commitA}, PipelineRef: "bifrost-ci-platform", RepoURL: landingci.PlatformRepoURL, ReadAt: x.clock.Now()}
				return stgrelease.ReleaseCIObservation(e), nil
			},
			Range: func(context.Context, string, string) ([]landing.ReleaseCommit, error) { return x.commits, nil },
			ReleaseEvidence: func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
				return store.MainEvidence{Head: p, HasR: p == x.p, Validity: "effective", ReadAt: x.clock.Now()}, nil
			},
		}}
}
func (x *b5hWorld) state(t *testing.T) store.StgState {
	t.Helper()
	s, err := store.ReadStgRelease(ctx, x.w.rt)
	ok(t, err)
	return s
}
func (x *b5hWorld) restart(t *testing.T) {
	t.Helper()
	before := x.state(t)
	ok(t, x.c.Close())
	x.c = x.controller(x.w.rt)
	after := x.state(t)
	if before.LastSeq != after.LastSeq {
		t.Fatal("restart changed ledger")
	}
}
func (x *b5hWorld) advance(t *testing.T, d time.Duration) {
	t.Helper()
	ok(t, x.c.Close())
	store.FreezeEventClock(store.DecisionAt().Add(d))
	x.clock.SetTime(store.DecisionAt())
}
func (x *b5hWorld) until(t *testing.T, want func(store.StgState) bool) {
	t.Helper()
	for range 12 {
		if want(x.state(t)) {
			return
		}
		ok(t, x.c.Pass(ctx))
	}
	t.Fatal("controller did not converge", x.state(t).Batches[b5Batch])
}
func (x *b5hWorld) terminal(t *testing.T, status string) {
	t.Helper()
	s := x.state(t)
	b := s.Batches[b5Batch]
	tr := b.Tries[b.Try]
	in := delivery.NamedDeliverInput{Batch: 1, Try: b.Try, IntentSeq: tr.Started.Seq, P: x.p, U: commitA, Infra: commitA}
	obs := b5hObservation(t, in, status, tr.Bound.Record.String("uid"), "d", "f")
	x.runs.mu.Lock()
	x.runs.observations[obs.Identity.UID] = obs
	x.runs.mu.Unlock()
}
func b5hWork(api, console string) map[string]any {
	w := map[string]any{}
	for _, name := range []string{"platform-api", "platform-workers", "platform-console"} {
		d := api
		if name == "platform-console" {
			d = console
		}
		w[name] = map[string]any{"uid": name + "-uid", "generation": int64(1), "digest": "sha256:" + strings.Repeat(d, 64)}
	}
	return w
}

type b5hRuns struct {
	mu                   sync.Mutex
	pipe                 *unstructured.Unstructured
	obj                  *unstructured.Unstructured
	observations         map[types.UID]delivery.STGRunObservation
	external             []*unstructured.Unstructured
	work                 map[string]any
	health               delivery.STGHealth
	readErr, responseErr error
	creates              int
	uids                 map[types.UID]bool
	beforeSend           func()
}

func (r *b5hRuns) ReadNamedDeliverRun(_ context.Context, in delivery.NamedDeliverInput) (*unstructured.Unstructured, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return nil, r.readErr
	}
	if r.obj == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pipelineruns"}, "missing")
	}
	return r.obj.DeepCopy(), delivery.MatchNamedDeliverRun(r.obj, in, true)
}
func (r *b5hRuns) CreatePermittedNamedDeliverRun(ctx context.Context, in delivery.NamedDeliverInput, check func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error) {
	if r.beforeSend != nil {
		r.beforeSend()
	}
	if err := check(ctx); err != nil {
		return cluster.ActuationResponse{}, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	obj, err := delivery.PrepareNamedDeliverRun(in, r.pipe)
	if err != nil {
		return cluster.ActuationResponse{}, nil, err
	}
	r.creates++
	obj.SetUID(types.UID(fmt.Sprintf("b5h-uid-%d", r.creates)))
	obj.SetResourceVersion("1")
	if r.uids == nil {
		r.uids = map[types.UID]bool{}
	}
	r.uids[obj.GetUID()] = true
	r.obj = obj
	if r.responseErr != nil {
		return cluster.ActuationResponse{}, nil, r.responseErr
	}
	return cluster.ActuationResponse{OK: true}, obj.DeepCopy(), nil
}
func (r *b5hRuns) ObserveRun(_ context.Context, id delivery.STGRunIdentity) (delivery.STGRunObservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return delivery.STGRunObservation{}, r.readErr
	}
	if o, ok := r.observations[id.UID]; ok {
		return o, nil
	}
	return delivery.STGRunObservation{Identity: id, Status: "Running"}, nil
}
func (r *b5hRuns) RemoveArchivedFinalizer(context.Context, delivery.NamedDeliverInput, types.UID) error {
	return nil
}
func (r *b5hRuns) ObserveWorkloads(context.Context) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.work, nil
}
func (r *b5hRuns) ReadPin(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}
func (r *b5hRuns) ReadHealth(context.Context) delivery.STGHealth { return r.health }
func (r *b5hRuns) ListSucceededSTG(context.Context) ([]*unstructured.Unstructured, error) {
	return r.external, nil
}

type b5hWindows struct {
	mu      sync.Mutex
	now     func() time.Time
	w       delivery.STGWindow
	version int
}

func (w *b5hWindows) ReadSTGWindow(context.Context) (delivery.STGWindow, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w, nil
}
func (w *b5hWindows) TakeSTGWindow(_ context.Context, old delivery.STGWindow, who string, gen int64, recover bool) (delivery.STGWindow, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if old.ResourceVersion != w.w.ResourceVersion || w.w.Live(w.now()) && (!recover || w.w.Who != who) {
		return delivery.STGWindow{}, &delivery.WindowChanged{}
	}
	w.version++
	w.w = delivery.STGWindow{Who: who, What: platformRepo, Env: "stg", Generation: gen, ResourceVersion: fmt.Sprint(w.version), Expires: w.now().Add(5 * time.Minute)}
	return w.w, nil
}
func (w *b5hWindows) RenewSTGWindow(_ context.Context, old delivery.STGWindow) (delivery.STGWindow, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if old.ResourceVersion != w.w.ResourceVersion {
		return delivery.STGWindow{}, &delivery.WindowChanged{}
	}
	w.version++
	w.w.ResourceVersion = fmt.Sprint(w.version)
	w.w.Expires = w.now().Add(5 * time.Minute)
	return w.w, nil
}
func (w *b5hWindows) EndSTGWindow(_ context.Context, old delivery.STGWindow) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if old.ResourceVersion != w.w.ResourceVersion {
		return &delivery.WindowChanged{}
	}
	w.version++
	w.w.ResourceVersion = fmt.Sprint(w.version)
	w.w.Expires = w.now()
	return nil
}

// Compile-time assertions keep the fake at transport seams only.
var _ stgrelease.Execution = (*b5hRuns)(nil)
var _ stgrelease.Windows = (*b5hWindows)(nil)
