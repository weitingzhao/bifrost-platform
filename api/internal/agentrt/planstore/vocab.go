package planstore

import (
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The plan's part of the event vocabulary: its event types, the bodies of the
// ones it writes, their value checks, and its check of every event. Each
// domain declares its own (the register is bus/registry/ledger/types.yaml,
// where every type below is listed under plan; TestPlanVocabularyIsRegistered
// holds the two together). Moved from store/vocab.go on 2026-11-17.

// planTypes are the plan domain's event types. The retitle type is history:
// no body is declared for it, so none can be written. The dispatch lane's
// types (mission_*, task_*, wave_*, route_decided, decision_recorded), which
// no production code writes, moved to agentrt/legacyvocab on 2026-11-21
// (Foundation, clearing the tests' reach into the plan's package).
var planTypes = []string{
	"owner_signoff",
	"plan_change_decided", "plan_change_proposed", "plan_changed", "plan_node_created", "plan_node_retitled",
	"plan_node_state_changed",
	// The plan's own records that were notes (step 5 of the note and
	// policy_alert split, 2026-11-17; migration 0032).
	"plan_card_answered_by_rule", "plan_card_settled", "plan_observation", "plan_observation_pass",
	"plan_step_marked_done", "plan_view_adopted",
	// The register of laws (lawregister.go, 2026-11-23; migration 0044).
	"law_recorded",
	// A conclusion on a standard's step, read by step (lawsview.go; migration 0045).
	"plan_step_judged",
	// An attribution on a standard's step, read by step (lawsview.go; migration 0046).
	"plan_step_attributed",
	// A purged law's tombstone (lawpurge.go; migration 0047).
	"law_purged",
	// note: a free note on a plan node (plannote) or an architecture row's
	// note (planimport). The plan's since 2026-11-17 (TD-583); every other
	// record that was a note has its own type now, and their old rows stay.
	"note",
}

func field(name string, kind eventlog.Kind, optional bool) eventlog.Field {
	return eventlog.Field{Name: name, Kind: kind, Optional: optional}
}

// planBodies is the closed body of each plan event type that is written.
var planBodies = map[string][]eventlog.Field{
	"owner_signoff": {field("ref", eventlog.Str, false), field("binding_sha256", eventlog.Str, true), field("plan_version", eventlog.Int, true),
		field("acceptance_sha256", eventlog.Str, true), field("evidence", eventlog.IDs, true)},
	"plan_change_decided": {field("proposal_event_id", eventlog.Int, false), field("decision", eventlog.Str, false), field("by", eventlog.Str, false),
		field("question_id", eventlog.Str, true), field("error", eventlog.Str, true)},
	"plan_change_proposed": {field("changes", eventlog.Any, false), field("reason", eventlog.Str, false), field("standing", eventlog.Bool, false),
		field("token", eventlog.Str, true), field("question_id", eventlog.Str, true)},
	"plan_changed": {field("change", eventlog.Str, false), field("decided_event_id", eventlog.Int, true), field("nodes", eventlog.Any, true),
		field("bindings", eventlog.Any, true), field("note", eventlog.Str, true)},
	"plan_node_created": {field("node_kind", eventlog.Str, false), field("label", eventlog.Str, false), field("title", eventlog.Str, false),
		field("ordinal", eventlog.Int, false)},
	"plan_node_state_changed": {field("from", eventlog.Str, false), field("to", eventlog.Str, false), field("cause", eventlog.Str, false)},
	// The plan's own records that were notes (step 5 of the note and
	// policy_alert split, 2026-11-17; migration 0032): the same bodies the
	// notes carried, so readers by field read old and new alike.
	"plan_step_marked_done":      {field("text", eventlog.Str, false), field("manual", eventlog.Str, false), field("evidence", eventlog.IDs, true)},
	"plan_card_answered_by_rule": {field("text", eventlog.Str, false)},
	"plan_card_settled":          {field("text", eventlog.Str, false)},
	"plan_observation":           {field("text", eventlog.Str, false), field("plan_observation", eventlog.Any, false)},
	"plan_observation_pass":      {field("text", eventlog.Str, false), field("plan_observation_pass", eventlog.Any, false)},
	"plan_view_adopted":          {field("text", eventlog.Str, false), field("plan_view", eventlog.Any, false)},
	"note":                       {field("text", eventlog.Str, false), field("evidence", eventlog.IDs, true)},
	"plan_step_attributed": {field("step", eventlog.Str, false), field("standard", eventlog.Str, false), field("category", eventlog.Str, false),
		field("note_event_id", eventlog.Int, false), field("cause", eventlog.Str, true), field("destination", eventlog.Str, true), field("version", eventlog.Str, true)},
	"plan_step_judged": {field("step", eventlog.Str, false), field("standard", eventlog.Str, false), field("grade", eventlog.Str, false),
		field("note_event_id", eventlog.Int, false), field("version", eventlog.Str, true)},
	"law_purged": {field("law", eventlog.Str, false), field("events", eventlog.Int, false), field("reason", eventlog.Str, false),
		field("repealed_at", eventlog.Str, false), field("approval_ids", eventlog.IDs, true)},
	"law_recorded": {field("law", eventlog.Str, false), field("version", eventlog.Int, false), field("op", eventlog.Str, false),
		field("text", eventlog.Str, false), field("status", eventlog.Str, false), field("source", eventlog.Str, false),
		field("said_on", eventlog.Str, true), field("where", eventlog.Str, true), field("enforced_by", eventlog.Str, true),
		field("old_wording", eventlog.Str, true), field("approval_id", eventlog.Str, true), field("standards", eventlog.Any, true),
		field("frame", eventlog.Any, true)},
}

// statesOf is every state a state machine names.
func statesOf(moves map[string][]string) []string {
	var out []string
	for from, tos := range moves {
		out = append(out, from)
		out = append(out, tos...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// planValues are the checked fields of the plan's bodies.
var planValues = map[string]map[string]eventlog.Value{
	"plan_change_decided":     {"decision": eventlog.OneOf("applied", "refused", "failed", "withdrawn", "expired")},
	"plan_node_created":       {"node_kind": eventlog.OneOf("round", "item", "step", "arch_root", "arch_row")},
	"law_recorded":            {"op": eventlog.OneOf("transcribed", "add", "amend", "repeal")},
	"plan_step_judged":        {"grade": eventlog.OneOf("达到", "部分", "没达到", "没核")},
	"plan_step_attributed":    {"category": eventlog.OneOf("法", "Agent", "工具", "没核")},
	"plan_node_state_changed": {"from": eventlog.OneOf(statesOf(StepMoves)...), "to": eventlog.OneOf(statesOf(StepMoves)...)},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  planTypes,
		Bodies: planBodies,
		Values: planValues,
		// The observation, pass and view records are held to their shapes by
		// the check the notes had (CheckViewNote picks the field present).
		Before: map[string]func(map[string]any) error{
			"note":             func(body map[string]any) error { s, _ := body["text"].(string); return checkNoteText(s) },
			"plan_observation": CheckViewNote, "plan_observation_pass": CheckViewNote, "plan_view_adopted": CheckViewNote,
		},
		Events: []func(eventlog.Event) error{CheckPlanObservation},
	})
}
