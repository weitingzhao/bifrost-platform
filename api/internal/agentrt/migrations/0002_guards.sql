-- agentrt 0002: guards in the database, not only in the interface (design v5
-- §5.1.1). They bind the runtime role (platform_agentrt_<env>) completely:
-- it has no DDL and cannot disable a trigger. Plain UPDATE, DELETE and
-- TRUNCATE by the owner role meet the same triggers, but the owner owns the
-- objects and can disable a trigger or replace a guard function with DDL, so
-- the owner (the migration identity) is part of the trusted management plane,
-- like a superuser (review 144 B4). The tamper test (T23b) does exactly that.

-- The closed event vocabulary (§5.1.3). A new type is a new migration.
ALTER TABLE agentrt.event ADD CONSTRAINT event_type_closed CHECK (event_type IN (
  'plan_node_created','plan_node_retitled','plan_node_state_changed','plan_change_proposed','plan_change_decided','plan_changed',
  'mission_proposed','mission_state_changed','mission_amended',
  'task_proposed','task_state_changed','task_dependency_added','task_cancelled','route_decided',
  'attempt_leased','attempt_started','attempt_progress','attempt_checkpointed','attempt_stale','attempt_renewed',
  'attempt_computed','attempt_persisted','attempt_submitted','attempt_lost','attempt_ended','attempt_salvaged',
  'artifact_put','verification_recorded',
  'owner_wait_opened','owner_wait_closed','approval_seen','question_seen','ci_seen','pipeline_seen','deploy_seen','owner_signoff',
  'actor_registered','host_registered','host_state_changed','host_retired',
  'control_set','control_cleared',
  'rule_proposed','rule_signed','rule_tightened','rule_expired',
  'capability_scored','candidate_status_changed','budget_charged','pool_cooldown_set','pool_measured',
  'message_posted','message_acked','policy_alert',
  'note','wait_limit_passed','legacy_imported'));

-- Append-only tables: no UPDATE, DELETE or TRUNCATE, owner included (T23a).
CREATE FUNCTION agentrt.forbid_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'agentrt: % on agentrt.% is not allowed: the table is append only', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END $$;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['event','artifact','artifact_blob','verification','budget_ledger',
                           'capability_score','rule','plan_node_binding','schema_migration'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change()',
                   t || '_append_only', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON agentrt.%I FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change()',
                   t || '_no_truncate', t);
  END LOOP;
END $$;

-- The event of this transaction: SET LOCAL agentrt.event_id names it, and
-- it must have been inserted by this transaction (event.txid).
CREATE FUNCTION agentrt.current_event() RETURNS agentrt.event LANGUAGE plpgsql AS $$
DECLARE
  raw text := current_setting('agentrt.event_id', true);
  ev  agentrt.event;
BEGIN
  IF raw IS NULL OR raw = '' THEN
    RAISE EXCEPTION 'agentrt: a state change needs SET LOCAL agentrt.event_id' USING ERRCODE = 'check_violation';
  END IF;
  SELECT * INTO ev FROM agentrt.event WHERE event_id = raw::bigint;
  IF NOT FOUND OR ev.txid <> txid_current() THEN
    RAISE EXCEPTION 'agentrt: event % was not written by this transaction', raw USING ERRCODE = 'check_violation';
  END IF;
  RETURN ev;
END $$;

-- The hash chain head moves with each event, in the same statement, so a
-- gap or a fork cannot be written even by direct SQL. The sha256 itself is
-- computed by the store over canonical JSON and checked by the verifier.
CREATE FUNCTION agentrt.chain_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head agentrt.event_stream;
BEGIN
  SELECT * INTO head FROM agentrt.event_stream WHERE event_stream_id = NEW.event_stream_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'agentrt: stream % does not exist', NEW.event_stream_id USING ERRCODE = 'foreign_key_violation';
  END IF;
  IF NEW.seq <> head.last_seq + 1 OR NEW.prev_sha256 <> head.last_sha256 THEN
    RAISE EXCEPTION 'agentrt: stream % expects seq % after %, got seq % after %', NEW.event_stream_id,
      head.last_seq + 1, head.last_sha256, NEW.seq, NEW.prev_sha256 USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.txid <> txid_current() THEN
    RAISE EXCEPTION 'agentrt: event txid is set by the database' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER event_chain BEFORE INSERT ON agentrt.event FOR EACH ROW EXECUTE FUNCTION agentrt.chain_event();

