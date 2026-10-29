package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	clocktesting "k8s.io/utils/clock/testing"
)

// Serial: b5Ready freezes the recorder clock; PG fixtures use separate DBs.
// The late callback is the last POST boundary used by the real run adapter.
func TestStgLeasePGSenderFencesTakeoverBeforeRenewalTick(t *testing.T) {
	for _, takeover := range []bool{false, true} {
		t.Run(fmt.Sprintf("takeover=%t", takeover), func(t *testing.T) {
			x := b5Ready(t)
			c := clocktesting.NewFakeClock(x.at)
			cs := fake.NewSimpleClientset()
			gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			cs.PrependReactor("create", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
				cm := a.(kt.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
				cm.ResourceVersion, cm.UID = "1", "window-uid"
				return true, cm, cs.Tracker().Create(gvr, cm, a.GetNamespace())
			})
			adapter := &delivery.STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return cs, nil }, Now: c.Now}
			rule := &rules.Rule{Header: rules.Header{Version: 1, Effective: x.at.Add(-time.Hour), Expires: x.at.Add(time.Hour)}, Kind: rules.KindLanding,
				Landing: &rules.Landing{Repos: []rules.LandingRepo{{Repo: "bifrost-platform", STGRelease: delivery.STGReleasePipeline}}}}
			h, err := stgrelease.HoldWindow(ctx, stgrelease.WindowConfig{Windows: adapter, Clock: c, Policy: func() stgrelease.WindowPolicy {
				return stgrelease.WindowPolicy{Environment: "prod", Live: true, Rule: rule}
			}}, x.read(t), b5Batch, 1)
			ok(t, err)
			defer func() { _ = h.Release() }()
			x.src.Generation = h.Generation
			x.src.Observe = func(ctx context.Context, _ store.StgRecord) (store.StgGateObservation, error) {
				obs := x.obs
				var err error
				obs.Lease, err = h.Observe(ctx)
				return obs, err
			}
			runs := b5RunAdapter(t)
			if takeover {
				runs.before = func() {
					cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, "bifrost-release-window", metav1.GetOptions{})
					ok(t, err)
					var record map[string]any
					ok(t, json.Unmarshal([]byte(cm.Data["window.json"]), &record))
					record["reason"] = "stgrel-gen:2"
					raw, err := json.Marshal(record)
					ok(t, err)
					cm.ResourceVersion, cm.Data["window.json"] = "2", string(raw)
					ok(t, cs.Tracker().Update(gvr, cm, "cicd"))
				}
			}
			sender := stgrelease.Sender{DB: x.w.rt, Runs: runs, Gate: x.src, Window: h, FinalizerGuaranteed: true}
			decision, err := sender.Start(ctx, b5Batch, 1)
			if !decision.Allowed {
				t.Fatal("must reach the POST boundary", decision, err)
			}
			s := x.read(t)
			tr := s.Batches[b5Batch].Tries[1]
			if !takeover {
				ok(t, err)
				if runs.creates != 1 || tr.Bound.Record.String("uid") != "uid-one" {
					t.Fatal("success control did not create/bind", runs.creates)
				}
			} else {
				if err == nil || runs.creates != 0 || tr.Bound.Seq != 0 || !tr.Occupied() || tr.Verify.Seq != 0 || tr.Unsent.Seq != 0 {
					t.Fatal("takeover allowed a POST or settled execution", err, runs.creates, tr)
				}
				loss := <-h.Losses()
				ok(t, store.RecordStgLeaseLost(ctx, x.w.rt, loss.Batch, loss.Try, loss.Generation))
				if s := x.read(t); s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 || len(s.Batches[b5Batch].Diagnostics) != 1 {
					t.Fatal("loss refunded the committed debit or lost its diagnostic")
				}
			}
		})
	}
}
