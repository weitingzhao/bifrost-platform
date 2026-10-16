-- agentrt 0006: the decision_recorded event (W-31 2.2b, the thin chief of
-- staff; the Owner chose D2 A on 2026-10-15). The chief of staff's planner
-- records each choice it makes for the Owner to audit (ADR §12.2 item 15):
-- what it decided, why, what it dropped and how to reverse it. The next
-- planning session reads these records instead of a conversation's memory.
-- The event writes no state table, so event_may_write (0002) needs nothing;
-- the closed vocabulary gets this one type and nothing else changes.
--
-- Upgrading: run this before the platform that writes the event. An older
-- platform still starts and never writes it; rolling the platform back
-- leaves the events in place, readable. The ALTER takes a strong lock on the
-- event table while it checks the existing rows.
ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed;
ALTER TABLE agentrt.event ADD CONSTRAINT event_type_closed CHECK (event_type IN (
  'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed','plan_change_decided','plan_changed',
  'mission_proposed','mission_state_changed','mission_amended',
  'task_proposed','task_state_changed','task_dependency_added','task_cancelled','route_decided',
  'attempt_leased','attempt_started','attempt_progress','attempt_checkpointed','attempt_stale','attempt_renewed',
  'attempt_computed','attempt_persisted','attempt_submitted','attempt_lost','attempt_ended','attempt_salvaged',
  'attempt_cancelled',
  'artifact_put','verification_recorded',
  'owner_wait_opened','owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen','deploy_seen','owner_signoff',
  'actor_registered','host_registered','host_state_changed','host_retired',
  'control_set','control_cleared',
  'rule_proposed','rule_signed','rule_tightened','rule_expired',
  'capability_scored','candidate_status_changed','budget_charged','pool_cooldown_set','pool_measured',
  'message_posted','message_acked','policy_alert',
  'decision_recorded',
  'note','wait_limit_passed','legacy_imported'));
