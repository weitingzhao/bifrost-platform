package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func TestRegisteredDeliveryExecutorRedactsSecret(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dyn := pipelineDyn(t)
	dyn.PrependReactor("create", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver token=SYNTHETIC_SECRET")
	})
	auditPath := filepath.Join(t.TempDir(), "audit.json")
	audit := actuation.NewAuditLog(auditPath)
	svc := wireStartPipeline(t, dyn, audit)
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
	if !strings.Contains(approved.Body.String(), "token=[redacted]") || strings.Contains(approved.Body.String(), "SYNTHETIC_SECRET") {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body.String())
	}
	stored, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("SYNTHETIC_SECRET")) || !bytes.Contains(stored, []byte("token=[redacted]")) {
		t.Fatalf("audit = %s", stored)
	}
	if strings.Contains(logs.String(), "SYNTHETIC_SECRET") || !strings.Contains(logs.String(), "[redacted]") {
		t.Fatalf("log = %s", logs.String())
	}
}
