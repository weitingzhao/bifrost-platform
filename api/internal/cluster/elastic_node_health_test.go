package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRollupNodeHealthElasticStandby(t *testing.T) {
	gpu := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-server"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionUnknown,
			}},
		},
	}
	core := readyNode("ubt-k3s-01")

	clientset := fake.NewSimpleClientset(&gpu, &core)
	entry := &config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{
			Name: "gpu-server",
			Workloads: []config.ComputeWorkloadSpec{{
				Namespace:  "ai",
				Deployment: "ollama",
			}},
		}},
	}
	svc := NewService(entry)
	rollup := svc.rollupNodeHealth(t.Context(), clientset, []corev1.Node{gpu, core})

	if rollup.CoreReady != 1 || rollup.CoreTotal != 1 {
		t.Fatalf("core ready/total: got %d/%d want 1/1", rollup.CoreReady, rollup.CoreTotal)
	}
	if rollup.ElasticStandby != 1 {
		t.Fatalf("elastic standby: got %d want 1", rollup.ElasticStandby)
	}
	if rollup.ElasticDegraded != 0 {
		t.Fatalf("elastic degraded: got %d want 0", rollup.ElasticDegraded)
	}
}

func TestRollupNodeHealthElasticDegradedWorkloadDemand(t *testing.T) {
	gpu := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-server"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionUnknown,
			}},
		},
	}
	core := readyNode("ubt-k3s-01")
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "ai"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
	}

	clientset := fake.NewSimpleClientset(&gpu, &core, dep)
	entry := &config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{
			Name: "gpu-server",
			Workloads: []config.ComputeWorkloadSpec{{
				Namespace:  "ai",
				Deployment: "ollama",
				Label:      "Ollama",
			}},
		}},
	}
	svc := NewService(entry)
	rollup := svc.rollupNodeHealth(t.Context(), clientset, []corev1.Node{gpu, core})

	if rollup.ElasticDegraded != 1 {
		t.Fatalf("elastic degraded: got %d want 1", rollup.ElasticDegraded)
	}
	if rollup.CoreTotal != 2 || rollup.CoreReady != 1 {
		t.Fatalf("core ready/total: got %d/%d want 1/2", rollup.CoreReady, rollup.CoreTotal)
	}
}

func TestSummaryReachFromNodes(t *testing.T) {
	reach, detail := summaryReachFromNodes(5, 5, 0, 2)
	if reach != probe.ReachDegraded {
		t.Fatalf("reach: got %s want degraded", reach)
	}
	if detail == "" {
		t.Fatal("expected detail")
	}

	reach, detail = summaryReachFromNodes(5, 5, 0, 0)
	if reach != probe.ReachOK {
		t.Fatalf("reach: got %s want ok", reach)
	}
	detail = appendElasticStandbyDetail(detail, 1)
	if detail != "1 elastic standby" {
		t.Fatalf("detail: got %q", detail)
	}
}

func readyNode(name string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type: corev1.NodeReady, Status: corev1.ConditionTrue,
			}},
		},
	}
}
