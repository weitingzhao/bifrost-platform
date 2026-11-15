-- 0021: the in-database debt ledger leaves the database (LANE-BIZ-ALIGN, 2026-11-14).
-- Its code left in 15fd7992: no route, page or tool ever wrote it, and its
-- tables were empty on PROD and STG when this was written. This file removes
-- what migration 0011 added: 16 tables, 36 functions, two triggers on
-- agentrt.event, and the debt branches of two shared functions; and it takes
-- the ledger's 29 event types out of the closed vocabulary.
--
-- agentrt.event: no row is read for change, changed or deleted. What changes
-- on it is the list of types event_type_closed allows (narrowed) and two
-- triggers that fired only for debt types (dropped). The hash chain covers
-- rows, and the rows are as they were.
--
-- The migrator runs this file in one transaction (migrate.apply): any error,
-- the two refusals below included, leaves the database as it was.
--
-- DROP list (grep -niE 'drop|truncate' finds these and nothing else; no
-- TRUNCATE, no DELETE, no CASCADE):
--   DROP TRIGGER   event_05_debt_shape, event_90_debt_facts ON agentrt.event
--   DROP FUNCTION  36 functions of the ledger (named below)
--   DROP TABLE     agentrt.debt, debt_acceptance_run, debt_counter, debt_decision, debt_edge, debt_evidence,
--                  debt_evidence_check, debt_part, debt_part_landing, debt_part_order, debt_ref, debt_text,
--                  debt_transition, debt_work, ratchet, accepted_gap
--   DROP SEQUENCE  agentrt.debt_record_seq
--   DROP CONSTRAINT event_type_closed ON agentrt.event (added again in the same statement group, narrower)
-- Rollback (structure only; there is no data to bring back): migrations 0011
-- as the base, see biz-align/ddl-0021/rollback.sql in the shared notes.

-- 1. Look before acting. The ledger's tables must be as empty as they were
--    measured, and no event of its types or on its streams may exist. debt_counter holds the
--    one row 0011 itself inserted (its floor); more than that row is data.
DO $$
DECLARE
  t text;
  n bigint;
BEGIN
  FOREACH t IN ARRAY ARRAY['debt','debt_acceptance_run','debt_decision','debt_edge','debt_evidence','debt_evidence_check','debt_part','debt_part_landing','debt_part_order','debt_ref','debt_text','debt_transition','debt_work','ratchet','accepted_gap']
  LOOP
    EXECUTE format('SELECT count(*) FROM agentrt.%I', t) INTO n;
    IF n > 0 THEN
      RAISE EXCEPTION 'agentrt 0021: agentrt.% has % row(s); the debt ledger is not empty and nothing is dropped', t, n;
    END IF;
  END LOOP;
  SELECT count(*) INTO n FROM agentrt.debt_counter;
  IF n > 1 THEN
    RAISE EXCEPTION 'agentrt 0021: agentrt.debt_counter has % rows; only the one 0011 inserted is expected and nothing is dropped', n;
  END IF;
  SELECT count(*) INTO n FROM agentrt.event WHERE event_type IN (
    'debt_filed','debt_edited','debt_text_added','debt_evidence_added',
    'debt_evidence_checked','debt_state_changed','debt_flow_set','debt_edge_added',
    'debt_edge_ended','debt_ref_added','debt_ref_ended','debt_work_linked',
    'debt_work_ended','debt_part_set','debt_part_landed','debt_acceptance_set',
    'debt_acceptance_reviewed','debt_acceptance_ran','debt_signed_off','debt_auto_closed',
    'debt_reopened','debt_signoff_rejected','debt_merged','debt_floor_raised',
    'debt_decision_set','ratchet_registered','ratchet_changed','gap_accepted',
    'gap_reopened');
  IF n > 0 THEN
    RAISE EXCEPTION 'agentrt 0021: % event(s) of the debt ledger''s types exist; the vocabulary is not narrowed and nothing is dropped', n;
  END IF;
  -- The two triggers dropped below fire for the ledger's streams (debt:...),
  -- whatever the event's type: an event on one is the ledger in use too.
  SELECT count(*) INTO n FROM agentrt.event WHERE event_stream_id LIKE 'debt:%';
  IF n > 0 THEN
    RAISE EXCEPTION 'agentrt 0021: % event(s) on a debt: stream exist; the ledger''s triggers on event stay and nothing is dropped', n;
  END IF;
