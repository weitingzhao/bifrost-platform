// Package tenanttopology owns the read-only tenant/deployment display mapping.
package tenanttopology

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/topologyview"
	"strings"
)

func Adapter() topologyview.Adapter {
	return topologyview.Adapter{TargetNodes: targetNodes, EdgeDetail: edgeDetail}
}

func targetNodes(id, environment string) []string {
	if strings.HasPrefix(id, "api-") || id == "nginx-spa" || id == "ops-capabilities" {
		if environment == "dev" {
			return []string{"macbook", "mac-mini-1"}
		}
		return []string{"mini-pc-a"}
	}
	return nil
}

func edgeDetail(kind string) (string, bool) {
	if kind == "ib" {
		return "IB/TWS external — not probed by platform (by design)", true
	}
	return "", false
}
