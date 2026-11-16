package planreadings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Card text only; caller retains validation, approval and writes.
func ChangeCardBody(root, reason string, changes []any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A thread proposes to change plan %s.\n\nWhy: %s\n\nChanges:\n", root, reason)
	for _, c := range changes {
		raw, _ := json.Marshal(c)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	b.WriteString("\nApplying it writes the change under your decision; not applying it leaves the plan as it is.")
	return b.String()
}
