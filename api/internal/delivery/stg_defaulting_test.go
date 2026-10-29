package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	kt "k8s.io/client-go/testing"
)

// This reproduces only the mandatory timeout part of Tekton v1.6.0
// PipelineRunSpec.SetDefaults, not an actual admission webhook invocation.
// https://github.com/tektoncd/pipeline/blob/v1.6.0/pkg/apis/pipeline/v1/pipelinerun_defaults.go#L51-L56
// Review 706 B-01: the pre-fix implementation rejected every timeout object.
func stgAdmissionDefaultTimeout(t *testing.T, obj *unstructured.Unstructured) {
	t.Helper()
	stgSet(t, obj, "1h0m0s", "spec", "timeouts", "pipeline")
}

func stgAdmissionDefaults(t *testing.T, obj *unstructured.Unstructured, mode string) {
	t.Helper()
	if mode == "control" {
		return
	}
	stgAdmissionDefaultTimeout(t, obj)
	if mode == "tekton_timeout" {
		return
	}
	// A non-1h configured default must work too. Exercise the real core/v1
	// template JSON serialization used inside Tekton's typed PipelineRun.
	stgSet(t, obj, "30m0s", "spec", "timeouts", "pipeline")
	stgSet(t, obj, "default", "spec", "taskRunTemplate", "serviceAccountName")
	workspaces := obj.Object["spec"].(map[string]any)["workspaces"].([]any)
	workspace := workspaces[0].(map[string]any)
	raw, err := json.Marshal(workspace["volumeClaimTemplate"])
	if err != nil {
		t.Fatal(err)
	}
	var pvc corev1.PersistentVolumeClaim
	if err := json.Unmarshal(raw, &pvc); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(pvc)
	if err != nil {
		t.Fatal(err)
	}
	var serialized map[string]any
	if err := json.Unmarshal(raw, &serialized); err != nil {
		t.Fatal(err)
	}
	workspace["volumeClaimTemplate"] = serialized
}

// Each assertion expresses the expected valid behavior. The control passes;
// the otherwise identical server-defaulted object must also be recoverable.
func TestSTGDefaultedRunAtEveryAdoption(t *testing.T) {
	for _, entry := range []string{"existing", "read", "created", "lost", "409"} {
		t.Run(entry, func(t *testing.T) {
			for _, mode := range []string{"control", "tekton_timeout", "tekton_defaults"} {
				t.Run(mode, func(t *testing.T) {
					obj := namedObject(t)
					stgAdmissionDefaults(t, obj, mode)
					a, dyn, _ := stgAdapter(t)
					creates := 0
					if entry == "existing" || entry == "read" {
						if err := dyn.Tracker().Create(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
							t.Fatal(err)
						}
					}
					dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
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
					var err error
					ok := true
					if entry == "read" {
						got, err = a.ReadNamedDeliverRun(context.Background(), stgInput())
					} else {
						resp, run, e := a.CreateNamedDeliverRun(context.Background(), stgInput())
						ok, got, err = resp.OK, run, e
					}
					wantCreates := 1
					if entry == "existing" || entry == "read" {
						wantCreates = 0
					}
					if creates != wantCreates {
						t.Fatalf("creates=%d want=%d", creates, wantCreates)
					}
					if err != nil || !ok || got == nil || got.GetUID() != obj.GetUID() {
						t.Fatalf("legitimate run rejected: OK=%v nil_run=%v creates=%d err=%v", ok, got == nil, creates, err)
					}
				})
			}
		})
	}
}

func TestSTGDefaultedRunFinalizerCleanup(t *testing.T) {
	for _, mode := range []string{"control", "tekton_timeout", "tekton_defaults"} {
		t.Run(mode, func(t *testing.T) {
			obj := namedObject(t)
			successful(obj)
			stgAdmissionDefaults(t, obj, mode)
			a, dyn, _ := stgAdapter(t, obj)
			a.Archive = stgArchive{present: true, id: STGRunIdentity{STGReleaseNamespace, obj.GetName(), obj.GetUID()}}
			err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), obj.GetUID())
			patches := 0
			for _, action := range dyn.Actions() {
				if action.GetVerb() == "patch" {
					patches++
				}
			}
			if err != nil || patches != 1 {
				t.Fatalf("archived terminal run cannot be cleaned: patches=%d err=%v", patches, err)
			}
		})
	}
}

