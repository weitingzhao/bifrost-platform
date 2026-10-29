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
		"POST /api/v1/approvals/claim",
		"POST /api/v1/approvals/{id}/heartbeat",
		"POST /api/v1/approvals/{id}/result",
	)
	exempt("agent runtime work queue (design v5 §5.2): a protocol for its own role, agentd, bound to one host; state changes and results are recorded by the store in one transaction; not a cluster write",
		"POST /api/v1/agentrt/claim",
		"POST /api/v1/agentrt/attempts/{id}/{action}",
		"PUT /api/v1/agentrt/artifacts/{id}",
		"POST /api/v1/agentrt/landing/refresh",
		"POST /api/v1/agentrt/claim/pinned",
	)
	exempt("agent runtime proposals and controls (design v5 §5.2): runtime state the store writes with its events in one transaction; a proposal starts nothing by itself (a mission waits for a signed standing envelope or the Owner, a task for the controller to ready it, a plan change outside the standing authorization and a plan's first import for the Owner's decision card; PV1 adopt_plan_view uses the same plan/proposals decision path), a note on an architecture row only adds words beside the tick its done rule decides, a control only stops or pauses work, and only the Owner (admin) lifts one; not a cluster write",
		"POST /api/v1/agentrt/missions",
		"POST /api/v1/agentrt/missions/{id}/tasks",
		"POST /api/v1/agentrt/goals",
		"POST /api/v1/agentrt/messages",
		"POST /api/v1/agentrt/messages/ack",
		"POST /api/v1/agentrt/controls",
		"DELETE /api/v1/agentrt/controls/{id}",
		"POST /api/v1/agentrt/hosts/{id}/drain",
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
	exempt("agent runtime landing grant revocation (W-31 core C-5, coordinator v3 §5.7, F2 A): only the Owner (admin) revokes a landing grant an approval wrote; revoking only tightens (the grant can no longer be consumed by the final gate) and needs no approval of its own; the store writes landing_grant_revoked under the repository stream lock in one transaction, and a grant already handed to the push is not revoked; no route writes a grant itself (an approve_*_landing approval's executor does); not a cluster write",
		"POST /api/v1/agentrt/landing/grants/{grant_id}/revoke",
	)
	exempt("agent runtime sign-off (W-31 2.5, the Owner 2026-10-21): only the Owner (admin) records his sign-off on a plan step whose done rule waits on it; the store writes the event in one transaction, a second press answers the first, and at most the step moves on the controller's next rules pass; not a cluster write",
		"POST /api/v1/agentrt/plan/{id}/signoff",
	)
	exempt("agent runtime vendor pools (W-31 2.4 slice ①): the Owner's (admin's) reading of a vendor quota window from its usage page; the store records it with its event in one transaction, and at most it holds work back from that account or lets it through again; not a cluster write",
		"POST /api/v1/agentrt/pools/{id}/reading",
	)
	exempt("agent runtime benchmark runs (W-31 2.4 slice ⑤): an agentd token hands in, for the host it is bound to, the outcome of the question set it ran with one executor; the store records it with its event in one transaction, and at most it changes which candidates routing's competence step lets through; not a cluster write",
		"POST /api/v1/agentrt/benchmark",
	)
	exempt("agent runtime chief exam (W-31 2.4 ④, the exam's cut §6 甲): an agentd token hands in, for the host it is bound to, the answers to a paper the platform issued it (GET /api/v1/agentrt/exams/{job}, which records the paper); the store records them with their events in one transaction and grades the run once it is whole; at most it adds capability scores of source exam, which routing does not read; not a cluster write",
		"POST /api/v1/agentrt/exams",
	)
	exempt("agent runtime branch cleanup (TD-383, the Owner's 「分支 B 用 agentd」): an agentd token reports, for the host it is bound to, what it did with the attempt branches it was given (GET /api/v1/agentrt/prunes); the deletion itself is agentd's, with its own Gitea identity, on branches Gitea lets it delete; the store records each outcome as a note in one transaction; not a cluster write",
		"POST /api/v1/agentrt/prunes",
	)
	exempt("agent runtime business issues, the crew's (W-31 BI-1 slice 2, DESIGN-crew-intake §2.2): for the issue readers only (operator and admin tokens on the readers list); runtime state the store writes with its events in one transaction, the database guarding each table; taking an issue makes its mission as POST /missions does (inside the standing envelope, or waiting on the Owner's card), and every other write only records texts, evidence or a display state; not a cluster write",
		"POST /api/v1/agentrt/issues",
		"POST /api/v1/agentrt/issues/merge",
		"POST /api/v1/agentrt/issues/{id}/additions",
		"POST /api/v1/agentrt/issues/{id}/responses",
		"POST /api/v1/agentrt/issues/{id}/brief",
		"POST /api/v1/agentrt/issues/{id}/classification",
		"POST /api/v1/agentrt/issues/{id}/design",
		"POST /api/v1/agentrt/issues/{id}/reviews",
		"POST /api/v1/agentrt/issues/{id}/close-checks",
		"POST /api/v1/agentrt/issues/{id}/owner-cards",
		"POST /api/v1/agentrt/issues/{id}/take",
		"POST /api/v1/agentrt/issues/{id}/replies",
		"POST /api/v1/agentrt/issues/{id}/close",
		"POST /api/v1/agentrt/issues/{id}/park",
		"POST /api/v1/agentrt/issues/{id}/resume",
		"POST /api/v1/agentrt/issues/{id}/reopen",
	)
	exempt("agent runtime intake (W-31 BI-1 slice 2, DESIGN-crew-intake §2.1): an external agent's intake token, below viewer, brings an issue, adds to it, relays its person's answer and acknowledges its updates, for the person it is bound to and its products only; the store records each with its event in one transaction; nothing it writes starts work; not a cluster write",
		"POST /api/v1/agentrt/intake/issues",
		"POST /api/v1/agentrt/intake/issues/{id}/additions",
		"POST /api/v1/agentrt/intake/issues/{id}/responses",
		"POST /api/v1/agentrt/intake/updates",
	)
	exempt("unclassified by LANE-B1: evidence, queue, or checklist — not a cluster write",
		"POST /api/v1/audit/append",
		"POST /api/v1/code-health/report",
		"POST /api/v1/code-health/rescan",
		"PUT /api/v1/lineage/thread-title",
		"PUT /api/v1/lineage/transcript-title",
		"POST /api/v1/agent/threads/heartbeat",
		"POST /api/v1/agent/hosts/heartbeat",
		"PUT /api/v1/agent/progress",
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
