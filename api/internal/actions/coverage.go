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
	exempt("owner question workflow, not a catalog action",
		"POST /api/v1/questions",
		"POST /api/v1/questions/{id}/answer",
		"POST /api/v1/questions/{id}/withdraw",
		"POST /api/v1/questions/{id}/ack",
	)
	exempt("approval workflow, not a catalog action",
		"POST /api/v1/approvals",
		"POST /api/v1/approvals/{id}/approve",
		"POST /api/v1/approvals/{id}/reject",
		"POST /api/v1/approvals/{id}/resolve",
		"POST /api/v1/approvals/{id}/withdraw",
		"POST /api/v1/approvals/claim",
		"POST /api/v1/approvals/{id}/heartbeat",
		"POST /api/v1/approvals/{id}/result",
	)
	exempt("agent runtime artifacts (design v5 §5.2): the store records an upload with its event in one transaction; not a cluster write",
		"PUT /api/v1/agentrt/artifacts/{id}",
	)
	exempt("agent runtime proposals and controls (design v5 §5.2): runtime state the store writes with its events in one transaction; a proposal starts nothing by itself (a mission waits for a signed standing envelope or the Owner, a task for the controller to ready it, a plan change outside the standing authorization and a plan's first import for the Owner's decision card; PV1 adopt_plan_view uses the same plan/proposals decision path), a note on an architecture row only adds words beside the tick its done rule decides, a control only stops or pauses work, and only the Owner (admin) lifts one, a withdrawal only cancels the proposer's own tasks that have not started; not a cluster write",
		"POST /api/v1/agentrt/messages",
		"POST /api/v1/agentrt/messages/ack",
		"POST /api/v1/agentrt/controls",
		"DELETE /api/v1/agentrt/controls/{id}",
		"POST /api/v1/agentrt/plan/proposals",
		"POST /api/v1/agentrt/plan/imports",
		"POST /api/v1/agentrt/plan/{id}/notes",
	)
	exempt("agent runtime external work (W-31 PV1): registration and observations are runtime records the store writes with their events in one transaction; they grant no execution authority and cannot verify or complete work; not a cluster write",
		"POST /api/v1/agentrt/external-work",
		"POST /api/v1/agentrt/external-work/{id}/events",
	)
	exempt("agent runtime rules (design v5 §5.2): a rule takes effect only with the Owner's signature over its exact text, checked against the compiled-in key in the runtime's own namespace, or as a tightening the store proves by comparing it with the version before; check and proposals start nothing; runtime state the store writes with its events in one transaction; not a cluster write",
		"POST /api/v1/agentrt/rules/{rule_id}/check",
		"POST /api/v1/agentrt/rules/{rule_id}/proposals",
		"POST /api/v1/agentrt/rules/{rule_id}/sign",
		"POST /api/v1/agentrt/rules/{rule_id}/tighten",
	)
	exempt("agent runtime sign-off (W-31 2.5, the Owner 2026-10-21): only the Owner (admin) records his sign-off on a plan step whose done rule waits on it; the store writes the event in one transaction, a second press answers the first, and at most the step moves on the controller's next rules pass; not a cluster write",
		"POST /api/v1/agentrt/plan/{id}/signoff",
	)
	exempt("event bus gate-git (bus/RULES.md §8, step 1, 2026-11-16): Gitea's push webhook, authenticated by the HMAC-SHA256 of the raw body under BUS_GITEA_HOOK_SECRET (503 without it, 401 on a bad signature); it can only append git.main.advanced for a watched repository's main to the event log, once per commit, and starts nothing by itself; not a cluster write",
		"POST /api/v1/bus/hooks/gitea",
	)
	exempt("known regressions on main (the coordinator 2026-11-17: information, no gate; bus/regressions): a seat records that a commit broke a repository's main, or its fix, as itself (reporter and above); it appends one event to the event log and starts, stops or refuses nothing; not a cluster write",
		"POST /api/v1/bus/regressions",
		"POST /api/v1/bus/regressions/fixed",
	)
	exempt("unclassified by LANE-B1: evidence, queue, or checklist — not a cluster write",
		"POST /api/v1/audit/append",
		"POST /api/v1/code-health/report",
		"POST /api/v1/code-health/rescan",
		"PUT /api/v1/lineage/thread-title",
		"PUT /api/v1/lineage/transcript-title",
		"POST /api/v1/agent/threads/heartbeat",
		"POST /api/v1/agent/hosts/heartbeat",
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
