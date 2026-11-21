package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func recoveryFixture(t *testing.T) (*fake.Clientset, *dynfake.FakeDynamicClient) {
	t.Helper()
	r := windowRecord{Who: "run", Reason: "PipelineRun/run", What: "bifrost-platform", StartedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), ExpiresAt: time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	cs := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd", ResourceVersion: "1"}, Data: map[string]string{releaseWindowDataKey: string(body)}})
	cs.PrependReactor("create", "selfsubjectaccessreviews", func(a kt.Action) (bool, runtime.Object, error) {
		attr := a.(kt.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview).Spec.ResourceAttributes
		if attr.Namespace != "cicd" || attr.Resource != "configmaps" || attr.Name != releaseWindowConfigMap || attr.Verb != "update" {
			t.Fatalf("wrong access review: %+v", attr)
		}
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	run := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": "run", "namespace": "cicd"}}}
	return cs, dynfake.NewSimpleDynamicClient(runtime.NewScheme(), run)
}

func TestRecoverRunWindow(t *testing.T) {
	for _, scenario := range []string{"running", "succeeded", "failed", "session", "reason-mismatch", "expired", "ended", "malformed", "absent", "missing-run", "run-error", "read-only", "access-error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			cs, dyn := recoveryFixture(t)
			cms := cs.CoreV1().ConfigMaps("cicd")
			cm, _ := cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
			r, _ := parseWindowCM(cm)
			res := dyn.Resource(pipelineRunGVR).Namespace("cicd")
			switch scenario {
			case "succeeded", "failed":
				run, _ := res.Get(ctx, "run", metav1.GetOptions{})
				status := "True"
				if scenario == "failed" {
					status = "False"
				}
				run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}}
				if _, err := res.Update(ctx, run, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "session":
				r.Who = "release-script"
			case "reason-mismatch":
				r.Reason = "manual"
			case "expired":
				r.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			case "ended":
				r.EndedAt = time.Now().UTC().Format(time.RFC3339)
			case "missing-run":
				if err := res.Delete(ctx, "run", metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			case "run-error":
				dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unavailable") })
			case "read-only", "access-error":
				cs.PrependReactor("create", "selfsubjectaccessreviews", func(kt.Action) (bool, runtime.Object, error) {
					if scenario == "access-error" {
						return true, nil, errors.New("unavailable")
					}
					return true, &authv1.SelfSubjectAccessReview{}, nil
				})
			}
			body, _ := json.Marshal(r)
			cm.Data[releaseWindowDataKey] = string(body)
			if scenario == "malformed" {
				cm.Data[releaseWindowDataKey] = "{"
			}
			if _, err := cms.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "absent" {
				if err := cms.Delete(ctx, releaseWindowConfigMap, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			cs.ClearActions()
			var events []WindowEvent
			w, reason, err := recoverRunWindow(ctx, cs, res, "cicd", func(_ context.Context, e WindowEvent) { events = append(events, e) })
			wantErr := scenario == "run-error" || scenario == "access-error"
			if (err != nil) != wantErr {
				t.Fatalf("reason=%s err=%v", reason, err)
			}
			if (w != nil) != (scenario == "running") {
				t.Fatalf("window=%+v reason=%s", w, reason)
			}
			writes := 0
			for _, a := range cs.Actions() {
				if a.GetVerb() == "update" {
					writes++
				}
			}
			wantWrites := 0
			if scenario == "running" || scenario == "succeeded" || scenario == "failed" {
				wantWrites = 1
			}
			if writes != wantWrites {
				t.Fatalf("writes=%d want=%d", writes, wantWrites)
			}
			if scenario == "succeeded" || scenario == "failed" {
				cm, _ = cms.Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
				ended, _ := parseWindowCM(cm)
				if ended.EndedAt == "" || len(events) != 1 || events[0].Action != "released" {
					t.Fatalf("not released: %+v events=%+v", ended, events)
				}
			}
			if w != nil { // Recovered lease uses the existing observer for later completion.
				if err := res.Delete(ctx, "run", metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				ended, err := w.observe(ctx, res, "run")
				if err != nil || !ended {
					t.Fatalf("follow deletion: %v %v", ended, err)
				}
			}
		})
	}
}

func TestRecoverRunWindowVersionFences(t *testing.T) {
	for _, scenario := range []string{"other-recovery", "session-takeover", "update-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			cs, dyn := recoveryFixture(t)
			res := dyn.Resource(pipelineRunGVR).Namespace("cicd")
			reads := 0
			dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
				reads++
				if reads == 1 {
					cm, err := cs.CoreV1().ConfigMaps("cicd").Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					cm.ResourceVersion = "2"
					if scenario == "session-takeover" {
						r, _ := parseWindowCM(cm)
						r.Who = "session"
						r.Reason = "manual"
						body, _ := json.Marshal(r)
						cm.Data[releaseWindowDataKey] = string(body)
					}
					if scenario != "update-conflict" {
						if _, err := cs.CoreV1().ConfigMaps("cicd").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
							t.Fatal(err)
						}
					}
				}
				return false, nil, nil
			})
			if scenario == "update-conflict" {
				cs.PrependReactor("update", "configmaps", func(kt.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, releaseWindowConfigMap, fmt.Errorf("stale version"))
				})
			}
			w, _, err := recoverRunWindow(ctx, cs, res, "cicd", nil)
			if w != nil || err == nil {
				t.Fatalf("stale recovery survived: %+v %v", w, err)
			}
			if scenario != "update-conflict" && !strings.Contains(err.Error(), "window changed") {
				t.Fatal(err)
			}
			if scenario != "update-conflict" {
				// A fresh startup attempt may acquire the new version, but
				// must still refuse a session that took over in the meantime.
				w, _, err = recoverRunWindow(ctx, cs, res, "cicd", nil)
				if err != nil || (w != nil) != (scenario == "other-recovery") {
					t.Fatalf("retry: window=%+v err=%v", w, err)
				}
			}
		})
	}
}
