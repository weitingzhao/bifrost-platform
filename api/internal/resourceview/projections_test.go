package resourceview_test

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestPodPreservesObservedCountsAndCallerPolicy(t *testing.T) {
	var phases []string
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns"}, Spec: corev1.PodSpec{Containers: make([]corev1.Container, 3)}, Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: 2}, {RestartCount: 4}}}}
	got := resourceview.Pod(p, "2d", func(phase string) probe.Reachability { phases = append(phases, phase); return probe.ReachUnknown })
	want := resourceview.DeploymentView{Namespace: "ns", Kind: "Pod", Name: "pod", Ready: "1/3", Status: "Pending", Restarts: 6, Age: "2d", Reachability: probe.ReachUnknown}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(phases, []string{"Pending"}) {
		t.Fatalf("got=%#v calls=%v", got, phases)
	}
	empty := resourceview.Pod(corev1.Pod{}, "", func(string) probe.Reachability { return probe.ReachUnknown })
	if empty.Ready != "0/0" || empty.Status != "" || empty.Restarts != 0 {
		t.Fatalf("empty=%#v", empty)
	}
}

func TestMonitoringCatalogOrderMatchingAndMissing(t *testing.T) {
	ds := []resourceview.DeploymentView{{Kind: "Deployment", Name: "grafana-prometheus", Ready: "1/1", Status: "Ready", Reachability: probe.ReachOK}, {Kind: "Deployment", Name: "PROMETHEUS-main", Ready: "0/1", Status: "Unavailable", Reachability: probe.ReachFail}}
	ss := []resourceview.DeploymentView{{Kind: "StatefulSet", Name: "prometheus-second", Reachability: probe.ReachOK}, {Kind: "StatefulSet", Name: "alertmanager", Ready: "1/1", Status: "Ready", Reachability: probe.ReachOK}}
	got := resourceview.MatchLayerBComponents(ds, ss)
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	ids := []string{}
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"prometheus", "grafana", "alertmanager", "loki"}) {
		t.Fatal(ids)
	}
	if got[0].Name != "PROMETHEUS-main" || got[0].Detail != "Unavailable (0/1)" || got[1].Name != "grafana-prometheus" || got[3].Status != "planned" {
		t.Fatalf("components=%#v", got)
	}
	if resourceview.CountReadyRequiredComponents(got) != 2 || resourceview.ComponentReach(got, "absent") != probe.ReachUnknown {
		t.Fatal("required count or missing reach changed")
	}
	missing := resourceview.MatchLayerBComponents(nil, nil)
	if !reflect.DeepEqual(missing, resourceview.MissingLayerBComponents()) || missing[0].Status != "missing" || missing[3].Phase != "planned" {
		t.Fatalf("missing=%#v", missing)
	}
}

func TestServiceReadinessRollupPriority(t *testing.T) {
	cases := []struct {
		rows   []resourceview.ServiceDomainView
		reach  probe.Reachability
		detail string
	}{
		{nil, probe.ReachOK, "all service domains ready"},
		{[]resourceview.ServiceDomainView{{Status: "standby", Reachability: probe.ReachFail}}, probe.ReachOK, "1 domain(s) on standby (scaled to zero)"},
		{[]resourceview.ServiceDomainView{{Status: "standby"}, {Status: "partial", Reachability: probe.ReachDegraded}}, probe.ReachDegraded, "1 domain(s) partial"},
		{[]resourceview.ServiceDomainView{{Status: "partial", Reachability: probe.ReachDegraded}, {Status: "unavailable"}}, probe.ReachDegraded, "1 domain(s) unavailable"},
	}
	for _, c := range cases {
		r, d := resourceview.AggregateServiceReadiness(c.rows)
		if r != c.reach || d != c.detail {
			t.Fatalf("got=%s %q want=%s %q", r, d, c.reach, c.detail)
		}
	}
}
