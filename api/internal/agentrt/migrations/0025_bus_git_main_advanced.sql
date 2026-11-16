-- agentrt 0025: the bus's first event type (bus/RULES.md §7, step 1 of the
-- communication layer, 2026-11-16). The ledger the bus relays is this event
-- log; a bus topic is an event type of it, registered in
-- api/internal/bus/registry/rocket.yaml and declared to the vocabulary by its
-- publisher (bus/hook declares git.main.advanced). Bus types are the
-- three-segment names of the register, dots included; the old types keep
-- their names.
--
-- The one statement widens event_type_closed by one type: 0023's list and
-- git.main.advanced. Nothing is read for change, changed or deleted; the
-- check is validated against every event when it is added, and every event
-- already passes the narrower list. One statement, so the constraint is never
-- missing between two.
--
-- Upgrading: run this before the platform that writes the type. An older
-- platform still starts and writes none of it. A newer platform on a
-- database without this file starts as well; only the bus's gate-git writes
-- the type, and its writes are refused (the hook answers 500, the reconcile
-- loop logs) until this file runs. The ALTER takes a strong lock on event
-- while it checks the rows; migrate.apply bounds the wait (lock_timeout 2s)
-- and the statement (30s), and a timeout rolls the file back.
-- Way back: 0023's constraint, once no event of the type is left.

ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed, ADD CONSTRAINT event_type_closed CHECK (event_type IN (
 'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed',
 'plan_change_decided','plan_changed','mission_proposed','mission_state_changed','mission_amended',
 'task_proposed','task_state_changed','task_dependency_added','task_cancelled','route_decided',
 'attempt_leased','attempt_started','attempt_progress','attempt_checkpointed','attempt_stale',
 'attempt_renewed','attempt_computed','attempt_persisted','attempt_submitted','attempt_lost','attempt_ended',
 'attempt_salvaged','attempt_cancelled','artifact_put','verification_recorded','owner_wait_opened',
 'owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen','deploy_seen','owner_signoff',
 'actor_registered','host_registered','host_state_changed','host_retired','control_set','control_cleared',
 'rule_proposed','rule_signed','rule_tightened','rule_expired','rule_approved','capability_scored',
 'candidate_status_changed','budget_charged','pool_cooldown_set','pool_measured','message_posted',
 'message_acked','policy_alert','decision_recorded','host_slots_set','wave_opened','wave_closed',
 'wave_abandoned','note','wait_limit_passed','legacy_imported','metrics_computed','metric_label_set',
 'ci_reused','ci_red_classified','git.main.advanced'));