-- After the row exists, the head moves to it (the stream row is still
-- locked by chain_event, so no other event can slip in between).
CREATE FUNCTION agentrt.advance_stream() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE agentrt.event_stream SET last_seq = NEW.seq, last_sha256 = NEW.sha256
   WHERE event_stream_id = NEW.event_stream_id;
  RETURN NULL;
END $$;
CREATE TRIGGER event_advance AFTER INSERT ON agentrt.event FOR EACH ROW EXECUTE FUNCTION agentrt.advance_stream();

-- Stream heads move only through chain_event.
CREATE FUNCTION agentrt.guard_stream() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'agentrt: a stream is never deleted' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF TG_OP = 'INSERT' AND (NEW.last_seq <> 0 OR NEW.last_sha256 <> '') THEN
    RAISE EXCEPTION 'agentrt: a new stream starts empty' USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.event_stream_id <> OLD.event_stream_id OR NEW.last_seq <> OLD.last_seq + 1
      OR NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = NEW.event_stream_id
                     AND e.seq = NEW.last_seq AND e.sha256 = NEW.last_sha256 AND e.txid = txid_current())) THEN
    RAISE EXCEPTION 'agentrt: stream % head moves only with an event of this transaction', OLD.event_stream_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN COALESCE(NEW, OLD);
END $$;
CREATE TRIGGER event_stream_guard BEFORE INSERT OR UPDATE OR DELETE ON agentrt.event_stream
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_stream();

-- Liveness columns change without an event: attempt.heartbeat_at,
-- attempt.lease_expires_at (forward only) and host.last_poll_at. Anything
-- else changing in the same statement needs an event as usual.
CREATE FUNCTION agentrt.only_liveness(tbl text, o jsonb, n jsonb) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE live text[];
BEGIN
  IF tbl = 'attempt' THEN
    live := ARRAY['heartbeat_at','lease_expires_at'];
  ELSIF tbl = 'host' THEN
    live := ARRAY['last_poll_at'];
  ELSE
    RETURN false;
  END IF;
  IF (o - live) IS DISTINCT FROM (n - live) THEN
    RETURN false;
  END IF;
  IF tbl = 'attempt' AND (n->>'lease_expires_at')::timestamptz < (o->>'lease_expires_at')::timestamptz THEN
    RAISE EXCEPTION 'agentrt: lease_expires_at only moves forward' USING ERRCODE = 'check_violation';
  END IF;
  RETURN true;
END $$;

-- Which event types may write which state table (review 144 A1). The
-- one-time import (legacy_imported) may write any of them.
CREATE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT event_type = 'legacy_imported' OR CASE tbl
    WHEN 'actor'           THEN event_type IN ('actor_registered','host_registered')
    WHEN 'host'            THEN event_type IN ('host_registered','host_state_changed','host_retired')
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

