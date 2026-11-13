package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kt "k8s.io/client-go/testing"
)

func TestRunWindowFollowerStopsOnFence(t *testing.T) {
	cs, dyn := recoveryFixture(t)
	attempts := 0
	cs.PrependReactor("update", "configmaps", func(kt.Action) (bool, runtime.Object, error) {
		attempts++
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, releaseWindowConfigMap, errors.New("new owner"))
	})
	w := &runWindow{cs: cs, ns: "cicd", who: "run", version: "1"}
	ticks := make(chan time.Time, 3)
	for i := 0; i < 3; i++ {
		ticks <- time.Now()
	}
	close(ticks)
	w.followTicks(context.Background(), dyn.Resource(pipelineRunGVR).Namespace("cicd"), "run", true, ticks)
	if attempts != 1 {
		t.Fatalf("fenced follower retried %d times", attempts)
	}
}

func TestRecoveryScannersDoNotStealFreshFollower(t *testing.T) {
	ctx := context.Background()
	cs, dyn := recoveryFixture(t)
	res := dyn.Resource(pipelineRunGVR).Namespace("cicd")
	w0 := &runWindow{cs: cs, ns: "cicd", who: "run", version: "1"}
	if err := w0.update(ctx, false); err != nil {
		t.Fatal(err)
	}
	w, reason, err := recoverRunWindow(ctx, cs, res, "cicd", nil)
	if err != nil || w != nil || reason != "run has a recent renewal" {
		t.Fatalf("fresh follower stolen: %v %s %v", w, reason, err)
	}
}

func TestConcurrentRecoveryOnlyOneRenews(t *testing.T) {
	ctx := context.Background()
	cs, dyn := recoveryFixture(t)
	cms := cs.CoreV1().ConfigMaps("cicd")
	cm, _ := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	cm.Data[releaseWindowDataKey] = fmt.Sprintf(`{"who":"run","reason":"PipelineRun/run","expires_at":%q}`, time.Now().Add(time.Minute).UTC().Format(time.RFC3339))
	_, _ = cms.Update(ctx, cm, metav1.UpdateOptions{})
	// The fake tracker needs the apiserver's atomic version check modeled.
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cs.PrependReactor("update", "configmaps", func(a kt.Action) (bool, runtime.Object, error) {
		next := a.(kt.UpdateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		old, _ := cs.Tracker().Get(gvr, "cicd", releaseWindowConfigMap)
		if next.ResourceVersion != old.(*corev1.ConfigMap).ResourceVersion {
			return true, nil, apierrors.NewConflict(gvr.GroupResource(), next.Name, errors.New("stale"))
		}
		next.ResourceVersion = next.ResourceVersion + "x"
		err := cs.Tracker().Update(gvr, next, "cicd")
		return true, next, err
	})
	var wg sync.WaitGroup
	results := make(chan *runWindow, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, _, err := recoverRunWindow(ctx, cs, dyn.Resource(pipelineRunGVR).Namespace("cicd"), "cicd", nil)
			if err != nil && !windowFenced(err) {
				t.Error(err)
			}
			results <- w
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for w := range results {
		if w != nil {
			winners++
			if err := w.update(ctx, false); err != nil {
				t.Error(err)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("followers=%d", winners)
	}
}

func TestWorkersRecoveryRefusesManualStart(t *testing.T) {
	ctx := context.Background()
	const pipeline = "bifrost-deliver-platform"
	pipe := baselinePipelines(t)[pipeline].DeepCopy()
	pipe.SetNamespace("cicd")
	s, _, _ := contractService(t, pipe, "unknown")
	dyn := listingDynamic(s, pipe)
	cs, _ := recoveryFixture(t)
	if _, err := cs.CoreV1().Nodes().Create(ctx, readyAmd64Node(), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	s.SetClientsetForTest(cs)
	if err := cs.CoreV1().ConfigMaps("cicd").Delete(ctx, releaseWindowConfigMap, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerForTest(s, nil)
	run, _, err := h.StartDeliveryIfFree(ctx, "head-release", pipeline, factSHA1, map[string]string{"uiRevision": strings.Repeat("b", 40)})
	if err != nil {
		t.Fatal(err)
	}
	cms := cs.CoreV1().ConfigMaps("cicd")
	cm, _ := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	cm.Data[releaseWindowDataKey] = fmt.Sprintf(`{"who":%q,"reason":%q,"expires_at":%q}`, run.Name, "PipelineRun/"+run.Name, time.Now().Add(time.Minute).UTC().Format(time.RFC3339))
	_, _ = cms.Update(ctx, cm, metav1.UpdateOptions{})
	recovered, _, err := recoverRunWindow(ctx, cs, dyn.Resource(pipelineRunGVR).Namespace("cicd"), "cicd", nil)
	if err != nil || recovered == nil {
		t.Fatalf("workers restart: %v %v", recovered, err)
	}
	// Check beyond the old renewal's expiry with the store's clock seam.
	held, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().Add(2*time.Minute))
	if err != nil || !live || held.Who != run.Name {
		t.Fatalf("renewal lost: %+v %v", held, err)
	}
	resp, _, err := s.StartPipelineRun(actions.WithCreateAttempt(ctx, "review-manual", 1), pipeline, factSHA1, "", "", map[string]string{"uiRevision": strings.Repeat("b", 40)})
	if err == nil || !strings.Contains(resp.Message, run.Name) {
		t.Fatalf("second run permitted: %+v %v", resp, err)
	}
}

func TestStartupRecoveryRetriesReads(t *testing.T) {
	for _, target := range []string{"window", "run"} {
		t.Run(target, func(t *testing.T) {
			cs, dyn := recoveryFixture(t)
			pipe := baselinePipelines(t)["bifrost-deliver-platform"].DeepCopy()
			s, _, _ := contractService(t, pipe, "unknown")
			s.SetClientsetForTest(cs)
			s.dynamicFactory = func() (dynamic.Interface, error) { return dyn, nil }
			reads := 0
			failOnce := func(kt.Action) (bool, runtime.Object, error) {
				reads++
				if reads == 1 {
					return true, nil, errors.New("startup read failed")
				}
				return false, nil, nil
			}
			if target == "window" {
				cs.PrependReactor("get", "configmaps", failOnce)
			} else {
				dyn.PrependReactor("get", "pipelineruns", failOnce)
			}
			h := NewHandlerForTest(s, nil)
			ticks := make(chan time.Time, 1)
			ticks <- time.Now()
			close(ticks)
			h.recoverRunWindowTicks(context.Background(), ticks)
			cm, err := cs.CoreV1().ConfigMaps("cicd").Get(context.Background(), releaseWindowConfigMap, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			r, _ := parseWindowCM(cm)
			expires, _ := time.Parse(time.RFC3339, r.ExpiresAt)
			if expires.Before(time.Now().Add(4 * time.Minute)) {
				t.Fatalf("startup failure was permanent: %+v", r)
			}
		})
	}
}
