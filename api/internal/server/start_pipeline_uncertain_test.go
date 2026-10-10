package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

const prodPipeline = "bifrost-deliver-platform-prod"

var (
	testPipelineGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelines"}
	testRunGVR      = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
)

func prodPipelineObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "Pipeline",
		"metadata":   map[string]any{"name": prodPipeline, "namespace": "cicd"},
	}}
}

func pipelineDyn(t *testing.T, extra ...runtime.Object) *dynamicfake.FakeDynamicClient {
	t.Helper()
	objs := []runtime.Object{prodPipelineObject()}
	objs = append(objs, extra...)
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			testPipelineGVR: "PipelineList",
			testRunGVR:      "PipelineRunList",
		}, objs...)
}

func wireStartPipeline(t *testing.T, dyn dynamic.Interface, audit *actuation.AuditLog) *approvals.Service {
	t.Helper()
	// Kaniko preflight needs one Ready amd64 node. Ref probes must not leave
	// the machine: point Gitea at a server that answers immediately.
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no gitea in unit test", http.StatusBadGateway)
	}))
	t.Cleanup(gitea.Close)
	t.Setenv("GITEA_BASE", gitea.URL)
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "ci"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			NodeInfo:   corev1.NodeSystemInfo{Architecture: "amd64"},
		},
	}
	del := delivery.NewService(&config.ClusterEntry{})
	del.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	del.SetClientsetForTest(fake.NewSimpleClientset(node))
	s := &Server{delivery: delivery.NewHandlerForTest(del, audit)}
	s.bindActionExecutors()
	return approvals.New(filepath.Join(t.TempDir(), "approvals"), audit)
}

func serveApprovals(svc *approvals.Service) http.Handler {
	r := chi.NewRouter()
	r.Post("/approvals", svc.HandleCreate)
	r.Get("/approvals/{id}", svc.HandleGet)
	r.Post("/approvals/{id}/approve", svc.HandleApprove)
	return r
}

