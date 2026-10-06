package cluster

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func minioService(selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: minioServiceName, Namespace: cnpgNamespace},
		Spec:       corev1.ServiceSpec{Selector: selector},
	}
}

func nasSlice(addr string, ready *bool) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minio-nas", Namespace: cnpgNamespace,
			Labels: map[string]string{discoveryv1.LabelServiceName: minioServiceName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: strPtr("console"), Port: int32Ptr(9001)},
			{Name: strPtr("api"), Port: int32Ptr(9000)},
		},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{addr}, Conditions: discoveryv1.EndpointConditions{Ready: ready}}},
	}
}

func TestMinioEndpointFromService(t *testing.T) {
	ext, ep := minioEndpointFromService(minioService(map[string]string{"app.kubernetes.io/name": "minio"}), nil)
	if ext || ep != "" {
		t.Fatalf("selector Service is in-cluster, got external=%v endpoint=%q", ext, ep)
	}
	ext, ep = minioEndpointFromService(minioService(nil), []discoveryv1.EndpointSlice{*nasSlice("192.168.10.20", boolPtr(true))})
	if !ext || ep != "http://192.168.10.20:9000" {
		t.Fatalf("selectorless Service → api port of the slice, got external=%v endpoint=%q", ext, ep)
	}
	ext, ep = minioEndpointFromService(minioService(nil), []discoveryv1.EndpointSlice{*nasSlice("192.168.10.20", boolPtr(false))})
	if !ext || ep != "" {
		t.Fatalf("not-ready address is skipped, got external=%v endpoint=%q", ext, ep)
	}
	ext, ep = minioEndpointFromService(minioService(nil), []discoveryv1.EndpointSlice{*nasSlice("192.168.10.20", nil)})
	if !ext || ep != "http://192.168.10.20:9000" {
		t.Fatalf("unset ready counts as ready, got external=%v endpoint=%q", ext, ep)
	}
}

func TestClassifyMinioHealth(t *testing.T) {
	ep := "http://192.168.10.20:9000"
	cases := []struct {
		status int
		err    error
		want   probe.Reachability
		detail string
	}{
		{http.StatusOK, nil, probe.ReachOK, "healthy"},
		{http.StatusServiceUnavailable, nil, probe.ReachFail, "drive offline"},
		{0, errors.New("connection refused"), probe.ReachFail, "unreachable"},
		{http.StatusInternalServerError, nil, probe.ReachDegraded, "HTTP 500"},
	}
	for _, c := range cases {
		got, detail := classifyMinioHealth(ep, c.status, c.err)
		if got != c.want || !strings.Contains(detail, c.detail) {
			t.Fatalf("status=%d err=%v → %s %q, want %s containing %q", c.status, c.err, got, detail, c.want, c.detail)
		}
	}
}

func externalMinioService(t *testing.T, status int) (*Service, *fake.Clientset) {
	t.Helper()
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: minioDeployName, Namespace: cnpgNamespace},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	clientset := fake.NewSimpleClientset(minioService(nil), nasSlice("192.168.10.20", boolPtr(true)), deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) { return clientset, "fake", nil }
	svc.minioHealthProbe = func(_ context.Context, endpoint string) (int, error) {
		if endpoint != "http://192.168.10.20:9000" {
			t.Fatalf("probed %q", endpoint)
		}
		return status, nil
	}
	return svc, clientset
}

func TestResolveMinioBackendExternal(t *testing.T) {
	svc, clientset := externalMinioService(t, http.StatusOK)
	b := svc.resolveMinioBackend(t.Context(), clientset)
	if !b.External || b.Endpoint != "http://192.168.10.20:9000" || b.Reach != probe.ReachOK {
		t.Fatalf("got %+v", b)
	}
	dep := minioDepLabeled(readinessSnapshot{minio: b}, "MinIO backup target")
	if dep.Reachability != probe.ReachOK || !strings.Contains(dep.Detail, "192.168.10.20:9000") {
		t.Fatalf("external MinIO dep should report the NAS health, got %+v", dep)
	}
}

func TestResolveMinioBackendInClusterKeepsDeploymentCheck(t *testing.T) {
	clientset := fake.NewSimpleClientset(minioService(map[string]string{"app.kubernetes.io/name": "minio"}))
	svc := NewService(nil)
	b := svc.resolveMinioBackend(t.Context(), clientset)
	if b.External {
		t.Fatalf("selector Service must stay in-cluster, got %+v", b)
	}
	dep := minioDepLabeled(readinessSnapshot{minio: b, deployments: map[string]appsv1.Deployment{}}, "MinIO backup target")
	if dep.ID != "workload-minio" {
		t.Fatalf("in-cluster check should be the Deployment one, got %+v", dep)
	}
}

// The case that matters: MinIO on the NAS is down. The repair must stop and say
// so, and must not restart (or otherwise touch) the in-cluster minio Deployment,
// which is 0 on purpose — a pod there would share the NAS MinIO's data directory.
func TestRepairWithUnhealthyExternalMinioLeavesDeploymentAlone(t *testing.T) {
	svc, clientset := externalMinioService(t, http.StatusServiceUnavailable)
	resp, err := svc.RepairPostgresWalStore(t.Context())
	if err == nil || resp.OK {
		t.Fatalf("expected an error for an unhealthy external MinIO, got %+v", resp)
	}
	if !resp.MinIOExternal || resp.MinIOEndpoint != "http://192.168.10.20:9000" {
		t.Fatalf("response should name the external endpoint, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "not touching the in-cluster minio Deployment") {
		t.Fatalf("message=%q", resp.Message)
	}
	d, gerr := clientset.AppsV1().Deployments(cnpgNamespace).Get(t.Context(), minioDeployName, metav1.GetOptions{})
	if gerr != nil {
		t.Fatal(gerr)
	}
	if *d.Spec.Replicas != 0 || len(d.Spec.Template.Annotations) != 0 {
		t.Fatalf("minio Deployment was touched: replicas=%d annotations=%v", *d.Spec.Replicas, d.Spec.Template.Annotations)
	}
	for _, a := range clientset.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("unexpected write to the cluster: %s %s", a.GetVerb(), a.GetResource().Resource)
		}
	}
}
