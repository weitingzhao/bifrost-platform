-- agentrt 0011: technical debt as records of the runtime (R5:5.8-3a;
-- bifrost-trade-infra agent-config/work/multi-agent/
-- DESIGN-tech-debt-runtime-2026-11-01.md §2.2-§2.5, §4.2 and its part 2 §5,
-- §6; the Owner took T1-T13 as recommended on 2026-11-01). A debt is filed
-- with a number only this environment issues (TD-n on PROD, TDS-n on STG,
-- TDD-n on DEV; §2.4), its texts and evidence are appended, its links to
-- other debts, plan steps, issues, approvals, questions, decisions, ratchets
-- and accepted gaps are typed rows, and its history is the event stream
-- debt:<id>; an event holds ids, enums and hashes, never text (§4.3).
--
-- Guards (the 0002 and 0009 way): every write of a state table carries an
-- event of its transaction, of a type allowed for the table, naming the row,
-- by a writer allowed for the type (debt_writer_ok: never an external agent
-- or a host; the Owner's and the runtime's own events only by them); the
-- append-only tables are written only with such an event. The one-time
-- import (legacy_imported) writes none of these tables: an import goes
-- through the ordinary debt events (§2.2). Only platform_agentrt_prod writes
-- a TD- number; the counter's floor only rises; a manual acceptance result
-- counts only when a second writer recorded it; the waiting graph (blocks
-- and part_of together, §5.5) has no cycle. Two 0009 functions are replaced
-- with the same signatures, their old branches word for word, so the
-- triggers that call them stay as they are: event_may_write and
-- event_names_row. only_liveness is not: no debt table has a liveness
-- column.
--
-- Upgrading (§2.5): forward only; run this before the platform that writes
-- debt. An older platform still starts (it reports the schema ahead) and
-- keeps working: it writes no debt and the vocabulary only grows. The ALTER
-- of the event vocabulary takes a strong lock on event while it checks the
-- rows; migrate.apply bounds the wait (lock_timeout 2s) and each statement
-- (30s), and a timeout rolls the whole file back.

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
  'issue_submitted','issue_added','issue_brief_drafted','issue_asked','issue_answered','issue_classified',
  'issue_design_put','issue_reviewed','issue_owner_decided','issue_priority_set','issue_taken','issue_replied',
  'issue_state_changed','issue_merged','intake_client_set','intake_acked',
  'debt_filed','debt_edited','debt_text_added','debt_evidence_added','debt_evidence_checked','debt_state_changed',
  'debt_flow_set','debt_edge_added','debt_edge_ended','debt_ref_added','debt_ref_ended','debt_work_linked','debt_work_ended',
  'debt_part_set','debt_part_landed','debt_acceptance_set','debt_acceptance_reviewed','debt_acceptance_ran',
  'debt_signed_off','debt_auto_closed','debt_reopened','debt_signoff_rejected','debt_merged','debt_floor_raised',
  'debt_decision_set','ratchet_registered','ratchet_changed','gap_accepted','gap_reopened',
  'note','wait_limit_passed','legacy_imported'));

-- The numbering floor: one row, written here at the number the hand-written
-- ledger had reached (de152c7, §2.4), so no environment ever issues a number
-- it used; afterwards it only rises, by debt_floor_raised (the import, the
-- Owner or the runtime). Filing does not write it: a new number is one more
-- than the largest of its prefix or the floor, whichever is larger.
CREATE TABLE agentrt.debt_counter (
  debt_counter_id int PRIMARY KEY CHECK (debt_counter_id = 1),
  floor           int NOT NULL CHECK (floor >= 0),
  version         int NOT NULL DEFAULT 0 CHECK (version >= 0)
);
INSERT INTO agentrt.debt_counter (debt_counter_id, floor) VALUES (1, 488);

-- Whether a text says anything: something besides white space, as Go's
-- unicode.IsSpace has it (the store's checkDebtText trims the same), so a
-- blank line never stands for a summary or a reason (review 631d R4-M4).
CREATE FUNCTION agentrt.debt_has_words(body text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT btrim(body, E' \t\n\r\f\u000b\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000') <> ''
$$;

-- A spec's hash, computed by the database from the stored spec (jsonb's own
-- text form is the same for the same value), so a review and a run name the
-- spec as it is, never a hash a writer claims (review 631 M-2, N-4).
CREATE FUNCTION agentrt.debt_spec_sha(spec jsonb) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT encode(sha256(convert_to(spec::text, 'UTF8')), 'hex')
$$;

-- The debt. project is an opaque key the debt-projects rule lists; state is
-- the four states and closed with its conclusion (T4: "blocked" is computed,
-- never stored); visibility is applied by the routes only (§4.3).
-- acceptance_by is who wrote the current spec (its filer, or the writer of
-- the last debt_acceptance_set): a review is never its author's, and is of
-- the spec as it is now (§2.2, §6.2). birth_event_id is the debt_filed event
-- that created the row, set by its guard: where the debt came from is read
-- from it and from nothing else (review 631d R4-M2).
CREATE TABLE agentrt.debt (
  debt_id       text PRIMARY KEY CHECK (debt_id ~ '^TD[SD]?-[1-9][0-9]*$'),
  project       text NOT NULL CHECK (project ~ '^[a-z][a-z0-9-]{0,31}$'),
  area          text CHECK (char_length(area) BETWEEN 1 AND 60),
  title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 400),
  priority      text NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),
  state         text NOT NULL CHECK (state IN ('open','doing','watching','awaiting_signoff','closed')),
  conclusion    text CHECK (conclusion IN ('signed_off','auto_closed','duplicate','withdrawn','boundary','superseded')),
  duplicate_of  text REFERENCES agentrt.debt(debt_id) DEFERRABLE INITIALLY DEFERRED,
  flow          text NOT NULL CHECK (flow IN ('manual','auto')),
  watch_until   timestamptz,
  watch_text_id bigint,
  cost          text CHECK (cost IN ('S','M','L','XL')),
  risk          text CHECK (risk IN ('low','medium','high')),
  visibility    text NOT NULL DEFAULT 'project' CHECK (visibility IN ('project','summary','private')),
  tags          text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 32 AND array_position(tags, NULL) IS NULL),
  acceptance_json jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(acceptance_json) = 'array'),
  acceptance_reviewed_sha256 text CHECK (acceptance_reviewed_sha256 ~ '^[0-9a-f]{64}$'),
  acceptance_reviewed_by text REFERENCES agentrt.actor(actor_id),
  acceptance_by text REFERENCES agentrt.actor(actor_id),
  filed_at      timestamptz NOT NULL,
  state_since   timestamptz NOT NULL,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  birth_event_id bigint NOT NULL REFERENCES agentrt.event(event_id),
  version       int NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK ((state = 'closed') = (conclusion IS NOT NULL)),
  CHECK ((state = 'watching') = (watch_until IS NOT NULL)),
  CHECK (watch_text_id IS NULL OR watch_until IS NOT NULL),
  CHECK ((conclusion IS NOT DISTINCT FROM 'duplicate') = (duplicate_of IS NOT NULL)),
  CHECK (duplicate_of IS DISTINCT FROM debt_id),
  CHECK ((acceptance_reviewed_sha256 IS NULL) = (acceptance_reviewed_by IS NULL)),
  CHECK ((acceptance_json = '[]'::jsonb) = (acceptance_by IS NULL)),
  CHECK (acceptance_reviewed_by IS NULL OR (acceptance_by IS NOT NULL AND acceptance_reviewed_by <> acceptance_by
         AND acceptance_reviewed_sha256 = agentrt.debt_spec_sha(acceptance_json)))
);
CREATE INDEX debt_project_idx ON agentrt.debt (project, state);
CREATE INDEX debt_watch_idx ON agentrt.debt (watch_until) WHERE state = 'watching';

-- Every real move of a debt's state, as the database saw it happen: written
-- by the debt's own trigger from the row's old and new state and the event
-- that moved it (record_debt_transition), never by a writer (the runtime
-- role only reads it). What happened to a debt (did it ever leave awaiting
-- the Owner, when was it last reopened, did an event make the move it
-- records) is read here, not from what events say (review 631d R4-M1).
-- One event makes at most one move: its transition is bound to it, so an
-- event checked early (SET CONSTRAINTS ... IMMEDIATE) cannot certify a
-- second move later (review 631e 2).
CREATE TABLE agentrt.debt_transition (
  debt_transition_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id),
  event_id      bigint NOT NULL REFERENCES agentrt.event(event_id),
  from_state    text NOT NULL,
  to_state      text NOT NULL,
  at            timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK (from_state <> to_state),
  CONSTRAINT debt_transition_one_per_event UNIQUE (event_id)
);
CREATE INDEX debt_transition_debt_idx ON agentrt.debt_transition (debt_id, from_state, at);

-- Every text of a debt, append only; the newest of a kind is current. A free
-- heading of the hand-written ledger is kept as it was (§3).
CREATE TABLE agentrt.debt_text (
  debt_text_id  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id) DEFERRABLE INITIALLY DEFERRED,
  text_kind     text NOT NULL CHECK (text_kind IN ('claim','measured','impact','fix','ratchet_plan','now','next','watch',
                  'acceptance_text','signoff_summary','ratchet_text','follow_up_text','no_ratchet_reason','no_follow_up_reason',
                  'reject_reason','note','import_raw')),
  heading       text CHECK (char_length(heading) BETWEEN 1 AND 200),
  body          text NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 262144 AND agentrt.debt_has_words(body)),
  sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (debt_text_id, debt_id)
);
CREATE INDEX debt_text_kind_idx ON agentrt.debt_text (debt_id, text_kind, debt_text_id);
ALTER TABLE agentrt.debt ADD FOREIGN KEY (watch_text_id, debt_id)
  REFERENCES agentrt.debt_text(debt_text_id, debt_id) DEFERRABLE INITIALLY DEFERRED;

