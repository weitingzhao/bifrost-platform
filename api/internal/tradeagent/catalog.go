package tradeagent

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	ServerName    = "mcp-server-trade"
	ServerVersion = "0.1.0"
)

type DomainView struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	Process   string `json:"process"`
	Port      int    `json:"port"`
	ProbePath string `json:"probe_path"`
	Route     string `json:"route"`
	ReadOnly  bool   `json:"read_only"`
}

// AliasView is an old gateway prefix that still reaches a process until TD-55 B2.
type AliasView struct {
	Prefix  string `json:"prefix"`
	Process string `json:"process"`
	Use     string `json:"use"`
}

type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       string `json:"level"`
	Method      string `json:"method"`
	Route       string `json:"route"`
	Domain      string `json:"domain,omitempty"`
	Implemented bool   `json:"implemented"`
}

func Domains() []DomainView {
	// One gateway prefix per process (TD-55 option B): the list is probe.TradeGatewayRoutes, the
	// catalog the connectivity matrix and the release smoke read, so the three cannot disagree.
	routes := probe.TradeGatewayRoutes()
	out := make([]DomainView, 0, len(routes))
	for _, r := range routes {
		out = append(out, DomainView{
			ID: r.ID, Prefix: r.Prefix, Process: r.Process, Port: r.Port,
			ProbePath: r.ProbePath, Route: r.GatewayPath(), ReadOnly: true,
		})
	}
	return out
}

// Aliases lists the alias prefixes B2 retires (probe.TradeGatewayAliases).
func Aliases() []AliasView {
	aliases := probe.TradeGatewayAliases()
	out := make([]AliasView, 0, len(aliases))
	for _, a := range aliases {
		out = append(out, AliasView{Prefix: a.Prefix, Process: a.Process, Use: a.Use})
	}
	return out
}

func tool(name, desc, domain, route string) ToolView {
	return ToolView{
		Name:        name,
		Description: desc,
		Level:       "read",
		Method:      "GET",
		Route:       route,
		Domain:      domain,
		Implemented: true,
	}
}

func Catalog() []ToolView {
	tools := []ToolView{
		{Name: "trade_mcp_health", Description: "MCP server health + read-only mode", Level: "read", Implemented: true},
		{Name: "trade_mcp_capabilities", Description: "List read-only Trade API tools", Level: "read", Method: "GET", Route: "/api/v1/trade-agent/catalog", Implemented: true},
		{Name: "list_trade_domains", Description: "Trade API routes: one gateway prefix per process (TD-55), plus the alias prefixes B2 retires", Level: "read", Method: "GET", Route: "/api/v1/trade-agent/domains", Implemented: true},
	}
	ids := map[string]bool{}
	for _, d := range Domains() {
		ids[d.ID] = true
		tools = append(tools, tool("get_"+d.ID+"_health", "Probe "+d.ID+" health ("+d.Process+")", d.ID, d.Route))
	}
	// The health tools agents knew by an alias name keep working until B2, and read the route
	// that replaced the alias, so they do not count as traffic on the alias prefix.
	for _, a := range Aliases() {
		if ids[a.Prefix] {
			continue
		}
		r, _ := probe.TradeRouteFor(a.Use)
		tools = append(tools, tool("get_"+a.Prefix+"_health",
			"Alias of get_"+a.Use+"_health until TD-55 B2 ("+r.Process+")", a.Use, r.GatewayPath()))
	}
	return tools
}

type CatalogResponse struct {
	ServerName       string       `json:"server_name"`
	ServerVersion    string       `json:"server_version"`
	Mode             string       `json:"mode"`
	DomainCount      int          `json:"domain_count"`
	Tools            []ToolView   `json:"tools"`
	ImplementedCount int          `json:"implemented_count"`
	GeneratedAt      time.Time    `json:"generated_at"`
}

func CatalogResponseNow() CatalogResponse {
	tools := Catalog()
	impl := 0
	for _, t := range tools {
		if t.Implemented {
			impl++
		}
	}
	return CatalogResponse{
		ServerName:       ServerName,
		ServerVersion:    ServerVersion,
		Mode:             "read_only",
		DomainCount:      len(Domains()),
		Tools:            tools,
		ImplementedCount: impl,
		GeneratedAt:      time.Now().UTC(),
	}
}
