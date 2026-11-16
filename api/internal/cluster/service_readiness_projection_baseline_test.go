package cluster

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"strings"
	"testing"
)

func baselineFinalizeDomain(id, label string, deps []ServiceDependencyView, purpose string) ServiceDomainView {
	status, reach, summary := baselineDomainStatusFromDeps(deps)
	if summary == "" {
		summary = purpose
	}
	return ServiceDomainView{
		ID: id, Label: label, Status: status, Reachability: reach, Summary: summary, Dependencies: deps,
	}
}

func baselineDependencyIsStandby(d ServiceDependencyView) bool {
	lower := strings.ToLower(d.Detail)
	standbyText := strings.Contains(lower, "standby") ||
		strings.Contains(lower, "scaled to zero") ||
		strings.Contains(lower, "retired wave 5")
	if !standbyText {
		return false
	}
	// Standby deps may be ReachOK (expected off) or ReachDegraded (legacy scaled-to-zero).
	return d.Reachability == probe.ReachOK || d.Reachability == probe.ReachDegraded || d.Reachability == probe.ReachUnknown
}

func baselineDomainStatusFromDeps(deps []ServiceDependencyView) (status string, reach probe.Reachability, summary string) {
	fail := 0
	realDegraded := 0
	standby := 0
	var gaps []string
	for _, d := range deps {
		if baselineDependencyIsStandby(d) {
			standby++
			continue
		}
		switch d.Reachability {
		case probe.ReachFail:
			fail++
			gaps = append(gaps, d.Label)
		case probe.ReachDegraded:
			realDegraded++
		}
	}
	switch {
	case fail > 0:
		return "unavailable", probe.ReachFail, fmt.Sprintf("Blocked: %s", strings.Join(gaps, ", "))
	case realDegraded > 0:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d dependency gap(s)", realDegraded)
	case standby > 0:
		// Product formula: standby + no demand → status standby + reachability ok.
		return "standby", probe.ReachOK, "Standby — no demand"
	default:
		return "ready", probe.ReachOK, "All dependencies satisfied"
	}
}

func baselineAggregateServiceReadiness(domains []ServiceDomainView) (probe.Reachability, string) {
	fail := 0
	degraded := 0
	standby := 0
	for _, d := range domains {
		switch d.Status {
		case "standby":
			standby++
		case "unavailable":
			fail++
		default:
			switch d.Reachability {
			case probe.ReachFail:
				fail++
			case probe.ReachDegraded:
				degraded++
			}
		}
	}
	switch {
	case fail > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) unavailable", fail)
	case degraded > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) partial", degraded)
	case standby > 0:
		return probe.ReachOK, fmt.Sprintf("%d domain(s) on standby (scaled to zero)", standby)
	default:
		return probe.ReachOK, "all service domains ready"
	}
}

func baselineDeploymentReach(d *appsv1.Deployment) (probe.Reachability, string) {
	desired := d.Status.Replicas
	if desired == 0 && d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if desired == 0 {
		return probe.ReachDegraded, "scaled to zero (standby)"
	}
	ready := d.Status.ReadyReplicas
	detail := fmt.Sprintf("ready %d/%d", ready, desired)
	switch {
	case ready >= desired:
		return probe.ReachOK, detail
	case ready > 0:
		return probe.ReachDegraded, detail
	default:
		return probe.ReachFail, detail
	}
}

func baselineCountReadyDeployments(snap readinessSnapshot, namespace, namePrefix string) (int, string) {
	total := 0
	ready := 0
	for key, d := range snap.deployments {
		if !strings.HasPrefix(key, namespace+"/") {
			continue
		}
		if !strings.HasPrefix(d.Name, namePrefix) {
			continue
		}
		total++
		desired := d.Status.Replicas
		if desired == 0 && d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if desired > 0 && d.Status.ReadyReplicas >= desired {
			ready++
		}
	}
	if total == 0 {
		return 0, "no api-* deployments in " + namespace
	}
	return ready, fmt.Sprintf("%d/%d api deployments ready", ready, total)
}

func baselineApiReadyReach(ready int) probe.Reachability {
	// Phase B: 4 process pods (monitor/account/market/research).
	switch {
	case ready >= 4:
		return probe.ReachOK
	case ready > 0:
		return probe.ReachDegraded
	default:
		return probe.ReachFail
	}
}

