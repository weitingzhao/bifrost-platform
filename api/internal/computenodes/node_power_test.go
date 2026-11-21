package computenodes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Moved from cluster/node_power_test.go on 2026-11-19 with the code they test.

func TestIsComputeManaged(t *testing.T) {
	entry := &config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{Name: "gpu-server"}},
	}
	svc := cluster.NewService(entry)
	managed := New().ClusterPort().Managed
	if !managed(svc, "gpu-server") {
		t.Fatal("expected gpu-server managed")
	}
	if managed(svc, "other") {
		t.Fatal("expected other not managed")
	}
}

func TestNodePowerManagedNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-server",
			Annotations: map[string]string{
				annoWolMAC:      "aa:bb:cc:dd:ee:ff",
				annoPowerPolicy: "on-demand",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	clientset := fake.NewSimpleClientset(node)

	entry := &config.ClusterEntry{
		ID: "test",
		ComputeNodes: []config.ComputeNodeSpec{{
			Name:   "gpu-server",
			WolMAC: "aa:bb:cc:dd:ee:ff",
			Workloads: []config.ComputeWorkloadSpec{
				{Namespace: "ai", Deployment: "ollama", Label: "Ollama"},
			},
		}},
	}
	svc := cluster.NewService(entry)
	svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})

	resp, err := NodePower(t.Context(), svc, "gpu-server")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.ComputeManaged || resp.PowerState != "online" || resp.NodeStatus != "Ready" {
		t.Fatalf("unexpected power response: %+v", resp)
	}
	if resp.WolMAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("wol mac: %q", resp.WolMAC)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("reachability: %s", resp.Reachability)
	}
}

func TestNodePowerUnknownNode(t *testing.T) {
	svc := cluster.NewService(&config.ClusterEntry{ComputeNodes: []config.ComputeNodeSpec{{Name: "gpu-server"}}})
	_, err := NodePower(t.Context(), svc, "worker-1")
	if err == nil {
		t.Fatal("expected error for unmanaged node")
	}
}