-- Evidence, append only: a moved line gets a new row that supersedes the old
-- one of the same debt. A command is recorded, never run (§6.4).
CREATE TABLE agentrt.debt_evidence (
  debt_evidence_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id) DEFERRABLE INITIALLY DEFERRED,
  evidence_kind text NOT NULL CHECK (evidence_kind IN ('code_ref','command','run_ref','commit','doc_ref','measurement')),
  repo          text CHECK (repo ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
  path          text CHECK (char_length(path) BETWEEN 1 AND 400),
  line_from     int CHECK (line_from >= 1),
  line_to       int CHECK (line_to >= line_from),
  commit_sha    text CHECK (commit_sha ~ '^[0-9a-f]{7,40}$'),
  quote         text CHECK (char_length(quote) <= 400),
  command       text CHECK (char_length(command) <= 4000),
  observed_at   timestamptz,
  supersedes_id bigint,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (evidence_kind <> 'code_ref' OR (repo IS NOT NULL AND path IS NOT NULL)),
  CHECK (line_to IS NULL OR line_from IS NOT NULL),
  UNIQUE (debt_evidence_id, debt_id),
  FOREIGN KEY (supersedes_id, debt_id) REFERENCES agentrt.debt_evidence(debt_evidence_id, debt_id)
);
CREATE INDEX debt_evidence_debt_idx ON agentrt.debt_evidence (debt_id, debt_evidence_id);

-- What the platform found when it read a code_ref again, append only (§6.5).
CREATE TABLE agentrt.debt_evidence_check (
  debt_evidence_check_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_evidence_id bigint NOT NULL REFERENCES agentrt.debt_evidence(debt_evidence_id),
  result        text NOT NULL CHECK (result IN ('matches','moved','missing','unreadable')),
  at_commit     text NOT NULL CHECK (at_commit ~ '^[0-9a-f]{40}$'),
  checked_at    timestamptz NOT NULL DEFAULT now()
);

-- Debt to debt; never deleted, only ended. blocks: from holds up to;
-- part_of: from is a part of the umbrella to (§5.1).
CREATE TABLE agentrt.debt_edge (
  debt_edge_id  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  edge_kind     text NOT NULL CHECK (edge_kind IN ('blocks','part_of','follows_from','related')),
  from_debt_id  text NOT NULL REFERENCES agentrt.debt(debt_id),
  to_debt_id    text NOT NULL REFERENCES agentrt.debt(debt_id),
  needs         text CHECK (needs IN ('landed','closed')),
  ended_at      timestamptz,
  ended_why     text CHECK (ended_why IN ('withdrawn_link','rewired_by_merge','blocker_withdrawn')),
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (from_debt_id <> to_debt_id),
  CHECK ((edge_kind = 'blocks') = (needs IS NOT NULL)),
  CHECK ((ended_at IS NULL) = (ended_why IS NULL))
);
CREATE UNIQUE INDEX debt_edge_live ON agentrt.debt_edge (edge_kind, from_debt_id, to_debt_id) WHERE ended_at IS NULL;
CREATE INDEX debt_edge_wait_idx ON agentrt.debt_edge (to_debt_id) WHERE ended_at IS NULL AND edge_kind IN ('blocks','part_of');

-- A point waiting on the Owner (the ledger's "需要你拍板"), not yet asked or
-- asked (T10).
CREATE TABLE agentrt.debt_decision (
  debt_decision_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 400),
  body_text     text NOT NULL CHECK (octet_length(body_text) BETWEEN 1 AND 262144),
  state         text NOT NULL CHECK (state IN ('pending','asked','answered','dropped')),
  question_id   text CHECK (question_id ~ '^Q[1-9][0-9]*$'),
  answer_option text CHECK (char_length(answer_option) BETWEEN 1 AND 200),
  version       int NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK ((state IN ('asked','answered')) = (question_id IS NOT NULL)),
  CHECK ((state = 'answered') = (answer_option IS NOT NULL))
);

-- A guard against a kind of debt (T7). td_refs are historical numbers, most
-- of them closed: no foreign key.
CREATE TABLE agentrt.ratchet (
  ratchet_id    text PRIMARY KEY CHECK (ratchet_id ~ '^R-[1-9][0-9]*$'),
  name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 400),
  location      text NOT NULL CHECK (char_length(location) BETWEEN 1 AND 2000),
  guards        text NOT NULL CHECK (char_length(guards) BETWEEN 1 AND 4000),
  strength      text NOT NULL CHECK (char_length(strength) BETWEEN 1 AND 400),
  scope_gaps    text CHECK (char_length(scope_gaps) <= 4000),
  strength_class text NOT NULL CHECK (strength_class IN ('blocking','warning','alert','manual','other')),
  td_refs       text[] NOT NULL DEFAULT '{}' CHECK (array_position(td_refs, NULL) IS NULL),
  state         text NOT NULL CHECK (state IN ('active','retired')),
  version       int NOT NULL DEFAULT 0 CHECK (version >= 0)
);

-- A data boundary or an accepted risk, accepted only by the Owner (T8).
CREATE TABLE agentrt.accepted_gap (
  accepted_gap_id text PRIMARY KEY CHECK (accepted_gap_id ~ '^AG-[1-9][0-9]*$'),
  project       text NOT NULL CHECK (project ~ '^[a-z][a-z0-9-]{0,31}$'),
  gap_kind      text NOT NULL CHECK (gap_kind IN ('data_boundary','accepted_risk')),
  title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 400),
  reason        text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 20000),
  reopen_when   text NOT NULL CHECK (char_length(reopen_when) BETWEEN 1 AND 4000),
  review_at     date,
  state         text NOT NULL CHECK (state IN ('accepted','reopened')),
  version       int NOT NULL DEFAULT 0 CHECK (version >= 0)
);

-- A part of a debt in one repository (a repository may have several, §5.2);
-- the gate is on the step that applies it, not on starting (§5.3).
CREATE TABLE agentrt.debt_part (
  debt_part_id  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id) DEFERRABLE INITIALLY DEFERRED,
  repo          text NOT NULL CHECK (repo ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
  ordinal       int NOT NULL CHECK (ordinal >= 1),
  apply_gate    text NOT NULL DEFAULT 'none' CHECK (apply_gate IN ('none','public_interface','ddl','security','delete',
                  'prod_change','cluster_write','rule_sign','cross_repo_release','other')),
  version       int NOT NULL DEFAULT 0 CHECK (version >= 0),
  UNIQUE (debt_id, ordinal),
  UNIQUE (debt_part_id, debt_id)
);
-- Which part comes after which, within one debt only. A surrogate id names
-- the row in its event, as every other state table's (<table>_id).
CREATE TABLE agentrt.debt_part_order (
  debt_part_order_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id            text NOT NULL,
  debt_part_id       bigint NOT NULL,
  after_debt_part_id bigint NOT NULL,
  UNIQUE (debt_part_id, after_debt_part_id),
  CHECK (debt_part_id <> after_debt_part_id),
  FOREIGN KEY (debt_part_id, debt_id) REFERENCES agentrt.debt_part(debt_part_id, debt_id),
  FOREIGN KEY (after_debt_part_id, debt_id) REFERENCES agentrt.debt_part(debt_part_id, debt_id)
);
-- A commit that landed a part, append only (§6.3).
CREATE TABLE agentrt.debt_part_landing (
  debt_part_landing_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_part_id  bigint NOT NULL REFERENCES agentrt.debt_part(debt_part_id),
  commit_sha    text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
  via           text NOT NULL CHECK (via IN ('runtime_landing','declared')),
  on_main_checked_at timestamptz,
  ci_status     text CHECK (ci_status IN ('succeeded','failed','unread')),
  deployed_env  text CHECK (deployed_env IN ('dev','stg','prod')),
  deployed_at   timestamptz,
  deploy_read   text CHECK (deploy_read IN ('observed','declared')),
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((deployed_env IS NULL) = (deployed_at IS NULL) AND (deployed_env IS NULL) = (deploy_read IS NULL))
);

