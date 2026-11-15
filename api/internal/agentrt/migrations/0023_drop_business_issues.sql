-- 0023: business issues (BI) leave the database (LANE-BIZ-ALIGN, 2026-11-15).
-- The Owner, 2026-11-15, as the coordinator relayed it: business issues are
-- deleted, no longer kept frozen. Their code left in cceff576. Their seven
-- tables were empty on PROD and STG when this was written and no event of
-- their types or on their streams existed. This file removes what migration
-- 0009 added: seven tables, fifteen functions, two triggers on
-- agentrt.mission, and the issue branches of three shared functions; and it
-- takes the sixteen issue and intake event types out of the closed vocabulary.
--
-- agentrt.event: no row is read for change, changed or deleted. What changes
-- on it is the list of types event_type_closed allows (narrowed). The hash
-- chain covers rows, and the rows are as they were.
--
-- The migrator runs this file in one transaction (migrate.apply): any error,
-- the refusals of the two helpers and of the narrowed constraint included, leaves the database as it was.
--
-- Written in the shape the "schema change with a release" class takes
-- (CI line, 2026-11-15): only statements that cannot destroy a row. A table
-- goes only through agentrt.drop_table_if_empty (one row and it raises, and
-- the whole file rolls back); the extra emptiness checks go through
-- agentrt.assert_no_rows; both come from migration 0022. No DO block, no
-- cascading drop, no bare DROP TABLE, TRUNCATE, DELETE, UPDATE or INSERT.
--
-- What is removed:
--   triggers   mission_80_end_issue_work, mission_90_issue_work ON agentrt.mission
--   functions  agentrt.intake_sees and the fourteen others of business issues (named below)
--   tables     agentrt.issue_check, issue_ask, issue_work, issue_text, issue, intake_client, person (each only if empty)
--   constraint event_type_closed ON agentrt.event (added again in the same statement group, narrower)
-- Rollback (structure only; there is no data to bring back): migration 0009
-- as the base, see biz-align/ddl-0022/rollback.sql (made for this file when it was numbered 0022) in the shared notes.

-- 1. Look before acting. No event may be on a stream of business issues and
--    no mission may carry an issue's work id. (That no event of the sixteen
--    types exists is checked by step 7: adding the narrowed constraint
--    validates every row. That the tables are empty is checked where each
--    is dropped.)
SELECT agentrt.assert_no_rows('agentrt.event', 'event_stream_id', 'issue:%');
SELECT agentrt.assert_no_rows('agentrt.event', 'event_stream_id', 'intake:%');
SELECT agentrt.assert_no_rows('agentrt.mission', 'work_id', 'BI-%');

-- 2. The two triggers on agentrt.mission that 0009 added: the check that a
--    BI- mission belongs to an issue's work, and the ending of an issue's
--    work with its mission.
DROP TRIGGER mission_80_end_issue_work ON agentrt.mission;
DROP TRIGGER mission_90_issue_work ON agentrt.mission;

-- 3. The three shared functions, as they are without the issue branches.
--    Every other branch is word for word what the database had (0021): for a
--    type and a table that remain, the answer is the same.
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported') OR CASE tbl
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
    ELSE true
  END
$$;

CREATE OR REPLACE FUNCTION agentrt.only_liveness(tbl text, o jsonb, n jsonb) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE live text[];
BEGIN
  IF tbl = 'host' THEN
    live := ARRAY['last_poll_at','signals_json'];
  ELSE
    RETURN false;
  END IF;
  IF (o - live) IS DISTINCT FROM (n - live) THEN
    RETURN false;
  END IF;
  RETURN true;
END $$;

-- 4. The one function that takes a row of agentrt.issue, before the table.
DROP FUNCTION agentrt.intake_sees(text, agentrt.issue);

-- 5. The seven tables, each only if it is empty. agentrt.issue and its texts
--    and asks point at each other, so the three constraints on issue that
--    point at them go first; after that each table is dropped once nothing
--    that remains points at it. Nothing outside the seven refers to any of
--    them (they refer out, to actor, mission and artifact). Their triggers go
--    with the tables.
ALTER TABLE agentrt.issue DROP CONSTRAINT issue_brief_text_id_issue_id_fkey;
ALTER TABLE agentrt.issue DROP CONSTRAINT issue_design_text_id_issue_id_fkey;
ALTER TABLE agentrt.issue DROP CONSTRAINT issue_brief_confirmed_ask_issue_id_fkey;
SELECT agentrt.drop_table_if_empty('agentrt.issue_check');
SELECT agentrt.drop_table_if_empty('agentrt.issue_ask');
SELECT agentrt.drop_table_if_empty('agentrt.issue_work');
SELECT agentrt.drop_table_if_empty('agentrt.issue_text');
SELECT agentrt.drop_table_if_empty('agentrt.issue');
SELECT agentrt.drop_table_if_empty('agentrt.intake_client');
SELECT agentrt.drop_table_if_empty('agentrt.person');

-- 6. The functions those tables' triggers and the two mission triggers ran,
--    and the helpers only they called.
DROP FUNCTION agentrt.check_issue_mission();
DROP FUNCTION agentrt.end_issue_work();
DROP FUNCTION agentrt.guard_intake_client();
DROP FUNCTION agentrt.guard_issue();
DROP FUNCTION agentrt.guard_issue_ask();
DROP FUNCTION agentrt.guard_issue_check();
DROP FUNCTION agentrt.guard_issue_text();
DROP FUNCTION agentrt.guard_issue_work();
DROP FUNCTION agentrt.guard_person();
DROP FUNCTION agentrt.intake_voice_ok(text, text, text);
DROP FUNCTION agentrt.issue_event(text);
DROP FUNCTION agentrt.issue_text_kind_ok(text, text);
DROP FUNCTION agentrt.issue_text_sha(bigint);
DROP FUNCTION agentrt.issue_writer_ok(text, text);

-- 7. The closed vocabulary without the sixteen issue and intake types. The
--    check is validated against every event when it is added: one event of
--    those types and it fails, and the whole file rolls back.
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
  'policy_alert','decision_recorded','host_slots_set','wave_opened','wave_closed',
  'wave_abandoned','note','wait_limit_passed','legacy_imported','metrics_computed',
  'metric_label_set','ci_reused','ci_red_classified'));
