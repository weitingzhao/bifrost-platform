package store

import "slices"

// debtValueCases are the debt events' checked fields, each with a value of
// its shape and one refused (TestEventFieldValues): every debt event's
// debt_id, and the rest one by one.
func debtValueCases() []struct{ event, field, good, bad string } {
	const sha = "05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311"
	out := []struct{ event, field, good, bad string }{
		{"debt_filed", "source", "import", "copy"},
		{"debt_filed", "project", "crew", "Crew"},
		{"debt_filed", "priority", "P0", "p0"},
		{"debt_filed", "state", "awaiting_signoff", "blocked"},
		{"debt_filed", "flow", "auto", "automatic"},
		{"debt_edited", "fields", "tags", "state"},
		{"debt_text_added", "text_kind", "no_follow_up_reason", "comment"},
		{"debt_text_added", "sha256", sha, "sha256:" + sha},
		{"debt_evidence_checked", "result", "moved", "changed"},
		{"debt_state_changed", "from", "watching", "observing"},
		{"debt_state_changed", "to", "closed", "done"},
		{"debt_state_changed", "conclusion", "boundary", "fixed"},
		{"debt_flow_set", "flow", "manual", "hand"},
		{"debt_edge_added", "edge_kind", "part_of", "child_of"},
		{"debt_edge_added", "from_debt_id", "TDD-7", "TDX-7"},
		{"debt_edge_added", "to_debt_id", "TD-12", "TD-012"},
		{"debt_edge_added", "needs", "landed", "merged"},
		{"debt_edge_ended", "ended_why", "rewired_by_merge", "deleted"},
		{"debt_ref_added", "ref_kind", "waits_on_owner", "waits"},
		{"debt_ref_ended", "outcome", "rebound", "moved"},
		{"debt_work_linked", "from", "open", "new"},
		{"debt_work_linked", "to", "doing", "working"},
		{"debt_part_landed", "commit", "c4daa4ef" + sha[:32], "c4daa4ef"},
		{"debt_part_landed", "via", "declared", "pushed"},
		{"debt_acceptance_set", "acceptance_sha256", sha, sha[:12]},
		{"debt_acceptance_reviewed", "acceptance_sha256", sha, "sha"},
		{"debt_acceptance_ran", "runner", "ci_evidence", "shell"},
		{"debt_acceptance_ran", "result", "partial", "ok"},
		{"debt_merged", "into", "TDS-3", "BI-3"},
		{"debt_merged", "from", "doing", "busy"},
		{"debt_merged", "to", "closed", "gone"},
		{"debt_signed_off", "from", "awaiting_signoff", "awaiting"},
		{"debt_signed_off", "to", "closed", "signed"},
		{"debt_auto_closed", "from", "watching", "watched"},
		{"debt_auto_closed", "to", "closed", "shut"},
		{"debt_reopened", "from", "closed", "done"},
		{"debt_reopened", "to", "doing", "redo"},
		{"debt_signoff_rejected", "from", "awaiting_signoff", "pending"},
		{"debt_signoff_rejected", "to", "doing", "back"},
		{"debt_decision_set", "state", "dropped", "closed"},
		{"ratchet_registered", "ratchet_id", "R-1", "R-0"},
		{"ratchet_changed", "ratchet_id", "R-240", "TD-240"},
		{"ratchet_changed", "state", "retired", "removed"},
		{"gap_accepted", "accepted_gap_id", "AG-3", "AG3"},
		{"gap_reopened", "accepted_gap_id", "AG-1", "TD-1"},
	}
	var events []string
	for event, fields := range debtFieldValues {
		if _, ok := fields["debt_id"]; ok {
			events = append(events, event)
		}
	}
	slices.Sort(events)
	for _, event := range events {
		out = append(out, struct{ event, field, good, bad string }{event, "debt_id", "TDS-489", "TD-0"})
	}
	return out
}
