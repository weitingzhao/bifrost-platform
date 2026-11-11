package landing

// BoundaryReason explains a recorded gate reason, never computes eligibility.
// Unknown reasons remain visible rather than being interpreted as a pass.
func BoundaryReason(reason string) (string, bool) {
	text, ok := boundaryReasons[reason]
	if !ok {
		return "Unmapped constraint: " + reason, false
	}
	return text, true
}

var boundaryReasons = map[string]string{
	"repo":                "The repository is not registered for automatic landing.",
	"mode":                "The requested environment and landing mode do not match an enabled execution path.",
	"before_enable":       "The mission or accepted result predates landing enablement, or a required timestamp is missing.",
	"stale":               "The mission, plan step or no-merge setting does not permit this change to land.",
	"result":              "An accepted attempt, valid base and commit, and the required single-commit result are not all established.",
	"history":             "Main history does not establish an unlanded change; unknown, effective and reverted are distinct states.",
	"terminal":            "This landing root has a terminal record.",
	"floor":               "A structural, protected-path or content boundary prevents this landing route; inspect the gate detail.",
	"dependencies":        "Required dependencies are not established as effective on main.",
	"rule":                "The current landing rule or its path or presentation checks do not permit this route.",
	"limit":               "The daily reservation or repository in-flight limit is reached; compare current counts with the effective rule.",
	"already_on_main":     "The candidate is already effective on main and requires reconciliation, not another delivery.",
	"S5_EVIDENCE_MISSING": "This path requires the registered S5 evidence before push authorization.",
	"awaiting_grant":      "The candidate has soft refusals that require a valid, scoped grant.",
	"facts_unreadable":    "The execution gate could not establish its input facts.",
	"stage":               "The requested landing stage is not registered.",
}
