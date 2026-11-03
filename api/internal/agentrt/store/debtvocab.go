package store

import "regexp"

// The debt events (migration 0011; DESIGN-tech-debt-runtime-2026-11-01.md
// §2.3): ids, enums and hashes, never text (§4.3). Each names the debt its
// stream is about (debt_id) and the rows it writes by the ids the store takes
// from their sequences first (<table>_id, or the list <table>_ids); the
// counter's, a decision's, a ratchet's and a gap's events are on streams of
// their own. Kept apart from vocab.go, which they join at start.
var debtBodies = map[string][]field{
	"debt_filed": {{"debt_id", kID, false}, {"source", kStr, false}, {"project", kStr, false}, {"priority", kStr, false},
		{"state", kStr, false}, {"flow", kStr, false}, {"debt_text_ids", kInts, true}, {"debt_evidence_ids", kInts, true},
		{"debt_part_ids", kInts, true}, {"debt_part_order_ids", kInts, true}},
	"debt_edited":           {{"debt_id", kID, false}, {"fields", kIDs, false}},
	"debt_text_added":       {{"debt_id", kID, false}, {"debt_text_id", kInt, false}, {"text_kind", kStr, false}, {"sha256", kStr, false}},
	"debt_evidence_added":   {{"debt_id", kID, false}, {"debt_evidence_ids", kInts, false}},
	"debt_evidence_checked": {{"debt_id", kID, false}, {"debt_evidence_check_id", kInt, false}, {"debt_evidence_id", kInt, false}, {"result", kStr, false}},
	// by_rule: the rule an automatic move rests on (the runtime's only).
	"debt_state_changed": {{"debt_id", kID, false}, {"from", kStr, false}, {"to", kStr, false}, {"conclusion", kStr, true},
		{"by_rule", kStr, true}, {"debt_text_ids", kInts, true}, {"debt_edge_ids", kInts, true}},
	"debt_flow_set": {{"debt_id", kID, false}, {"flow", kStr, false}},
	"debt_edge_added": {{"debt_id", kID, false}, {"debt_edge_id", kInt, false}, {"edge_kind", kStr, false},
		{"from_debt_id", kID, false}, {"to_debt_id", kID, false}, {"needs", kStr, true}},
	"debt_edge_ended":  {{"debt_id", kID, false}, {"debt_edge_id", kInt, false}, {"ended_why", kStr, false}},
	"debt_ref_added":   {{"debt_id", kID, false}, {"debt_ref_id", kInt, false}, {"ref_kind", kStr, false}},
	"debt_ref_ended":   {{"debt_id", kID, false}, {"debt_ref_id", kInt, false}, {"outcome", kStr, false}},
	"debt_work_linked": {{"debt_id", kID, false}, {"debt_work_id", kInt, false}, {"from", kStr, true}, {"to", kStr, true}},
	"debt_work_ended":  {{"debt_id", kID, false}, {"debt_work_id", kInt, false}},
	"debt_part_set":    {{"debt_id", kID, false}, {"debt_part_ids", kInts, true}, {"debt_part_order_ids", kInts, true}},
	"debt_part_landed": {{"debt_id", kID, false}, {"debt_part_id", kInt, false}, {"debt_part_landing_id", kInt, false},
		{"commit", kStr, false}, {"via", kStr, false}},
	"debt_acceptance_set": {{"debt_id", kID, false}, {"acceptance_sha256", kStr, false}, {"debt_text_id", kInt, true}},
	// spec_author, when given, is compared with who really wrote the spec;
	// the database reads that from the debt, never from here (0011).
	"debt_acceptance_reviewed": {{"debt_id", kID, false}, {"acceptance_sha256", kStr, false}, {"spec_author", kID, true}},
	"debt_acceptance_ran": {{"debt_id", kID, false}, {"debt_acceptance_run_id", kInt, false}, {"runner", kStr, false},
		{"result", kStr, false}},
	// from and to: the move each of these makes, which the database holds
	// to the move made (0011; a move without them is refused).
	"debt_signed_off":       {{"debt_id", kID, false}, {"debt_text_id", kInt, true}, {"from", kStr, true}, {"to", kStr, true}},
	"debt_auto_closed":      {{"debt_id", kID, false}, {"debt_text_id", kInt, true}, {"from", kStr, true}, {"to", kStr, true}},
	"debt_reopened":         {{"debt_id", kID, false}, {"debt_text_ids", kInts, true}, {"from", kStr, true}, {"to", kStr, true}},
	"debt_signoff_rejected": {{"debt_id", kID, false}, {"debt_text_ids", kInts, false}, {"from", kStr, true}, {"to", kStr, true}},
	"debt_merged": {{"debt_id", kID, false}, {"into", kID, false}, {"debt_edge_ids", kInts, true}, {"debt_text_id", kInt, true},
		{"from", kStr, true}, {"to", kStr, true}},
	"debt_floor_raised":  {{"floor", kInt, false}, {"from", kInt, false}},
	"debt_decision_set":  {{"debt_decision_id", kInt, false}, {"state", kStr, false}},
	"ratchet_registered": {{"ratchet_id", kID, false}},
	"ratchet_changed":    {{"ratchet_id", kID, false}, {"state", kStr, true}},
	"gap_accepted":       {{"accepted_gap_id", kID, false}},
	"gap_reopened":       {{"accepted_gap_id", kID, false}},
}

