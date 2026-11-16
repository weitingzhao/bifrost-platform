package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
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
