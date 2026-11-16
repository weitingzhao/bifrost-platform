package workregistration

import "net/url"

// Reply projects the recorded response without deciding identity or work state.
func Reply(n Note, event int64) map[string]any {
	out := map[string]any{"external_work_id": n.ID, "event_id": event, "seq": n.Seq, "state": n.State, "plan_url": "#work?section=plan&root=" + url.QueryEscape(n.Root) + "&node=" + url.QueryEscape(n.Node)}
	// When it was registered (R5:5.5-12 §3.3): every note of the work
	// carries its registration's time, so a replay and a recovery read give
	// the first answer's.
	if n.RegisteredAt != "" {
		out["registered_at"] = n.RegisteredAt
	}
	return out
}
