package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestAutomaticRunWindow(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod", "bifrost-deliver-stg", "bifrost-deliver-prod"} {
		for _, state := range []string{"absent", "expired", "foreign", "holder"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				ctx := context.Background()
				pipe := baselinePipelines(t)[name].DeepCopy()
				pipe.SetNamespace("cicd")
				s, dyn, _ := contractService(t, pipe, "unknown")
				cs := fake.NewSimpleClientset(readyAmd64Node())
				s.SetClientsetForTest(cs)
				who := ""
				if state != "absent" {
					when := time.Now().UTC()
					if state == "expired" {
						when = when.Add(-2 * windowLifetime)
					}
					_, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "session", What: sortedKeys(windowPipelines[name].repos)}, when)
					if err != nil {
						t.Fatal(err)
					}
				}
				if state == "holder" {
					who = "session"
				}
				extra := map[string]string{"uiRevision": strings.Repeat("b", 40)}
				expected := s.releaseWindowMessage(ctx, name, who)
				resp, _, err := s.StartPipelineRun(ctx, name, factSHA1, "", who, extra)
				if state == "foreign" {
					if err == nil || resp.Message != expected || !strings.Contains(resp.Message, "session") {
						t.Fatalf("refusal: %+v %v; want %s", resp, err, expected)
					}
					assertNoContractCreate(t, dyn)
					return
				}
				if err != nil || !resp.OK {
					t.Fatalf("start: %+v %v", resp, err)
				}
				run := createdContractRun(t, dyn)
				held, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC())
				if err != nil || !live {
					t.Fatalf("window: %+v %v", held, err)
				}
				if state == "holder" {
					if held.Who != "session" {
						t.Fatalf("took session window: %+v", held)
					}
					for _, a := range cs.Actions() {
						if a.GetVerb() == "update" && a.GetResource().Resource == "configmaps" {
							t.Fatal("session window updated")
						}
					}
					return
				}
				if held.Who != run.GetName() {
					t.Fatalf("holder: %+v", held)
				}
				// A later direct request is refused before object-name collisions matter.
				expected = s.releaseWindowMessage(ctx, name, "")
				resp, _, err = s.StartPipelineRun(ctx, name, factSHA1, "", "", extra)
				if err == nil || resp.Message != expected || !strings.Contains(resp.Message, run.GetName()) {
					t.Fatalf("second start: %+v %v", resp, err)
				}
				cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				lease := &runWindow{cs: cs, ns: "cicd", who: held.Who, version: cm.ResourceVersion}
				res := dyn.Resource(pipelineRunGVR).Namespace("cicd")
				run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}}
				if _, err := res.Update(ctx, run, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				if ended, err := lease.observe(ctx, res, run.GetName()); err != nil || !ended {
					t.Fatalf("completion: %v %v", ended, err)
				}
				if _, live, err := getReleaseWindow(ctx, cs, "cicd", time.Now().UTC()); err != nil || live {
					t.Fatalf("release: %v %v", live, err)
				}
			})
		}
	}
}

func TestRequiredWindowStillRequired(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-research", "bifrost-build-research-dagster", "bifrost-build-market-data", "bifrost-build-flex-query", "bifrost-build-ib-gateway"} {
		t.Run(name, func(t *testing.T) {
			pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}}}
			s, dyn, _ := contractService(t, pipe, "unknown")
			assertContractRefused(t, s, dyn, name, factSHA1, nil, "Open one first: release.sh hold --what")
		})
	}
}

// The real API arbitrates absent-window creates with AlreadyExists and expired
// window updates with resourceVersion Conflict. putReleaseWindow rereads after
// either conflict (at most five attempts), then rejects the winning foreign who.
func TestRunWindowAcquisitionRace(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "create-already-exists", true: "update-conflict"}[expired], func(t *testing.T) {
			ctx := context.Background()
			cs := fake.NewSimpleClientset()
			now := time.Now().UTC()
			winner := windowRecord{Who: "winning-run", What: "bifrost-platform", StartedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(windowLifetime).Format(time.RFC3339)}
			if expired {
				if _, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: "old", What: "bifrost-platform"}, now.Add(-2*windowLifetime)); err != nil {
					t.Fatal(err)
				}
				replaceWindowBeforeUpdate(t, cs, winner)
			} else {
				cs.PrependReactor("create", "configmaps", func(a clienttesting.Action) (bool, runtime.Object, error) {
					cm := a.(clienttesting.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
					cm.Data[releaseWindowDataKey] = `{"who":"winning-run","what":"bifrost-platform","expires_at":"2099-01-01T00:00:00Z"}`
					if err := cs.Tracker().Create(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, cm, "cicd"); err != nil {
						t.Fatal(err)
					}
					return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, cm.Name)
				})
			}
			s, _, _ := contractService(t, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": "test", "namespace": "cicd"}}}, "unknown")
			s.SetClientsetForTest(cs)
			if _, err := s.takeRunWindow(ctx, "bifrost-deliver-platform", "losing-run"); err == nil || !strings.Contains(err.Error(), "winning-run") {
				t.Fatalf("loser: %v", err)
			}
		})
	}
}
