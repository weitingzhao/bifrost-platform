package cluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCordonNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: false},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	clientset := fake.NewSimpleClientset(node)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.CordonNode(t.Context(), "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed || resp.Action != "cluster.node.cordon" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	updated, _ := clientset.CoreV1().Nodes().Get(t.Context(), "worker-1", metav1.GetOptions{})
	if !updated.Spec.Unschedulable {
		t.Fatal("expected node cordoned")
	}
}

func TestUncordonNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	}
	clientset := fake.NewSimpleClientset(node)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.UncordonNode(t.Context(), "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}

	updated, _ := clientset.CoreV1().Nodes().Get(t.Context(), "worker-1", metav1.GetOptions{})
	if updated.Spec.Unschedulable {
		t.Fatal("expected node uncordoned")
	}
}

func TestJoinProfilesDefault(t *testing.T) {
	svc := NewService(nil)
	resp := svc.JoinProfiles()
	if len(resp.Profiles) == 0 {
		t.Fatal("expected default join profiles")
	}
	if resp.Profiles[0].ID != "gpu-server" {
		t.Fatalf("got %q", resp.Profiles[0].ID)
	}
}

func TestResolveJoinProfile(t *testing.T) {
	svc := NewService(nil)
	p, err := svc.resolveJoinProfile("gpu-server")
	if err != nil {
		t.Fatal(err)
	}
	if p.Script != "join-gpu-server.sh" {
		t.Fatalf("script: %q", p.Script)
	}
	_, err = svc.resolveJoinProfile("missing")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}
