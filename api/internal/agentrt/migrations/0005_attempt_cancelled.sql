-- agentrt 0005: the attempt_cancelled event (design v5 T34 case 8; review
-- 152 A3; the Owner chose option A on 2026-10-14). A salvaged result of a
-- lost attempt can be accepted once its own verification and review pass;
-- the transaction that accepts it cancels the task's live new attempt, so
-- the task has one accepted attempt and nothing left running for it. The
-- closed vocabulary (0002) gets this one type and nothing else changes: the
-- attempt table's guard already lets any attempt_* event write an attempt
-- row, and 'cancelled' is already an attempt state.
--
-- Upgrading: run this before the platform that writes the event, and do not
-- run an older acceptance controller beside a newer one. An older migrator
-- refuses a database that has this file; an older platform still starts
-- (it checks only the database's identity), but its controller finds no
-- salvaged results and closes a salvage check it meets as a stale ordinary
-- pin; once such a chain has its deterministic pass, the result is not
-- checked again. So settle the salvage checks in flight before rolling the
-- platform back (review 166 B2). The ALTER takes a strong lock on the event
-- table while it checks the existing rows.
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
  'note','wait_limit_passed','legacy_imported'));
