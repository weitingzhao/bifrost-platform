package actions

import "strings"

// ExemptionReason is why a write route is intentionally outside the catalog.
// An empty reason means the route is not exempt. The route ratchet fails when
// a new write route is neither catalogued nor listed here.
func ExemptionReason(method, route string) string {
	return exemptions[routeKey(method, route)]
}

// Exemptions is the explicit list, keyed "METHOD /api/v1/...".
func Exemptions() map[string]string {
	out := make(map[string]string, len(exemptions))
	for k, v := range exemptions {
		out[k] = v
	}
	return out
}

// Covers reports whether a write route is the direct endpoint of a catalog action.
func Covers(method, route string) bool {
	key := routeKey(method, route)
	for _, a := range catalog {
		if a.Method == "" || a.Pattern == "" {
			continue
		}
		if routeKey(a.Method, a.Pattern) == key {
			return true
		}
	}
	return false
}

func routeKey(method, route string) string {
	route = strings.TrimSuffix(route, "/")
	route = strings.ReplaceAll(route, "{*}", "*")
	return strings.ToUpper(method) + " " + route
}

func exempt(reason string, keys ...string) {
	for _, k := range keys {
		exemptions[k] = reason
	}
}

var exemptions = map[string]string{}

func init() {
	exempt("approval workflow, not a catalog action",
		"POST /api/v1/approvals",
		"POST /api/v1/approvals/{id}/approve",
		"POST /api/v1/approvals/{id}/reject",
		"POST /api/v1/approvals/claim",
		"POST /api/v1/approvals/{id}/heartbeat",
		"POST /api/v1/approvals/{id}/result",
	)
	exempt("unclassified by LANE-B1: evidence, queue, or checklist — not a cluster write",
		"POST /api/v1/audit/append",
		"POST /api/v1/code-health/report",
		"POST /api/v1/code-health/rescan",
		"PUT /api/v1/lineage/thread-title",
		"PUT /api/v1/lineage/transcript-title",
		"POST /api/v1/agent/governance/skill-runs",
		"POST /api/v1/telemetry/attention-mute",
	)
	exempt("unclassified by LANE-B1: delivery supply-chain writes that are not pipeline start/delete",
		"POST /api/v1/delivery/supply-chain/mirror-sync",
		"POST /api/v1/delivery/supply-chain/dockerfile-configmaps/refresh",
	)
	exempt("unclassified by LANE-B1: wildcard plugin proxy. market_data_heal (tier B) is the catalogued heal path; DELETE is market_data_delete (tier C)",
		"POST /api/v1/plugins/market-data/api/*",
		"POST /api/v1/plugins/flex-query/api/*",
	)
	exempt("unclassified by LANE-B1: namespace ensure stays outside the catalog until phase 3",
		"POST /api/v1/cluster/namespaces/ensure-bifrost",
	)
	exempt("unclassified by LANE-B1: dev-session control. D10 still refuses daemon scale-up inside that handler",
		"POST /api/v1/dev-sessions/{name}/control",
	)
	exempt("unclassified by LANE-B1: ops-agent alertmanager receiver",
		"POST /api/v1/ops-agent/alertmanager",
	)
	exempt("unclassified by LANE-B1: operator-plane patrol writes",
		"PUT /api/v1/patrol/skills/{id}/enable",
		"POST /api/v1/patrol/trigger/{id}",
		"POST /api/v1/patrol/webhook/{event}",
	)
}
