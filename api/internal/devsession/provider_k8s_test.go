package devsession

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

func int32Ptr(v int32) *int32 { return &v }

func testK8sProvider(t *testing.T, clientset kubernetes.Interface, cat *SessionsCatalog) *K8sProvider {
	t.Helper()
	svc := cluster.NewService(nil)
	svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})
	return NewK8sProvider(svc, cat, "stg")
}

func TestK8sProvider_ListMapsDeployment(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-api", Namespace: "bifrost-platform-stg"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "platform-api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "platform-api"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "platform-api",
						Image: "registry.local/platform-api:stg-abc",
					}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	cat := &SessionsCatalog{Envs: map[string][]CatalogEntry{
		"stg": {{
			Name: "platform", Label: "Platform API", Group: "platform",
			Namespace: "bifrost-platform-stg", Deployment: "platform-api",
		}},
	}}
	p := testK8sProvider(t, fake.NewSimpleClientset(deploy), cat)
	sessions, err := p.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len=%d", len(sessions))
	}
	s := sessions[0]
	if s.Name != "platform" || s.Status != "running" || s.Mode != ModeK8s {
		t.Fatalf("session=%+v", s)
	}
	if s.ImageTag != "stg-abc" {
		t.Fatalf("image_tag=%q", s.ImageTag)
	}
	if s.ReadyReplicas == nil || *s.ReadyReplicas != 1 {
		t.Fatalf("ready_replicas=%v", s.ReadyReplicas)
	}
}

func TestK8sProvider_ClearLogsNoop(t *testing.T) {
	cat := &SessionsCatalog{Envs: map[string][]CatalogEntry{
		"stg": {{Name: "api-monitor", Label: "Monitor", Group: "api", Namespace: "bifrost-stg", Deployment: "api-monitor"}},
	}}
	p := testK8sProvider(t, fake.NewSimpleClientset(), cat)
	resp, err := p.Control(t.Context(), "api-monitor", "clear-logs")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !strings.Contains(resp.Message, "not applicable") {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestK8sProvider_StartDaemonBlockedByD10(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-stg"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	cat := &SessionsCatalog{Envs: map[string][]CatalogEntry{
		"stg": {{Name: "daemon", Label: "Daemon", Group: "worker", Namespace: "bifrost-stg", Deployment: "daemon"}},
	}}
	p := testK8sProvider(t, fake.NewSimpleClientset(deploy), cat)
	resp, err := p.Control(t.Context(), "daemon", "start")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success {
		t.Fatalf("expected D10 block, got success: %+v", resp)
	}
	if !strings.Contains(resp.Message, "D10") {
		t.Fatalf("message=%q", resp.Message)
	}
}

func TestK8sProvider_Restart(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-monitor", Namespace: "bifrost-stg"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			},
		},
	}
	cat := &SessionsCatalog{Envs: map[string][]CatalogEntry{
		"stg": {{Name: "api-monitor", Label: "Monitor", Group: "api", Namespace: "bifrost-stg", Deployment: "api-monitor"}},
	}}
	p := testK8sProvider(t, fake.NewSimpleClientset(deploy), cat)
	resp, err := p.Control(t.Context(), "api-monitor", "restart")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestK8sProvider_ListGracefulWhenKubeUnavailable(t *testing.T) {
	svc := cluster.NewService(nil)
	svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return nil, "", &cluster.ClientError{Detail: "kubeconfig not found"}
	})
	cat := &SessionsCatalog{Envs: map[string][]CatalogEntry{
		"stg": {{Name: "platform", Label: "Platform", Group: "platform", Namespace: "ns", Deployment: "platform-api"}},
	}}
	p := NewK8sProvider(svc, cat, "stg")
	_, err := p.List(t.Context())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kubernetes unavailable") {
		t.Fatalf("err=%v", err)
	}
}

func TestFirstImageTag(t *testing.T) {
	deploy := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Image: "ghcr.io/org/app:v1.2.3"}},
				},
			},
		},
	}
	if got := firstImageTag(deploy); got != "v1.2.3" {
		t.Fatalf("got %q", got)
	}
}