-- A debt's link to something else, its foreign key typed (§5.1). An
-- approval lives in the platform's approval store, not here (as
-- plan_node_binding). covers=apply waits on one part of the same debt.
CREATE TABLE agentrt.debt_ref (
  debt_ref_id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id),
  ref_kind      text NOT NULL CHECK (ref_kind IN ('paid_by_step','waits_on_owner','guarded_by','raised_by_issue','reclassified_as')),
  plan_node_id  text REFERENCES agentrt.plan_node(plan_node_id),
  issue_id      text REFERENCES agentrt.issue(issue_id),
  ratchet_id    text REFERENCES agentrt.ratchet(ratchet_id),
  accepted_gap_id text REFERENCES agentrt.accepted_gap(accepted_gap_id),
  debt_decision_id bigint REFERENCES agentrt.debt_decision(debt_decision_id),
  approval_no   bigint CHECK (approval_no > 0),
  question_id   text CHECK (question_id ~ '^Q[1-9][0-9]*$'),
  covers        text CHECK (covers IN ('start','apply')),
  debt_part_id  bigint,
  go_options    text[] CHECK (cardinality(go_options) BETWEEN 1 AND 20 AND array_position(go_options, NULL) IS NULL),
  ended_at      timestamptz,
  outcome       text CHECK (outcome IN ('satisfied','rejected','declined','dropped','rebound','superseded')),
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(plan_node_id, issue_id, ratchet_id, accepted_gap_id, debt_decision_id, approval_no, question_id) = 1),
  CHECK (ref_kind <> 'paid_by_step'    OR plan_node_id IS NOT NULL),
  CHECK (ref_kind <> 'guarded_by'      OR ratchet_id IS NOT NULL),
  CHECK (ref_kind <> 'raised_by_issue' OR issue_id IS NOT NULL),
  CHECK (ref_kind <> 'reclassified_as' OR accepted_gap_id IS NOT NULL),
  CHECK ((ref_kind = 'waits_on_owner') = (debt_decision_id IS NOT NULL OR approval_no IS NOT NULL OR question_id IS NOT NULL)),
  CHECK ((ref_kind = 'waits_on_owner') = (covers IS NOT NULL)),
  CHECK ((covers IS NOT DISTINCT FROM 'apply') = (debt_part_id IS NOT NULL)),
  CHECK ((go_options IS NOT NULL) = (ref_kind = 'waits_on_owner' AND approval_no IS NULL)),
  CHECK ((ended_at IS NULL) = (outcome IS NULL)),
  FOREIGN KEY (debt_part_id, debt_id) REFERENCES agentrt.debt_part(debt_part_id, debt_id)
);
CREATE INDEX debt_ref_debt_idx ON agentrt.debt_ref (debt_id) WHERE ended_at IS NULL;
-- One live ref per typed target: an approval and a decision of the same
-- number are two targets (review 631 M-4).
CREATE UNIQUE INDEX debt_ref_live ON agentrt.debt_ref (debt_id, ref_kind, (CASE
  WHEN plan_node_id IS NOT NULL THEN 'step:' || plan_node_id WHEN issue_id IS NOT NULL THEN 'issue:' || issue_id
  WHEN ratchet_id IS NOT NULL THEN 'ratchet:' || ratchet_id WHEN accepted_gap_id IS NOT NULL THEN 'gap:' || accepted_gap_id
  WHEN debt_decision_id IS NOT NULL THEN 'decision:' || debt_decision_id WHEN approval_no IS NOT NULL THEN 'approval:' || approval_no
  ELSE 'question:' || question_id END), COALESCE(debt_part_id, 0)) WHERE ended_at IS NULL;

-- A mission working on a debt; a debt's missions are the dev tenant's (§4.1).
CREATE TABLE agentrt.debt_work (
  debt_work_id  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id),
  mission_id    text NOT NULL REFERENCES agentrt.mission(mission_id),
  started_at    timestamptz NOT NULL,
  ended_at      timestamptz,
  UNIQUE (debt_id, mission_id, started_at)
);
CREATE UNIQUE INDEX debt_work_live ON agentrt.debt_work (debt_id, mission_id) WHERE ended_at IS NULL;

-- An acceptance run, append only (§6.4). An imported result keeps the hash
-- of the ledger's words and no spec; every other run names the spec it ran
-- (review 631 N-4); a manual result counts only with a checker who is not
-- its author (R4), and names the one record of its author it checks
-- (checks_run_id), whose run time it keeps (review 631b R2-M3).
CREATE TABLE agentrt.debt_acceptance_run (
  debt_acceptance_run_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  debt_id       text NOT NULL REFERENCES agentrt.debt(debt_id),
  runner        text NOT NULL CHECK (runner IN ('imported','ci_evidence','promql','owner','manual')),
  spec_sha256   text CHECK (spec_sha256 ~ '^[0-9a-f]{64}$'),
  source_sha256 text CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
  result        text NOT NULL CHECK (result IN ('pass','partial','fail','unknown')),
  at_commits    jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(at_commits) = 'object'),
  ran_at        timestamptz NOT NULL,
  evidence_artifact_id text REFERENCES agentrt.artifact(artifact_id),
  checked_by_actor_id text REFERENCES agentrt.actor(actor_id),
  checks_run_id bigint REFERENCES agentrt.debt_acceptance_run(debt_acceptance_run_id),
  recorded_seq  bigint NOT NULL,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  CHECK ((runner = 'manual' AND result <> 'unknown') = (checks_run_id IS NOT NULL)),
  CHECK ((runner = 'imported') = (source_sha256 IS NOT NULL)),
  CHECK ((runner = 'imported') = (spec_sha256 IS NULL)),
  CHECK (runner <> 'manual' OR result = 'unknown'
         OR (checked_by_actor_id IS NOT NULL AND checked_by_actor_id <> created_by_actor_id))
);
CREATE INDEX debt_acceptance_run_debt_idx ON agentrt.debt_acceptance_run (debt_id, recorded_seq);
-- The order runs were written in, assigned by their guard at insert, never
-- by the writer (review 631d R4-M3): "the latest run" is the largest.
CREATE SEQUENCE agentrt.debt_record_seq;
-- An author's record is checked once (review 631b R2-M3).
CREATE UNIQUE INDEX debt_acceptance_run_checks_once ON agentrt.debt_acceptance_run (checks_run_id) WHERE checks_run_id IS NOT NULL;

-- Which event types write which state table (0009, same signature): the old
-- tables as before, word for word; the debt tables only by the debt events,
-- never by the import (§2.2).
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported'
          AND tbl NOT IN ('person','intake_client','issue','issue_text','issue_ask','issue_work','issue_check')
          AND tbl NOT IN ('debt','debt_counter','debt_text','debt_evidence','debt_evidence_check','debt_edge','debt_ref',
                          'debt_work','debt_part','debt_part_order','debt_part_landing','debt_acceptance_run','debt_decision',
                          'ratchet','accepted_gap')) OR CASE tbl
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

-- Whether body names row id of tbl: <tbl>_id, or the list <tbl>_ids.
CREATE FUNCTION agentrt.debt_body_names(body jsonb, tbl text, id text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT id IS NOT NULL AND (body->>(tbl || '_id') IS NOT DISTINCT FROM id
         OR COALESCE(body->(tbl || '_ids') @> jsonb_build_array(id::bigint), false))
$$;

-- Rows named in the body (0009, same signature, its branches word for
-- word): a debt by its debt_id, every other debt row by its own id, alone or
-- in a list (the store takes the ids from the sequences before the event);
-- the one counter row by being the counter's event; debt work also by the
-- envelope's mission_id and a step's ref by its plan_node_id, which
-- require_event checks against the row.
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
    WHEN 'debt'            THEN body->>'debt_id' IS NOT DISTINCT FROM id
    WHEN 'debt_counter'    THEN body ? 'floor'
    WHEN 'debt_decision'   THEN body->>'debt_decision_id' IS NOT DISTINCT FROM id
    WHEN 'ratchet'         THEN body->>'ratchet_id' IS NOT DISTINCT FROM id
    WHEN 'accepted_gap'    THEN body->>'accepted_gap_id' IS NOT DISTINCT FROM id
    WHEN 'debt_text'       THEN agentrt.debt_body_names(body, 'debt_text', id)
    WHEN 'debt_evidence'   THEN agentrt.debt_body_names(body, 'debt_evidence', id)
    WHEN 'debt_evidence_check' THEN agentrt.debt_body_names(body, 'debt_evidence_check', id)
    WHEN 'debt_edge'       THEN agentrt.debt_body_names(body, 'debt_edge', id)
    WHEN 'debt_ref'        THEN agentrt.debt_body_names(body, 'debt_ref', id)
    WHEN 'debt_work'       THEN agentrt.debt_body_names(body, 'debt_work', id)
    WHEN 'debt_part'       THEN agentrt.debt_body_names(body, 'debt_part', id)
    WHEN 'debt_part_order' THEN agentrt.debt_body_names(body, 'debt_part_order', id)
    WHEN 'debt_part_landing' THEN agentrt.debt_body_names(body, 'debt_part_landing', id)
    WHEN 'debt_acceptance_run' THEN agentrt.debt_body_names(body, 'debt_acceptance_run', id)
    ELSE true
  END
$$;

-- Who writes which debt event (§2.2, §4.2): never an external agent (an
-- intake token) or a host; the Owner signs off, rejects a sign-off and
-- accepts a gap; the runtime closes on its own, moves a state by a rule,
-- records the CI's and Prometheus's results and the evidence it read again;
-- the floor is raised by the Owner or the runtime; a reopen is the Owner's,
-- or the crew's within seven days of an automatic close (guard_debt), never
-- the runtime's (§4.2); an
-- imported or Owner-run result is the runtime's or the Owner's; everything
-- else the crew's (a thread, a seat), the Owner's or the runtime's.
CREATE FUNCTION agentrt.debt_writer_ok(event_type text, actor text, body jsonb) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN actor LIKE 'agent:%' OR actor LIKE 'host:%' THEN false
    WHEN event_type IN ('debt_signed_off','debt_signoff_rejected','gap_accepted') THEN actor = 'owner'
    WHEN event_type IN ('debt_auto_closed','debt_evidence_checked') THEN actor = 'controller'
    WHEN event_type = 'debt_state_changed' AND body ? 'by_rule' THEN actor = 'controller'
    WHEN event_type = 'debt_acceptance_ran' AND body->>'runner' IN ('ci_evidence','promql') THEN actor = 'controller'
    WHEN event_type = 'debt_acceptance_ran' AND body->>'runner' IN ('imported','owner') THEN actor IN ('owner','controller')
    WHEN event_type = 'debt_floor_raised' THEN actor IN ('owner','controller')
    WHEN event_type = 'debt_reopened' THEN actor = 'owner' OR actor LIKE 'thread:%' OR actor LIKE 'seat:%'
    WHEN event_type LIKE 'debt\_%' OR event_type IN ('ratchet_registered','ratchet_changed','gap_reopened')
      THEN actor LIKE 'thread:%' OR actor LIKE 'seat:%' OR actor IN ('owner','controller')
    ELSE false
  END
$$;

-- The debt event of this transaction: written by a writer allowed for its
-- type, on the stream of one of the debts it may be about, naming that debt
-- in its body.
CREATE FUNCTION agentrt.debt_event(debts text[]) RETURNS agentrt.event LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.current_event();
BEGIN
  IF NOT agentrt.debt_writer_ok(ev.event_type, ev.actor_id, ev.body_json) THEN
    RAISE EXCEPTION 'agentrt: % does not write a % event', ev.actor_id, ev.event_type USING ERRCODE = 'check_violation';
  END IF;
  IF (ev.body_json->>'debt_id' = ANY (debts)) IS NOT TRUE OR ev.event_stream_id IS DISTINCT FROM 'debt:' || (ev.body_json->>'debt_id') THEN
    RAISE EXCEPTION 'agentrt: event % (%) on % is not about debt %', ev.event_id, ev.event_type, ev.event_stream_id,
      array_to_string(debts, ' or ') USING ERRCODE = 'check_violation';
  END IF;
  RETURN ev;
END $$;

-- The event of a table that is not one debt's (the counter, a decision, a
-- ratchet, a gap): written by a writer allowed for its type, on stream.
CREATE FUNCTION agentrt.debt_side_event(stream text) RETURNS agentrt.event LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.current_event();
BEGIN
  IF NOT agentrt.debt_writer_ok(ev.event_type, ev.actor_id, ev.body_json) THEN
    RAISE EXCEPTION 'agentrt: % does not write a % event', ev.actor_id, ev.event_type USING ERRCODE = 'check_violation';
  END IF;
  IF ev.event_stream_id IS DISTINCT FROM stream THEN
    RAISE EXCEPTION 'agentrt: event % (%) is on %, not %', ev.event_id, ev.event_type, ev.event_stream_id, stream USING ERRCODE = 'check_violation';
  END IF;
  RETURN ev;