END $$;

-- 2. The two triggers on agentrt.event that 0011 added. Both returned at once
--    for any type that is not the ledger's.
DROP TRIGGER event_05_debt_shape ON agentrt.event;
DROP TRIGGER event_90_debt_facts ON agentrt.event;

-- 3. The two shared functions, as they are without the ledger's branches.
--    Every other branch is word for word what the database had (0011, 0020):
--    for a type and a table that remain, the answer is the same.
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported'
          AND tbl NOT IN ('person','intake_client','issue','issue_text','issue_ask','issue_work','issue_check')) OR CASE tbl
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
    ELSE false
  END
$$;

CREATE OR REPLACE FUNCTION agentrt.event_names_row(body jsonb, tbl text, id text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE tbl
    WHEN 'actor'           THEN COALESCE(body->'actors' ? id, false) OR body->>'actor_id' IS NOT DISTINCT FROM id
    WHEN 'capability_fact' THEN COALESCE(body->'capability_facts' ? id, false)
    WHEN 'host'            THEN body->>'host_id' IS NOT DISTINCT FROM id
    WHEN 'control'         THEN body->>'control_id' IS NOT DISTINCT FROM id
    WHEN 'vendor_pool'     THEN body->>'vendor_pool_id' IS NOT DISTINCT FROM id
    WHEN 'message'         THEN body->>'message_id' IS NOT DISTINCT FROM id
    WHEN 'job'             THEN COALESCE(body->'job_ids' ? id, false)
    WHEN 'job_candidate'   THEN COALESCE(body->'job_candidate_ids' ? id, false) OR body->>'job_candidate_id' IS NOT DISTINCT FROM id
    WHEN 'person'          THEN body->>'person_id' IS NOT DISTINCT FROM id
    WHEN 'intake_client'   THEN body->>'intake_client_id' IS NOT DISTINCT FROM id
    WHEN 'issue'           THEN body->>'issue_id' IS NOT DISTINCT FROM id
    WHEN 'issue_text'      THEN body->>'text_id' IS NOT DISTINCT FROM id
                                OR COALESCE(body->'text_ids' @> jsonb_build_array(id::bigint), false)
    WHEN 'issue_ask'       THEN body->>'ask_id' IS NOT DISTINCT FROM id
                                OR COALESCE(body->'ask_ids' @> jsonb_build_array(id::bigint), false)
    WHEN 'issue_check'     THEN body->>'check_id' IS NOT DISTINCT FROM id
    WHEN 'issue_work'      THEN true
    ELSE true
  END
$$;

-- 4. The functions that take a row of a ledger table: before the tables.
DROP FUNCTION agentrt.debt_owner_signs(agentrt.debt);
DROP FUNCTION agentrt.debt_verified_landing(agentrt.debt_part_landing);
DROP FUNCTION agentrt.debt_close_refusal(agentrt.debt);
DROP FUNCTION agentrt.debt_move_refusal(agentrt.debt,agentrt.debt,agentrt.event);

-- 5. The tables, in one statement so their references to each other need no
--    order, and without CASCADE: a dependency nobody measured is an error
--    here and the whole file rolls back. Their triggers, indexes and identity
--    sequences go with them.
DROP TABLE agentrt.debt, agentrt.debt_acceptance_run, agentrt.debt_decision, agentrt.debt_edge, agentrt.debt_evidence, agentrt.debt_evidence_check,
  agentrt.debt_part, agentrt.debt_part_landing, agentrt.debt_part_order, agentrt.debt_ref, agentrt.debt_text, agentrt.debt_transition,
  agentrt.debt_work, agentrt.ratchet, agentrt.accepted_gap, agentrt.debt_counter;
DROP SEQUENCE agentrt.debt_record_seq;

-- 6. The rest of the ledger's functions.
DROP FUNCTION agentrt.check_debt_event();
DROP FUNCTION agentrt.check_debt_event_shape();
DROP FUNCTION agentrt.check_debt_flow();
DROP FUNCTION agentrt.record_debt_transition();
DROP FUNCTION agentrt.guard_debt();
DROP FUNCTION agentrt.guard_debt_acceptance_run();
DROP FUNCTION agentrt.guard_debt_counter();
DROP FUNCTION agentrt.guard_debt_edge();
DROP FUNCTION agentrt.guard_debt_evidence();
DROP FUNCTION agentrt.guard_debt_evidence_check();
DROP FUNCTION agentrt.guard_debt_part();
DROP FUNCTION agentrt.guard_debt_part_landing();
DROP FUNCTION agentrt.guard_debt_part_order();
DROP FUNCTION agentrt.guard_debt_ref();
DROP FUNCTION agentrt.guard_debt_side();
DROP FUNCTION agentrt.guard_debt_text();
DROP FUNCTION agentrt.guard_debt_work();
DROP FUNCTION agentrt.debt_body_names(jsonb,text,text);
DROP FUNCTION agentrt.debt_event(text[]);
DROP FUNCTION agentrt.debt_has_words(text);
DROP FUNCTION agentrt.debt_imported_awaiting(text);
DROP FUNCTION agentrt.debt_imported_refusal(text);
DROP FUNCTION agentrt.debt_landed_refusal(text);
DROP FUNCTION agentrt.debt_last_deploy(text);
DROP FUNCTION agentrt.debt_live_work(text);
DROP FUNCTION agentrt.debt_named_text(text,text,jsonb);
DROP FUNCTION agentrt.debt_part_after(bigint,bigint);
DROP FUNCTION agentrt.debt_side_event(text);
DROP FUNCTION agentrt.debt_spec_sha(jsonb);
DROP FUNCTION agentrt.debt_text_kind_ok(text,text,text);
DROP FUNCTION agentrt.debt_waits_on(text,text);
DROP FUNCTION agentrt.debt_writer_ok(text,text,jsonb);

-- 7. The closed vocabulary without the ledger's 29 types. The check is
--    validated against every event: step 1 found none of those types.
ALTER TABLE agentrt.event DROP CONSTRAINT event_type_closed;
ALTER TABLE agentrt.event ADD CONSTRAINT event_type_closed CHECK (event_type IN (
  'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed','plan_change_decided',
  'plan_changed','mission_proposed','mission_state_changed','mission_amended','task_proposed',
  'task_state_changed','task_dependency_added','task_cancelled','route_decided','attempt_leased',
  'attempt_started','attempt_progress','attempt_checkpointed','attempt_stale','attempt_renewed',
  'attempt_computed','attempt_persisted','attempt_submitted','attempt_lost','attempt_ended',
  'attempt_salvaged','attempt_cancelled','artifact_put','verification_recorded','owner_wait_opened',
  'owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen',
  'deploy_seen','owner_signoff','actor_registered','host_registered','host_state_changed',
  'host_retired','control_set','control_cleared','rule_proposed','rule_signed',
  'rule_tightened','rule_expired','rule_approved','capability_scored','candidate_status_changed',
  'budget_charged','pool_cooldown_set','pool_measured','message_posted','message_acked',
  'policy_alert','decision_recorded','issue_submitted','issue_added','issue_brief_drafted',
  'issue_asked','issue_answered','issue_classified','issue_design_put','issue_reviewed',
  'issue_owner_decided','issue_priority_set','issue_taken','issue_replied','issue_state_changed',
  'issue_merged','intake_client_set','intake_acked','host_slots_set','wave_opened',
  'wave_closed','wave_abandoned','note','wait_limit_passed','legacy_imported',
  'metrics_computed','metric_label_set','ci_reused','ci_red_classified'));
