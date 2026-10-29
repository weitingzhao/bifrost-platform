package delivery

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

// Review 702 R-02: none of these mutations changes the v1 fingerprint.
// Check all adoption paths, including server-mutated Create responses and
// recovery after an uncertain Create, against the same successful control.
func TestSTGExecutionIdentityAtEveryAdoption(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"existing", "read", "created", "lost", "409"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []string{"control", "task sa", "missing task", "extra task", "duplicate task", "template sa", "node selector", "workspace", "extra workspace", "timeout", "extra spec"} {
				t.Run(mode, func(t *testing.T) {
					t.Parallel()
					obj := namedObject(t)
					// Tekton-owned metadata/status is allowed in every case.
					labels := obj.GetLabels()
					labels["tekton.dev/pipeline"] = STGReleasePipeline
					obj.SetLabels(labels)
					annotations := obj.GetAnnotations()
					annotations["chains.tekton.dev/signed"] = "true"
					obj.SetAnnotations(annotations)
					successful(obj)
					spec := obj.Object["spec"].(map[string]any)
					tasks := spec["taskRunSpecs"].([]any)
					switch mode {
					case "task sa":
						tasks[1].(map[string]any)["serviceAccountName"] = "default"
					case "missing task":
						spec["taskRunSpecs"] = tasks[:2]
					case "extra task":
						spec["taskRunSpecs"] = append(tasks, map[string]any{"pipelineTaskName": "clone-platform", "serviceAccountName": "tekton-deliver"})
					case "duplicate task":
						spec["taskRunSpecs"] = append(tasks, tasks[0])
					case "template sa":
						stgSet(t, obj, "tekton-deliver", "spec", "taskRunTemplate", "serviceAccountName")
					case "node selector":
						stgSet(t, obj, "arm64", "spec", "taskRunTemplate", "podTemplate", "nodeSelector", "kubernetes.io/arch")
					case "workspace":
						spec["workspaces"] = []any{map[string]any{"name": "build-context", "persistentVolumeClaim": map[string]any{"claimName": "other-build"}}}
					case "extra workspace":
						spec["workspaces"] = append(spec["workspaces"].([]any), map[string]any{"name": "other", "emptyDir": map[string]any{}})
					case "timeout":
						spec["timeouts"] = map[string]any{"pipeline": "1h"}
					case "extra spec":
						spec["taskRunTemplate"].(map[string]any)["podTemplate"].(map[string]any)["hostNetwork"] = true
					}
					id, err := stgInput().Identity()
					if err != nil {
						t.Fatal(err)
					}
					params, err := namedStrings(obj, "spec", "params")
					if err != nil {
						t.Fatal(err)
					}
					hash, err := stgRunFingerprint(params, id.Labels, id.Annotations)
					if err != nil || hash != id.Fingerprint {
						t.Fatal("mutation changed fingerprint", hash, err)
					}
					a, dyn, _ := stgAdapter(t)
					creates := 0
					if entry == "existing" || entry == "read" {
						if err := dyn.Tracker().Create(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
							t.Fatal(err)
						}
					}
					dyn.PrependReactor("create", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
						creates++
						if err := dyn.Tracker().Create(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
							t.Fatal(err)
						}
						if entry == "lost" {
							return true, nil, context.DeadlineExceeded
						}
						if entry == "409" {
							return true, nil, apierrors.NewAlreadyExists(pipelineRunGVR.GroupResource(), obj.GetName())
						}
						return true, obj.DeepCopy(), nil
					})
					var got *unstructured.Unstructured
					if entry == "read" {
						got, err = a.ReadNamedDeliverRun(context.Background(), stgInput())
					} else {
						resp, run, e := a.CreateNamedDeliverRun(context.Background(), stgInput())
						got, err = run, e
						if resp.OK != (mode == "control") {
							t.Errorf("actuation OK=%v, error=%v", resp.OK, err)
						}
					}
					wantCreates := 1
					if entry == "existing" || entry == "read" {
						wantCreates = 0
					}
					if creates != wantCreates {
						t.Errorf("creates=%d, want %d", creates, wantCreates)
					}
					if mode == "control" {
						if err != nil || got == nil || got.GetUID() != obj.GetUID() {
							t.Fatal("control", got, err)
						}
					} else if !errors.Is(err, ErrSTGCollision) || got != nil {
						t.Fatal("execution collision was adopted", got, err)
					}
					for _, action := range dyn.Actions() {
						if action.GetVerb() != "get" && action.GetVerb() != "create" {
							t.Fatal("mutated collision", action)
						}
					}
				})
			}
		})
	}
}

func TestSTGExecutionContractRejectsChangedConstructor(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "timeout annotation", "arch annotation", "migration removed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			a, dyn, _ := stgAdapter(t)
			pipe := stgPipe(t)
			switch mode {
			case "timeout annotation":
				pipe.SetAnnotations(map[string]string{"bifrost.io/run-timeout": "1h"})
			case "arch annotation":
				pipe.SetAnnotations(map[string]string{"bifrost.io/run-arch": "amd64"})
			case "migration removed":
				tasks, _, _ := unstructured.NestedSlice(pipe.Object, "spec", "tasks")
				kept := []any{}
				for _, task := range tasks {
					if task.(map[string]any)["name"] != "agentrt-migrate" {
						kept = append(kept, task)
					}
				}
				stgSet(t, pipe, kept, "spec", "tasks")
			}
			if err := dyn.Tracker().Update(pipelineGVR, pipe, STGReleaseNamespace); err != nil {
				t.Fatal(err)
			}
			count := trackSTGCreates(t, dyn, "")
			_, _, err := a.CreateNamedDeliverRun(context.Background(), stgInput())
			if mode == "control" {
				if err != nil || *count != 1 {
					t.Fatal("control", err, *count)
				}
			} else if err == nil || *count != 0 {
				t.Fatal("changed constructor allowed a create", err, *count)
			}
		})
	}
}

func TestSTGExecutionRecoveryDoesNotReadCurrentPipeline(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"existing", "read"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			obj := namedObject(t)
			a, dyn, _ := stgAdapter(t, obj)
			dyn.PrependReactor("get", "pipelines", func(kt.Action) (bool, runtime.Object, error) {
				t.Error("recovery consulted today's Pipeline")
				return true, nil, errors.New("unavailable")
			})
			count := trackSTGCreates(t, dyn, "")
			var got *unstructured.Unstructured
			var err error
			if entry == "read" {
				got, err = a.ReadNamedDeliverRun(context.Background(), stgInput())
			} else {
				_, got, err = a.CreateNamedDeliverRun(context.Background(), stgInput())
			}
			if err != nil || got == nil || got.GetUID() != obj.GetUID() || *count != 0 {
				t.Fatal(got, err, *count)
			}
		})
	}
}
