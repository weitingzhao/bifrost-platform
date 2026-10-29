package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	kt "k8s.io/client-go/testing"
)

func TestSTGAccessProbeChecksEveryPermission(t *testing.T) {
	t.Parallel()
	for i := -1; i < len(RequiredSTGReleaseAccess()); i++ {
		t.Run(string(rune('A'+i+1)), func(t *testing.T) {
			t.Parallel()
			a, _, core := stgAdapter(t)
			seen := []authv1.ResourceAttributes{}
			core.PrependReactor("create", "selfsubjectaccessreviews", func(action kt.Action) (bool, runtime.Object, error) {
				request := action.(kt.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
				seen = append(seen, *request.Spec.ResourceAttributes)
				return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: len(seen)-1 != i}}, nil
			})
			err := a.ProbeAccess(context.Background())
			if i < 0 {
				if err != nil || !reflect.DeepEqual(seen, RequiredSTGReleaseAccess()) {
					t.Fatal(seen, err)
				}
			} else if err == nil || len(seen) != i+1 {
				t.Fatal("missing refusal", seen, err)
			}
			for _, action := range core.Actions() {
				if action.GetResource().Resource != "selfsubjectaccessreviews" {
					t.Fatal("persistent action", action)
				}
			}
		})
	}
}
func TestSTGAdmissionProbeUsesServerDryRunOnly(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "create denied", "patch denied", "finalizer stripped", "missing sample"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			sample := namedObject(t)
			before := sample.DeepCopy()
			created, patched := 0, 0
			// The stock dynamic fake discards CreateOptions/PatchOptions (client-go
			// v0.32). Exercise the real client's serialization against a fake HTTP API.
			client, err := dynamic.NewForConfigAndClient(&rest.Config{Host: "https://fake.invalid"}, &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				var result any
				code := 200
				switch r.Method {
				case "GET":
					if strings.HasSuffix(r.URL.Path, "/pipelines/"+STGReleasePipeline) {
						result = stgPipe(t).Object
					} else if strings.HasSuffix(r.URL.Path, "/pipelineruns/"+sample.GetName()) {
						if mode == "missing sample" {
							code = 404
						}
						result = sample.Object
					} else {
						t.Fatal("unexpected read", r.URL)
					}
				case "POST":
					created++
					if r.URL.Query().Get("dryRun") != metav1.DryRunAll {
						t.Fatal("persistent create", r.URL)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					obj := &unstructured.Unstructured{Object: body}
					obj.SetUID("probe-uid")
					obj.SetResourceVersion("1")
					if mode == "finalizer stripped" {
						obj.SetFinalizers(nil)
					}
					result = obj.Object
					if mode == "create denied" {
						code = 403
					}
				case "PATCH":
					patched++
					if r.URL.Query().Get("dryRun") != metav1.DryRunAll {
						t.Fatal("persistent patch", r.URL)
					}
					if r.Header.Get("Content-Type") != "application/json-patch+json" {
						t.Fatal(r.Header)
					}
					var ops []map[string]any
					if err := json.NewDecoder(r.Body).Decode(&ops); err != nil {
						t.Fatal(err)
					}
					if len(ops) != 4 || ops[0]["value"] != string(sample.GetUID()) || ops[1]["value"] != sample.GetResourceVersion() {
						t.Fatal("unguarded patch", ops)
					}
					result = sample.Object
					if mode == "patch denied" {
						code = 403
					}
				default:
					t.Fatal("persistent unexpected action", r.Method)
				}
				if code != 200 {
					result = map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": "Forbidden", "message": mode, "code": code}
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			a := &STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return client, nil }}
			next := stgInput()
			next.Try = 2
			err = a.ProbeFinalizerAdmission(context.Background(), next, stgInput(), sample.GetUID())
			if mode == "control" {
				if err != nil || created != 1 || patched != 1 {
					t.Fatal(err, created, patched)
				}
			} else if err == nil {
				t.Fatal("probe accepted refusal")
			}
			if !reflect.DeepEqual(sample, before) {
				t.Fatal("probe changed sample")
			}
		})
	}
}