// The values the debt tables check (0011).
var (
	debtIDRE          = regexp.MustCompile(`^TD[SD]?-[1-9][0-9]*$`)
	debtRef           = shaped("a debt id (TD-<n>, TDS-<n> or TDD-<n>)", debtIDRE)
	debtStates        = []string{"open", "doing", "watching", "awaiting_signoff", "closed"}
	debtConclusions   = []string{"signed_off", "auto_closed", "duplicate", "withdrawn", "boundary", "superseded"}
	debtPriorities    = []string{"P0", "P1", "P2", "P3"}
	debtFlows         = []string{"manual", "auto"}
	debtCosts         = []string{"S", "M", "L", "XL"}
	debtRisks         = []string{"low", "medium", "high"}
	debtVisibilities  = []string{"project", "summary", "private"}
	debtEdgeKinds     = []string{"blocks", "part_of", "follows_from", "related"}
	debtNeeds         = []string{"landed", "closed"}
	debtEdgeEnds      = []string{"withdrawn_link", "rewired_by_merge", "blocker_withdrawn"}
	debtRefKinds      = []string{"paid_by_step", "waits_on_owner", "guarded_by", "raised_by_issue", "reclassified_as"}
	debtRefOutcomes   = []string{"satisfied", "rejected", "declined", "dropped", "rebound", "superseded"}
	debtRunners       = []string{"imported", "ci_evidence", "promql", "owner", "manual"}
	debtEvidenceKinds = []string{"code_ref", "command", "run_ref", "commit", "doc_ref", "measurement"}
	debtTextKinds     = []string{"claim", "measured", "impact", "fix", "ratchet_plan", "now", "next", "watch", "acceptance_text",
		"signoff_summary", "ratchet_text", "follow_up_text", "no_ratchet_reason", "no_follow_up_reason", "reject_reason", "note", "import_raw"}
	// debtEditable are the fields debt_edited changes.
	debtEditable = []string{"title", "area", "priority", "project", "cost", "risk", "visibility", "tags"}
	ratchetRef   = shaped("a ratchet id (R-<n>)", regexp.MustCompile(`^R-[1-9][0-9]*$`))
	gapRef       = shaped("an accepted gap id (AG-<n>)", regexp.MustCompile(`^AG-[1-9][0-9]*$`))
)

var debtFieldValues = map[string]map[string]value{
	"debt_filed": {"debt_id": debtRef, "source": oneOf("file", "import"), "project": productKey, "priority": oneOf(debtPriorities...),
		"state": oneOf(debtStates...), "flow": oneOf(debtFlows...)},
	"debt_edited":           {"debt_id": debtRef, "fields": oneOf(debtEditable...)},
	"debt_text_added":       {"debt_id": debtRef, "text_kind": oneOf(debtTextKinds...), "sha256": hex64},
	"debt_evidence_added":   {"debt_id": debtRef},
	"debt_evidence_checked": {"debt_id": debtRef, "result": oneOf("matches", "moved", "missing", "unreadable")},
	"debt_state_changed": {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...),
		"conclusion": oneOf(debtConclusions...)},
	"debt_flow_set": {"debt_id": debtRef, "flow": oneOf(debtFlows...)},
	"debt_edge_added": {"debt_id": debtRef, "edge_kind": oneOf(debtEdgeKinds...), "from_debt_id": debtRef, "to_debt_id": debtRef,
		"needs": oneOf(debtNeeds...)},
	"debt_edge_ended":          {"debt_id": debtRef, "ended_why": oneOf(debtEdgeEnds...)},
	"debt_ref_added":           {"debt_id": debtRef, "ref_kind": oneOf(debtRefKinds...)},
	"debt_ref_ended":           {"debt_id": debtRef, "outcome": oneOf(debtRefOutcomes...)},
	"debt_work_linked":         {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_work_ended":          {"debt_id": debtRef},
	"debt_part_set":            {"debt_id": debtRef},
	"debt_part_landed":         {"debt_id": debtRef, "commit": commit, "via": oneOf("runtime_landing", "declared")},
	"debt_acceptance_set":      {"debt_id": debtRef, "acceptance_sha256": hex64},
	"debt_acceptance_reviewed": {"debt_id": debtRef, "acceptance_sha256": hex64},
	"debt_acceptance_ran": {"debt_id": debtRef, "runner": oneOf(debtRunners...),
		"result": oneOf("pass", "partial", "fail", "unknown")},
	"debt_signed_off":       {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_auto_closed":      {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_reopened":         {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_signoff_rejected": {"debt_id": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_merged":           {"debt_id": debtRef, "into": debtRef, "from": oneOf(debtStates...), "to": oneOf(debtStates...)},
	"debt_decision_set":     {"state": oneOf("pending", "asked", "answered", "dropped")},
	"ratchet_registered":    {"ratchet_id": ratchetRef},
	"ratchet_changed":       {"ratchet_id": ratchetRef, "state": oneOf("active", "retired")},
	"gap_accepted":          {"accepted_gap_id": gapRef},
	"gap_reopened":          {"accepted_gap_id": gapRef},
}

// checkDebtMove holds a debt event that names a move to name it whole: from
// and to both or neither (0011 refuses the same; review 631d R4-S1).
func checkDebtMove(eventType string, body map[string]any) error {
	spec, ok := debtBodies[eventType]
	if !ok {
		return nil
	}
	var hasFrom, hasTo bool
	for _, f := range spec {
		hasFrom, hasTo = hasFrom || f.name == "from", hasTo || f.name == "to"
	}
	if !hasFrom || !hasTo {
		return nil
	}
	_, from := body["from"]
	_, to := body["to"]
	if from != to {
		return invalid("%s names a move whole, from and to, or not at all", eventType)
	}
	return nil
}

func init() {
	for t, spec := range debtBodies {
		eventTypes[t] = true
		bodies[t] = spec
	}
	for t, vals := range debtFieldValues {
		fieldValues[t] = vals
	}
}