END $$;

-- Whether waiter already waits on target in the waiting graph: blocks (the
-- one held up waits on the one holding it) and part_of (the umbrella waits on
-- its part) together, live edges only (§5.5). Each row waits from to_debt_id
-- on from_debt_id, whatever its kind.
CREATE FUNCTION agentrt.debt_waits_on(waiter text, target text) RETURNS boolean LANGUAGE sql STABLE AS $$
  WITH RECURSIVE w(debt_id) AS (
    SELECT waiter
    UNION
    SELECT e.from_debt_id FROM w JOIN agentrt.debt_edge e ON e.to_debt_id = w.debt_id
     WHERE e.ended_at IS NULL AND e.edge_kind IN ('blocks','part_of'))
  SELECT EXISTS (SELECT 1 FROM w WHERE w.debt_id = target)
$$;

-- Which text kinds each debt event writes; a ledger's raw words only by an
-- import.
CREATE FUNCTION agentrt.debt_text_kind_ok(event_type text, text_kind text, source text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE event_type
    WHEN 'debt_filed'            THEN text_kind <> 'reject_reason' AND (text_kind <> 'import_raw' OR source IS NOT DISTINCT FROM 'import')
    WHEN 'debt_text_added'       THEN text_kind NOT IN ('import_raw','acceptance_text','watch','reject_reason')
    WHEN 'debt_acceptance_set'   THEN text_kind = 'acceptance_text'
    WHEN 'debt_state_changed'    THEN text_kind IN ('watch','note','now','next')
    WHEN 'debt_signoff_rejected' THEN text_kind IN ('reject_reason','next')
    WHEN 'debt_reopened'         THEN text_kind IN ('note','next')
    WHEN 'debt_signed_off'       THEN text_kind = 'note'
    WHEN 'debt_auto_closed'      THEN text_kind = 'note'
    WHEN 'debt_merged'           THEN text_kind = 'note'
    ELSE false
  END
$$;

-- Small reads of the moves below: a text of kind its event names, live
-- work, the Owner's class (T12: a boundary's gap, an Owner-run acceptance
-- step, a part that signs a rule), and the latest verified deploy.
CREATE FUNCTION agentrt.debt_named_text(debt text, kind text, named jsonb) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = debt AND t.text_kind = kind
                   AND named @> jsonb_build_array(t.debt_text_id))
$$;
CREATE FUNCTION agentrt.debt_live_work(debt text) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM agentrt.debt_work w WHERE w.debt_id = debt AND w.ended_at IS NULL)
$$;
CREATE FUNCTION agentrt.debt_owner_signs(d agentrt.debt) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM agentrt.debt_ref r WHERE r.debt_id = d.debt_id AND r.ref_kind = 'reclassified_as' AND r.ended_at IS NULL)
      OR d.acceptance_json @> '[{"kind":"owner"}]'::jsonb
      OR EXISTS (SELECT 1 FROM agentrt.debt_part p WHERE p.debt_id = d.debt_id AND p.apply_gate = 'rule_sign')
$$;
-- A part's landing counts once the runtime verified it (§6.3: its own
-- landing, or a declared commit it found on main under a CI it read) and
-- its deploy is recorded; only the runtime writes those facts
-- (guard_debt_part_landing).
CREATE FUNCTION agentrt.debt_verified_landing(l agentrt.debt_part_landing) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT l.deployed_at IS NOT NULL AND (l.via = 'runtime_landing' OR (l.on_main_checked_at IS NOT NULL AND l.ci_status IN ('succeeded','unread')))
$$;
CREATE FUNCTION agentrt.debt_landed_refusal(debt text) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT CASE
    WHEN NOT EXISTS (SELECT 1 FROM agentrt.debt_part p WHERE p.debt_id = debt) THEN 'it has no part, so whether it landed is unknown (§6.1)'
    WHEN EXISTS (SELECT 1 FROM agentrt.debt_part p WHERE p.debt_id = debt AND NOT EXISTS (SELECT 1 FROM agentrt.debt_part_landing l
                   WHERE l.debt_part_id = p.debt_part_id AND agentrt.debt_verified_landing(l)))
      THEN 'a part has no landing the runtime verified, deployed (§6.3)'
  END
$$;
CREATE FUNCTION agentrt.debt_last_deploy(debt text) RETURNS timestamptz LANGUAGE sql STABLE AS $$
  SELECT max(l.deployed_at) FROM agentrt.debt_part_landing l JOIN agentrt.debt_part p USING (debt_part_id)
   WHERE p.debt_id = debt AND agentrt.debt_verified_landing(l)
$$;

-- Why debt d may not close yet, NULL when it may (§6.6, T12; the same for
-- an automatic close, for awaiting the Owner and for his sign-off): its spec
-- reviewed by another writer (the table binds the review to the spec as it
-- is); every part landed, its last deploy watched for 24 hours (§6.2); its
-- latest run other than an import a pass of that very
-- spec, recorded after its last landing and its last reopening, naming a
-- commit in every part's repository; a sign-off summary; a ratchet or why
-- none; a follow-up or why none. Whether that commit contains each part's
-- landing is Gitea's to say: the runtime, the only writer of an automatic
-- close, checks it before it writes (5.8-4).
CREATE FUNCTION agentrt.debt_close_refusal(d agentrt.debt) RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE r agentrt.debt_acceptance_run;
BEGIN
  IF d.acceptance_reviewed_sha256 IS NULL THEN
    RETURN 'its acceptance spec was not reviewed by a writer who did not write it';
  END IF;
  IF agentrt.debt_landed_refusal(d.debt_id) IS NOT NULL THEN
    RETURN agentrt.debt_landed_refusal(d.debt_id);
  END IF;
  -- A deploy since the watch began starts it again (review 631b R2-M1).
  IF clock_timestamp() < agentrt.debt_last_deploy(d.debt_id) + interval '24 hours' THEN
    RETURN 'its last deploy has not been watched for 24 hours';
  END IF;
  SELECT * INTO r FROM agentrt.debt_acceptance_run WHERE debt_id = d.debt_id AND runner <> 'imported'
   ORDER BY recorded_seq DESC LIMIT 1;
  IF NOT FOUND OR r.result <> 'pass' OR r.spec_sha256 IS DISTINCT FROM d.acceptance_reviewed_sha256 THEN
    RETURN 'its latest acceptance run is not a pass of its reviewed spec';
  END IF;
  IF r.ran_at <= (SELECT max(l.created_at) FROM agentrt.debt_part_landing l JOIN agentrt.debt_part p USING (debt_part_id)
                   WHERE p.debt_id = d.debt_id)
     OR r.ran_at <= (SELECT max(t.at) FROM agentrt.debt_transition t WHERE t.debt_id = d.debt_id AND t.from_state = 'closed') THEN
    RETURN 'its passing run is older than its last landing or reopening';
  END IF;
  IF EXISTS (SELECT 1 FROM agentrt.debt_part p WHERE p.debt_id = d.debt_id AND jsonb_typeof(r.at_commits->p.repo) IS DISTINCT FROM 'string') THEN
    RETURN 'its passing run names no commit of some part''s repository';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = d.debt_id AND t.text_kind = 'signoff_summary') THEN
    RETURN 'it has no sign-off summary';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM agentrt.debt_ref f WHERE f.debt_id = d.debt_id AND f.ref_kind = 'guarded_by' AND f.ended_at IS NULL)
     AND NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = d.debt_id AND t.text_kind = 'no_ratchet_reason') THEN
    RETURN 'it names no ratchet, nor why there is none';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM agentrt.debt_edge e WHERE e.to_debt_id = d.debt_id AND e.edge_kind = 'follows_from' AND e.ended_at IS NULL)
     AND NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = d.debt_id AND t.text_kind = 'no_follow_up_reason') THEN
    RETURN 'it names no follow-up, nor why there is none';
  END IF;
  RETURN NULL;
END $$;

-- Whether a debt was imported awaiting the Owner and never left (§6.6: only
-- then does the ledger's imported pass stand for the sign-off): the event
-- that created it says import and awaiting_signoff, and no transition the
-- database recorded ever took it out of awaiting the Owner (review 631d
-- R4-M1, R4-M2). Linking work or adding a text moves nothing; the sign-off
-- being judged is recorded only after it is judged.
CREATE FUNCTION agentrt.debt_imported_awaiting(debt text) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM agentrt.debt d JOIN agentrt.event e ON e.event_id = d.birth_event_id WHERE d.debt_id = debt
                   AND e.body_json->>'source' = 'import' AND e.body_json->>'state' = 'awaiting_signoff')
     AND NOT EXISTS (SELECT 1 FROM agentrt.debt_transition t WHERE t.debt_id = debt AND t.from_state = 'awaiting_signoff')