-- Tables whose rows the envelope keys cannot name are named in the body: a
-- single id field, or a list for the events that write several rows (the
-- seed's actors and capability facts). The id column is <table>_id.
CREATE FUNCTION agentrt.event_names_row(body jsonb, tbl text, id text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
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

-- State tables: every INSERT and UPDATE carries an event of this
-- transaction, of a type allowed for the table, that names the row: by the
-- envelope keys the row has (mission, plan node, task, attempt) and, for
-- tables those keys cannot name, by the id in the body (T25, database half).
-- Only an authorized plan change may move several plan nodes or tasks the
-- envelope does not name, and only the import may write anything. Ids never
-- change. Writes run under READ COMMITTED: the locks below rely on each
-- statement reading what was committed before it (review 144 A3).
CREATE FUNCTION agentrt.require_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev   agentrt.event;
  evj  jsonb;
  r    jsonb := to_jsonb(NEW);
  idc  text := TG_TABLE_NAME || '_id';
  k    text;
  wild boolean;
BEGIN
  IF current_setting('transaction_isolation') <> 'read committed' THEN
    RAISE EXCEPTION 'agentrt: state is written under READ COMMITTED, not %', current_setting('transaction_isolation')
      USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'UPDATE' AND (r -> idc) IS DISTINCT FROM (to_jsonb(OLD) -> idc) THEN
    RAISE EXCEPTION 'agentrt: % of a row never changes', idc USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'UPDATE' AND agentrt.only_liveness(TG_TABLE_NAME, to_jsonb(OLD), r) THEN
    RETURN NEW;
  END IF;
  ev := agentrt.current_event();
  evj := to_jsonb(ev);
  IF NOT agentrt.event_may_write(ev.event_type, TG_TABLE_NAME) THEN
    RAISE EXCEPTION 'agentrt: a % event does not write agentrt.%', ev.event_type, TG_TABLE_NAME
      USING ERRCODE = 'check_violation';
  END IF;
  wild := ev.event_type = 'legacy_imported'
          OR (ev.event_type = 'plan_changed' AND TG_TABLE_NAME IN ('plan_node','task'));
  FOREACH k IN ARRAY ARRAY['mission_id','plan_node_id','task_id','attempt_id'] LOOP
    CONTINUE WHEN NOT r ? k;
    CONTINUE WHEN (r -> k) = (evj -> k);
    CONTINUE WHEN (evj -> k) = 'null'::jsonb AND wild;
    RAISE EXCEPTION 'agentrt: % on agentrt.% with %=% but event % (%) has %=%', TG_OP, TG_TABLE_NAME,
      k, r -> k, ev.event_id, ev.event_type, k, evj -> k USING ERRCODE = 'check_violation';
  END LOOP;
  IF ev.event_type <> 'legacy_imported' AND NOT agentrt.event_names_row(ev.body_json, TG_TABLE_NAME, r ->> idc) THEN
    RAISE EXCEPTION 'agentrt: event % (%) does not name agentrt.% row %', ev.event_id, ev.event_type, TG_TABLE_NAME, r ->> idc
      USING ERRCODE = 'check_violation';
  END IF;
  IF TG_TABLE_NAME = 'task_dependency' AND ev.event_type = 'task_dependency_added'
     AND ev.body_json ->> 'depends_on_task_id' IS DISTINCT FROM r ->> 'depends_on_task_id' THEN
    RAISE EXCEPTION 'agentrt: event % names another dependency', ev.event_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- State rows are never deleted or truncated; they end by state (B4).
CREATE FUNCTION agentrt.forbid_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'agentrt: % on agentrt.% is not allowed; state rows end by state, not by deletion', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END $$;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['actor','host','plan_node','mission','task','task_dependency','attempt',
                           'message','control','vendor_pool','job','job_candidate','capability_fact'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.require_event()',
                   t || '_00_event', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE DELETE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_delete', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON agentrt.%I FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_truncate', t);
  END LOOP;
END $$;

-- Plan nodes: numbering never changes, nodes are never deleted, the tree has
-- the right kinds, and each kind of change comes with its own event type.
CREATE FUNCTION agentrt.guard_plan_node() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  parent_kind text;
  ev agentrt.event;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'agentrt: plan nodes are never deleted; drop the step instead' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.parent_plan_node_id IS NOT NULL THEN
      SELECT node_kind INTO parent_kind FROM agentrt.plan_node WHERE plan_node_id = NEW.parent_plan_node_id;
      IF (NEW.node_kind, parent_kind) NOT IN (('item','round'), ('step','item'), ('arch_row','arch_root')) THEN
        RAISE EXCEPTION 'agentrt: a % cannot sit under a %', NEW.node_kind, COALESCE(parent_kind, 'missing node')
          USING ERRCODE = 'check_violation';
      END IF;
    END IF;
    IF NEW.state_event_id IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: a new node has no state event yet' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.plan_node_id <> OLD.plan_node_id OR NEW.parent_plan_node_id IS DISTINCT FROM OLD.parent_plan_node_id
     OR NEW.node_kind <> OLD.node_kind OR NEW.ordinal <> OLD.ordinal
     OR NEW.created_by_actor_id <> OLD.created_by_actor_id OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'agentrt: plan node % keeps its id, parent, kind and position', OLD.plan_node_id
      USING ERRCODE = 'check_violation';
  END IF;
  ev := agentrt.current_event();
  IF NEW.state IS DISTINCT FROM OLD.state THEN
    IF ev.event_type <> 'plan_node_state_changed' OR NEW.state_event_id IS DISTINCT FROM ev.event_id THEN
      RAISE EXCEPTION 'agentrt: a step changes state only with its plan_node_state_changed event'
        USING ERRCODE = 'check_violation';
    END IF;
  ELSIF NEW.state_event_id IS DISTINCT FROM OLD.state_event_id THEN
    RAISE EXCEPTION 'agentrt: state_event_id moves only with the state' USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.done_rule_json <> OLD.done_rule_json OR NEW.mission_id IS DISTINCT FROM OLD.mission_id
      OR NEW.step_kind IS DISTINCT FROM OLD.step_kind OR NEW.owner_step <> OLD.owner_step
      OR NEW.effect_step <> OLD.effect_step) AND ev.event_type <> 'plan_changed' THEN
    RAISE EXCEPTION 'agentrt: done rule, mission binding and step kind change only by an authorized plan change'
      USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.title <> OLD.title OR NEW.label <> OLD.label OR NEW.work_id IS DISTINCT FROM OLD.work_id)
     AND ev.event_type NOT IN ('plan_node_retitled','plan_changed') THEN
    RAISE EXCEPTION 'agentrt: a title changes only by plan_node_retitled or plan_changed' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER plan_node_10_guard BEFORE INSERT OR UPDATE OR DELETE ON agentrt.plan_node
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_plan_node();

