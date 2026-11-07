-- agentrt 0014: elastic executor slots (R5 phase 2 step 3b; the Owner
-- 2026-11-06 Q3, Q4). How many attempts a host is given at once moves with
-- its headroom, between the concurrency rule's min and its per_host: the
-- controller records each move as host_slots_set on the host's stream
-- (host:<id>, body: policy, params, signals, old, new, reason) and the
-- current count in host.capacity_json.effective_slots, in one transaction,
-- and the claim leases up to it. The headroom a host's agentd sends with each
-- claim (load, CPUs, memory) is kept in host.signals_json, a liveness column
-- like last_poll_at: written by the claim without an event, read by the
-- controller.
--
-- Guards (the 0002 way): host_slots_set joins the closed vocabulary and the
-- types that may write agentrt.host (it names its host by host_id, as every
-- host event does). Two functions are replaced with the same signatures,
-- their old branches word for word, so the 0002 triggers that call them stay
-- as they are: only_liveness (0009's, with signals_json beside last_poll_at)
-- and event_may_write (0011's, with host_slots_set for host).
--
-- Upgrading: run this before the platform that writes slots. An older
-- platform still starts (it reports the schema ahead) and keeps working: it
-- writes neither the column nor the event, the vocabulary only grows, and its
-- GET /hosts shows the new column as one more field. The ALTER of the event
-- vocabulary takes a strong lock on event while it checks the rows;
-- migrate.apply bounds the wait (lock_timeout 2s) and each statement (30s),
-- and a timeout rolls the whole file back.

-- The headroom of the host's latest claim that sent one (load1, cpus,
-- mem_avail_bytes, mem_total_bytes, at); NULL until then.
ALTER TABLE agentrt.host ADD COLUMN signals_json jsonb CHECK (signals_json IS NULL OR jsonb_typeof(signals_json) = 'object');

-- The closed vocabulary (0011's list and host_slots_set).
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
  'host_slots_set',
  'note','wait_limit_passed','legacy_imported'));

-- Liveness (0009, same signature): attempt and intake client as before, word
-- for word; a host's signals_json moves without an event, as its
-- last_poll_at does. With anything else in the same statement it is not
-- liveness and needs an event as usual.
CREATE OR REPLACE FUNCTION agentrt.only_liveness(tbl text, o jsonb, n jsonb) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE live text[];
BEGIN
  IF tbl = 'attempt' THEN
    live := ARRAY['heartbeat_at','lease_expires_at'];
  ELSIF tbl = 'host' THEN
    live := ARRAY['last_poll_at','signals_json'];
  ELSIF tbl = 'intake_client' THEN
    live := ARRAY['served_txid','served_event'];
  ELSE
    RETURN false;
  END IF;
  IF (o - live) IS DISTINCT FROM (n - live) THEN
    RETURN false;
  END IF;
  IF tbl = 'attempt' AND (n->>'lease_expires_at')::timestamptz < (o->>'lease_expires_at')::timestamptz THEN
    RAISE EXCEPTION 'agentrt: lease_expires_at only moves forward' USING ERRCODE = 'check_violation';
  END IF;
  IF tbl = 'intake_client' THEN
    IF (n->'served_txid', n->'served_event') IS NOT DISTINCT FROM (o->'served_txid', o->'served_event') THEN
      RETURN false; -- nothing moved: not liveness
    END IF;
    IF jsonb_typeof(n->'served_txid') IS DISTINCT FROM 'number' OR jsonb_typeof(n->'served_event') IS DISTINCT FROM 'number'
       OR (jsonb_typeof(o->'served_txid') = 'number'
           AND ((n->>'served_txid')::bigint, (n->>'served_event')::bigint) <= ((o->>'served_txid')::bigint, (o->>'served_event')::bigint)) THEN
      RAISE EXCEPTION 'agentrt: an intake client''s served cursor only moves forward' USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN true;
END $$;