$$;
-- Why the Owner may not sign off a debt imported awaiting him, NULL when he
-- may (§6.6, review 631c R3-M1): the ledger's word stands in for the new
-- system's spec review, landings and watch, not for the evidence itself.
-- Its latest acceptance run is the ledger's imported pass, and it keeps the
-- sign-off summary, the ratchet's text (or why none) and the follow-up's
-- (or why none) the ledger's line gave.
CREATE FUNCTION agentrt.debt_imported_refusal(debt text) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT CASE
    WHEN (SELECT r.runner || ':' || r.result FROM agentrt.debt_acceptance_run r WHERE r.debt_id = debt
           ORDER BY r.recorded_seq DESC LIMIT 1) IS DISTINCT FROM 'imported:pass'
      THEN 'its latest acceptance run is not the ledger''s imported pass'
    WHEN NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = debt AND t.text_kind = 'signoff_summary')
      THEN 'it has no sign-off summary'
    WHEN NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = debt AND t.text_kind IN ('ratchet_text','no_ratchet_reason'))
      THEN 'it names no ratchet, nor why there is none'
    WHEN NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_id = debt AND t.text_kind IN ('follow_up_text','no_follow_up_reason'))
      THEN 'it names no follow-up, nor why there is none'
  END
$$;
-- Why debt o may not become n by ev, NULL when it may: only the moves §6.2
-- lists, each with its evidence. The Owner signs off what still meets every
-- close condition (a debt imported awaiting him and never moved since on
-- the ledger's imported pass and lines instead) and rejects (with the
-- reason); a reopen is the Owner's, or the crew's within seven days of an
-- automatic close, with the reason; a merge closes as a duplicate; linking
-- live work starts it. The runtime's automatic close needs an automatic
-- flow, its watch ended, a debt outside the Owner's class and every close
-- condition. A plain state event (the crew's, or the runtime's by a rule)
-- records the move it makes, and: starts with live work; goes back to open
-- when all its work ended and nothing landed; watches once every part
-- landed, for at least 24 hours after its last deploy, saying what it
-- watches; watches longer; goes back to doing on a failed run; awaits the
-- Owner when his class and every close condition hold after the watch; or
-- closes withdrawn or superseded with its reason, or as a boundary over its
-- own link to a gap of its project the Owner accepted and still holds (T8).
CREATE FUNCTION agentrt.debt_move_refusal(o agentrt.debt, n agentrt.debt, ev agentrt.event) RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE
  typ   text := ev.event_type;
  named jsonb := COALESCE(ev.body_json->'debt_text_ids', '[]'::jsonb);
  move  text := o.state || '>' || n.state;
BEGIN
  IF typ = 'debt_signed_off' AND move = 'awaiting_signoff>closed' AND n.conclusion = 'signed_off' THEN
    RETURN CASE WHEN agentrt.debt_imported_awaiting(o.debt_id) THEN agentrt.debt_imported_refusal(o.debt_id)
                ELSE agentrt.debt_close_refusal(o) END;
  ELSIF typ = 'debt_signoff_rejected' AND move = 'awaiting_signoff>doing' THEN
    RETURN CASE WHEN NOT agentrt.debt_named_text(o.debt_id, 'reject_reason', named) THEN 'a rejection carries its reason' END;
  ELSIF typ = 'debt_reopened' AND move = 'closed>doing' THEN
    RETURN CASE WHEN NOT agentrt.debt_named_text(o.debt_id, 'note', named) THEN 'a reopening carries its reason'
                WHEN ev.actor_id <> 'owner' AND NOT (o.conclusion = 'auto_closed' AND o.state_since > clock_timestamp() - interval '7 days')
                  THEN 'only the Owner reopens it; the crew reopens an automatic close within seven days' END;
  ELSIF typ = 'debt_merged' AND o.state <> 'closed' AND n.conclusion = 'duplicate' THEN
    RETURN NULL;
  ELSIF typ = 'debt_work_linked' AND move = 'open>doing' THEN
    RETURN CASE WHEN NOT agentrt.debt_live_work(o.debt_id) THEN 'it starts with live work' END;
  ELSIF typ = 'debt_auto_closed' AND move = 'watching>closed' AND n.conclusion = 'auto_closed' THEN
    RETURN COALESCE(CASE WHEN o.flow <> 'auto' THEN 'a debt worked by hand is not closed automatically'
                         WHEN o.watch_until > clock_timestamp() THEN 'its watch has not ended'
                         WHEN agentrt.debt_owner_signs(o) THEN 'the Owner signs it off (T12)' END, agentrt.debt_close_refusal(o));
  ELSIF typ <> 'debt_state_changed' THEN
    RETURN 'it is no move of a ' || typ || ' event';
  END IF;
  IF ev.body_json->>'conclusion' IS DISTINCT FROM n.conclusion THEN
    RETURN 'its event records another conclusion';
  END IF;
  IF n.state = 'closed' THEN
    RETURN CASE
      WHEN o.state = 'closed' OR n.conclusion NOT IN ('withdrawn','boundary','superseded')
        THEN 'a plain state event closes it withdrawn, superseded or as a boundary, and nothing closed'
      WHEN NOT agentrt.debt_named_text(o.debt_id, 'note', named) THEN 'a close carries its reason'
      WHEN n.conclusion = 'boundary' AND NOT EXISTS (SELECT 1 FROM agentrt.debt_ref r JOIN agentrt.accepted_gap g USING (accepted_gap_id)
             WHERE r.debt_id = o.debt_id AND r.ref_kind = 'reclassified_as' AND r.ended_at IS NULL AND g.state = 'accepted'
               AND g.project = o.project)
        THEN 'a boundary is its own link to a gap of its project the Owner accepted and still holds accepted (T8)' END;
  END IF;
  RETURN CASE move
    WHEN 'open>doing' THEN CASE WHEN NOT agentrt.debt_live_work(o.debt_id) THEN 'it starts with live work' END
    WHEN 'doing>open' THEN CASE WHEN agentrt.debt_live_work(o.debt_id) OR NOT EXISTS (SELECT 1 FROM agentrt.debt_work w WHERE w.debt_id = o.debt_id)
                                  OR EXISTS (SELECT 1 FROM agentrt.debt_part_landing l JOIN agentrt.debt_part p USING (debt_part_id)
                                               WHERE p.debt_id = o.debt_id)
                                THEN 'it goes back to open once all its work ended with nothing landed' END
    WHEN 'doing>watching' THEN COALESCE(agentrt.debt_landed_refusal(o.debt_id), CASE
                                WHEN n.watch_text_id IS NULL THEN 'a watch says what it watches for'
                                WHEN n.watch_until < agentrt.debt_last_deploy(o.debt_id) + interval '24 hours'
                                  THEN 'a watch lasts at least 24 hours from the last deploy' END)
    WHEN 'watching>watching' THEN CASE WHEN n.watch_until <= o.watch_until THEN 'a watch only grows longer' END
    WHEN 'watching>doing' THEN CASE WHEN (SELECT r.result FROM agentrt.debt_acceptance_run r WHERE r.debt_id = o.debt_id
                                            AND r.runner <> 'imported' ORDER BY r.recorded_seq DESC LIMIT 1) IS DISTINCT FROM 'fail'
                                THEN 'it goes back to doing when its latest acceptance run failed' END
    WHEN 'watching>awaiting_signoff' THEN COALESCE(CASE WHEN o.watch_until > clock_timestamp() THEN 'its watch has not ended'
                                WHEN NOT agentrt.debt_owner_signs(o) THEN 'it is none the Owner signs (T12): it closes on its own' END,
                                agentrt.debt_close_refusal(o))
    ELSE 'it is no move §6.2 lists'
  END;
END $$;

