-- 0020: the dispatch lane's four tables go (store cut 4, 2026-11-14).
--
-- Nothing proposes, leases, verifies or links a task any more, and since
-- platform 3a5285ad no code outside tests reads or writes agentrt.task,
-- attempt, verification or task_dependency. This drops the four tables, with
-- what only they held up. One file, one transaction: any step that fails
-- rolls all of it back.
--
-- What this file drops (the whole list; no TRUNCATE, no CASCADE, no DELETE):
--   constraints  event_task_id_fkey, event_attempt_id_fkey   on agentrt.event
--                message_task_id_fkey                        on agentrt.message
--                budget_ledger_attempt_id_fkey               on agentrt.budget_ledger
--   tables       agentrt.verification, task_dependency, attempt, task
--   functions    guard_verification, guard_task_dependency, guard_attempt,
--                attempt_pin_guard, guard_task, pin_immutable,
--                recheck_task_bindings, check_task_subjects
--
-- The event, message and budget_ledger rows are not touched: their task_id
-- and attempt_id columns stay, with the ids they hold, and no longer point
-- at a row (the coordinator, 2026-11-14: dropping the constraint does not
-- change the event log's content). budget_ledger itself stays: the quota
-- pools still read it.

-- 1. A step's check no longer reads the tasks' table: whether the tasks on a
--    step are of its mission, whether a task sits with its subject, and
--    whether a task a rule names is on the step. The approval and question
--    parts are as they were.
CREATE OR REPLACE FUNCTION agentrt.check_step(step_id text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  s   agentrt.plan_node;
  bad text;
BEGIN
  IF step_id IS NULL THEN
    RETURN;
  END IF;
  SELECT * INTO s FROM agentrt.plan_node WHERE plan_node_id = step_id;
  IF NOT FOUND OR s.node_kind <> 'step' THEN
    RETURN;
  END IF;
  SELECT q.ref #>> '{}' INTO bad FROM jsonb_path_query(s.done_rule_json, 'lax $.**.approval_executed') AS q(ref)
   WHERE NOT EXISTS (SELECT 1 FROM agentrt.plan_node_binding b WHERE b.plan_node_id = step_id
                       AND b.bound_kind = 'approval' AND b.bound_ref = q.ref #>> '{}')
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'agentrt: the done rule of % names approval #%, which is not bound to this step', step_id, bad
      USING ERRCODE = 'check_violation';
  END IF;
  SELECT q.ref #>> '{}' INTO bad FROM jsonb_path_query(s.done_rule_json, 'lax $.**.question_answered.question') AS q(ref)
   WHERE NOT EXISTS (SELECT 1 FROM agentrt.plan_node_binding b WHERE b.plan_node_id = step_id
                       AND b.bound_kind = 'question' AND b.bound_ref = q.ref #>> '{}')
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'agentrt: the done rule of % names question %, which is not bound to this step', step_id, bad
      USING ERRCODE = 'check_violation';
  END IF;
END $$;

-- 2. The business issues' check of evidence (0009): one statement changes.
CREATE OR REPLACE FUNCTION agentrt.guard_issue_check() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev         agentrt.event := agentrt.issue_event(NEW.issue_id);
  i          agentrt.issue;
  prev_round int;
  prev_id    bigint;
BEGIN
  IF NEW.check_kind = 'review' THEN
    SELECT review_round, issue_check_id INTO prev_round, prev_id FROM agentrt.issue_check
      WHERE issue_id = NEW.issue_id AND check_kind = 'review' ORDER BY issue_check_id DESC LIMIT 1;
    IF NEW.review_round IS DISTINCT FROM COALESCE(prev_round, 0) + 1 THEN
      RAISE EXCEPTION 'agentrt: the next review of % is round %, not %', NEW.issue_id, COALESCE(prev_round, 0) + 1, NEW.review_round
        USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.review_round >= 3 AND (SELECT d.result FROM agentrt.issue_check d WHERE d.issue_id = NEW.issue_id
         AND d.check_kind = 'owner_decision' AND d.category = 'review_dispute' AND d.issue_check_id > prev_id
         AND d.brief_sha256 = NEW.brief_sha256 AND d.design_sha256 = NEW.design_sha256
         ORDER BY d.issue_check_id DESC LIMIT 1) IS DISTINCT FROM 'another_round' THEN
      RAISE EXCEPTION 'agentrt: round % of % needs the Owner''s another_round on this design since round %', NEW.review_round,
        NEW.issue_id, prev_round USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF (NEW.check_kind, ev.event_type) NOT IN (('classification','issue_classified'),('review','issue_reviewed'),
       ('close_check','issue_reviewed'),('owner_decision','issue_owner_decided'),('priority','issue_priority_set')) THEN
    RAISE EXCEPTION 'agentrt: a % event records no % evidence', ev.event_type, NEW.check_kind USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: evidence is recorded by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  SELECT * INTO i FROM agentrt.issue WHERE issue_id = NEW.issue_id;
  IF NEW.brief_sha256 IS DISTINCT FROM agentrt.issue_text_sha(i.brief_text_id)
     OR (NEW.design_sha256 IS NOT NULL AND NEW.design_sha256 IS DISTINCT FROM agentrt.issue_text_sha(i.design_text_id)) THEN
    RAISE EXCEPTION 'agentrt: evidence for % is recorded for its current brief and design', NEW.issue_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.report_text_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM agentrt.issue_text t WHERE t.issue_text_id = NEW.report_text_id
       AND t.text_kind = (CASE NEW.check_kind WHEN 'review' THEN 'review' WHEN 'close_check' THEN 'check' ELSE 'note' END)) THEN
    RAISE EXCEPTION 'agentrt: % evidence names a text of another kind', NEW.check_kind USING ERRCODE = 'check_violation';
  END IF;
  -- 0020: a seat's answer was the report of a brief task's attempt. The
  -- tasks' and attempts' tables are dropped below, so that it is the report
  -- of a brief task can no longer be read; that it is a report still is.
  -- Nothing else of this function changes.
  IF NEW.seat_artifact_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM agentrt.artifact a
       WHERE a.artifact_id = NEW.seat_artifact_id AND a.artifact_kind = 'report') THEN
    RAISE EXCEPTION 'agentrt: a seat''s answer is a report; % is not', NEW.seat_artifact_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.check_kind = 'close_check' AND NOT EXISTS (SELECT 1 FROM agentrt.issue_ask k WHERE k.issue_ask_id = NEW.ask_id
       AND k.verdict IS NOT NULL) THEN
    RAISE EXCEPTION 'agentrt: a seat checks an answer that was given' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- 2b. The liveness columns (0014) no longer name an attempt's: its table goes
--     below. A host's and an intake client's are as they were.
CREATE OR REPLACE FUNCTION agentrt.only_liveness(tbl text, o jsonb, n jsonb) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE live text[];
BEGIN
  IF tbl = 'host' THEN
    live := ARRAY['last_poll_at','signals_json'];
  ELSIF tbl = 'intake_client' THEN
    live := ARRAY['served_txid','served_event'];
  ELSE
    RETURN false;
  END IF;
  IF (o - live) IS DISTINCT FROM (n - live) THEN
    RETURN false;
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

-- 3. The constraints that point at the tables; columns and rows stay.
ALTER TABLE agentrt.event         DROP CONSTRAINT event_task_id_fkey;
ALTER TABLE agentrt.event         DROP CONSTRAINT event_attempt_id_fkey;
ALTER TABLE agentrt.message       DROP CONSTRAINT message_task_id_fkey;
ALTER TABLE agentrt.budget_ledger DROP CONSTRAINT budget_ledger_attempt_id_fkey;

-- 4. The tables, in the order of what points at what, without CASCADE: a
--    dependency this file does not know of fails here and rolls it all back.
--    Their triggers go with them.
DROP TABLE agentrt.verification;
DROP TABLE agentrt.task_dependency;
DROP TABLE agentrt.attempt;
DROP TABLE agentrt.task;

-- 5. The functions that served only those tables.
DROP FUNCTION agentrt.guard_verification();
DROP FUNCTION agentrt.guard_task_dependency();
DROP FUNCTION agentrt.guard_attempt();
DROP FUNCTION agentrt.attempt_pin_guard();
DROP FUNCTION agentrt.guard_task();
DROP FUNCTION agentrt.pin_immutable();
DROP FUNCTION agentrt.recheck_task_bindings();
DROP FUNCTION agentrt.check_task_subjects(text);
