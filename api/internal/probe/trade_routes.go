package probe

import (
	"encoding/json"
	"fmt"
	"mime"
	"strings"
)

// TradeGatewayRoute is one Trade gateway prefix and the process that answers it.
//
// The Trade gateway (bifrost-trade-infra k8s/overlays/{dev,stg,prod}/trade-ingressroute.yaml)
// strips `/api/<Prefix>` and forwards to a Service; the Services api-docs and api-ops select the
// api-monitor pods, and api-trading, api-strategy and api-portfolio select the api-account pods
// (k8s/base/apis/manifest.yaml). Eight prefixes, four processes: a prefix is an alias, not a
// boundary, so three healthy prefixes can be one healthy process (TD-55).
//
// ProbePath is the path under the prefix that the prefix's own router answers, and Service is the
// `service` its JSON names (measured on DEV/STG/PROD 2026-10-04). `/api/ops/health` and
// `/api/docs/health` are NOT those: the gateway forwards them to api-monitor's generic /health,
// which answers `bifrost-monitor` — so a probe there says nothing about the ops or docs routers.
// An unrouted `/api/<x>/…` falls through to the SPA and answers index.html with 200, which is why
// a probe reads the body and not only the status code.
type TradeGatewayRoute struct {
	Prefix    string // gateway prefix without /api/, e.g. "ops"
	Process   string // Deployment whose pods answer it, e.g. "api-monitor"
	Port      int    // container port of that process
	ProbePath string // path under the prefix, e.g. "/ops/health" (→ /api/ops/ops/health)
	Service   string // `service` the probe path's JSON names; "" = not checked (/status has none)
}

var tradeGatewayRoutes = []TradeGatewayRoute{
	{Prefix: "monitor", Process: "api-monitor", Port: 8765, ProbePath: "/status"},
	{Prefix: "docs", Process: "api-monitor", Port: 8765, ProbePath: "/research/docs/health", Service: "bifrost-docs"},
	{Prefix: "ops", Process: "api-monitor", Port: 8765, ProbePath: "/ops/health", Service: "bifrost-ops"},
	{Prefix: "trading", Process: "api-account", Port: 8769, ProbePath: "/health", Service: "bifrost-account"},
	{Prefix: "strategy", Process: "api-account", Port: 8769, ProbePath: "/health", Service: "bifrost-account"},
	{Prefix: "portfolio", Process: "api-account", Port: 8769, ProbePath: "/health", Service: "bifrost-account"},
	{Prefix: "market", Process: "api-market", Port: 8772, ProbePath: "/health", Service: "bifrost-market"},
	{Prefix: "research", Process: "api-research", Port: 8773, ProbePath: "/health", Service: "bifrost-research"},
}

// TradeGatewayRoutes returns the eight Trade gateway prefixes in probe order.
func TradeGatewayRoutes() []TradeGatewayRoute {
	out := make([]TradeGatewayRoute, len(tradeGatewayRoutes))
	copy(out, tradeGatewayRoutes)
	return out
}

// TradeRouteFor looks a gateway prefix up ("ops", not "/api/ops").
func TradeRouteFor(prefix string) (TradeGatewayRoute, bool) {
	for _, r := range tradeGatewayRoutes {
		if r.Prefix == prefix {
			return r, true
		}
	}
	return TradeGatewayRoute{}, false
}

// TradeProcesses returns the distinct processes behind the prefixes, in first-seen order.
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

// GatewayPath is the full path the gateway is asked for, e.g. /api/ops/ops/health.
func (r TradeGatewayRoute) GatewayPath() string {
	return "/api/" + r.Prefix + r.ProbePath
}

// TargetID is the matrix target id for the prefix ("api-ops"). It names the gateway prefix, not a
// process — the process is in Target.Process.
func (r TradeGatewayRoute) TargetID() string {
	return "api-" + r.Prefix
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