-- Debts (§2.4, §6.1, §6.2): created open by debt_filed (an import keeps the
-- state the ledger had, and is manual); a TD- number only by PROD's runtime
-- role; filed by the writer of its event, at version 0; the version moves by
-- one with every write; when and by whom it was filed never change; each
-- kind of change comes with its own event: the fields by debt_edited, the
-- flow by debt_flow_set, the spec by debt_acceptance_set (its writer's, and
-- it clears its review), the review by debt_acceptance_reviewed from a
-- writer who did not write the spec, duplicate_of once by a merge; and the
-- state only by a move §6.2 lists, with its evidence (debt_move_refusal).
CREATE FUNCTION agentrt.guard_debt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev  agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
  typ text := ev.event_type;
  why text;
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF typ <> 'debt_filed' THEN
      RAISE EXCEPTION 'agentrt: a debt is created by debt_filed, not %', typ USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.debt_id LIKE 'TD-%' AND current_user <> 'platform_agentrt_prod' THEN
      RAISE EXCEPTION 'agentrt: only PROD''s runtime (platform_agentrt_prod) issues TD- numbers, not %', current_user
        USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.created_by_actor_id <> ev.actor_id OR NEW.version <> 0 OR NEW.conclusion IS NOT NULL OR NEW.duplicate_of IS NOT NULL
       OR NEW.acceptance_reviewed_sha256 IS NOT NULL OR NEW.acceptance_by IS DISTINCT FROM (CASE WHEN NEW.acceptance_json = '[]'::jsonb
         THEN NULL ELSE ev.actor_id END) THEN
      RAISE EXCEPTION 'agentrt: a new debt and its spec are its event''s writer''s, at version 0, not closed, its spec not reviewed'
        USING ERRCODE = 'check_violation';
    END IF;
    IF (ev.body_json->>'project', ev.body_json->>'priority', ev.body_json->>'state', ev.body_json->>'flow')
       IS DISTINCT FROM (NEW.project, NEW.priority, NEW.state, NEW.flow) THEN
      RAISE EXCEPTION 'agentrt: debt_filed records the debt it files' USING ERRCODE = 'check_violation';
    END IF;
    NEW.birth_event_id := ev.event_id; -- whatever the writer said
    IF ev.body_json->>'source' = 'import' THEN
      IF NEW.flow <> 'manual' THEN
        RAISE EXCEPTION 'agentrt: an imported debt is manual until its spec is rewritten' USING ERRCODE = 'check_violation';
      END IF;
    ELSIF NEW.state <> 'open' THEN
      RAISE EXCEPTION 'agentrt: a filed debt starts open' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'agentrt: debt % moves from version % to %; one at a time', OLD.debt_id, OLD.version, NEW.version
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.filed_at <> OLD.filed_at OR NEW.created_by_actor_id <> OLD.created_by_actor_id OR NEW.birth_event_id <> OLD.birth_event_id THEN
    RAISE EXCEPTION 'agentrt: debt % keeps when and by whom it was filed', OLD.debt_id USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.project, NEW.area, NEW.title, NEW.priority, NEW.cost, NEW.risk, NEW.visibility, NEW.tags)
     IS DISTINCT FROM (OLD.project, OLD.area, OLD.title, OLD.priority, OLD.cost, OLD.risk, OLD.visibility, OLD.tags)
     AND typ <> 'debt_edited' THEN
    RAISE EXCEPTION 'agentrt: the fields of a debt change by debt_edited, not %', typ USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.flow <> OLD.flow AND typ <> 'debt_flow_set' THEN
    RAISE EXCEPTION 'agentrt: the flow of a debt changes by debt_flow_set, not %', typ USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.acceptance_json, NEW.acceptance_by) IS DISTINCT FROM (OLD.acceptance_json, OLD.acceptance_by)
     AND (typ <> 'debt_acceptance_set' OR NEW.acceptance_reviewed_sha256 IS NOT NULL
          OR NEW.acceptance_by IS DISTINCT FROM (CASE WHEN NEW.acceptance_json = '[]'::jsonb THEN NULL ELSE ev.actor_id END)) THEN
    RAISE EXCEPTION 'agentrt: a spec is written by debt_acceptance_set, as its writer''s, which clears its review' USING ERRCODE = 'check_violation';
  END IF;
  -- The table's check binds a review to the spec as it is and keeps it from
  -- the spec's author; what the event says of them is only compared.
  IF (NEW.acceptance_reviewed_sha256, NEW.acceptance_reviewed_by) IS DISTINCT FROM (OLD.acceptance_reviewed_sha256, OLD.acceptance_reviewed_by)
     AND NOT (typ = 'debt_acceptance_set' AND NEW.acceptance_reviewed_sha256 IS NULL)
     AND (typ <> 'debt_acceptance_reviewed' OR NEW.acceptance_reviewed_by IS DISTINCT FROM ev.actor_id
          OR NEW.acceptance_reviewed_sha256 IS DISTINCT FROM ev.body_json->>'acceptance_sha256'
          OR (ev.body_json ? 'spec_author' AND ev.body_json->>'spec_author' IS DISTINCT FROM NEW.acceptance_by)) THEN
    RAISE EXCEPTION 'agentrt: a spec is reviewed by debt_acceptance_reviewed, of the spec as it is, by a writer who did not write it'
      USING ERRCODE = 'check_violation';
  END IF;
  -- A merge sets it, once; a reopening of a duplicate clears it (§6.2: the
  -- Owner reopens any close; the merge event keeps whose duplicate it was;
  -- review 631d R4-M5).
  IF NEW.duplicate_of IS DISTINCT FROM OLD.duplicate_of AND NOT (typ = 'debt_merged' AND OLD.duplicate_of IS NULL)
     AND NOT (typ = 'debt_reopened' AND NEW.duplicate_of IS NULL) THEN
    RAISE EXCEPTION 'agentrt: only a merge sets duplicate_of, once, and only a reopening clears it' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.watch_text_id IS DISTINCT FROM OLD.watch_text_id AND NEW.watch_text_id IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM agentrt.debt_text t WHERE t.debt_text_id = NEW.watch_text_id AND t.text_kind = 'watch') THEN
    RAISE EXCEPTION 'agentrt: what debt % watches for is a watch text', OLD.debt_id USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.state <> OLD.state) <> (NEW.state_since IS DISTINCT FROM OLD.state_since)
     OR (NEW.state <> OLD.state AND NEW.state_since NOT BETWEEN transaction_timestamp() AND clock_timestamp()) THEN
    RAISE EXCEPTION 'agentrt: state_since moves with the state of debt %, only then, to the time of the move', OLD.debt_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.state, NEW.conclusion, NEW.watch_until, NEW.watch_text_id) IS DISTINCT FROM (OLD.state, OLD.conclusion, OLD.watch_until, OLD.watch_text_id) THEN
    -- Every move records itself: its event names the from and to it makes.
    IF (NEW.state <> OLD.state OR typ = 'debt_state_changed')
       AND (ev.body_json->>'from', ev.body_json->>'to') IS DISTINCT FROM (OLD.state, NEW.state) THEN
      RAISE EXCEPTION 'agentrt: the move of % from % to % is not the one its % event records', OLD.debt_id, OLD.state, NEW.state, typ
        USING ERRCODE = 'check_violation';
    END IF;
    why := agentrt.debt_move_refusal(OLD, NEW, ev);
    IF why IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: debt % does not move from % to % (%) by % (by %): %', OLD.debt_id, OLD.state, NEW.state,
        COALESCE(NEW.conclusion, 'no conclusion'), typ, ev.actor_id, why USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- An automatic flow needs a spec and at least one part (§6.1), checked at
-- commit, when the parts a filing writes are there.
CREATE FUNCTION agentrt.check_debt_flow() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM agentrt.debt d WHERE d.debt_id = NEW.debt_id AND d.flow = 'auto'
             AND (d.acceptance_json = '[]'::jsonb OR NOT EXISTS (SELECT 1 FROM agentrt.debt_part p WHERE p.debt_id = d.debt_id))) THEN
    RAISE EXCEPTION 'agentrt: debt % flows automatically only with an acceptance spec and at least one part', NEW.debt_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
-- The database's own record of each move: one transition per real change
-- of a debt's state, with the event that made it (current_event). It runs
-- as the migration owner (SECURITY DEFINER), so the runtime role, which may
-- only read debt_transition, cannot write one itself; a trigger function is
-- never called directly. It records only as debt_20_transition on
-- agentrt.debt: attached anywhere else (a writer's own temporary table) it
-- refuses, and no role but its owner may attach it (EXECUTE is revoked
-- below; review 631e 1). pg_temp is searched last.
CREATE FUNCTION agentrt.record_debt_transition() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
  SET search_path = pg_catalog, agentrt, pg_temp AS $$
BEGIN
  IF TG_RELID <> 'agentrt.debt'::regclass OR TG_NAME <> 'debt_20_transition' OR TG_WHEN <> 'AFTER'
     OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE' THEN
    RAISE EXCEPTION 'agentrt: record_debt_transition records only as debt_20_transition after each update of agentrt.debt, not as % % % % on %',
      TG_NAME, TG_WHEN, TG_LEVEL, TG_OP, TG_RELID::regclass USING ERRCODE = 'insufficient_privilege';
  END IF;
  INSERT INTO agentrt.debt_transition (debt_id, event_id, from_state, to_state)
  VALUES (NEW.debt_id, (agentrt.current_event()).event_id, OLD.state, NEW.state);
  RETURN NULL;
END $$;
CREATE TRIGGER debt_20_transition AFTER UPDATE OF state ON agentrt.debt FOR EACH ROW
  WHEN (OLD.state IS DISTINCT FROM NEW.state) EXECUTE FUNCTION agentrt.record_debt_transition();
REVOKE ALL ON FUNCTION agentrt.record_debt_transition() FROM PUBLIC;

-- A debt event names a move whole or not at all: from and to both, of the
-- states a debt has, or neither (review 631d R4-S1). The test is null safe:
-- a JSON null (or a missing end) is no state, and refused (review 631e 3).
CREATE FUNCTION agentrt.check_debt_event_shape() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE states text[] := ARRAY['open','doing','watching','awaiting_signoff','closed'];
BEGIN
  IF (NEW.body_json ? 'from' OR NEW.body_json ? 'to')
     AND NOT coalesce(NEW.body_json->>'from' = ANY (states) AND NEW.body_json->>'to' = ANY (states), false) THEN
    RAISE EXCEPTION 'agentrt: debt event % names a move whole (from and to, of a debt''s states) or not at all', NEW.event_type
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER event_05_debt_shape BEFORE INSERT ON agentrt.event FOR EACH ROW
  WHEN (NEW.event_stream_id LIKE 'debt:%') EXECUTE FUNCTION agentrt.check_debt_event_shape();

-- At commit, what a debt event says happened did: a filing created the
-- debt it is the birth of, and an event that records a move (another to
-- than its from) made exactly that move itself, as the one transition the
-- database recorded with this event's id shows (review 631d R4-M1, R4-M2).
-- Checked early, before the move, it refuses; after it, the event's one
-- transition is taken and no later move can reuse the event (631e 2).
CREATE FUNCTION agentrt.check_debt_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  debt  text := substr(NEW.event_stream_id, 6);
  moved agentrt.debt_transition;
BEGIN
  IF NEW.event_type = 'debt_filed'
     AND NOT EXISTS (SELECT 1 FROM agentrt.debt d WHERE d.debt_id = debt AND d.birth_event_id = NEW.event_id) THEN
    RAISE EXCEPTION 'agentrt: filing event % created no debt %', NEW.event_id, debt USING ERRCODE = 'check_violation';
  END IF;
  IF NOT (NEW.body_json ? 'from' AND NEW.body_json->>'from' IS DISTINCT FROM NEW.body_json->>'to') THEN
    RETURN NULL;
  END IF;
  SELECT * INTO moved FROM agentrt.debt_transition t WHERE t.event_id = NEW.event_id;
  IF NOT FOUND OR moved.debt_id <> debt OR moved.from_state <> NEW.body_json->>'from'
     OR moved.to_state <> NEW.body_json->>'to' THEN
    RAISE EXCEPTION 'agentrt: event % records a move of % from % to % it did not make', NEW.event_id, debt,
      NEW.body_json->>'from', NEW.body_json->>'to' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER event_90_debt_facts AFTER INSERT ON agentrt.event DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
  WHEN (NEW.event_stream_id LIKE 'debt:%' AND (NEW.event_type = 'debt_filed'
        OR (NEW.body_json ? 'from' AND NEW.body_json->>'from' IS DISTINCT FROM NEW.body_json->>'to')))
  EXECUTE FUNCTION agentrt.check_debt_event();

CREATE CONSTRAINT TRIGGER debt_90_flow AFTER INSERT OR UPDATE ON agentrt.debt
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agentrt.check_debt_flow();

-- The floor: the one row this file wrote, raised by debt_floor_raised to the
-- floor its event records, never lowered (§2.4).
CREATE FUNCTION agentrt.guard_debt_counter() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_side_event('debt-counter');
BEGIN
  IF TG_OP = 'INSERT' THEN
    RAISE EXCEPTION 'agentrt: the debt counter is the one row 0011 wrote' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.version <> OLD.version + 1 OR NEW.floor <= OLD.floor OR NEW.floor::text IS DISTINCT FROM ev.body_json->>'floor' THEN
    RAISE EXCEPTION 'agentrt: the debt floor only rises (% to %), to the floor its event records, one version at a time', OLD.floor, NEW.floor
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Texts: by their event's writer, of a kind its type writes, the sha256 the
-- body's.
CREATE FUNCTION agentrt.guard_debt_text() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
BEGIN
  IF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: a debt text is written by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  IF NOT agentrt.debt_text_kind_ok(ev.event_type, NEW.text_kind, ev.body_json->>'source') THEN
    RAISE EXCEPTION 'agentrt: a % event writes no % text', ev.event_type, NEW.text_kind USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.sha256 <> encode(sha256(convert_to(NEW.body, 'UTF8')), 'hex') THEN
    RAISE EXCEPTION 'agentrt: a text''s sha256 is its body''s' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Evidence and landings: by their event's writer, on their debt's stream; a
-- check of evidence on the stream of the evidence's debt (by the runtime:
-- debt_writer_ok).
CREATE FUNCTION agentrt.guard_debt_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
BEGIN
  IF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: evidence is recorded by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION agentrt.guard_debt_evidence_check() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM agentrt.debt_event(ARRAY(SELECT debt_id FROM agentrt.debt_evidence WHERE debt_evidence_id = NEW.debt_evidence_id));
  RETURN NEW;
