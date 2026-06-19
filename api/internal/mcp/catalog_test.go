package mcp

import "testing"

func TestCatalogHasImplementedTools(t *testing.T) {
	resp := ToolsResponseNow()
	if len(resp.Tools) < 10 {
		t.Fatalf("tools: got %d want >= 10", len(resp.Tools))
	}
	if resp.ImplementedCount < 10 {
		t.Fatalf("implemented: got %d", resp.ImplementedCount)
	}
	found := false
	for _, tool := range resp.Tools {
		if tool.Name == "gitops_sync_app" && tool.Implemented {
			found = true
		}
	}
	if !found {
		t.Fatal("gitops_sync_app not in catalog")
	}
}