-- Tasks: attached only to a step, of the step's mission, on the same step as
-- the task they verify or review. The step row is locked first (step lock,
-- review 143): FOR SHARE when a task is created, FOR UPDATE on both steps
-- when a binding moves, so creating and moving never interleave.
CREATE FUNCTION agentrt.guard_task() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  step_kind_now  text;
  step_mission   text;
  subj_plan      text;
  subj_off_plan  boolean;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'agentrt: tasks are never deleted; cancel them' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.task_id <> OLD.task_id OR NEW.role <> OLD.role
       OR NEW.subject_task_id IS DISTINCT FROM OLD.subject_task_id
       OR NEW.parent_task_id IS DISTINCT FROM OLD.parent_task_id
       OR NEW.created_by_actor_id <> OLD.created_by_actor_id OR NEW.created_at <> OLD.created_at THEN
      RAISE EXCEPTION 'agentrt: task % keeps its id, role, subject, parent and creator', OLD.task_id
        USING ERRCODE = 'check_violation';
    END IF;
    IF (agentrt.current_event()).event_type = 'plan_changed'
       AND (to_jsonb(NEW) - ARRAY['plan_node_id','mission_id','off_plan','off_plan_reason','version','updated_at'])
           IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['plan_node_id','mission_id','off_plan','off_plan_reason','version','updated_at']) THEN
      RAISE EXCEPTION 'agentrt: a plan change moves task % but changes nothing else about it', OLD.task_id
        USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.plan_node_id IS NOT DISTINCT FROM OLD.plan_node_id AND NEW.mission_id = OLD.mission_id
       AND NEW.off_plan = OLD.off_plan THEN
      RETURN NEW;
    END IF;
    IF (agentrt.current_event()).event_type <> 'plan_changed' THEN
      RAISE EXCEPTION 'agentrt: task % keeps its step and mission; moving it is an authorized plan change', OLD.task_id
        USING ERRCODE = 'check_violation';
    END IF;
    PERFORM 1 FROM agentrt.plan_node
      WHERE plan_node_id IN (OLD.plan_node_id, NEW.plan_node_id) ORDER BY plan_node_id FOR UPDATE;
  END IF;
  IF NEW.plan_node_id IS NOT NULL THEN
    IF TG_OP = 'INSERT' THEN
      SELECT node_kind, mission_id INTO step_kind_now, step_mission
        FROM agentrt.plan_node WHERE plan_node_id = NEW.plan_node_id FOR SHARE;
    ELSE
      SELECT node_kind, mission_id INTO step_kind_now, step_mission
        FROM agentrt.plan_node WHERE plan_node_id = NEW.plan_node_id;
    END IF;
    IF step_kind_now IS DISTINCT FROM 'step' THEN
      RAISE EXCEPTION 'agentrt: task % must hang on a step, not on %', NEW.task_id, COALESCE(step_kind_now, 'a missing node')
        USING ERRCODE = 'check_violation';
    END IF;
    IF step_mission IS NOT NULL AND step_mission <> NEW.mission_id THEN
      RAISE EXCEPTION 'agentrt: step % belongs to %, task % to %', NEW.plan_node_id, step_mission, NEW.task_id, NEW.mission_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.subject_task_id IS NOT NULL THEN
    -- The subject row is locked too, so moving the subject and creating its
    -- review or verification wait for each other (review 144 A2).
    SELECT plan_node_id, off_plan INTO subj_plan, subj_off_plan FROM agentrt.task WHERE task_id = NEW.subject_task_id FOR SHARE;
    IF NOT FOUND OR subj_plan IS DISTINCT FROM NEW.plan_node_id OR subj_off_plan <> NEW.off_plan THEN
      RAISE EXCEPTION 'agentrt: % task % must sit where its subject % sits', NEW.role, NEW.task_id, NEW.subject_task_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER task_10_guard BEFORE INSERT OR UPDATE OR DELETE ON agentrt.task
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_task();

