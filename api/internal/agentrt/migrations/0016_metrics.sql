-- agentrt 0016: delivery measurement (R6 group 0, AM-1 to AM-7; the
-- Owner 2026-11-10 plan B). Raw records stay events: approval_seen,
-- question_seen and pipeline_seen are in the vocabulary since 0002 and their
-- bodies in the store (592acd46); this file adds no type for them. It adds
-- the readings computed from them, one table, the two types that write and
-- label readings, and the two CI facts wave M-b records:
--   metrics_computed   one computation, or one import (source imported), on
--                      the metrics stream; its metric_reading rows name it in
--                      computed_from_event_id (the capability_scored way).
--   metric_label_set   a window named baseline:<name> or group:<name>, on the
--                      metric-labels stream; that window's rows carry the label.
--   ci_reused          a trigger whose content (tree) a green run already
--                      tested, recorded instead of running again.
--   ci_red_classified  a red CI run classified code | test | infra | unknown.
-- metric_reading is append only (0002's forbid_change), like
-- capability_score, and a row is written only with the metrics_computed event
-- of its own transaction (guard_metric_reading). One row per metric,
-- definition version, scope, label, window and source: a window is computed
-- once it has closed and settled, and a changed definition is a new
-- definition_version, so a recomputation never rewrites a row (AM-7).
--
-- Upgrading: run this before the platform that writes readings. An older
-- platform still starts (it reports the schema ahead) and keeps working: it
-- writes no reading and none of the four types; the vocabulary only grows.
-- The ALTER of the vocabulary and the index on event take strong locks on
-- event while they check and read its rows; migrate.apply bounds the wait
-- (lock_timeout 2s) and each statement (30s), and a timeout rolls the file back.

-- The closed vocabulary (0015's list and the four types).
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
  'host_slots_set','wave_opened','wave_closed','wave_abandoned',
  'note','wait_limit_passed','legacy_imported',
  'metrics_computed','metric_label_set','ci_reused','ci_red_classified'));

-- One reading: a metric, by the definition version it was computed with,
-- over one window of one scope. value is the definition's headline statistic
-- (NULL with no sample, but a count may be 0); stats_json holds the rest
-- (p50, mean, p90, min, max, and a breakdown such as doing / owner_wait /
-- idle). conclusive is false below the definition's sample floor. scope_ref
-- names a repo, a plan node (round, item or step), a wave or a mission by its
-- id; it has no foreign key because its table depends on scope_kind.
CREATE TABLE agentrt.metric_reading (
  metric_reading_id text PRIMARY KEY CHECK (metric_reading_id ~ '^mr_[0-9A-HJKMNP-TV-Z]{26}$'),
  metric_id text NOT NULL CHECK (metric_id ~ '^[a-z][a-z0-9_]{0,63}$'),
  definition_version int NOT NULL CHECK (definition_version >= 1),
  definition_sha256 text NOT NULL CHECK (definition_sha256 ~ '^[0-9a-f]{64}$'),
  scope_kind text NOT NULL CHECK (scope_kind IN ('all','repo','plan_node','wave','mission')),
  scope_ref text NOT NULL DEFAULT '',
  label text NOT NULL DEFAULT '' CHECK (label = '' OR label ~ '^(baseline|group):[A-Za-z0-9._-]{1,64}$'),
  window_kind text NOT NULL CHECK (window_kind IN ('hour','day','week','span')),
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  source text NOT NULL CHECK (source IN ('computed','imported')),
  unit text NOT NULL CHECK (unit IN ('seconds','count','ratio')),
  samples int NOT NULL CHECK (samples >= 0),
  value numeric,
  stats_json jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(stats_json) = 'object'),
  conclusive boolean NOT NULL,
  evidence_artifact_id text REFERENCES agentrt.artifact(artifact_id),
  computed_from_event_id bigint NOT NULL REFERENCES agentrt.event(event_id),
  computed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (metric_id, definition_version, scope_kind, scope_ref, label, window_start, window_end, source),
  CHECK (window_end > window_start),
  CHECK ((scope_kind = 'all') = (scope_ref = '')),
  CHECK (label = '' OR window_kind = 'span'),
  CHECK (value IS NOT NULL OR NOT conclusive),
  CHECK (samples > 0 OR value IS NULL OR unit = 'count'),
  CHECK (source = 'computed' OR evidence_artifact_id IS NOT NULL)
);
CREATE INDEX metric_reading_window_idx ON agentrt.metric_reading (metric_id, scope_kind, scope_ref, window_end DESC);
CREATE INDEX metric_reading_label_idx ON agentrt.metric_reading (label, metric_id, definition_version) WHERE label <> '';

-- A row is written only with the metrics_computed event of its own
-- transaction (as a state row needs its event, without 0002's event_may_write).
CREATE FUNCTION agentrt.guard_metric_reading() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  typ text;
  tx bigint;
BEGIN
  SELECT event_type, txid INTO typ, tx FROM agentrt.event WHERE event_id = NEW.computed_from_event_id;
  IF typ IS DISTINCT FROM 'metrics_computed' OR tx IS DISTINCT FROM txid_current() THEN
    RAISE EXCEPTION 'agentrt: a metric reading is written with the metrics_computed event of its own transaction, not event %',
      NEW.computed_from_event_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER metric_reading_10_guard BEFORE INSERT ON agentrt.metric_reading
  FOR EACH ROW EXECUTE FUNCTION agentrt.guard_metric_reading();
CREATE TRIGGER metric_reading_append_only BEFORE UPDATE OR DELETE ON agentrt.metric_reading
  FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change();
CREATE TRIGGER metric_reading_no_truncate BEFORE TRUNCATE ON agentrt.metric_reading
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change();

-- A0 (wave M-b): the content (git tree) a CI run tested, pipeline_seen.tree
-- and ci_reused.tree, is looked up when a trigger may reuse a green run.
CREATE INDEX event_ci_tree_idx ON agentrt.event ((body_json->>'tree'), event_id)
  WHERE event_type IN ('pipeline_seen','ci_reused');

-- Grants, named one by one (0003's ON ALL covered only the objects there were
-- then): append only, SELECT and INSERT; the reader may read readings, which
-- hold no person's words.
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT SELECT, INSERT ON agentrt.metric_reading TO %I', rt);
  EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.guard_metric_reading() TO %I', rt);
  GRANT SELECT ON agentrt.metric_reading TO agent_reader;
END $$;
