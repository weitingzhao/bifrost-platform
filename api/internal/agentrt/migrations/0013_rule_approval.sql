-- agentrt 0013: a capacity change approved on a card (R5 phase 2 step 3b;
-- the Owner 2026-11-06 Q1 A, Q2 A, Q5 A). A rule version that changes only
-- capacity fields (rules.CapacityOnly) may take effect without the Owner's
-- signature once he approved the agentrt_rule_approve card for it on the
-- PROD Console: the row names that approval in approval_id, and its event is
-- rule_approved. Everything else still needs a signature or a shown
-- tightening.
--
-- Upgrading: forward only; run this before the platform that approves rules
-- by card. An older platform still reads every row (it never selects
-- approval_id) and writes none with it.
ALTER TABLE agentrt.rule ADD COLUMN approval_id text CHECK (approval_id IS NULL OR approval_id ~ '^appr_[0-9a-f]{16}$');
DO $$
DECLARE
  name text;
  n int;
BEGIN
  SELECT count(*), min(conname) INTO n, name FROM pg_constraint
    WHERE conrelid = 'agentrt.rule'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) = 'CHECK (((direction = ''tighten''::text) OR (signature IS NOT NULL)))';
  IF n <> 1 THEN
    RAISE EXCEPTION 'agentrt 0013: expected one CHECK tying an untightening rule to a signature, found %', n;
  END IF;
  EXECUTE format('ALTER TABLE agentrt.rule DROP CONSTRAINT %I', name);
END $$;
ALTER TABLE agentrt.rule ADD CONSTRAINT rule_signed_or_approved
  CHECK (direction = 'tighten' OR signature IS NOT NULL OR approval_id IS NOT NULL);

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
  'rule_proposed','rule_signed','rule_tightened','rule_expired','rule_approved',
  'capability_scored','candidate_status_changed','budget_charged','pool_cooldown_set','pool_measured',
  'message_posted','message_acked','policy_alert',
  'decision_recorded',
  'issue_submitted','issue_added','issue_brief_drafted','issue_asked','issue_answered','issue_classified',
  'issue_design_put','issue_reviewed','issue_owner_decided','issue_priority_set','issue_taken','issue_replied',
  'issue_state_changed','issue_merged','intake_client_set','intake_acked',
  'debt_filed','debt_edited','debt_text_added','debt_evidence_added','debt_evidence_checked','debt_state_changed',
  'debt_flow_set','debt_edge_added','debt_edge_ended','debt_ref_added','debt_ref_ended','debt_work_linked','debt_work_ended',
  'debt_part_set','debt_part_landed','debt_acceptance_set','debt_acceptance_reviewed','debt_acceptance_ran',
  'debt_signed_off','debt_auto_closed','debt_reopened','debt_signoff_rejected','debt_merged','debt_floor_raised',
  'debt_decision_set','ratchet_registered','ratchet_changed','gap_accepted','gap_reopened',
  'note','wait_limit_passed','legacy_imported'));
