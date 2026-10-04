package probe

import (
	"encoding/json"
	"fmt"
	"mime"
	"strings"
)

// TradeGatewayRoute is one thing the platform probes on the Trade gateway: a router inside one
// API process, reached through that process's own gateway prefix.
//
// One gateway prefix per process (TD-55, Owner 2026-10-04 option B): /api/monitor (api-monitor,
// which also serves the ops router at /ops/* and the docs aggregate at /research/docs/*),
// /api/account (api-account), /api/market (api-market) and /api/research (api-research). The
// gateway (bifrost-trade-infra k8s/overlays/{dev,stg,prod}/trade-ingressroute.yaml) strips
// `/api/<Prefix>` and forwards to the process's Service.
//
// The older prefixes /api/docs, /api/ops (api-monitor) and /api/trading, /api/strategy,
// /api/portfolio (api-account) were aliases of the same processes. B1 moved every platform caller
// off them; B2 (Owner 2026-10-04) removed their routes and Services, so they now fall through to
// the SPA (RetiredTradeGatewayPrefixes; nothing may probe them).
//
// ProbePath is the path under the prefix that the route's own router answers, and Service is the
// `service` its JSON names (measured on DEV/STG/PROD 2026-10-04). An unrouted `/api/<x>/…` falls
// through to the SPA and answers index.html with 200, which is why a probe reads the body and not
// only the status code.
type TradeGatewayRoute struct {
	ID        string // matrix target "api-<ID>" and registry id, e.g. "ops"
	Prefix    string // the process's own gateway prefix without /api/, e.g. "monitor"
	Process   string // Deployment whose pods answer it, e.g. "api-monitor"
	Port      int    // container port of that process
	ProbePath string // path under the prefix, e.g. "/ops/health" (→ /api/monitor/ops/health)
	Service   string // `service` the probe path's JSON names; "" = not checked (/status has none)
}

var tradeGatewayRoutes = []TradeGatewayRoute{
	{ID: "monitor", Prefix: "monitor", Process: "api-monitor", Port: 8765, ProbePath: "/status"},
	{ID: "docs", Prefix: "monitor", Process: "api-monitor", Port: 8765, ProbePath: "/research/docs/health", Service: "bifrost-docs"},
	{ID: "ops", Prefix: "monitor", Process: "api-monitor", Port: 8765, ProbePath: "/ops/health", Service: "bifrost-ops"},
	{ID: "account", Prefix: "account", Process: "api-account", Port: 8769, ProbePath: "/health", Service: "bifrost-account"},
	{ID: "market", Prefix: "market", Process: "api-market", Port: 8772, ProbePath: "/health", Service: "bifrost-market"},
	{ID: "research", Prefix: "research", Process: "api-research", Port: 8773, ProbePath: "/health", Service: "bifrost-research"},
}

// retiredTradeGatewayPrefixes are the alias prefixes TD-55 B2 removed from the gateway.
var retiredTradeGatewayPrefixes = []string{"docs", "ops", "trading", "strategy", "portfolio"}

// TradeGatewayRoutes returns the probed routes in probe order: four processes, plus the ops and
// docs routers inside api-monitor.
func TradeGatewayRoutes() []TradeGatewayRoute {
	out := make([]TradeGatewayRoute, len(tradeGatewayRoutes))
	copy(out, tradeGatewayRoutes)
	return out
}

// RetiredTradeGatewayPrefixes returns the gateway prefixes TD-55 B2 removed (without /api/).
// "docs" and "ops" stay route IDs (routers inside api-monitor, at /api/monitor/...); only their
// own prefixes are gone.
func RetiredTradeGatewayPrefixes() []string {
	out := make([]string, len(retiredTradeGatewayPrefixes))
	copy(out, retiredTradeGatewayPrefixes)
	return out
}

// TradeRouteFor looks a route up by ID ("ops", not "/api/ops"). The retired account alias names
// ("trading", "strategy", "portfolio") are not IDs and resolve to nothing since TD-55 B2.
func TradeRouteFor(id string) (TradeGatewayRoute, bool) {
	for _, r := range tradeGatewayRoutes {
		if r.ID == id {
			return r, true
		}
	}
	return TradeGatewayRoute{}, false
}

// TradeProcesses returns the distinct processes behind the routes, in first-seen order.
func TradeProcesses() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range tradeGatewayRoutes {
		if !seen[r.Process] {
			seen[r.Process] = true
			out = append(out, r.Process)
		}
	}
	return out
}

// TradeGatewayPath is the full gateway path of a path under a process's router, e.g.
// TradeGatewayPath("ops", "/ops/data-probe") = /api/monitor/ops/data-probe. It panics on an
// unknown id: the ids are compile-time constants of this package's callers.
func TradeGatewayPath(id, path string) string {
	r, ok := TradeRouteFor(id)
	if !ok {
		panic("probe: unknown Trade route " + id)
	}
	return "/api/" + r.Prefix + path
}

// GatewayPath is the full path the gateway is asked for, e.g. /api/monitor/ops/health.
func (r TradeGatewayRoute) GatewayPath() string {
	return "/api/" + r.Prefix + r.ProbePath
}

// TargetID is the matrix target id ("api-ops"). It names the router, not the prefix or the
// process — the process is in Target.Process.
func (r TradeGatewayRoute) TargetID() string {
	return "api-" + r.ID
}

// CheckAnswer says whether a 200 answer came from the process this route expects. An HTML body is
// the SPA fallback (the gateway has no route for the prefix); a JSON body naming another service
// means the prefix reaches the wrong router. Both fail: a green row must mean the right process
// answered.
func (r TradeGatewayRoute) CheckAnswer(contentType string, body []byte) (bool, string) {
	mt, _, _ := mime.ParseMediaType(contentType)
	trimmed := strings.TrimSpace(string(body))
	if mt == "text/html" || strings.HasPrefix(strings.ToLower(trimmed), "<!doctype") || strings.HasPrefix(trimmed, "<html") {
		return false, fmt.Sprintf("HTTP 200 but HTML — the SPA fallback answered; the gateway has no route for /api/%s", r.Prefix)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, "HTTP 200 but the body is not a JSON object"
	}
	if r.Service == "" {
		return true, fmt.Sprintf("HTTP 200 · %s", r.Process)
	}
	got, _ := payload["service"].(string)
	if got != r.Service {
		return false, fmt.Sprintf("HTTP 200 from %q, expected %q (%s)", got, r.Service, r.Process)
	}
	return true, fmt.Sprintf("HTTP 200 · %s (%s)", r.Service, r.Process)
}
