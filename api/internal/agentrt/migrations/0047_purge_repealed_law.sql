-- agentrt 0047: a repealed law's records can be deleted (legislation,
-- 2026-11-24). Owner, in the legislation thread: a repealed law must not
-- linger as poison; "满七天或者我直接要求删..都可以物理删除" (seven days
-- after it is repealed, or at once when he asks for it).
--
-- The event table stays append only for everything else. One narrow way out:
-- agentrt.purge_repealed_law(stream, forced), run as the owner (SECURITY
-- DEFINER), deletes the whole stream law:<n> when its last record says the
-- law is repealed (status 废止) and either it was repealed seven days ago or
-- forced is true (the Owner's own request, an approved card). A whole stream
-- goes, so no other stream's hash chain is touched. The append-only trigger
-- on agentrt.event and the stream guard let that delete through only inside
-- the function: the transaction names the stream (agentrt.purge_law) and the
-- current user is the table's owner, which only the function's body is
-- (or someone holding the owner's role, who could drop the trigger anyway),
-- and the stream's head says the law is repealed. Laws in force and material
-- cannot be deleted.
--
-- Way back: drop the function and restore 0002's triggers (event_append_only
-- with forbid_change, guard_stream's DELETE branch) — nothing a purge
-- deleted comes back.

-- The trigger checks the law itself too, not only the setting a session
-- could set by hand: the stream's head (its last record, deleted last by the
-- function) must say the law is repealed. A law in force or material is never
-- deleted, whoever asks.
CREATE FUNCTION agentrt.law_head_repealed(stream text) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM agentrt.event_stream s JOIN agentrt.event e
                   ON e.event_stream_id = s.event_stream_id AND e.seq = s.last_seq
                  WHERE s.event_stream_id = stream AND e.event_type = 'law_recorded' AND e.body_json->>'status' = '废止')
$$;

CREATE FUNCTION agentrt.event_forbid_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND starts_with(OLD.event_stream_id, 'law:')
     AND current_setting('agentrt.purge_law', true) = OLD.event_stream_id
     AND current_user = (SELECT tableowner FROM pg_catalog.pg_tables WHERE schemaname = 'agentrt' AND tablename = 'event')
     AND agentrt.law_head_repealed(OLD.event_stream_id) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'agentrt: % on agentrt.% is not allowed: the table is append only', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'insufficient_privilege';
END $$;
DROP TRIGGER event_append_only ON agentrt.event;
CREATE TRIGGER event_append_only BEFORE DELETE OR UPDATE ON agentrt.event FOR EACH ROW EXECUTE FUNCTION agentrt.event_forbid_change();

-- guard_stream: 0002's body word for word, but a law stream may be deleted
-- inside agentrt.purge_repealed_law.
CREATE OR REPLACE FUNCTION agentrt.guard_stream() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF starts_with(OLD.event_stream_id, 'law:') AND current_setting('agentrt.purge_law', true) = OLD.event_stream_id
       AND current_user = (SELECT tableowner FROM pg_catalog.pg_tables WHERE schemaname = 'agentrt' AND tablename = 'event')
       AND NOT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = OLD.event_stream_id) THEN
      RETURN OLD;
    END IF;
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

-- The one way to delete a law's records. Returns how many events went and
-- the approval ids they carried (the planstore keeps those in a tombstone,
-- law_purged, so a card is never written in twice); 0 when the stream does
-- not qualify (not a law, not repealed, or repealed less than seven days ago
-- and not forced).
CREATE FUNCTION agentrt.purge_repealed_law(stream text, forced boolean) RETURNS TABLE (deleted integer, approval_ids text[])
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, agentrt, pg_temp AS $$
DECLARE
  last_body jsonb;
  last_type text;
  last_at timestamptz;
  ev bigint;
BEGIN
  deleted := 0;
  approval_ids := '{}';
  IF NOT starts_with(stream, 'law:') THEN
    RETURN NEXT;
    RETURN;
  END IF;
  SELECT e.body_json, e.event_type, e.at INTO last_body, last_type, last_at FROM agentrt.event e
   WHERE e.event_stream_id = stream ORDER BY e.seq DESC LIMIT 1;
  IF NOT FOUND OR last_type <> 'law_recorded' OR last_body->>'status' IS DISTINCT FROM '废止'
     OR (NOT forced AND last_at > now() - interval '7 days') THEN
    RETURN NEXT;
    RETURN;
  END IF;
  SELECT COALESCE(array_agg(DISTINCT e.body_json->>'approval_id') FILTER (WHERE COALESCE(e.body_json->>'approval_id', '') <> ''), '{}')
    INTO approval_ids FROM agentrt.event e WHERE e.event_stream_id = stream;
  PERFORM set_config('agentrt.purge_law', stream, true);
  -- One record at a time, oldest first: the head goes last, so the trigger
  -- finds it saying 废止 for every record before it.
  FOR ev IN SELECT e.event_id FROM agentrt.event e WHERE e.event_stream_id = stream ORDER BY e.seq LOOP
    DELETE FROM agentrt.event WHERE event_id = ev;
    deleted := deleted + 1;
  END LOOP;
  DELETE FROM agentrt.event_stream WHERE event_stream_id = stream;
  PERFORM set_config('agentrt.purge_law', '', true);
  RETURN NEXT;
END $$;
REVOKE ALL ON FUNCTION agentrt.purge_repealed_law(text, boolean) FROM PUBLIC;

-- The tombstone of a purged law (law_purged, stream law-purged:<n>): its
-- number and the approval ids its records carried, nothing of its text.
-- 0046's list and the new type; every row passes 0046, so none is changed.
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
 'plan_view_adopted','action_traced','home_layout_saved','prod_release_judged','law_recorded','plan_step_judged','plan_step_attributed','law_purged')
 OR event_type ~ '^[a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+$');

DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.purge_repealed_law(text, boolean) TO %I', rt);
  EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.event_forbid_change() TO %I', rt);
  EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.law_head_repealed(text) TO %I', rt);
END $$;
