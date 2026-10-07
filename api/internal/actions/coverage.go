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
	)
	exempt("unclassified by LANE-B1: evidence, queue, checklist, or console session — not a cluster write",
		"POST /api/v1/audit/append",
		"PUT /api/v1/agent/governance/trust-overrides/{skill_id}",
		"POST /api/v1/code-health/report",
		"POST /api/v1/code-health/rescan",
		"PUT /api/v1/lineage/thread-title",
		"PUT /api/v1/lineage/transcript-title",
		"POST /api/v1/operate/queue",
		"POST /api/v1/operate/queue/{id}/execution",
		"POST /api/v1/operate/queue/{id}/close",
		"POST /api/v1/operate/queue/{id}/dismiss",
		"POST /api/v1/operate/sweep",
		"POST /api/v1/operate/briefs/{id}/decide",
		"POST /api/v1/checklist/signals",
		"POST /api/v1/checklist/husbandry-sync",
		"POST /api/v1/console/ws-ticket",
		"POST /api/v1/agent/governance/skill-runs",
		"POST /api/v1/telemetry/attention-mute",
	)
	exempt("unclassified by LANE-B1: remediation runner control",
		"POST /api/v1/remediation/start",
		"POST /api/v1/remediation/{id}/cancel",
		"POST /api/v1/remediation/{id}/respond",
	)
	exempt("unclassified by LANE-B1: delivery supply-chain writes that are not pipeline start/delete",
		"POST /api/v1/delivery/supply-chain/mirror-sync",
		"POST /api/v1/delivery/supply-chain/dockerfile-configmaps/refresh",
	)
	exempt("unclassified by LANE-B1: wildcard plugin proxy. market_data_heal (tier B) is the catalogued heal path under this prefix; other plugin writes are not leveled",
		"POST /api/v1/plugins/market-data/api/*",
		"DELETE /api/v1/plugins/market-data/api/*",
		"POST /api/v1/plugins/flex-query/api/*",
	)
	exempt("unclassified by LANE-B1: release gates, vision, build-phase, and migration sign-off",
		"POST /api/v1/promote/release-gate",
		"POST /api/v1/promote/tier-b/signoff",
		"POST /api/v1/build-phase/{phase}/gate",
		"POST /api/v1/build-phase/{phase}/signoff",
		"POST /api/v1/platform/escape-hatch/drill",
		"POST /api/v1/migrate-streams/{streamId}/waves/{waveId}/deliver",
		"POST /api/v1/migrate-streams/{streamId}/waves/{waveId}/signoff",
		"POST /api/v1/vision/v1/gate",
		"POST /api/v1/vision/v1/signoff",
		"POST /api/v1/vision/s3/gate",
		"POST /api/v1/vision/s3/signoff",
		"POST /api/v1/vision/v2/gate",
		"POST /api/v1/vision/v2/signoff",
		"POST /api/v1/vision/v3/gate",
		"POST /api/v1/vision/v3/signoff",
		"POST /api/v1/vision/v4/gate",
		"POST /api/v1/vision/v4/signoff",
		"POST /api/v1/vision/v5/gate",
		"POST /api/v1/vision/v5/signoff",
	)
	exempt("unclassified by LANE-B1: cluster writes outside the named catalog (kubeconfig, namespace ensure, backup sweep, addon ensure, clone schedule)",
		"POST /api/v1/cluster/sync-kubeconfig",
		"POST /api/v1/cluster/namespaces/ensure-bifrost",
		"POST /api/v1/cluster/postgres/backups/sweep-failed",
		"POST /api/v1/cluster/kubeconfig-secret/ensure",
		"POST /api/v1/cluster/addons/metrics-server/ensure",
		"POST /api/v1/cluster/addons/kube-prometheus-stack/ensure",
		"PUT /api/v1/cluster/data-clone/schedule",
	)
	exempt("unclassified by LANE-B1: dev-session control. D10 still refuses daemon scale-up inside that handler",
		"POST /api/v1/dev-sessions/{name}/control",
	)
	exempt("unclassified by LANE-B1: ops-agent alertmanager receiver",
		"POST /api/v1/ops-agent/alertmanager",
	)
	exempt("unclassified by LANE-B1: operator-plane L-1 writes (patrol, hermes, drift proposals, agent deploy)",
		"POST /api/v1/hermes/run-first-task",
		"POST /api/v1/agent/nightly-run",
		"POST /api/v1/agent/deploy",
		"PUT /api/v1/agent/skills/{id}/actuation-level",
		"PUT /api/v1/patrol/skills/{id}/enable",
		"POST /api/v1/patrol/trigger/{id}",
		"POST /api/v1/patrol/webhook/{event}",
		"POST /api/v1/agent/drift-proposals",
		"POST /api/v1/agent/drift-proposals/{id}/approve",
		"POST /api/v1/agent/drift-proposals/{id}/reject",
	)
}
