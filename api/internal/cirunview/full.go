package cirunview

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"time"
)

// FullCIAnswer presents already-recorded daily full CI notes in their input order.
// It does not judge a run or initiate a check.
func FullCIAnswer(notes []evidence.FullCINote) map[string]any {
	runs := make([]map[string]any, len(notes))
	for i, n := range notes {
		runs[i] = map[string]any{"at": n.At.UTC().Format(time.RFC3339), "text": n.Text, "full_ci": n.Body}
	}
	return map[string]any{"runs": runs}
}
