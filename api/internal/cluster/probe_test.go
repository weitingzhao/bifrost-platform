package cluster

import (
	"os"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSummaryMissingKubeconfig(t *testing.T) {
	t.Setenv("PLATFORM_KUBECONFIG", t.TempDir()+"/missing.yaml")

	entry := &config.ClusterEntry{
		ID:            "test",
		Label:         "Test",
		Distribution:  "k3s",
		KubeconfigEnv: "PLATFORM_KUBECONFIG",
	}
	svc := NewService(entry)
	sum := svc.Summary(t.Context())

	if sum.Reachability != probe.ReachFail {
		t.Fatalf("reachability: got %s want fail", sum.Reachability)
	}
	if sum.Detail == "" {
		t.Fatal("expected detail message")
	}
}

func TestSyncDisabled(t *testing.T) {
	t.Setenv("PLATFORM_CLUSTER_SYNC_ENABLED", "0")
	entry := &config.ClusterEntry{ID: "test"}
	svc := NewService(entry)
	resp := svc.SyncKubeconfig()
	if resp.OK {
		t.Fatal("expected sync disabled")
	}
}

func TestPodReachability(t *testing.T) {
	cases := []struct {
		phase string
		want  probe.Reachability
	}{
		{"Running", probe.ReachOK},
		{"Succeeded", probe.ReachOK},
		{"Pending", probe.ReachDegraded},
		{"Failed", probe.ReachFail},
	}
	for _, tc := range cases {
		if got := podReachability(tc.phase); got != tc.want {
			t.Errorf("phase %s: got %s want %s", tc.phase, got, tc.want)
		}
	}
}

func TestBifrostNSFilter(t *testing.T) {
	entry := &config.ClusterEntry{
		BifrostNamespaces: []string{"cicd", "bifrost"},
	}
	m := bifrostNSFilter(entry, "bifrost")
	if m == nil || !m["cicd"] || !m["bifrost"] || m["kube-system"] {
		t.Fatalf("unexpected filter map: %v", m)
	}
	if bifrostNSFilter(entry, "") != nil {
		t.Fatal("empty filter should return nil")
	}
}

func TestEnsureBifrostNamespacesCreatesMissing(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	entry := &config.ClusterEntry{
		ID:                "test",
		BifrostNamespaces: []string{"bifrost", "cicd"},
	}
	svc := NewService(entry)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.EnsureBifrostNamespaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}
	for _, ns := range entry.BifrostNamespaces {
		if _, err := clientset.CoreV1().Namespaces().Get(t.Context(), ns, metav1.GetOptions{}); err != nil {
			t.Fatalf("namespace %s not created: %v", ns, err)
		}
	}

	resp, err = svc.EnsureBifrostNamespaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Changed {
		t.Fatalf("second ensure should be idempotent: %+v", resp)
	}
}

func TestIntegrationSummary(t *testing.T) {
	kc := os.Getenv("PLATFORM_KUBECONFIG")
	if kc == "" {
		kc = os.Getenv("KUBECONFIG")
	}
	if kc == "" {
		t.Skip("PLATFORM_KUBECONFIG or KUBECONFIG not set")
	}
	t.Setenv("PLATFORM_KUBECONFIG", kc)

	entry := &config.ClusterEntry{
		ID:            "integration",
		Label:         "Integration",
		KubeconfigEnv: "PLATFORM_KUBECONFIG",
	}
	svc := NewService(entry)
	sum := svc.Summary(t.Context())
	if sum.Reachability == probe.ReachFail && sum.NodesTotal == 0 {
		t.Fatalf("cluster unreachable: %s", sum.Detail)
	}
}
