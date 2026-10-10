package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

func storeThenUnexpectedEOF(dyn *dynamicfake.FakeDynamicClient) {
	dyn.PrependReactor("create", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		ca := action.(k8stesting.CreateAction)
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
		return true, nil, io.ErrUnexpectedEOF
	})
}

func TestRegisteredStartPipelineUnexpectedEOFIsUnknown(t *testing.T) {
	dyn := pipelineDyn(t)
	storeThenUnexpectedEOF(dyn)
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
	text := approved.Body.String()
	if approved.Code != http.StatusOK || !strings.Contains(text, `"status":"unknown"`) || !strings.Contains(text, "create outcome unknown") || strings.Contains(text, `"status":"failed"`) || strings.Contains(text, `"status":"executed"`) {
		t.Fatalf("approve = %d %s", approved.Code, text)
	}
	list, err := dyn.Resource(testRunGVR).Namespace("cicd").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("runs = %d, want the one create whose response was lost", len(list.Items))
	}
}

func TestRegisteredApplyUnexpectedEOFIsUnknown(t *testing.T) {
	const planID = "plan-1dfc9375-1791567153"
	plan := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name":      planID,
			"namespace": "cicd",
			"labels": map[string]any{
				"bifrost.io/trigger": "platform-api",
				"bifrost.io/mode":    "plan",
			},
		},
		"spec": map[string]any{
			"pipelineRef": map[string]any{"name": "apply-pipe"},
			"params": []any{
				map[string]any{"name": "mode", "value": "plan"},
				map[string]any{"name": "repo", "value": "example-repo"},
				map[string]any{"name": "path", "value": "k8s/app"},
				map[string]any{"name": "commit", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
			"results": []any{
				map[string]any{"name": "policy", "value": "pass"},
				map[string]any{"name": "objects", "value": `[{"namespace":"bifrost-dev","kind":"ConfigMap","name":"x","tier":"B"}]`},
			},
		},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}: "PipelineRunList",
		}, plan)
	storeThenUnexpectedEOF(dyn)
	work := &workactions.Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe", ServiceAccount: "sa"},
			Apply: actuationpolicy.Apply{
				Namespaces:       map[string]string{"bifrost-dev": "B"},
				Resources:        []actuationpolicy.Resource{{Kind: "ConfigMap"}},
				DaemonDeployment: "daemon",
			},
		},
		Clients: workactions.Clients{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }},
	}
	s := &Server{work: &workactions.Handler{Svc: work}}
	s.bindActionExecutors()
	svc := approvals.New(filepath.Join(t.TempDir(), "approvals"), nil)
	h := serveApprovals(svc)
	created := doApproval(h, http.MethodPost, "/approvals", `{"action":"apply_manifest","reason":"apply","params":{"plan_id":"`+planID+`"}}`)
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
	text := approved.Body.String()
	if approved.Code != http.StatusOK || !strings.Contains(text, `"status":"unknown"`) || !strings.Contains(text, "create outcome unknown") || strings.Contains(text, `"status":"executed"`) {
		t.Fatalf("approve = %d %s", approved.Code, text)
	}
}