-- Which event types write which state table (0011, same signature): every
-- table as before, word for word, and host_slots_set for host.
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported'
          AND tbl NOT IN ('person','intake_client','issue','issue_text','issue_ask','issue_work','issue_check')
          AND tbl NOT IN ('debt','debt_counter','debt_text','debt_evidence','debt_evidence_check','debt_edge','debt_ref',
                          'debt_work','debt_part','debt_part_order','debt_part_landing','debt_acceptance_run','debt_decision',
                          'ratchet','accepted_gap')) OR CASE tbl
    WHEN 'actor'           THEN event_type IN ('actor_registered','host_registered')
    WHEN 'host'            THEN event_type IN ('host_registered','host_state_changed','host_retired','host_slots_set')
    WHEN 'plan_node'       THEN event_type IN ('plan_node_created','plan_node_retitled','plan_node_state_changed','plan_changed')
    WHEN 'mission'         THEN event_type IN ('mission_proposed','mission_state_changed','mission_amended')
    WHEN 'task'            THEN event_type IN ('task_proposed','task_state_changed','task_cancelled','route_decided','plan_changed')
    WHEN 'task_dependency' THEN event_type = 'task_dependency_added'
    WHEN 'attempt'         THEN event_type LIKE 'attempt\_%'
    WHEN 'message'         THEN event_type IN ('message_posted','message_acked')
    WHEN 'control'         THEN event_type IN ('control_set','control_cleared')
    WHEN 'vendor_pool'     THEN event_type IN ('pool_cooldown_set','pool_measured','budget_charged')
    WHEN 'job'             THEN event_type = 'rule_signed'
    WHEN 'job_candidate'   THEN event_type IN ('rule_signed','candidate_status_changed')
    WHEN 'capability_fact' THEN event_type IN ('actor_registered','rule_signed','rule_tightened')
    WHEN 'person'          THEN event_type = 'intake_client_set'
    WHEN 'intake_client'   THEN event_type IN ('intake_client_set','intake_acked')
    WHEN 'issue'           THEN event_type IN ('issue_submitted','issue_added','issue_brief_drafted','issue_asked','issue_answered',
                                  'issue_classified','issue_design_put','issue_reviewed','issue_owner_decided','issue_priority_set',
                                  'issue_taken','issue_replied','issue_state_changed','issue_merged')
    WHEN 'issue_text'      THEN event_type IN ('issue_submitted','issue_added','issue_brief_drafted','issue_asked','issue_answered',
                                  'issue_classified','issue_design_put','issue_reviewed','issue_taken','issue_replied','issue_state_changed')
    WHEN 'issue_ask'       THEN event_type IN ('issue_brief_drafted','issue_asked','issue_answered','issue_state_changed')
    WHEN 'issue_check'     THEN event_type IN ('issue_classified','issue_reviewed','issue_owner_decided','issue_priority_set')
    WHEN 'issue_work'      THEN event_type IN ('issue_taken','mission_state_changed')
    WHEN 'debt'            THEN event_type IN ('debt_filed','debt_edited','debt_state_changed','debt_flow_set','debt_acceptance_set',
                                  'debt_acceptance_reviewed','debt_signed_off','debt_auto_closed','debt_reopened','debt_signoff_rejected',
                                  'debt_merged','debt_work_linked')
    WHEN 'debt_counter'    THEN event_type = 'debt_floor_raised'
    WHEN 'debt_text'       THEN event_type IN ('debt_filed','debt_text_added','debt_state_changed','debt_acceptance_set','debt_signed_off',
                                  'debt_auto_closed','debt_reopened','debt_signoff_rejected','debt_merged')
    WHEN 'debt_evidence'   THEN event_type IN ('debt_filed','debt_evidence_added')
    WHEN 'debt_evidence_check' THEN event_type = 'debt_evidence_checked'
    WHEN 'debt_edge'       THEN event_type IN ('debt_edge_added','debt_edge_ended','debt_merged','debt_state_changed')
    WHEN 'debt_ref'        THEN event_type IN ('debt_ref_added','debt_ref_ended')
    WHEN 'debt_work'       THEN event_type IN ('debt_work_linked','debt_work_ended')
    WHEN 'debt_part'       THEN event_type IN ('debt_filed','debt_part_set')
    WHEN 'debt_part_order' THEN event_type IN ('debt_filed','debt_part_set')
    WHEN 'debt_part_landing' THEN event_type = 'debt_part_landed'
    WHEN 'debt_acceptance_run' THEN event_type = 'debt_acceptance_ran'
    WHEN 'debt_decision'   THEN event_type = 'debt_decision_set'
    WHEN 'ratchet'         THEN event_type IN ('ratchet_registered','ratchet_changed')
    WHEN 'accepted_gap'    THEN event_type IN ('gap_accepted','gap_reopened')
    ELSE false
  END
$$;
