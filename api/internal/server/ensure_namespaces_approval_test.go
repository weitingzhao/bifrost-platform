//bifrost:owner approvals

package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// 2026-11-16, card #585: the Owner approved ensure_bifrost_namespaces and it
// failed at once with "no executor". Decision 2026-11-25 #783 (wall:116) made
// it tier B: a card is refused ("call directly") and the registered executor
// runs it. This checks both against a fake cluster: the missing namespace
// exists afterwards and nothing else is created.
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
	if created.Code != http.StatusBadRequest || !strings.Contains(created.Body.String(), "call directly") {
		t.Fatalf("create = %d %s, want call directly", created.Code, created.Body.String())
	}
	if _, err := actions.Execute(t.Context(), "ensure_bifrost_namespaces", map[string]any{"names": []any{"bifrost-bus"}}); err != nil {
		t.Fatalf("execute: %v", err)
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
