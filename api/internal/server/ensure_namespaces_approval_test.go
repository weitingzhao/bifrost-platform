//bifrost:owner approvals

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// 2026-11-16, card #585: the Owner approved ensure_bifrost_namespaces and it
// failed at once with "no executor". This walks the path a card takes after
// approval (create → approve → registered executor → guarded handler) against
// a fake cluster and checks that the missing namespace exists afterwards.
func TestApprovedEnsureBifrostNamespacesCreatesTheNamespace(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	if _, err := clientset.CoreV1().Namespaces().Create(t.Context(), nsObj("data"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Clusters: &config.ClustersFile{Clusters: []config.ClusterEntry{
		{ID: "test", BifrostNamespaces: []string{"data", "bifrost", "bifrost-bus"}},
	}}}
	ch := cluster.NewHandler(cfg, nil)
	ch.Service().SetClientFactoryForTest(func() (kubernetes.Interface, string, error) { return clientset, "fake", nil })
	s := &Server{cluster: ch}
	s.bindActionExecutors()

	h := serveApprovals(approvals.New(filepath.Join(t.TempDir(), "approvals"), nil))
	created := doApproval(h, http.MethodPost, "/approvals", `{"action":"ensure_bifrost_namespaces","reason":"bus namespace","params":{"names":["bifrost-bus"]}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &row); err != nil || row.ID == "" {
		t.Fatalf("created id: %v %s", err, created.Body.String())
	}
	var opened struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
	}
	got := doApproval(h, http.MethodGet, "/approvals/"+row.ID, "")
	if err := json.Unmarshal(got.Body.Bytes(), &opened); err != nil || opened.ApprovalLine == "" {
		t.Fatalf("get = %d %s", got.Code, got.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"channel": "console", "approval_line": opened.ApprovalLine, "params_hash": opened.ParamsHash})
	approved := doApproval(h, http.MethodPost, "/approvals/"+row.ID+"/approve", string(body))
	text := approved.Body.String()
	if approved.Code != http.StatusOK || !strings.Contains(text, `"status":"executed"`) || strings.Contains(text, "no executor") {
		t.Fatalf("approve = %d %s", approved.Code, text)
	}
	if _, err := clientset.CoreV1().Namespaces().Get(t.Context(), "bifrost-bus", metav1.GetOptions{}); err != nil {
		t.Fatalf("bifrost-bus not created after approval: %v", err)
	}
	if _, err := clientset.CoreV1().Namespaces().Get(t.Context(), "bifrost", metav1.GetOptions{}); err == nil {
		t.Fatal("bifrost was created although the card named only bifrost-bus")
	}
}

func nsObj(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}
