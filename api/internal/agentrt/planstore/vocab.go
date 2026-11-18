package planstore

import (
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// The plan's part of the event vocabulary: its event types, the bodies of the
// ones it writes, their value checks, and its check of every event. Each
// domain declares its own (the register is bus/registry/ledger/types.yaml,
// where every type below is listed under plan; TestPlanVocabularyIsRegistered
// holds the two together). Moved from store/vocab.go on 2026-11-17.

// planTypes are the plan domain's event types. The dispatch lane's (task_*,
// wave_*, route_decided, decision_recorded) and the retitle and amendment
// types are history: no body is declared for them, so none can be written,
// and the database still lists them for the rows it holds.
var planTypes = []string{
	"decision_recorded", "mission_amended", "mission_proposed", "mission_state_changed", "owner_signoff",
	"plan_change_decided", "plan_change_proposed", "plan_changed", "plan_node_created", "plan_node_retitled",
	"plan_node_state_changed", "route_decided", "task_cancelled", "task_dependency_added", "task_proposed",
	"task_state_changed", "wave_abandoned", "wave_closed", "wave_opened",
	// The plan's own records that were notes (step 5 of the note and
	// policy_alert split, 2026-11-17; migration 0032).
	"plan_card_answered_by_rule", "plan_card_settled", "plan_observation", "plan_observation_pass",
	"plan_step_marked_done", "plan_view_adopted",
}

// executorPins are the pins there are (0010 task_executor_pin_check).
var executorPins = []string{"external:coordinator"}

func field(name string, kind eventlog.Kind, optional bool) eventlog.Field {
	return eventlog.Field{Name: name, Kind: kind, Optional: optional}
}

// planBodies is the closed body of each plan event type that is written.
var planBodies = map[string][]eventlog.Field{
	"mission_proposed": {field("title", eventlog.Str, false), field("token", eventlog.Str, true)},
	"mission_state_changed": {field("from", eventlog.Str, false), field("to", eventlog.Str, false), field("reason", eventlog.Str, true),
		field("question_id", eventlog.ID, true), field("answer", eventlog.Str, true)},
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
	// task_proposed: kept for migrate's 0010 trigger probe (runtime0010_test.go),
	// the one writer left; nothing in production proposes a task since the
	// dispatch lane ended. It goes with that probe and the task tables.
	"task_proposed": {field("wave", eventlog.Str, true), field("urgent", eventlog.Bool, true), field("title", eventlog.Str, false),
		field("role", eventlog.Str, false), field("off_plan_reason", eventlog.Str, true), field("core", eventlog.Bool, true),
		field("needs", eventlog.IDs, true), field("repo", eventlog.Str, true), field("base_commit", eventlog.Str, true),
		field("token", eventlog.Str, true), field("executor_pin", eventlog.Str, true), field("via_session", eventlog.Any, true),
		field("providers", eventlog.IDs, true), field("pin_reason", eventlog.Str, true)},
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

// planValues are the checked fields of the plan's bodies. A mission's states
// are storekit's table (the one its writes move missions by).
var planValues = map[string]map[string]eventlog.Value{
	"mission_state_changed":   {"from": eventlog.OneOf(statesOf(storekit.MissionMoves())...), "to": eventlog.OneOf(statesOf(storekit.MissionMoves())...)},
	"plan_change_decided":     {"decision": eventlog.OneOf("applied", "refused", "failed", "withdrawn", "expired")},
	"plan_node_created":       {"node_kind": eventlog.OneOf("round", "item", "step", "arch_root", "arch_row")},
	"plan_node_state_changed": {"from": eventlog.OneOf(statesOf(StepMoves)...), "to": eventlog.OneOf(statesOf(StepMoves)...)},
	"task_proposed": {"role": eventlog.OneOf("plan", "implement", "verify", "review", "brief", "other"),
		"executor_pin": eventlog.OneOf(executorPins...)},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  planTypes,
		Bodies: planBodies,
		Values: planValues,
		// The observation, pass and view records are held to their shapes by
		// the check the notes had (CheckViewNote picks the field present).
		Before: map[string]func(map[string]any) error{
			"plan_observation": CheckViewNote, "plan_observation_pass": CheckViewNote, "plan_view_adopted": CheckViewNote,
		},
		Events: []func(eventlog.Event) error{CheckPlanObservation},
	})
}
