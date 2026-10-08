package checklist

import (
	"os"
	"strings"
	"testing"
)

// ADR §6 retired checklist-driven agent dispatch (TD-208): husbandry-sync and the
// prober only merge signals. The old path reached the runners through
// internal/remediation, so one import is enough to bring it back unnoticed.
func TestChecklistNeverImportsRemediation(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `bifrost-platform/api/internal/remediation"`) {
			t.Errorf("%s imports internal/remediation — the checklist records signals and never starts an agent (TD-208)", name)
		}
	}
}
