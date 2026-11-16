package appreadings

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
	appsv1 "k8s.io/api/apps/v1"
	"strings"
)

type ServiceDependencyView = resourceview.ServiceDependencyView
type ServiceDomainView = resourceview.ServiceDomainView
type Snapshot struct {
	Finalize                       func(string, string, []ServiceDependencyView, string) ServiceDomainView
	DeploymentReach                func(*appsv1.Deployment) (probe.Reachability, string)
	Deployments                    map[string]appsv1.Deployment
	IngressRoutes                  map[string]bool
	Environments                   []config.AppEnv
	Schedulable, IngressController ServiceDependencyView
}

func CountReadyDeployments(snap Snapshot, namespace, namePrefix string) (int, string) {
	total := 0
	ready := 0
	for key, d := range snap.Deployments {
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

func ApiReadyReach(ready int) probe.Reachability {
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

func IngressRouteDep(snap Snapshot, ns, name, label string) ServiceDependencyView {
	key := ns + "/" + name
	id := "ingressroute-" + ns + "-" + name
	if snap.IngressRoutes != nil && snap.IngressRoutes[key] {
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

func EvalApplicationsDomain(snap Snapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		snap.Schedulable,
		snap.IngressController,
	}
	// Edge is Traefik IngressRoute trade-gateway per Trade NS (not per-NS nginx).
	for _, env := range snap.Environments {
		deps = append(deps, IngressRouteDep(snap, env.Namespace, "trade-gateway", "Trade gateway ("+env.ID+")"))
		deps = append(deps, deploymentDep(snap.Deployments, snap.DeploymentReach, env.Namespace, "frontend", "Trade frontend ("+env.ID+")"))
		apiReady, apiDetail := CountReadyDeployments(snap, env.Namespace, "api-")
		deps = append(deps, ServiceDependencyView{
			ID: "apis-" + env.ID, Label: "FastAPI services (" + env.ID + ")",
			Reachability: ApiReadyReach(apiReady),
			Detail:       apiDetail,
		})
	}
	return snap.Finalize("applications", "General applications", deps, "amd64 Trade stack · Traefik trade-gateway · frontend · 4 API pods (Phase B)")
}
func deploymentDep(deployments map[string]appsv1.Deployment, reachOf func(*appsv1.Deployment) (probe.Reachability, string), ns, name, label string) ServiceDependencyView {
	key := ns + "/" + name
	d, ok := deployments[key]
	if !ok {
		return ServiceDependencyView{ID: "workload-" + name, Label: label, Reachability: probe.ReachDegraded, Detail: "not deployed in " + ns}
	}
	reach, detail := reachOf(&d)
	return ServiceDependencyView{ID: "workload-" + ns + "-" + name, Label: label, Reachability: reach, Detail: detail}
}