-- At commit, re-check every step a change touched: its tasks belong to its
-- mission, verify and review tasks sit with their subjects, and its done
-- rule names only tasks on this step and approvals and questions bound to it. A plan change that leaves any of
-- these broken rolls back whole (review 142 R3).
CREATE FUNCTION agentrt.check_step(step_id text) RETURNS void LANGUAGE plpgsql AS $$
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
  IF s.mission_id IS NOT NULL THEN
    SELECT task_id INTO bad FROM agentrt.task WHERE plan_node_id = step_id AND mission_id <> s.mission_id LIMIT 1;
    IF FOUND THEN
      RAISE EXCEPTION 'agentrt: step % belongs to % but task % does not', step_id, s.mission_id, bad
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  SELECT t.task_id INTO bad FROM agentrt.task t JOIN agentrt.task subj ON subj.task_id = t.subject_task_id
   WHERE (t.plan_node_id = step_id OR subj.plan_node_id = step_id)
     AND (t.plan_node_id IS DISTINCT FROM subj.plan_node_id OR t.off_plan <> subj.off_plan)
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'agentrt: task % no longer sits with its subject', bad USING ERRCODE = 'check_violation';
  END IF;
  SELECT q.ref #>> '{}' INTO bad FROM jsonb_path_query(s.done_rule_json, 'lax $.**.task_accepted') AS q(ref)
   WHERE NOT EXISTS (SELECT 1 FROM agentrt.task t WHERE t.task_id = q.ref #>> '{}' AND t.plan_node_id = step_id)
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'agentrt: the done rule of % names task %, which is not on this step', step_id, bad
      USING ERRCODE = 'check_violation';
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

-- A task and its subject sit together, and so do the tasks whose subject it
-- is, whether or not they are on a step (review 144 A2).
CREATE FUNCTION agentrt.check_task_subjects(t_id text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE bad text;
BEGIN
  SELECT t.task_id INTO bad FROM agentrt.task t JOIN agentrt.task subj ON subj.task_id = t.subject_task_id
   WHERE (t.task_id = t_id OR subj.task_id = t_id)
     AND (t.plan_node_id IS DISTINCT FROM subj.plan_node_id OR t.off_plan <> subj.off_plan)
   LIMIT 1;
  IF FOUND THEN
    RAISE EXCEPTION 'agentrt: task % does not sit with its subject', bad USING ERRCODE = 'check_violation';
  END IF;
END $$;

CREATE FUNCTION agentrt.recheck_task_bindings() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE step_id text;
BEGIN
  PERFORM agentrt.check_task_subjects(NEW.task_id);
  PERFORM agentrt.check_step(NEW.plan_node_id);
  IF TG_OP = 'UPDATE' AND OLD.plan_node_id IS DISTINCT FROM NEW.plan_node_id THEN
    PERFORM agentrt.check_step(OLD.plan_node_id);
    FOR step_id IN SELECT plan_node_id FROM agentrt.plan_node
                    WHERE jsonb_path_exists(done_rule_json, 'lax $.**.task_accepted ? (@ == $t)',
                                            jsonb_build_object('t', NEW.task_id)) LOOP
      PERFORM agentrt.check_step(step_id);
    END LOOP;
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER task_90_recheck AFTER INSERT OR UPDATE ON agentrt.task
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agentrt.recheck_task_bindings();

CREATE FUNCTION agentrt.recheck_step_bindings() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM agentrt.check_step(NEW.plan_node_id);
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER plan_node_90_recheck AFTER INSERT OR UPDATE ON agentrt.plan_node
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agentrt.recheck_step_bindings();

-- Dependencies: edges are never rewired; one lock for the whole graph, then
-- a fresh read sees every committed edge, so two transactions cannot close a
-- cycle between them (review 142 R3).
CREATE FUNCTION agentrt.guard_task_dependency() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'agentrt: dependency edges are never changed or removed' USING ERRCODE = 'insufficient_privilege';
  END IF;
  PERFORM pg_advisory_xact_lock(7421000001);
  IF EXISTS (
    WITH RECURSIVE reach(t) AS (
      SELECT d.depends_on_task_id FROM agentrt.task_dependency d WHERE d.task_id = NEW.depends_on_task_id
      UNION
      SELECT d.depends_on_task_id FROM agentrt.task_dependency d JOIN reach r ON d.task_id = r.t
    )
    SELECT 1 FROM reach WHERE t = NEW.task_id
  ) OR NEW.depends_on_task_id = NEW.task_id THEN
    RAISE EXCEPTION 'agentrt: % -> % would close a dependency cycle', NEW.task_id, NEW.depends_on_task_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER task_dependency_10_guard BEFORE INSERT OR UPDATE OR DELETE ON agentrt.task_dependency
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_task_dependency();

-- Verifications: the attempt belongs to the task; an accepting one needs a
-- result that has left the host (persisted_at), and a verifier is never the
-- attempt it verifies.
CREATE FUNCTION agentrt.guard_verification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  a_task      text;
  a_persisted timestamptz;
BEGIN
  SELECT task_id, persisted_at INTO a_task, a_persisted FROM agentrt.attempt WHERE attempt_id = NEW.attempt_id FOR SHARE;
  IF a_task IS DISTINCT FROM NEW.task_id THEN
    RAISE EXCEPTION 'agentrt: attempt % is not an attempt of task %', NEW.attempt_id, NEW.task_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.accepts_attempt AND a_persisted IS NULL THEN
    RAISE EXCEPTION 'agentrt: attempt % cannot be accepted before its result is persisted', NEW.attempt_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.verifier_attempt_id = NEW.attempt_id THEN
    RAISE EXCEPTION 'agentrt: an attempt does not verify itself' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER verification_10_guard BEFORE INSERT ON agentrt.verification
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_verification();

-- Outbox: written only with an event of this transaction; afterwards only the
-- delivery columns move.
CREATE FUNCTION agentrt.guard_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NOT EXISTS (SELECT 1 FROM agentrt.event WHERE event_id = NEW.event_id AND txid = txid_current()) THEN
      RAISE EXCEPTION 'agentrt: an outbox row is written with its event' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.delivered_at IS NOT NULL OR NEW.tries <> 0 THEN
      RAISE EXCEPTION 'agentrt: a new outbox row is undelivered' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - ARRAY['delivered_at','tries']) IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['delivered_at','tries'])
     OR NEW.tries < OLD.tries OR (OLD.delivered_at IS NOT NULL AND NEW.delivered_at IS DISTINCT FROM OLD.delivered_at) THEN
    RAISE EXCEPTION 'agentrt: only an undelivered outbox row is marked delivered' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER outbox_10_guard BEFORE INSERT OR UPDATE ON agentrt.outbox
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_outbox();

