package cluster

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestObservabilityNoMonitoringNamespace(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	entry := &config.ClusterEntry{ID: "test", MonitoringNS: "monitoring"}
	svc := NewService(entry)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp := svc.Observability(t.Context())
	if resp.LayerBStatus != "not_installed" {
		t.Fatalf("layer_b_status: got %s want not_installed", resp.LayerBStatus)
	}
	if resp.Reachability != probe.ReachDegraded {
		t.Fatalf("reachability: got %s want degraded", resp.Reachability)
	}
	if len(resp.Components) != 4 {
		t.Fatalf("components: got %d want 4", len(resp.Components))
	}
	for _, c := range resp.Components {
		if c.Status != "missing" {
			t.Fatalf("component %s: expected missing, got %s", c.ID, c.Status)
		}
	}
}

func TestObservabilityGrafanaDeploymentPartial(t *testing.T) {
	replicas := int32(1)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-prometheus-stack-grafana",
			Namespace: "monitoring",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     1,
			AvailableReplicas: 1,
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring"}}
	clientset := fake.NewSimpleClientset(ns, deploy)

	entry := &config.ClusterEntry{
		ID:                  "test",
		MonitoringNS: "monitoring",
		ObservabilityURLs: config.ObservabilityURLs{
			Grafana: "http://192.168.10.73:3000",
		},
	}
	svc := NewService(entry)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp := svc.Observability(t.Context())
	if resp.LayerBStatus != "partial" {
		t.Fatalf("layer_b_status: got %s want partial", resp.LayerBStatus)
	}

	var grafana ObservabilityComponentView
	for _, c := range resp.Components {
		if c.ID == "grafana" {
			grafana = c
			break
		}
	}
	if grafana.Reachability != probe.ReachOK {
		t.Fatalf("grafana reachability: got %s want ok", grafana.Reachability)
	}
	if grafana.Ready != "1/1" {
		t.Fatalf("grafana ready: got %s want 1/1", grafana.Ready)
	}
	if resp.GrafanaURL != "http://192.168.10.73:3000" {
		t.Fatalf("grafana_url: got %q", resp.GrafanaURL)
	}
}

func TestAggregateLayerBStatus(t *testing.T) {
	status, reach, _ := aggregateLayerBStatus(0, 4)
	if status != "not_installed" || reach != probe.ReachDegraded {
		t.Fatalf("0/4: got %s %s", status, reach)
	}
	status, reach, _ = aggregateLayerBStatus(2, 4)
	if status != "partial" || reach != probe.ReachDegraded {
		t.Fatalf("2/4: got %s %s", status, reach)
	}
	status, reach, _ = aggregateLayerBStatus(4, 4)
	if status != "ready" || reach != probe.ReachOK {
		t.Fatalf("4/4: got %s %s", status, reach)
	}
}

func TestMatchLayerBComponentsStatefulSet(t *testing.T) {
	replicas := int32(1)
	ss := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "loki-stack", Namespace: "monitoring"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: 1},
	}
	components := matchLayerBComponents(nil, []appsv1.StatefulSet{ss})
	var loki ObservabilityComponentView
	for _, c := range components {
		if c.ID == "loki" {
			loki = c
		}
	}
	if loki.Kind != "StatefulSet" || loki.Reachability != probe.ReachOK {
		t.Fatalf("loki: %+v", loki)
	}
}
