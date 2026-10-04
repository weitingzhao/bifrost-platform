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
	Process   string `json:"process"`
	Port      int    `json:"port"`
	ProbePath string `json:"probe_path"`
	ReadOnly  bool   `json:"read_only"`
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
	// Eight gateway prefixes, four processes (TD-55): the list is probe.TradeGatewayRoutes, the
	// catalog the connectivity matrix and the release smoke read, so the three cannot disagree.
	routes := probe.TradeGatewayRoutes()
	out := make([]DomainView, 0, len(routes))
	for _, r := range routes {
		out = append(out, DomainView{ID: r.Prefix, Process: r.Process, Port: r.Port, ProbePath: r.ProbePath, ReadOnly: true})
	}
	return out
}

func tool(name, desc, domain, probe string) ToolView {
	return ToolView{
		Name:        name,
		Description: desc,
		Level:       "read",
		Method:      "GET",
		Route:       "/api/" + domain + probe,
		Domain:      domain,
		Implemented: true,
	}
}

func Catalog() []ToolView {
	tools := []ToolView{
		{Name: "trade_mcp_health", Description: "MCP server health + read-only mode", Level: "read", Implemented: true},
		{Name: "trade_mcp_capabilities", Description: "List read-only Trade API tools", Level: "read", Method: "GET", Route: "/api/v1/trade-agent/catalog", Implemented: true},
		{Name: "list_trade_domains", Description: "Trade API path domains (Phase B: 4 pods, path aliases preserved)", Level: "read", Method: "GET", Route: "/api/v1/trade-agent/domains", Implemented: true},
	}
	for _, d := range Domains() {
		tools = append(tools, tool("get_"+d.ID+"_health", "Probe "+d.ID+" domain health", d.ID, d.ProbePath))
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