END $$;
CREATE FUNCTION agentrt.guard_debt_part_landing() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_event(ARRAY(SELECT debt_id FROM agentrt.debt_part WHERE debt_part_id = NEW.debt_part_id));
BEGIN
  IF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: a landing is recorded by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  -- The crew declares a commit and a deploy; what the runtime saw (its own
  -- landing, the commit on main, the CI, a deploy it read) only it records
  -- (§6.3), so no writer certifies its own landing.
  IF (NEW.via = 'runtime_landing' OR NEW.on_main_checked_at IS NOT NULL OR NEW.ci_status IS NOT NULL OR NEW.deploy_read = 'observed')
     AND ev.actor_id <> 'controller' THEN
    RAISE EXCEPTION 'agentrt: what the runtime saw of a landing only the runtime records, not %', ev.actor_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Edges: added live by debt_edge_added (or rewired by a merge into one of
-- its debts), on the stream of one of its debts, by its event's writer; a
-- blocks or part_of edge never closes a cycle of the waiting graph, checked
-- under the debt graph lock (742132, the store's) so two halves of a cycle
-- written at once cannot both commit (§5.5). Afterwards only its end is
-- written, once.
CREATE FUNCTION agentrt.guard_debt_edge() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  cur agentrt.event := agentrt.current_event();
  ev  agentrt.event := agentrt.debt_event(CASE WHEN cur.event_type = 'debt_merged'
                                                AND cur.body_json->>'into' IN (NEW.from_debt_id, NEW.to_debt_id)
                                           THEN ARRAY[NEW.from_debt_id, NEW.to_debt_id, cur.body_json->>'debt_id']
                                           ELSE ARRAY[NEW.from_debt_id, NEW.to_debt_id] END);
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF ev.event_type NOT IN ('debt_edge_added','debt_merged') OR NEW.ended_at IS NOT NULL OR NEW.created_by_actor_id <> ev.actor_id THEN
      RAISE EXCEPTION 'agentrt: an edge is added live by debt_edge_added or a merge, by its event''s writer' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.edge_kind IN ('blocks','part_of') THEN
      PERFORM pg_advisory_xact_lock(742132);
      IF agentrt.debt_waits_on(NEW.from_debt_id, NEW.to_debt_id) THEN
        RAISE EXCEPTION 'agentrt: % % % closes a cycle: % already waits on %', NEW.from_debt_id, NEW.edge_kind, NEW.to_debt_id,
          NEW.from_debt_id, NEW.to_debt_id USING ERRCODE = 'check_violation';
      END IF;
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - ARRAY['ended_at','ended_why']) IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['ended_at','ended_why'])
     OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR ev.event_type NOT IN ('debt_edge_ended','debt_merged','debt_state_changed') THEN
    RAISE EXCEPTION 'agentrt: edge % changes nothing but its end, once', OLD.debt_edge_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Refs and work: added live by their own event, by its writer; a debt is
-- paid by a step, and worked by a live mission of the dev tenant (§4.1,
-- §6.2); afterwards only their end, once, by their ending event.
CREATE FUNCTION agentrt.guard_debt_ref() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF ev.event_type <> 'debt_ref_added' OR NEW.ended_at IS NOT NULL OR NEW.created_by_actor_id <> ev.actor_id THEN
      RAISE EXCEPTION 'agentrt: a ref is added live by debt_ref_added, by its event''s writer' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.plan_node_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM agentrt.plan_node n WHERE n.plan_node_id = NEW.plan_node_id
                                                      AND n.node_kind = 'step') THEN
      RAISE EXCEPTION 'agentrt: a debt is paid by a step; % is not one', NEW.plan_node_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - ARRAY['ended_at','outcome']) IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['ended_at','outcome'])
     OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR ev.event_type <> 'debt_ref_ended' THEN
    RAISE EXCEPTION 'agentrt: ref % changes nothing but its end, once, by debt_ref_ended', OLD.debt_ref_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION agentrt.guard_debt_work() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF ev.event_type <> 'debt_work_linked' OR NEW.ended_at IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: debt work is linked live by debt_work_linked' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM agentrt.mission m WHERE m.mission_id = NEW.mission_id AND m.tenant = 'dev'
                     AND m.state NOT IN ('done','stopped','expired')) THEN
      RAISE EXCEPTION 'agentrt: debt % is worked by a live mission of the dev tenant; % is not one', NEW.debt_id, NEW.mission_id
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'ended_at') IS DISTINCT FROM (to_jsonb(OLD) - 'ended_at') OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL
     OR ev.event_type <> 'debt_work_ended' THEN
    RAISE EXCEPTION 'agentrt: debt work % changes nothing but its end, once, by debt_work_ended', OLD.debt_work_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Parts: on their debt's stream, at version 0, then one version at a time,
