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

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

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
}
