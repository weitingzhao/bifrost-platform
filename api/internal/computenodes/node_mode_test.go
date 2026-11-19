package computenodes

import (
	"context"
	"sync/atomic"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// bind binds n to cluster for one test, as the server does at start-up.
func bind(t *testing.T, n *Nodes) {
	t.Helper()
	cluster.BindComputeNodes(n.ClusterPort())
	t.Cleanup(func() { cluster.BindComputeNodes(cluster.ComputeNodes{}) })
}

func fakeService(entry *config.ClusterEntry, objects ...k8sruntime.Object) *cluster.Service {
	clientset := fake.NewSimpleClientset(objects...)
	svc := cluster.NewService(entry)
	svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})
	return svc
}

func notReadyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionUnknown,
			}},
		},
	}
}

func readyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionTrue,
			}},
		},
	}
}

// Origin: 2026-11-19, cluster.TestRollupNodeHealthElasticStandby moved here
// with the classification of elastic nodes; it reads the rollup through
// cluster's Summary, with compute's nodes bound as the server binds them. A
// powered-off GPU node with no demand is standby and not a core node.
func TestSummaryCountsAnIdleGPUNodeAsStandby(t *testing.T) {
	bind(t, New())
	svc := fakeService(&config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{
			Name: "gpu-server",
			Workloads: []config.ComputeWorkloadSpec{{
				Namespace:  "ai",
				Deployment: "ollama",
			}},
		}},
	}, notReadyNode("gpu-server"), readyNode("ubt-k3s-01"))

	sum := svc.Summary(t.Context())
	if sum.NodesReady != 1 || sum.NodesTotal != 1 {
		t.Fatalf("core ready/total: got %d/%d want 1/1", sum.NodesReady, sum.NodesTotal)
	}
	if sum.ElasticStandby != 1 {
		t.Fatalf("elastic standby: got %d want 1", sum.ElasticStandby)
	}
	if sum.ElasticDegraded != 0 {
		t.Fatalf("elastic degraded: got %d want 0", sum.ElasticDegraded)
	}
}

// Origin: 2026-11-19, cluster.TestRollupNodeHealthElasticDegradedWorkloadDemand
// moved here with the classification of elastic nodes: a powered-off GPU node
// whose workload wants a replica is degraded and counts as a core node that
// is not ready.
func TestSummaryCountsAGPUNodeWithDemandAsDegraded(t *testing.T) {
	bind(t, New())
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "ai"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
	}
	svc := fakeService(&config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{
			Name: "gpu-server",
			Workloads: []config.ComputeWorkloadSpec{{
				Namespace:  "ai",
				Deployment: "ollama",
				Label:      "Ollama",
			}},
		}},
	}, notReadyNode("gpu-server"), readyNode("ubt-k3s-01"), dep)

	sum := svc.Summary(t.Context())
	if sum.ElasticDegraded != 1 {
		t.Fatalf("elastic degraded: got %d want 1", sum.ElasticDegraded)
	}
	if sum.NodesTotal != 2 || sum.NodesReady != 1 {
		t.Fatalf("core ready/total: got %d/%d want 1/2", sum.NodesReady, sum.NodesTotal)
	}
}

// Origin: 2026-11-19, the port's answer when compute's nodes are not bound:
// no node is compute's, so a powered-off GPU node counts as a core node that
// is not ready (never hidden as standby), and the power read refuses as it
// does for an unmanaged node. The two tests above are the positive control.
func TestUnboundNodesAreCoreAndPowerRefuses(t *testing.T) {
	cluster.BindComputeNodes(cluster.ComputeNodes{})
	svc := fakeService(&config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{Name: "gpu-server"}},
	}, notReadyNode("gpu-server"), readyNode("ubt-k3s-01"))

	sum := svc.Summary(t.Context())
	if sum.NodesTotal != 2 || sum.NodesReady != 1 || sum.ElasticStandby != 0 || sum.ElasticDegraded != 0 {
		t.Fatalf("unbound: core %d/%d standby %d degraded %d, want 1/2 0 0", sum.NodesReady, sum.NodesTotal, sum.ElasticStandby, sum.ElasticDegraded)
	}
	if _, err := svc.NodePower(t.Context(), "gpu-server"); err == nil || err.Error() != `node "gpu-server" is not a managed compute node` {
		t.Fatalf("unbound power read: %v", err)
	}
	if _, err := svc.PowerOffNode(t.Context(), "gpu-server"); err == nil {
		t.Fatal("unbound power off: want an error")
	}
}

// Origin: 2026-11-18, the cluster page waited out an SSH timeout per poll; the
// reading it now shows may be old. The patrol and the delivery preflight act on
// Nodes, so it must stay live;
// the live count is also the positive control for the display count.
// Moved from cluster/elastic_node_health_test.go on 2026-11-19 with the host
// probes.
func TestNodesProbesEveryCallAndDisplayDoesNot(t *testing.T) {
	n := New()
	var calls atomic.Int32
	n.liveProbe = func(context.Context, string) bool { calls.Add(1); return false }
	bind(t, n)
	svc := fakeService(&config.ClusterEntry{ComputeNodes: []config.ComputeNodeSpec{{
		Name: "gpu-server", SSHHost: "gpu",
	}}}, notReadyNode("gpu-server"))

	shown := svc.NodesForDisplay(t.Context())
	for range 2 {
		shown = svc.NodesForDisplay(t.Context())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("display probes: got %d want 1", got)
	}
	calls.Store(0)
	live := svc.Nodes(t.Context())
	for range 2 {
		live = svc.Nodes(t.Context())
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("live probes: got %d want 3", got)
	}
	if shown.Nodes[0].ElasticMode != live.Nodes[0].ElasticMode || live.Nodes[0].ElasticMode != "standby" {
		t.Fatalf("elastic mode: shown %q live %q want standby", shown.Nodes[0].ElasticMode, live.Nodes[0].ElasticMode)
	}
}
