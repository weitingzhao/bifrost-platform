package tradeagent

import "testing"

func TestDomainsReturnsOnePrefixPerProcess(t *testing.T) {
	domains := Domains()
	if len(domains) != 6 {
		t.Fatalf("Domains() len = %d, want 6 (4 processes; ops and docs routers on api-monitor)", len(domains))
	}
	prefixes := map[string]bool{}
	for _, d := range domains {
		prefixes[d.Prefix] = true
		if d.Route != "/api/"+d.Prefix+d.ProbePath {
			t.Fatalf("domain %q route %q", d.ID, d.Route)
		}
	}
	if len(prefixes) != 4 || !prefixes["monitor"] || !prefixes["account"] || !prefixes["market"] || !prefixes["research"] {
		t.Fatalf("prefixes = %v, want monitor/account/market/research (TD-55)", prefixes)
	}
	for _, d := range domains {
		if !d.ReadOnly {
			t.Fatalf("domain %q is not read-only: %+v", d.ID, d)
		}
		if d.Port == 0 || d.ProbePath == "" {
			t.Fatalf("domain %q missing port/probe path: %+v", d.ID, d)
		}
	}
}

func TestCatalogIncludesBaseToolsAndPerDomainHealthTools(t *testing.T) {
	tools := Catalog()
	// 3 base tools + one get_<domain>_health tool per domain + one per account alias name
	// (get_trading/strategy/portfolio_health keep working until TD-55 B2).
	want := 3 + len(Domains()) + 3
	if len(tools) != want {
		t.Fatalf("Catalog() len = %d, want %d", len(tools), want)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		for _, alias := range []string{"/api/trading/", "/api/strategy/", "/api/portfolio/", "/api/ops/", "/api/docs/"} {
			if len(tool.Route) >= len(alias) && tool.Route[:len(alias)] == alias {
				t.Fatalf("tool %q reads alias prefix %s (TD-55 B2 retires it): %+v", tool.Name, alias, tool)
			}
		}
		if !tool.Implemented {
			t.Fatalf("tool %q not implemented, want all read-only tools implemented", tool.Name)
		}
	}
	for _, want := range []string{"trade_mcp_health", "trade_mcp_capabilities", "list_trade_domains", "get_monitor_health", "get_research_health", "get_account_health", "get_trading_health"} {
		if !names[want] {
			t.Fatalf("Catalog() missing tool %q: %+v", want, names)
		}
	}
}

func TestCatalogResponseNowCountsImplementedTools(t *testing.T) {
	resp := CatalogResponseNow()
	if resp.ServerName != ServerName || resp.ServerVersion != ServerVersion {
		t.Fatalf("resp server identity = %+v", resp)
	}
	if resp.Mode != "read_only" {
		t.Fatalf("resp.Mode = %q, want read_only", resp.Mode)
	}
	if resp.DomainCount != len(Domains()) {
		t.Fatalf("resp.DomainCount = %d, want %d", resp.DomainCount, len(Domains()))
	}
	if resp.ImplementedCount != len(resp.Tools) {
		t.Fatalf("resp.ImplementedCount = %d, want all %d tools implemented", resp.ImplementedCount, len(resp.Tools))
	}
}
