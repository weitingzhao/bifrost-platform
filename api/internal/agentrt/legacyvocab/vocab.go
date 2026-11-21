// Package legacyvocab declares the event types of the runtime's closed
// vocabulary that no domain package declares: the attempt, host, budget,
// pool, capability and CI types the database's event_type_closed constraint
// (migrations 0009, 0013) still lists, which are read from old streams and
// written by no code here. It moved out of agentrt/store (decoupling item 5,
// S4, 2026-11-18) with the retired-record check, which is not any one
// domain's either; the rule types, their bodies and their values stayed with
// the rule store, and every domain declares its own types the same way.
//
// A binary or a test that reads or writes these types links this package;
// agentrt/store links it too, for the tests of the trust anchors that always
// had the vocabulary from it.
package legacyvocab

import (
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// The dispatch lane's types (2026-11-21, moved from the plan's vocabulary,
// agentrt/planstore): no production code writes them since that lane ended,
// and the database still lists them for the rows it holds. Foundation's test
// kit (storekit) still writes missions, and migrate's 0010 trigger probe a
// proposed task, so the bodies of those three stay declared; the rest have
// none and cannot be written.
var dispatchTypes = []string{
	"decision_recorded", "mission_amended", "mission_proposed", "mission_state_changed", "route_decided",
	"task_cancelled", "task_dependency_added", "task_proposed", "task_state_changed",
	"wave_abandoned", "wave_closed", "wave_opened",
}

func field(name string, kind eventlog.Kind, optional bool) eventlog.Field {
	return eventlog.Field{Name: name, Kind: kind, Optional: optional}
}

// executorPins are the pins there are (0010 task_executor_pin_check).
var executorPins = []string{"external:coordinator"}

var dispatchBodies = map[string][]eventlog.Field{
	"mission_proposed": {field("title", eventlog.Str, false), field("token", eventlog.Str, true)},
	"mission_state_changed": {field("from", eventlog.Str, false), field("to", eventlog.Str, false), field("reason", eventlog.Str, true),
		field("question_id", eventlog.ID, true), field("answer", eventlog.Str, true)},
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

// A mission's states are storekit's table (the one its writes move missions by).
var dispatchValues = map[string]map[string]eventlog.Value{
	"mission_state_changed": {"from": eventlog.OneOf(statesOf(storekit.MissionMoves())...), "to": eventlog.OneOf(statesOf(storekit.MissionMoves())...)},
	"task_proposed": {"role": eventlog.OneOf("plan", "implement", "verify", "review", "brief", "other"),
		"executor_pin": eventlog.OneOf(executorPins...)},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{
			"attempt_leased", "attempt_started", "attempt_progress", "attempt_checkpointed", "attempt_stale",
			"attempt_renewed", "attempt_computed", "attempt_persisted", "attempt_submitted",
			"attempt_lost", "attempt_ended", "attempt_salvaged", "attempt_cancelled",
			"verification_recorded", "owner_wait_opened", "owner_wait_closed", "host_registered", "host_state_changed",
			"host_retired", "capability_scored", "candidate_status_changed",
			"budget_charged", "pool_cooldown_set", "pool_measured", "wait_limit_passed",
			"host_slots_set",
			"ci_reused", "ci_red_classified",
		},
		Events: []func(eventlog.Event) error{eventlog.RetiredRecord},
	})
	eventlog.Declare(eventlog.Vocabulary{Types: dispatchTypes, Bodies: dispatchBodies, Values: dispatchValues})
}
