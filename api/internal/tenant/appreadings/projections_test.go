package appreadings_test

import (
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
	"time"
)

func TestApplicationProjectionOrderAndInjectedPolicy(t *testing.T) {
	calls := 0
	finalize := func(id, label string, deps []appreadings.ServiceDependencyView, purpose string) appreadings.ServiceDomainView {
		calls++
		if id != "applications" || label != "General applications" || purpose == "" {
			t.Fatal("domain metadata lost")
		}
		return appreadings.ServiceDomainView{ID: id, Label: label, Status: "caller-status", Dependencies: deps}
	}
	snap := appreadings.Snapshot{Finalize: finalize, Environments: []config.AppEnv{{ID: "first", Namespace: "one"}, {ID: "second", Namespace: "two"}}, Schedulable: appreadings.ServiceDependencyView{ID: "nodes"}, IngressController: appreadings.ServiceDependencyView{ID: "ingress"}, IngressRoutes: map[string]bool{"one/trade-gateway": true}, Deployments: map[string]appsv1.Deployment{"one/frontend": {ObjectMeta: metav1.ObjectMeta{Name: "frontend"}}}, DeploymentReach: func(d *appsv1.Deployment) (probe.Reachability, string) {
		if d.Name != "frontend" {
			t.Fatal("wrong deployment")
		}
		return probe.ReachUnknown, "caller observation"
	}}
	got := appreadings.EvalApplicationsDomain(snap)
	ids := []string{}
	for _, d := range got.Dependencies {
		ids = append(ids, d.ID)
	}
	want := []string{"nodes", "ingress", "ingressroute-one-trade-gateway", "workload-one-frontend", "apis-first", "ingressroute-two-trade-gateway", "workload-frontend", "apis-second"}
	if calls != 1 || got.Status != "caller-status" || !reflect.DeepEqual(ids, want) {
		t.Fatalf("calls=%d got=%#v", calls, got)
	}
	if got.Dependencies[3].Detail != "caller observation" || got.Dependencies[3].Reachability != probe.ReachUnknown || got.Dependencies[6].Detail != "not deployed in two" || got.Dependencies[7].Reachability != probe.ReachFail {
		t.Fatal("missing/policy projection changed")
	}
	empty := appreadings.EvalApplicationsDomain(appreadings.Snapshot{Finalize: finalize})
	if len(empty.Dependencies) != 2 {
		t.Fatal("empty environments must retain infrastructure observations")
	}
}

func TestApplicationReadinessCountsOnlyRequestedNamespace(t *testing.T) {
	one := int32(1)
	snap := appreadings.Snapshot{Deployments: map[string]appsv1.Deployment{}}
	for _, name := range []string{"api-a", "api-b", "api-c", "api-d"} {
		snap.Deployments["ns/"+name] = appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: appsv1.DeploymentSpec{Replicas: &one}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	}
	snap.Deployments["other/api-extra"] = appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api-extra"}, Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1}}
	n, detail := appreadings.CountReadyDeployments(snap, "ns", "api-")
	if n != 4 || detail != "4/4 api deployments ready" {
		t.Fatalf("%d %s", n, detail)
	}
	for n, want := range map[int]probe.Reachability{0: probe.ReachFail, 1: probe.ReachDegraded, 3: probe.ReachDegraded, 4: probe.ReachOK, 5: probe.ReachOK} {
		if appreadings.ApiReadyReach(n) != want {
			t.Fatalf("boundary %d", n)
		}
	}
}

func TestFreshnessReferenceLagBoundariesAndUnknown(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	prod := now.Add(-24 * time.Hour)
	for _, c := range []struct {
		days    float64
		verdict string
	}{{-1, "fresh"}, {0, "fresh"}, {2.9, "fresh"}, {3, "aging"}, {6.9, "aging"}, {7, "stale"}} {
		at := prod.Add(-time.Duration(c.days * 24 * float64(time.Hour)))
		rows := []appreadings.DataFreshnessDB{{Name: "target"}, {Name: "prod"}}
		got := appreadings.FreshnessDatabases(rows, []appreadings.ActivityObservation{{At: &at, Sources: []string{"source"}}, {At: &prod}}, "prod", nil, now)
		if got[0].Verdict != c.verdict || got[0].LagVsProdDays == nil || *got[0].LagVsProdDays < 0 || got[1].Verdict != "reference" || got[1].LagVsProdDays != nil || got[0].Sources[0] != "source" {
			t.Fatalf("days=%v got=%#v", c.days, got)
		}
	}
	rows := []appreadings.DataFreshnessDB{{Name: "target"}, {Name: "prod"}}
	got := appreadings.FreshnessDatabases(rows, []appreadings.ActivityObservation{{At: &prod}, {Err: errors.New("offline")}}, "prod", nil, now)
	if got[0].Verdict != "unknown" || got[0].Detail != "prod activity unavailable: offline" || got[0].LagVsProdDays != nil {
		t.Fatalf("unknown=%#v", got[0])
	}
	if appreadings.FreshnessDatabases(nil, nil, "prod", nil, now) != nil {
		t.Fatal("nil changed")
	}
}