-- Idempotency keys: never rewritten; only expired ones are deleted.
CREATE FUNCTION agentrt.guard_idempotency_key() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'agentrt: an idempotency key is never rewritten' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.expires_at > clock_timestamp() THEN
    RAISE EXCEPTION 'agentrt: idempotency key % has not expired', OLD.idempotency_key_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN OLD;
END $$;
CREATE TRIGGER idempotency_key_10_guard BEFORE UPDATE OR DELETE ON agentrt.idempotency_key
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_idempotency_key();

-- Bindings of approvals and questions to a step are written only by an
-- authorized plan change of this transaction, and only to a step.
CREATE FUNCTION agentrt.guard_plan_node_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event;
BEGIN
  SELECT * INTO ev FROM agentrt.event WHERE event_id = NEW.event_id;
  IF NOT FOUND OR ev.txid <> txid_current() OR ev.event_type <> 'plan_changed'
     OR (ev.plan_node_id IS NOT NULL AND ev.plan_node_id <> NEW.plan_node_id) THEN
    RAISE EXCEPTION 'agentrt: a binding is written with the plan_changed event of this transaction'
      USING ERRCODE = 'check_violation';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = NEW.plan_node_id AND node_kind = 'step') THEN
    RAISE EXCEPTION 'agentrt: only a step takes bindings' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER plan_node_binding_10_guard BEFORE INSERT ON agentrt.plan_node_binding
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_plan_node_binding();

