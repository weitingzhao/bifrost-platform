package tradeagent

import "testing"

func TestDomainsReturnsNineReadOnlyDomains(t *testing.T) {
	domains := Domains()
	if len(domains) != 9 {
		t.Fatalf("Domains() len = %d, want 9", len(domains))
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
	// 3 base tools + one get_<domain>_health tool per domain.
	want := 3 + len(Domains())
	if len(tools) != want {
		t.Fatalf("Catalog() len = %d, want %d", len(tools), want)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if !tool.Implemented {
			t.Fatalf("tool %q not implemented, want all read-only tools implemented", tool.Name)
		}
	}
	for _, want := range []string{"trade_mcp_health", "trade_mcp_capabilities", "list_trade_domains", "get_monitor_health", "get_research_health"} {
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
