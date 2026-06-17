package stack

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func newTestService(entry *config.ClusterEntry, clientset kubernetes.Interface) *Service {
	svc := NewService(entry)
	cs := cluster.NewService(entry)
	cs.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})
	svc.cluster = cs
	return svc
}

func TestAddonsNotInstalled(t *testing.T) {
	replicas := int32(1)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cicd"}}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: "cicd"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
	}
	clientset := k8sfake.NewSimpleClientset(ns, deploy)
	entry := &config.ClusterEntry{ID: "test"}
	svc := newTestService(entry, clientset)

	resp := svc.Addons(t.Context())
	if len(resp.Addons) != 3 {
		t.Fatalf("addons: got %d want 3", len(resp.Addons))
	}
	for _, a := range resp.Addons {
		if a.Status != "not_installed" {
			t.Fatalf("%s status: got %q want not_installed", a.ID, a.Status)
		}
	}
	if resp.Reachability != probe.ReachDegraded {
		t.Fatalf("reachability: got %q want degraded", resp.Reachability)
	}
}

func TestAddonsGiteaInstalled(t *testing.T) {
	replicas := int32(1)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cicd"}}
	gitea := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "gitea", Namespace: "cicd"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
	}
	clientset := k8sfake.NewSimpleClientset(ns, gitea)
	entry := &config.ClusterEntry{ID: "test"}
	svc := newTestService(entry, clientset)

	resp := svc.Addons(t.Context())
	giteaView := resp.Addons[0]
	if giteaView.ID != "gitea" || giteaView.Status != "installed" {
		t.Fatalf("gitea: %+v", giteaView)
	}
}
