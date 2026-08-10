package operatequeue

import "strings"

// FleetProbe locates the Fleet Desk cell that a remediation fixScope maps to.
// Ported from console/src/lib/control-room/fleetCellFix.ts ROUTE_TABLE (scope→cell).
type FleetProbe struct {
	Role string // rocket | satellite | engineer | vendor | ground
	Env  string // prod | stg | dev | "" (span)
}

// scopeToFleetProbe mirrors the plan + fleetCellFix reverse mapping.
var scopeToFleetProbe = map[string]FleetProbe{
	"cluster_issues_full_auto":     {Role: "satellite", Env: "prod"},
	"data-layer-backup":            {Role: "satellite", Env: "prod"},
	"platform-self-health-recover": {Role: "rocket", Env: "prod"},
	"deliver-stg-recover":          {Role: "rocket", Env: "stg"},
	"operator-plane-remediate":     {Role: "engineer", Env: ""},
	"git-dirty-remediate":          {Role: "engineer", Env: ""},
	"gitops-config-repair":         {Role: "vendor", Env: ""},
}

// liveTradingScopes always classify as NEEDS_DECISION (D10 — trade execution freeze).
var liveTradingScopes = map[string]bool{
	"trade-deploy":                true,
	"satellite-bus-ingest-triage": true,
	"massive-feed-recover":        true,
}

// liveTradingChecklistItems never auto-drain (D10 observe).
var liveTradingChecklistItems = map[string]bool{
	"ib-feed": true,
}

// FleetProbeForScope returns the Fleet cell probe target for a fixScope.
func FleetProbeForScope(scope string) (FleetProbe, bool) {
	p, ok := scopeToFleetProbe[strings.TrimSpace(scope)]
	return p, ok
}

func isLiveTradingScope(scope string) bool {
	scope = strings.TrimSpace(scope)
	if liveTradingScopes[scope] {
		return true
	}
	lower := strings.ToLower(scope)
	if strings.Contains(lower, "live-trad") || strings.Contains(lower, "daemon-scale") {
		return true
	}
	return false
}

func isLiveTradingChecklistItem(itemID string) bool {
	return liveTradingChecklistItems[strings.TrimSpace(itemID)]
}
