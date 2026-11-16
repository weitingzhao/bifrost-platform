package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
	"strings"
)

type ServiceDependencyView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail,omitempty"`
}

type ServiceDomainView struct {
	ID           string                  `json:"id"`
	Label        string                  `json:"label"`
	Status       string                  `json:"status"`
	Reachability probe.Reachability      `json:"reachability"`
	Summary      string                  `json:"summary"`
	Dependencies []ServiceDependencyView `json:"dependencies"`
}

func FinalizeDomain(id, label string, deps []ServiceDependencyView, purpose string) ServiceDomainView {
	status, reach, summary := DomainStatusFromDeps(deps)
	if summary == "" {
		summary = purpose
	}
	return ServiceDomainView{
		ID: id, Label: label, Status: status, Reachability: reach, Summary: summary, Dependencies: deps,
	}
}

func DependencyIsStandby(d ServiceDependencyView) bool {
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

func DomainStatusFromDeps(deps []ServiceDependencyView) (status string, reach probe.Reachability, summary string) {
	fail := 0
	realDegraded := 0
	standby := 0
	var gaps []string
	for _, d := range deps {
		if DependencyIsStandby(d) {
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

func AggregateServiceReadiness(domains []ServiceDomainView) (probe.Reachability, string) {
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

func DeploymentReach(d *appsv1.Deployment) (probe.Reachability, string) {
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
