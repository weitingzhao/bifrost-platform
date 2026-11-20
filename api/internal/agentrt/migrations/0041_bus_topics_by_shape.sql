-- agentrt 0041: bus topics are allowed by their shape, not one by one (LANE-C, 2026-11-20).
-- The Owner (2026-11-20): a module that joins or leaves must not mean a code
-- change. Until now every new bus topic needed a migration that listed it here
-- (0026 to 0040). From now on the closed set of bus topics is the bus's register
-- (api/internal/bus/registry/<tenant>.yaml), which the platform checks before it
-- writes; the database allows any type shaped like the register's names (a.b.c), and keeps
-- listing the ledger's own types one by one, as before.
--
-- The one statement replaces event_type_closed with a constraint that allows at
-- least everything 0040 allowed: the ledger types listed, and every bus topic by
-- its shape. Adding it checks every row, and every row passes 0040, so none is
-- read for change, changed or deleted. The ALTER's lock and statement are bounded
-- by migrate.apply (2s, 30s), and a timeout rolls the file back.
-- Way back: 0040's constraint, once no event of an unlisted bus type is left.
ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed, ADD CONSTRAINT event_type_closed CHECK (
 event_type IN (
 'plan_node_created','plan_node_retitled','plan_node_state_changed',
 'plan_change_proposed','plan_change_decided','plan_changed','mission_proposed',
 'mission_state_changed','mission_amended','task_proposed','task_state_changed',
 'task_dependency_added','task_cancelled','route_decided','attempt_leased',
 'attempt_started','attempt_progress','attempt_checkpointed','attempt_stale',
 'attempt_renewed','attempt_computed','attempt_persisted','attempt_submitted',
 'attempt_lost','attempt_ended','attempt_salvaged','attempt_cancelled','artifact_put',
 'verification_recorded','owner_wait_opened','owner_wait_closed','approval_seen',
 'question_seen','ci_seen','pipeline_seen','deploy_seen','owner_signoff',
 'actor_registered','host_registered','host_state_changed','host_retired','control_set',
 'control_cleared','rule_proposed','rule_signed','rule_tightened','rule_expired',
 'rule_approved','capability_scored','candidate_status_changed','budget_charged',
 'pool_cooldown_set','pool_measured','message_posted','message_acked','policy_alert',
 'decision_recorded','host_slots_set','wave_opened','wave_closed','wave_abandoned',
 'note','wait_limit_passed','legacy_imported','metrics_computed','metric_label_set',
 'ci_reused','ci_red_classified','controller_loop_failing','controller_loop_recovered',
 'artifact_refused','full_ci_noted','external_work_noted','plan_card_answered_by_rule',
 'plan_card_settled','plan_observation','plan_observation_pass','plan_step_marked_done',
 'plan_view_adopted','action_traced','home_layout_saved')
 OR event_type ~ '^[a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+$');
