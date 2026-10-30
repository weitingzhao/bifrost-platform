package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	clocktesting "k8s.io/utils/clock/testing"
)

type reviewControlRuns struct {
	*b5Runs
	workloads map[string]any
	reads     int
}

func (r *reviewControlRuns) ObserveWorkloads(context.Context) (map[string]any, error) {
	r.reads++ // This read distinguishes final authorization from preparation.
	return r.workloads, nil
}
func (*reviewControlRuns) ObserveRun(context.Context, delivery.STGRunIdentity) (delivery.STGRunObservation, error) {
	return delivery.STGRunObservation{}, errors.New("test stops at the first authorized Create")
}
func (*reviewControlRuns) RemoveArchivedFinalizer(context.Context, delivery.NamedDeliverInput, types.UID) error {
	return errors.New("no terminal execution in selection test")
}
func (*reviewControlRuns) ReadPin(context.Context, string, string) (map[string]any, error) {
	return nil, errors.New("no terminal execution in selection test")
}
func (*reviewControlRuns) ReadHealth(context.Context) delivery.STGHealth {
	return delivery.STGHealth{Outcome: "unknown"}
}

func reviewOpenPG(t *testing.T) (*b5Fixture, store.LandingSource) {
	t.Helper()
	w := newKernelWorld(t)
	// PostgreSQL owns defaults on task/mission rows. Use its clock as the
	// reference for claim/prepared as well as the gate. A fixed historical
	// claim combined with database now() would exhaust the six-hour window.
	var dbNow time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT now()`).Scan(&dbNow))
	w, root, op, src, prepared := liveReadyInWorld(t, w, dbNow.UTC().Truncate(time.Microsecond))
	at := prepared.Add(10 * time.Minute)
	t.Cleanup(store.FreezeEventClock(at))
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal("original landing prerequisite", d)
	}
	_, err = store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "landed", "R", commitB)))
	x := b5StateFixture(t, w, root, op, at, false)
	if x.read(t).Batches[b5Batch].Phase != "OPEN" {
		t.Fatal("fixture must admit before reversal")
	}
	return x, src
}

func reviewLandReversalPG(t *testing.T, x *b5Fixture, src store.LandingSource, p string) (string, string) {
	t.Helper()
	w := x.w
	root := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "authorized reversal", BaseCommit: commitB})
	w.passRound(t, root, "h1", "h2", "h3")
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, root, store.DecisionAt())
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal("reversal claim", d)
	}
	prepared := store.DecisionAt()
	ref := "refs/heads/land/prod/" + root + "/1"
	for _, r := range []map[string]any{
		newLandingRecord(op, "prepared", "R", p, "M0", commitB, "committer_date", prepared.Format(time.RFC3339Nano), "dry_run", false),
		newLandingRecord(op, "branch_intent", "ref", ref, "R", p),
		newLandingRecord(op, "branch_pushed", "ref", ref, "R", p),
	} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	store.FreezeEventClock(prepared.Add(time.Minute))
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, func(context.Context, landing.Operation) (store.LiveObservation, error) {
		return store.LiveObservation{Main: commitB, CI: landing.CIRun{Run: "reversal-ci", R: p, Status: "Succeeded", CreatedAt: prepared.Add(time.Second)}}, nil
	})
	ok(t, err)
	if !d.Allowed {
		t.Fatal("reversal authority", d)
	}
	_, err = store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "landed", "R", p)))
	return root, op
}

// Serial: real PostgreSQL plus FreezeEventClock; all external I/O is injected.
func TestStgControlPGOpenReversalAndReadFailure(t *testing.T) {
	for _, mode := range []string{"manual_revert", "landed_revert", "read_failure"} {
		t.Run(mode, func(t *testing.T) {
			x, src := reviewOpenPG(t)
			p := strings.Repeat("f", 40)
			var reversalRoot, reversalOp string
			if mode == "landed_revert" {
				reversalRoot, reversalOp = reviewLandReversalPG(t, x, src, p)
			}
			at := store.DecisionAt().Add(time.Second)
			store.FreezeEventClock(at)
			clock := clocktesting.NewFakeClock(at)
			core := fake.NewSimpleClientset()
			gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			core.PrependReactor("create", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
				cm := a.(kt.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
				cm.ResourceVersion, cm.UID = "1", "window-uid"
				return true, cm, core.Tracker().Create(gvr, cm, a.GetNamespace())
			})
			window := &delivery.STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }, Now: clock.Now}
			runs := &reviewControlRuns{b5Runs: b5RunAdapter(t), workloads: x.obs.Workloads}
			rule := inForce(t, x.w, "landing:dev", at)
			ci := x.obs.CI
			ci.Accepted["platform_commit"], ci.Accepted["read_at"] = p, at.Format(time.RFC3339Nano)
			ci.ReadAt = at
			commits := append([]landing.ReleaseCommit{}, x.obs.Commits...)
			message := "Manual revert"
			if mode == "landed_revert" {
				message = "Revert\n\nAgentrt-Task: " + reversalRoot + "\nLanded-By: agentrt-prod/" + reversalOp + "\n"
			}
			commits = append(commits, landing.ReleaseCommit{SHA: p, Parents: []string{commitB}, Message: message, Changes: []rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Reverted</p>"}}})
			ranges, memberReads := 0, 0
			readErr := errors.New("Git transport unavailable")
			c := &stgrelease.Controller{DB: x.w.rt, Tenant: "dev", Cluster: "cluster", Runs: runs, Windows: window, Clock: clock, FinalizerGuaranteed: true,
				Policy: func() stgrelease.WindowPolicy {
					return stgrelease.WindowPolicy{Environment: "prod", Live: true, Rule: &rule.Rule}
				},
				Sources: stgrelease.Sources{
					Head: func(context.Context) (string, error) { return p, nil },
					CI:   func(context.Context, string) (store.StgCIObservation, error) { return ci, nil },
					Range: func(_ context.Context, base, head string) ([]landing.ReleaseCommit, error) {
						ranges++
						if base != commitA || head != p {
							t.Fatal("final gate must authorize the complete base..P range")
						}
						return commits, nil
					},
					MainEvidence: func(_ context.Context, _, root, _, _ string) (store.MainEvidence, error) {
						memberReads++
						if mode == "read_failure" {
							return store.MainEvidence{}, readErr
						}
						if root == reversalRoot {
							return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
						}
						return store.MainEvidence{Head: p, HasR: true, Validity: "reverted", RevertedBy: p}, nil
					},
				}}
			t.Cleanup(func() { ok(t, c.Close()) })
			for range 8 {
				err := c.Pass(ctx)
				if err != nil && !(mode == "read_failure" && errors.Is(err, readErr)) {
					t.Fatal(err)
				}
				if x.read(t).Batches[b5Batch].Phase == "CLOSED" {
					break
				}
			}
			s := x.read(t)
			m := store.StgMemberKey{Root: x.root, Op: x.op, R: commitB}
			switch mode {
			case "manual_revert":
				if s.Batches[b5Batch].Phase != "CLOSED" || s.Members[m].To != "released" || len(s.Blocks) != 1 || ranges < 2 || runs.reads != 1 {
					t.Fatalf("manual reversal missed final gate/settlement: phase=%s member=%s blocks=%d ranges=%d gate=%d", s.Batches[b5Batch].Phase, s.Members[m].To, len(s.Blocks), ranges, runs.reads)
				}
				for _, b := range s.Blocks {
					if !b.IsOpen() || b.Opened.Record.String("why") != "diff_unlanded" {
						t.Fatal("manual reversal must have a durable diff_unlanded trigger")
					}
				}
			case "landed_revert":
				if s.Batches[b5Batch].Phase != "CLOSED" || s.Members[m].To != "disposed" || len(s.Blocks) != 0 || ranges != 1 || runs.reads != 0 {
					t.Fatal("later authorized reversal must hand off before a send")
				}
			case "read_failure":
				if s.Batches[b5Batch].Phase != "OPEN" || s.Batches[b5Batch].Try != 0 || s.Members[m].To != "" || len(s.Blocks) != 0 || ranges != 0 || runs.reads != 0 || memberReads == 0 {
					t.Fatal("read failure became authorization or disposal")
				}
			}
			if runs.creates != 0 || len(s.Budget) != 0 {
				t.Fatal("selection refusal/handoff spent budget or created a run")
			}
			if mode == "landed_revert" {
				o := s.Legacy.Operations[reversalOp]
				_, err := store.ReconcileStgMainOne(ctx, x.w.rt, "dev", "bifrost-platform", store.MainCandidate{Op: reversalOp, Root: reversalRoot, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A}, store.MainEvidence{Head: p, HasR: true, Validity: "effective", ReadAt: at})
				ok(t, err)
				for range 8 {
					ok(t, c.Pass(ctx))
					if runs.creates > 0 {
						break
					}
				}
				s = x.read(t)
				if runs.creates != 1 || runs.reads != 1 || ranges < 3 || s.LastBatch != 2 || s.Budget[store.StgBudgetKey{R: p, Gen: 1}] != 1 || s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 0 {
					t.Fatalf("handoff failed to reauthorize the original and reversal together: creates=%d gate=%d ranges=%d batch=%d budget=%v", runs.creates, runs.reads, ranges, s.LastBatch, s.Budget)
				}
			}
		})
	}
}