func TestSTGDefaultedAdmissionProbe(t *testing.T) {
	for _, mode := range []string{"control", "tekton_timeout", "tekton_defaults"} {
		t.Run(mode, func(t *testing.T) {
			sample := namedObject(t)
			stgAdmissionDefaults(t, sample, mode)
			creates, patches := 0, 0
			client, err := dynamic.NewForConfigAndClient(&rest.Config{Host: "https://fake.invalid"}, &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				var obj *unstructured.Unstructured
				switch r.Method {
				case "GET":
					if strings.Contains(r.URL.Path, "/pipelines/") {
						obj = stgPipe(t)
					} else {
						obj = sample.DeepCopy()
					}
				case "POST":
					creates++
					if r.URL.Query().Get("dryRun") != metav1.DryRunAll {
						t.Fatal("persistent create")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					obj = &unstructured.Unstructured{Object: body}
					obj.SetUID("dryrun-uid")
					obj.SetResourceVersion("1")
					stgAdmissionDefaults(t, obj, mode)
				case "PATCH":
					patches++
					if r.URL.Query().Get("dryRun") != metav1.DryRunAll {
						t.Fatal("persistent patch")
					}
					obj = sample.DeepCopy()
					obj.SetFinalizers(nil)
				default:
					t.Fatal("unexpected method", r.Method)
				}
				raw, err := json.Marshal(obj.Object)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			a := &STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return client, nil }}
			next := stgInput()
			next.Try++
			err = a.ProbeFinalizerAdmission(context.Background(), next, stgInput(), sample.GetUID())
			if err != nil || creates != 1 || patches != 1 {
				t.Fatalf("admission probe rejected allowed defaulting: dry_creates=%d dry_patches=%d err=%v", creates, patches, err)
			}
		})
	}
}

func TestSTGDefaultedExecutionStillRejectsDrift(t *testing.T) {
	for _, mode := range []string{
		"control", "task sa", "template sa", "missing sa", "empty sa", "workspace", "pod template",
		"unknown spec", "unknown timeout", "tasks timeout", "finally timeout",
		"invalid timeout", "negative timeout", "numeric timeout", "null timeout",
		"empty timeout", "pvc metadata", "pvc status", "pvc spec",
	} {
		t.Run(mode, func(t *testing.T) {
			obj := namedObject(t)
			stgAdmissionDefaults(t, obj, "tekton_defaults")
			spec := obj.Object["spec"].(map[string]any)
			timeouts := spec["timeouts"].(map[string]any)
			claim := spec["workspaces"].([]any)[0].(map[string]any)["volumeClaimTemplate"].(map[string]any)
			switch mode {
			case "task sa":
				spec["taskRunSpecs"].([]any)[1].(map[string]any)["serviceAccountName"] = "default"
			case "template sa":
				stgSet(t, obj, "tekton-deliver", "spec", "taskRunTemplate", "serviceAccountName")
			case "missing sa":
				unstructured.RemoveNestedField(obj.Object, "spec", "taskRunTemplate", "serviceAccountName")
			case "empty sa":
				stgSet(t, obj, "", "spec", "taskRunTemplate", "serviceAccountName")
			case "workspace":
				spec["workspaces"] = []any{map[string]any{"name": "build-context", "emptyDir": map[string]any{}}}
			case "pod template":
				stgSet(t, obj, true, "spec", "taskRunTemplate", "podTemplate", "hostNetwork")
			case "unknown spec":
				spec["unknownExecution"] = true
			case "unknown timeout":
				timeouts["unknown"] = "1h"
			case "tasks timeout":
				timeouts["tasks"] = "20m"
			case "finally timeout":
				timeouts["finally"] = "10m"
			case "invalid timeout":
				timeouts["pipeline"] = "invalid"
			case "negative timeout":
				timeouts["pipeline"] = "-1h"
			case "numeric timeout":
				timeouts["pipeline"] = int64(3600)
			case "null timeout":
				spec["timeouts"] = nil
			case "empty timeout":
				spec["timeouts"] = map[string]any{}
			case "pvc metadata":
				claim["metadata"].(map[string]any)["labels"] = map[string]any{"unexpected": "value"}
			case "pvc status":
				claim["status"].(map[string]any)["phase"] = "Bound"
			case "pvc spec":
				claim["spec"].(map[string]any)["storageClassName"] = "other"
			}
			before := obj.DeepCopy()
			err := MatchNamedDeliverRun(obj, stgInput(), true)
			if mode == "control" {
				if err != nil {
					t.Fatal("defaulted control rejected", err)
				}
			} else if !errors.Is(err, ErrSTGCollision) {
				t.Fatal("defaulting hid execution drift", err)
			}
			if !reflect.DeepEqual(obj.Object, before.Object) {
				t.Fatal("identity check rewrote observed evidence")
			}
		})
	}
}

func TestSTGDefaultingRequestAndReadback(t *testing.T) {
	obj := namedObject(t)
	if sa, _, _ := unstructured.NestedString(obj.Object, "spec", "taskRunTemplate", "serviceAccountName"); sa != "default" {
		t.Fatal("request does not pin default SA", sa)
	}
	if _, present, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "timeouts"); present {
		t.Fatal("v1 request unexpectedly specifies timeouts")
	}
	for _, duration := range []string{"1h", "1h0m0s", "30m0s", "0s"} {
		t.Run(duration, func(t *testing.T) {
			observed := obj.DeepCopy()
			stgSet(t, observed, duration, "spec", "timeouts", "pipeline")
			if err := MatchNamedDeliverRun(observed, stgInput(), true); err != nil {
				t.Fatal("known default representation rejected", err)
			}
		})
	}
}