-- never moved to another debt, nor to another repository once landed; an
-- order between two parts never changes.
CREATE FUNCTION agentrt.guard_debt_part() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM agentrt.debt_event(ARRAY[NEW.debt_id]);
  IF (TG_OP = 'INSERT' AND NEW.version <> 0) OR (TG_OP = 'UPDATE' AND (NEW.version <> OLD.version + 1 OR NEW.debt_id <> OLD.debt_id)) THEN
    RAISE EXCEPTION 'agentrt: a part starts at version 0, moves one version at a time and stays its debt''s' USING ERRCODE = 'check_violation';
  END IF;
  -- A landing is of the part's repository: once one is recorded the
  -- repository stays (another one is a new part that lands on its own;
  -- review 631c R3-M2).
  IF TG_OP = 'UPDATE' AND NEW.repo <> OLD.repo
     AND EXISTS (SELECT 1 FROM agentrt.debt_part_landing l WHERE l.debt_part_id = OLD.debt_part_id) THEN
    RAISE EXCEPTION 'agentrt: part % has a landing in %; another repository is another part', OLD.debt_part_id, OLD.repo
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE FUNCTION agentrt.guard_debt_part_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM agentrt.debt_event(ARRAY[NEW.debt_id]);
  IF TG_OP = 'UPDATE' THEN
    RAISE EXCEPTION 'agentrt: an order between parts never changes' USING ERRCODE = 'check_violation';
  END IF;
  -- Within one debt, under its own lock, a part never comes after itself
  -- (§5.5: the parts' order is checked within the debt; review 631 M-3).
  PERFORM pg_advisory_xact_lock(742133, hashtext(NEW.debt_id));
  IF agentrt.debt_part_after(NEW.after_debt_part_id, NEW.debt_part_id) THEN
    RAISE EXCEPTION 'agentrt: part % after part % closes a cycle of the parts of %', NEW.debt_part_id, NEW.after_debt_part_id, NEW.debt_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- Whether part already comes (transitively) after target.
CREATE FUNCTION agentrt.debt_part_after(part bigint, target bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
  WITH RECURSIVE a(debt_part_id) AS (
    SELECT part
    UNION
    SELECT o.after_debt_part_id FROM a JOIN agentrt.debt_part_order o ON o.debt_part_id = a.debt_part_id)
  SELECT EXISTS (SELECT 1 FROM a WHERE a.debt_part_id = target)
$$;

-- Acceptance runs: the run its event records, by its writer, except a
-- manual result other than unknown: recorded by its checker, who is not its
-- author (the table's check), over the one record its author made with the
-- same evidence, spec, commits and run time, recorded after the last
-- landing and reopening (§6.4: one writer records, another checks; §6.2: a
-- reopening leaves the old runs behind).
CREATE FUNCTION agentrt.guard_debt_acceptance_run() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev  agentrt.event := agentrt.debt_event(ARRAY[NEW.debt_id]);
  src agentrt.debt_acceptance_run;
BEGIN
  IF ev.body_json->>'runner' IS DISTINCT FROM NEW.runner OR ev.body_json->>'result' IS DISTINCT FROM NEW.result THEN
    RAISE EXCEPTION 'agentrt: an acceptance run is the one its event records' USING ERRCODE = 'check_violation';
  END IF;
  NEW.recorded_seq := nextval('agentrt.debt_record_seq'); -- the write order, whatever the writer said
  -- A run is of the spec as it is now, and has already happened (review 631
  -- N-4): an import keeps none.
  IF NEW.ran_at > clock_timestamp() OR (NEW.runner <> 'imported' AND NEW.spec_sha256 IS DISTINCT FROM
       (SELECT agentrt.debt_spec_sha(d.acceptance_json) FROM agentrt.debt d WHERE d.debt_id = NEW.debt_id AND d.acceptance_json <> '[]'::jsonb)) THEN
    RAISE EXCEPTION 'agentrt: an acceptance run of % is of its current spec, and in the past', NEW.debt_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.runner = 'manual' AND NEW.result <> 'unknown' THEN
    IF NEW.checked_by_actor_id IS DISTINCT FROM ev.actor_id OR NEW.checked_by_actor_id = NEW.created_by_actor_id THEN
      RAISE EXCEPTION 'agentrt: a manual result counts when a checker who is not its author records it' USING ERRCODE = 'check_violation';
    END IF;
    SELECT * INTO src FROM agentrt.debt_acceptance_run r WHERE r.debt_acceptance_run_id = NEW.checks_run_id;
    IF NOT FOUND OR src.debt_id <> NEW.debt_id OR src.runner <> 'manual' OR src.result <> 'unknown'
       OR src.created_by_actor_id <> NEW.created_by_actor_id OR NEW.evidence_artifact_id IS NULL
       OR src.evidence_artifact_id IS DISTINCT FROM NEW.evidence_artifact_id OR src.spec_sha256 IS DISTINCT FROM NEW.spec_sha256
       OR src.at_commits <> NEW.at_commits OR src.ran_at <> NEW.ran_at THEN
      RAISE EXCEPTION 'agentrt: a manual result checks the one record its author made, with the same evidence, spec, commits and run time'
        USING ERRCODE = 'check_violation';
    END IF;
    IF src.ran_at <= (SELECT max(l.created_at) FROM agentrt.debt_part_landing l JOIN agentrt.debt_part p USING (debt_part_id)
                       WHERE p.debt_id = NEW.debt_id)
       OR src.ran_at <= (SELECT max(t.at) FROM agentrt.debt_transition t WHERE t.debt_id = NEW.debt_id AND t.from_state = 'closed') THEN
      RAISE EXCEPTION 'agentrt: the record checked ran before the last landing or reopening of %: it no longer counts', NEW.debt_id
        USING ERRCODE = 'check_violation';
    END IF;
  ELSIF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: an acceptance run is recorded by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Decisions, ratchets and gaps: on their own streams (debt-decisions,
-- ratchet:<id>, gap:<id>), created by their first event and then one
-- version at a time; a gap is accepted only by the Owner (debt_writer_ok).
CREATE FUNCTION agentrt.guard_debt_side() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  r  jsonb := to_jsonb(NEW);
  ev agentrt.event := agentrt.debt_side_event(CASE TG_TABLE_NAME WHEN 'debt_decision' THEN 'debt-decisions'
                                                WHEN 'ratchet' THEN 'ratchet:' || (r->>'ratchet_id') ELSE 'gap:' || (r->>'accepted_gap_id') END);
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF (r->>'version')::int <> 0 OR ev.event_type NOT IN ('debt_decision_set','ratchet_registered','gap_accepted') THEN
      RAISE EXCEPTION 'agentrt: a new % row starts at version 0 with its first event, not %', TG_TABLE_NAME, ev.event_type
        USING ERRCODE = 'check_violation';
    END IF;
  ELSIF (r->>'version')::int <> (to_jsonb(OLD)->>'version')::int + 1 THEN
    RAISE EXCEPTION 'agentrt: a % row moves one version at a time', TG_TABLE_NAME USING ERRCODE = 'check_violation';
  END IF;
  IF TG_TABLE_NAME = 'accepted_gap' AND (r->>'state' = 'accepted') <> (ev.event_type = 'gap_accepted') THEN
    RAISE EXCEPTION 'agentrt: a gap is accepted by gap_accepted and reopened by gap_reopened' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- The state tables' event check and no deletion (0002's functions) and the
-- guards above; the append-only tables are written only with an event.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['debt','debt_counter','debt_edge','debt_ref','debt_work','debt_part','debt_part_order',
                           'debt_decision','ratchet','accepted_gap'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.require_event()',
                   t || '_00_event', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE DELETE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_delete', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON agentrt.%I FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_truncate', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.%I()',
                   t || '_10_guard', t, CASE WHEN t IN ('debt_decision','ratchet','accepted_gap') THEN 'guard_debt_side' ELSE 'guard_' || t END);
  END LOOP;
  EXECUTE 'CREATE TRIGGER debt_transition_append_only BEFORE UPDATE OR DELETE ON agentrt.debt_transition FOR EACH ROW
             EXECUTE FUNCTION agentrt.forbid_change()';
  EXECUTE 'CREATE TRIGGER debt_transition_no_truncate BEFORE TRUNCATE ON agentrt.debt_transition FOR EACH STATEMENT
             EXECUTE FUNCTION agentrt.forbid_change()';
  FOREACH t IN ARRAY ARRAY['debt_text','debt_evidence','debt_evidence_check','debt_part_landing','debt_acceptance_run'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.require_event()',
                   t || '_00_event', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change()',
                   t || '_append_only', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON agentrt.%I FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change()',
                   t || '_no_truncate', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.%I()',
                   t || '_10_guard', t, 'guard_' || t);
  END LOOP;
END $$;

-- Grants, named one by one (as 0009): append only SELECT and INSERT; state
-- tables SELECT, INSERT and UPDATE; never DELETE; the identity sequences for
-- the ids the store takes before the event that names them. agent_reader
-- gets nothing here (where it has pg_read_all_data it still reads them;
-- §4.3, T13).
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
  t  text;
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  FOREACH t IN ARRAY ARRAY['debt_text','debt_evidence','debt_evidence_check','debt_part_landing','debt_acceptance_run'] LOOP
    EXECUTE format('GRANT SELECT, INSERT ON agentrt.%I TO %I', t, rt);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['debt','debt_counter','debt_edge','debt_ref','debt_work','debt_part','debt_part_order',
                           'debt_decision','ratchet','accepted_gap'] LOOP
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON agentrt.%I TO %I', t, rt);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['debt_text','debt_evidence','debt_evidence_check','debt_edge','debt_decision','debt_part',
                           'debt_part_order','debt_part_landing','debt_ref','debt_work','debt_acceptance_run'] LOOP
    EXECUTE format('GRANT USAGE ON SEQUENCE %s TO %I', pg_get_serial_sequence('agentrt.' || t, t || '_id'), rt);
  END LOOP;
  -- The transitions are the database's own: the runtime reads them only.
  EXECUTE format('GRANT SELECT ON agentrt.debt_transition TO %I', rt);
  -- The one SECURITY DEFINER function: nobody but its owner may attach it
  -- (it is revoked from PUBLIC where it is created; never granted here).
  EXECUTE format('REVOKE ALL ON FUNCTION agentrt.record_debt_transition() FROM %I', rt);
  EXECUTE format('GRANT USAGE ON SEQUENCE agentrt.debt_record_seq TO %I', rt);
  FOREACH t IN ARRAY ARRAY['debt_body_names(jsonb,text,text)','debt_writer_ok(text,text,jsonb)','debt_event(text[])',
                           'debt_side_event(text)','debt_waits_on(text,text)','debt_text_kind_ok(text,text,text)',
                           'guard_debt()','check_debt_flow()','guard_debt_counter()','guard_debt_text()','guard_debt_evidence()',
                           'guard_debt_evidence_check()','guard_debt_part_landing()','guard_debt_edge()','guard_debt_ref()',
                           'guard_debt_work()','guard_debt_part()','guard_debt_part_order()','guard_debt_acceptance_run()',
                           'guard_debt_side()','debt_spec_sha(jsonb)','debt_named_text(text,text,jsonb)','debt_live_work(text)',
                           'debt_owner_signs(agentrt.debt)','debt_verified_landing(agentrt.debt_part_landing)',
                           'debt_landed_refusal(text)','debt_last_deploy(text)','debt_close_refusal(agentrt.debt)',
                           'debt_move_refusal(agentrt.debt,agentrt.debt,agentrt.event)','debt_part_after(bigint,bigint)',
                           'debt_imported_awaiting(text)','debt_imported_refusal(text)','debt_has_words(text)',
                           'check_debt_event_shape()','check_debt_event()'] LOOP
    EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.%s TO %I', t, rt);
  END LOOP;
END $$;
