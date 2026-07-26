package cluster

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestScaleDaemonFromZeroBlockedByD10(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-stg"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-stg",
		Kind:      "Deployment",
		Name:      "daemon",
		Replicas:  2,
	})
	if err == nil {
		t.Fatal("expected D10 error")
	}
	if resp.OK {
		t.Fatalf("expected ok=false, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "BLOCKED (D10)") {
		t.Fatalf("message=%q", resp.Message)
	}
}

func TestScaleDaemonDownAllowed(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-prod",
		Kind:      "Deployment",
		Name:      "daemon",
		Replicas:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestScaleAccountSyncAllowedFromZero(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "account-sync", Namespace: "bifrost-prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-prod",
		Kind:      "Deployment",
		Name:      "account-sync",
		Replicas:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
