-- agentrt 0048: what the Owner says in the threads is its own event type,
-- owner_said (plan domain, 2026-11-25; Owner in the legislation thread: 「我觉得B
-- 好，需要自动收集」). A session hook reads the Owner's own messages from the
-- transcript and the platform records each once (stream owner-said:<transcript>),
-- so the legislation line can count, round by round, whether the same pain comes
-- back (LAW-1 s7 and the standards that wait on it). Declared by planstore,
-- registered under plan.
--
-- The one statement widens event_type_closed: 0047's list (0046's and
-- law_purged) and the new type, and 0041's shape rule for bus topics. Adding it
-- checks every row, and every row passes 0047, so none is read for change,
-- changed or deleted. The ALTER's lock and statement are bounded by
-- migrate.apply (2s, 30s), and a timeout rolls the file back.
-- Way back: 0047's constraint, once no owner_said event is left.
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
 'plan_view_adopted','action_traced','home_layout_saved','prod_release_judged','law_recorded','plan_step_judged','plan_step_attributed','law_purged','owner_said')
 OR event_type ~ '^[a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+$');