func doApproval(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Bifrost-Session", "s")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The registered start_pipeline_run executor (HTTP handler, not work.Apply)
// must leave a create timeout as unknown.
func TestRegisteredStartPipelineRunTimeoutIsUnknown(t *testing.T) {
	dyn := pipelineDyn(t)
	dyn.PrependReactor("create", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ca := action.(k8stesting.CreateAction)
		// The run spec carries []map[string]any, which Unstructured cannot deep-copy.
		raw, err := json.Marshal(ca.GetObject())
		if err != nil {
			return true, nil, err
		}
		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(raw, &obj.Object); err != nil {
			return true, nil, err
		}
		if err := dyn.Tracker().Create(ca.GetResource(), obj, ca.GetNamespace()); err != nil {
			return true, nil, err
		}
		return true, nil, apierrors.NewTimeoutError("timeout", 1)
	})
	svc := wireStartPipeline(t, dyn, nil)
	h := serveApprovals(svc)
	created := doApproval(h, http.MethodPost, "/approvals", `{"action":"start_pipeline_run","reason":"ship","params":{"name":"`+prodPipeline+`","revision":"main","who":"owner"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &row); err != nil || row.ID == "" {
		t.Fatalf("created id: %v %s", err, created.Body.String())
	}
	opened := doApproval(h, http.MethodGet, "/approvals/"+row.ID, "")
	var openedRow struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &openedRow); err != nil || openedRow.ApprovalLine == "" {
		t.Fatalf("get = %d %s", opened.Code, opened.Body.String())
	}
	body, _ := json.Marshal(map[string]any{
		"channel": "console", "approval_line": openedRow.ApprovalLine, "params_hash": openedRow.ParamsHash,
	})
	approved := doApproval(h, http.MethodPost, "/approvals/"+row.ID+"/approve", string(body))
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), `"status":"unknown"`) || !strings.Contains(approved.Body.String(), "needs checking") {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body.String())
	}
	if strings.Contains(approved.Body.String(), `"status":"failed"`) {
		t.Fatalf("create timeout was reported failed: %s", approved.Body.String())
	}
	list, err := dyn.Resource(testRunGVR).Namespace("cicd").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("runs = %d, want 1", len(list.Items))
	}
	want := actions.ObjectName(prodPipeline, actions.CreateAttempt{ApprovalID: row.ID, Attempt: 1})
	got := list.Items[0]
	if got.GetName() != want {
		t.Fatalf("run name = %q, want %q", got.GetName(), want)
	}
	ann := got.GetAnnotations()
	if ann[actions.AnnApprovalID] != row.ID || ann[actions.AnnAttempt] != "1" || ann[actions.AnnParamsHash] != openedRow.ParamsHash {
		t.Fatalf("annotations = %#v", ann)
	}
}

func TestRegisteredStartPipelineRunRejectsForeignObject(t *testing.T) {
	dyn := pipelineDyn(t)
	svc := wireStartPipeline(t, dyn, nil)
	h := serveApprovals(svc)
	created := doApproval(h, http.MethodPost, "/approvals", `{"action":"start_pipeline_run","reason":"ship","params":{"name":"`+prodPipeline+`","revision":"main","who":"owner"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	opened := doApproval(h, http.MethodGet, "/approvals/"+row.ID, "")
	var openedRow struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &openedRow); err != nil {
		t.Fatal(err)
	}
	name := actions.ObjectName(prodPipeline, actions.CreateAttempt{ApprovalID: row.ID, Attempt: 1})
	foreign := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name": name, "namespace": "cicd",
			"annotations": map[string]any{
				actions.AnnApprovalID: "appr_ffffffffffffffff",
				actions.AnnAttempt:    "1",
				actions.AnnParamsHash: strings.Repeat("cd", 32),
			},
		},
	}}
	if _, err := dyn.Resource(testRunGVR).Namespace("cicd").Create(context.Background(), foreign, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"channel": "console", "approval_line": openedRow.ApprovalLine, "params_hash": openedRow.ParamsHash,
	})
	approved := doApproval(h, http.MethodPost, "/approvals/"+row.ID+"/approve", string(body))
	if !strings.Contains(approved.Body.String(), `"status":"unknown"`) || !strings.Contains(approved.Body.String(), name) || strings.Contains(approved.Body.String(), `"status":"executed"`) {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body.String())
	}
}

// Matching annotations are still not adoption. The object has to be checked
// by hand and the approval is unknown.
func TestRegisteredStartPipelineRunMatchingAnnotationsAreUnknown(t *testing.T) {
	dyn := pipelineDyn(t)
	svc := wireStartPipeline(t, dyn, nil)
	h := serveApprovals(svc)
	created := doApproval(h, http.MethodPost, "/approvals", `{"action":"start_pipeline_run","reason":"ship","params":{"name":"`+prodPipeline+`","revision":"main","who":"owner"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	opened := doApproval(h, http.MethodGet, "/approvals/"+row.ID, "")
	var openedRow struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &openedRow); err != nil {
		t.Fatal(err)
	}
	name := actions.ObjectName(prodPipeline, actions.CreateAttempt{ApprovalID: row.ID, Attempt: 1})
	own := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name": name, "namespace": "cicd",
			"annotations": map[string]any{
				actions.AnnApprovalID: row.ID,
				actions.AnnAttempt:    "1",
				actions.AnnParamsHash: openedRow.ParamsHash,
			},
		},
	}}
	if _, err := dyn.Resource(testRunGVR).Namespace("cicd").Create(context.Background(), own, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"channel": "console", "approval_line": openedRow.ApprovalLine, "params_hash": openedRow.ParamsHash,
	})
	approved := doApproval(h, http.MethodPost, "/approvals/"+row.ID+"/approve", string(body))
	if !strings.Contains(approved.Body.String(), `"status":"unknown"`) || !strings.Contains(approved.Body.String(), name) || !strings.Contains(approved.Body.String(), "checked by hand") || strings.Contains(approved.Body.String(), `"status":"executed"`) {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body.String())
	}
}
