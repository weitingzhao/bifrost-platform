package store

import (
	"fmt"
	"sort"
)

// bodySpec is the closed body of one event type: every required key must be
// present and no other key is allowed (design v5 §5.1.3). The database
// checks the type against the full vocabulary; this table holds the bodies
// the store writes so far. A type the store does not know is refused here
// until its body is defined, rather than written with a guessed shape.
type bodySpec struct {
	required []string
	optional []string
}

var bodies = map[string]bodySpec{
	"actor_registered":        {required: []string{"actors"}, optional: []string{"note"}},
	"plan_node_created":       {required: []string{"node_kind", "label", "title", "ordinal"}},
	"plan_node_retitled":      {required: []string{"title"}, optional: []string{"label"}},
	"plan_node_state_changed": {required: []string{"from", "to", "cause"}},
	"plan_changed":            {required: []string{"change"}, optional: []string{"decided_event_id", "nodes", "bindings", "note"}},
	"mission_proposed":        {required: []string{"title"}},
	"mission_state_changed":   {required: []string{"from", "to"}, optional: []string{"reason"}},
	"task_proposed":           {required: []string{"title", "role"}, optional: []string{"off_plan_reason"}},
	"task_state_changed":      {required: []string{"from", "to"}, optional: []string{"reason"}},
	"task_dependency_added":   {required: []string{"depends_on_task_id"}},
	"attempt_leased":          {required: []string{"attempt_no", "lease_epoch", "executor"}, optional: []string{"host_id"}},
	"attempt_persisted":       {required: []string{"pushed_commit"}, optional: []string{"pushed_ref", "artifacts"}},
	"verification_recorded":   {required: []string{"verification_id", "method", "verdict"}, optional: []string{"check_ref", "env"}},
	"message_posted":          {required: []string{"message_id", "to", "kind"}},
	"message_acked":           {required: []string{"message_id"}},
	"control_set":             {required: []string{"control_id", "scope", "kind", "reason"}, optional: []string{"scope_ref"}},
	"control_cleared":         {required: []string{"control_id"}},
	"pool_cooldown_set":       {required: []string{"vendor_pool_id", "until", "reason"}},
	"legacy_imported":         {required: []string{"source"}, optional: []string{"count"}},
	"note":                    {required: []string{"text"}},
}

func checkBody(eventType string, body map[string]any) error {
	spec, ok := bodies[eventType]
	if !ok {
		return fmt.Errorf("store: event type %q has no body defined yet", eventType)
	}
	allowed := map[string]bool{}
	for _, k := range spec.required {
		if _, ok := body[k]; !ok {
			return fmt.Errorf("store: %s needs %q", eventType, k)
		}
		allowed[k] = true
	}
	for _, k := range spec.optional {
		allowed[k] = true
	}
	var extra []string
	for k := range body {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("store: %s does not take %v", eventType, extra)
	}
	return nil
}