func baselineIngressRouteDep(snap readinessSnapshot, ns, name, label string) ServiceDependencyView {
	key := ns + "/" + name
	id := "ingressroute-" + ns + "-" + name
	if snap.ingressRoutes != nil && snap.ingressRoutes[key] {
		return ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       "IngressRoute " + name + " present",
		}
	}
	return ServiceDependencyView{
		ID: id, Label: label,
		Reachability: probe.ReachDegraded,
		Detail:       "IngressRoute " + name + " missing in " + ns,
	}
}

func baselineEvalApplicationsDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		schedulableAnyDep(snap, "Schedulable nodes"),
		deploymentDep(snap, "kube-system", "traefik", "Traefik ingress controller"),
	}
	// Edge is Traefik IngressRoute trade-gateway per Trade NS (not per-NS nginx).
	for _, env := range snap.appEnvs {
		deps = append(deps, baselineIngressRouteDep(snap, env.Namespace, tradeGatewayIngressRoute, "Trade gateway ("+env.ID+")"))
		deps = append(deps, deploymentDep(snap, env.Namespace, "frontend", "Trade frontend ("+env.ID+")"))
		apiReady, apiDetail := baselineCountReadyDeployments(snap, env.Namespace, "api-")
		deps = append(deps, ServiceDependencyView{
			ID: "apis-" + env.ID, Label: "FastAPI services (" + env.ID + ")",
			Reachability: baselineApiReadyReach(apiReady),
			Detail:       apiDetail,
		})
	}
	return baselineFinalizeDomain("applications", "General applications", deps, "amd64 Trade stack · Traefik trade-gateway · frontend · 4 API pods (Phase B)")
}

func TestServiceReadinessProjectionOriginalParity(t *testing.T) {
	reaches := []probe.Reachability{"", probe.ReachOK, probe.ReachFail, probe.ReachDegraded, probe.ReachUnknown}
	for _, reach := range reaches {
		for _, detail := range []string{"", "standby", "scaled to zero", "retired Wave 5", "not deployed"} {
			deps := []ServiceDependencyView{{ID: "a", Label: "A", Reachability: reach, Detail: detail}, {ID: "b", Label: "B", Reachability: probe.ReachOK}}
			got, want := finalizeDomain("x", "X", deps, "purpose"), baselineFinalizeDomain("x", "X", deps, "purpose")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("domain %#v != %#v", got, want)
			}
			a, b := aggregateServiceReadiness([]ServiceDomainView{got})
			x, y := baselineAggregateServiceReadiness([]ServiceDomainView{want})
			if a != x || b != y {
				t.Fatal("aggregate mismatch")
			}
		}
	}
	for _, desired := range []int32{0, 1, 4} {
		for _, ready := range []int32{0, 1, 4} {
			d := appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &desired}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready}}
			a, b := deploymentReach(&d)
			x, y := baselineDeploymentReach(&d)
			if a != x || b != y {
				t.Fatal("deployment mismatch")
			}
			for _, n := range []int{0, 1, 3, 4, 5} {
				for _, ingress := range []bool{false, true} {
					snap := readinessSnapshot{deployments: map[string]appsv1.Deployment{}, ingressRoutes: map[string]bool{"ns/trade-gateway": ingress}, appEnvs: []config.AppEnv{{ID: "stg", Namespace: "ns"}}}
					for i := 0; i < n; i++ {
						copy := d
						copy.ObjectMeta = metav1.ObjectMeta{Name: fmt.Sprintf("api-%d", i), Namespace: "ns"}
						snap.deployments["ns/"+copy.Name] = copy
					}
					if n > 0 {
						snap.deployments["ns/frontend"] = d
						snap.deployments["kube-system/traefik"] = d
					}
					got, want := evalApplicationsDomain(snap), baselineEvalApplicationsDomain(snap)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("apps desired=%d ready=%d n=%d ingress=%v: %#v != %#v", desired, ready, n, ingress, got, want)
					}
				}
			}
		}
	}
	if got, want := evalApplicationsDomain(readinessSnapshot{}), baselineEvalApplicationsDomain(readinessSnapshot{}); !reflect.DeepEqual(got, want) {
		t.Fatal("empty mismatch")
	}
}
