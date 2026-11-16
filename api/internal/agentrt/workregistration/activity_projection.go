package workregistration

import "time"

// ActivityBuckets formats external observations without deciding their verification.
func ActivityBuckets(ext []map[string]any, now time.Time, verification func(string) any) ([]any, []any) {
	working := []any{}
	finished := []any{}
	for _, e := range ext {
		e["verification"] = verification(e["external_work_id"].(string))
		if e["state"] == "Running" || e["state"] == "Registered" {
			working = append(working, e)
		}
		if end, _ := e["ended_at"].(string); len(end) >= 10 && end[:10] == now.UTC().Format("2006-01-02") {
			finished = append(finished, e)
		}
	}
	return working, finished
}
