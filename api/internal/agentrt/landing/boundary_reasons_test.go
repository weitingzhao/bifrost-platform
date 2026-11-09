package landing

import (
	"os"
	"regexp"
	"testing"
)

// The registered eligibility entry point cannot add a literal refusal without
// its explanation. Dynamic/unregistered reasons are still exposed as unmapped.
func TestEligibilityBoundaryReasonsRegistered(t *testing.T) {
	text, err := os.ReadFile("eligibility.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{`refuse\("([^"]+)"\)`, `Reason: "([^"]+)"`} {
		for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(string(text), -1) {
			if _, ok := BoundaryReason(match[1]); !ok {
				t.Fatalf("unregistered eligibility reason: %s", match[1])
			}
		}
	}
	if text, ok := BoundaryReason("future_reason"); ok || text != "Unmapped constraint: future_reason" {
		t.Fatal("unknown refusal hidden")
	}
}