-- Attempts keep who they are: id, task, number, executor, host and fencing
-- epoch never change; a result that has left the host (computed_at,
-- persisted_at) and the branch it was pushed to are written once and never
-- taken back, so a verification that relied on them stays true (A5).
CREATE FUNCTION agentrt.guard_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.task_id <> OLD.task_id OR NEW.attempt_no <> OLD.attempt_no OR NEW.executor_actor_id <> OLD.executor_actor_id
     OR NEW.host_id IS DISTINCT FROM OLD.host_id OR NEW.lease_epoch <> OLD.lease_epoch THEN
    RAISE EXCEPTION 'agentrt: attempt % keeps its task, number, executor, host and epoch', OLD.attempt_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF (OLD.computed_at IS NOT NULL AND NEW.computed_at IS DISTINCT FROM OLD.computed_at)
     OR (OLD.persisted_at IS NOT NULL AND NEW.persisted_at IS DISTINCT FROM OLD.persisted_at)
     OR (OLD.pushed_commit IS NOT NULL AND (NEW.pushed_commit IS DISTINCT FROM OLD.pushed_commit
                                           OR NEW.pushed_ref IS DISTINCT FROM OLD.pushed_ref)) THEN
    RAISE EXCEPTION 'agentrt: attempt % has a result that is written once', OLD.attempt_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER attempt_10_guard BEFORE UPDATE ON agentrt.attempt
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_attempt();
