-- agentrt 0001: the state database of the agent runtime (design v5 §5.1.2).
-- Forward only. Runs as agentrt_owner_<env> inside one transaction; the
-- migrator has already created schema agentrt and agentrt.schema_migration.
-- Guards (triggers) are in 0002, grants in 0003.

-- Who
CREATE TABLE agentrt.actor (
  actor_id        text PRIMARY KEY,
  actor_kind      text NOT NULL CHECK (actor_kind IN ('host','executor','controller','owner','seat','thread')),
  tool            text CHECK (tool IN ('claude','cursor','codex','deterministic')),
  display_name    text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  retired_at      timestamptz,
  CHECK ((actor_kind = 'executor') = (tool IS NOT NULL))
);

CREATE TABLE agentrt.host (
  host_id         text PRIMARY KEY,
  actor_id        text NOT NULL UNIQUE REFERENCES agentrt.actor(actor_id),
  host_kind       text NOT NULL CHECK (host_kind IN ('dev','vm')),
  state           text NOT NULL CHECK (state IN ('active','draining','retired')),
  capacity_json   jsonb NOT NULL DEFAULT '{}',
  tools_json      jsonb NOT NULL DEFAULT '{}',
  agentd_version  text,
  last_poll_at    timestamptz,               -- liveness column: written without an event
  version         int NOT NULL DEFAULT 0
);

-- Artifacts are content addressed; the body lives apart so listings stay small.
CREATE TABLE agentrt.artifact (
  artifact_id            text PRIMARY KEY CHECK (artifact_id ~ '^sha256:[0-9a-f]{64}$'),
  artifact_kind          text NOT NULL CHECK (artifact_kind IN ('task_spec','mission_spec','report','verify','review',
                           'log_tail','checkpoint_patch','handover','brief','message','exam','other')),
  media_type             text NOT NULL,
  size_bytes             bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 1048576),
  supersedes_artifact_id text REFERENCES agentrt.artifact(artifact_id),
  task_id                text,               -- no foreign key: a task spec exists before its task
  attempt_id             text,
  created_by_actor_id    text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agentrt.artifact_blob (
  artifact_id  text PRIMARY KEY REFERENCES agentrt.artifact(artifact_id),
  body         bytea NOT NULL
);

-- Rules: append only; loosening needs a signature (v4 §7.5).
CREATE TABLE agentrt.rule (
  rule_id               text NOT NULL,
  rule_version          int  NOT NULL CHECK (rule_version >= 1),
  body_json             jsonb NOT NULL,
  body_text             text NOT NULL,
  direction             text NOT NULL CHECK (direction IN ('initial','loosen','tighten')),
  signed_by             text,
  signature             text,
  effective_at          timestamptz NOT NULL,
  expires_at            timestamptz NOT NULL,
  proposed_by_actor_id  text REFERENCES agentrt.actor(actor_id),
  evidence_json         jsonb NOT NULL DEFAULT '[]',
  PRIMARY KEY (rule_id, rule_version),
  CHECK (direction = 'tighten' OR signature IS NOT NULL),
  CHECK (expires_at > effective_at)
);

CREATE TABLE agentrt.mission (
  mission_id             text PRIMARY KEY CHECK (mission_id ~ '^M-[1-9][0-9]*$'),
  work_id                text,
  title                  text NOT NULL,
  goal                   text NOT NULL,
  success_json           jsonb NOT NULL,
  scope_json             jsonb NOT NULL,
  tenant                 text NOT NULL DEFAULT 'dev' CHECK (tenant IN ('dev','business')),
  envelope_rule_id       text,
  envelope_rule_version  int,
  state                  text NOT NULL CHECK (state IN ('proposed','awaiting_owner','active','paused','done','stopped','expired')),
  deadline_at            timestamptz NOT NULL,
  budget_json            jsonb NOT NULL,
  spec_artifact_id       text REFERENCES agentrt.artifact(artifact_id),
  created_by_actor_id    text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at             timestamptz NOT NULL DEFAULT now(),
  version                int NOT NULL DEFAULT 0,
  FOREIGN KEY (envelope_rule_id, envelope_rule_version) REFERENCES agentrt.rule(rule_id, rule_version),
  CHECK ((envelope_rule_id IS NULL) = (envelope_rule_version IS NULL))
);

-- Plan: round -> item -> step, and the architecture table (arch_root -> arch_row).
CREATE TABLE agentrt.plan_node (
  plan_node_id         text PRIMARY KEY,
  parent_plan_node_id  text REFERENCES agentrt.plan_node(plan_node_id),
  node_kind            text NOT NULL CHECK (node_kind IN ('round','item','step','arch_root','arch_row')),
  ordinal              int  NOT NULL CHECK (ordinal >= 0),
  label                text NOT NULL,
  title                text NOT NULL,
  work_id              text,
  mission_id           text REFERENCES agentrt.mission(mission_id),
  step_kind            text CHECK (step_kind IN ('agent','review','owner','approval','release','ci','gate','manual')),
  owner_step           boolean NOT NULL DEFAULT false,
  effect_step          boolean NOT NULL DEFAULT false,
  done_rule_json       jsonb NOT NULL DEFAULT '{}',
  state                text CHECK (state IN ('planned','active','done','stopped','dropped')),
  state_event_id       bigint,
  usual_s              int CHECK (usual_s > 0),
  created_by_actor_id  text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  version              int NOT NULL DEFAULT 0,
  UNIQUE (parent_plan_node_id, ordinal),
  CHECK ((node_kind IN ('round','arch_root')) = (parent_plan_node_id IS NULL)),
  CHECK ((node_kind = 'step') = (step_kind IS NOT NULL)),
  CHECK ((node_kind = 'step') = (state IS NOT NULL)),
  CHECK (node_kind IN ('step','arch_row') OR done_rule_json = '{}'::jsonb),
  CHECK (node_kind = 'step' OR mission_id IS NULL)
);
-- Roots are unique per ordinal too (UNIQUE treats NULL parents as distinct).
CREATE UNIQUE INDEX plan_node_root_ordinal ON agentrt.plan_node (node_kind, ordinal) WHERE parent_plan_node_id IS NULL;

CREATE TABLE agentrt.task (
  task_id              text PRIMARY KEY CHECK (task_id ~ '^t_[0-9A-HJKMNP-TV-Z]{26}$'),
  mission_id           text NOT NULL REFERENCES agentrt.mission(mission_id),
  plan_node_id         text REFERENCES agentrt.plan_node(plan_node_id),
  off_plan             boolean NOT NULL DEFAULT false,
  off_plan_reason      text,
  parent_task_id       text REFERENCES agentrt.task(task_id),
  subject_task_id      text REFERENCES agentrt.task(task_id),
  role                 text NOT NULL CHECK (role IN ('plan','implement','verify','review','brief','other')),
  task_class           text NOT NULL,
  core                 boolean NOT NULL DEFAULT false,
  title                text NOT NULL,
  spec_artifact_id     text NOT NULL REFERENCES agentrt.artifact(artifact_id),
  repo                 text,
  base_commit          text CHECK (base_commit ~ '^[0-9a-f]{40}$'),
  effect_class         text NOT NULL CHECK (effect_class IN ('none','branch','external')),
  needs_json           jsonb NOT NULL DEFAULT '[]',
  route_json           jsonb NOT NULL DEFAULT '[]',
  state                text NOT NULL CHECK (state IN ('proposed','awaiting_owner','ready','leased','running',
                         'submitted','verifying','accepted','rework','rejected','blocked','paused','cancelled')),
  round_no             int NOT NULL DEFAULT 1 CHECK (round_no >= 1),
  max_rounds           int NOT NULL DEFAULT 2 CHECK (max_rounds >= 1),
  usual_s              int CHECK (usual_s > 0),
  hard_limit_s         int NOT NULL CHECK (hard_limit_s > 0),
  version              int NOT NULL DEFAULT 0,
  created_by_actor_id  text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CHECK (off_plan = (plan_node_id IS NULL)),
  CHECK (off_plan = (off_plan_reason IS NOT NULL)),
  CHECK ((role IN ('verify','review')) = (subject_task_id IS NOT NULL)),
  CHECK (subject_task_id IS DISTINCT FROM task_id)
);
CREATE INDEX task_ready_idx   ON agentrt.task (created_at) WHERE state = 'ready';
CREATE INDEX task_mission_idx ON agentrt.task (mission_id, state);
CREATE INDEX task_plan_idx    ON agentrt.task (plan_node_id);
CREATE INDEX task_subject_idx ON agentrt.task (subject_task_id) WHERE subject_task_id IS NOT NULL;
CREATE UNIQUE INDEX task_one_live_plan ON agentrt.task (mission_id)
  WHERE role = 'plan' AND state IN ('leased','running');

CREATE TABLE agentrt.task_dependency (
  task_dependency_id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id              text NOT NULL REFERENCES agentrt.task(task_id),
  depends_on_task_id   text NOT NULL REFERENCES agentrt.task(task_id),
  UNIQUE (task_id, depends_on_task_id),
  CHECK (task_id <> depends_on_task_id)
);
CREATE INDEX task_dependency_on_idx ON agentrt.task_dependency (depends_on_task_id);

CREATE SEQUENCE agentrt.lease_epoch_seq;

CREATE TABLE agentrt.attempt (
  attempt_id           text PRIMARY KEY CHECK (attempt_id ~ '^a_[0-9A-HJKMNP-TV-Z]{26}$'),
  task_id              text NOT NULL REFERENCES agentrt.task(task_id),
  attempt_no           int  NOT NULL CHECK (attempt_no >= 1),
  executor_actor_id    text NOT NULL REFERENCES agentrt.actor(actor_id),
  host_id              text REFERENCES agentrt.host(host_id),
  model                text,
  model_provider       text,
  lease_epoch          bigint NOT NULL,
  lease_expires_at     timestamptz NOT NULL,   -- liveness column, forward only
  heartbeat_at         timestamptz NOT NULL,   -- liveness column
  computed_at          timestamptz,
  persisted_at         timestamptz,
  state                text NOT NULL CHECK (state IN ('leased','running','stale','submitted','finished','lost','cancelled','failed')),
  started_at           timestamptz,
  ended_at             timestamptz,
  end_how              text CHECK (end_how IN ('exit','hard_limit','over_capacity','cancelled','daemon_restart','host_reboot','lease_lost')),
  exit_code            int,
  conclusion           text CHECK (conclusion IN ('done','failed','none')),
  report_artifact_id   text REFERENCES agentrt.artifact(artifact_id),
  pushed_ref           text CHECK (pushed_ref ~ '^refs/heads/agent/'),
  pushed_commit        text CHECK (pushed_commit ~ '^[0-9a-f]{40}$'),
  UNIQUE (task_id, attempt_no),
  CHECK ((pushed_ref IS NULL) = (pushed_commit IS NULL))
);
CREATE UNIQUE INDEX attempt_one_live ON agentrt.attempt (task_id) WHERE state IN ('leased','running','stale');
CREATE INDEX attempt_lease_idx ON agentrt.attempt (lease_expires_at) WHERE state IN ('leased','running','stale');

-- Events: append only, hash chained per stream.
CREATE TABLE agentrt.event_stream (
  event_stream_id   text PRIMARY KEY,
  last_seq          bigint NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
  last_sha256       text   NOT NULL DEFAULT '' CHECK (last_sha256 = '' OR last_sha256 ~ '^[0-9a-f]{64}$'),
  created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agentrt.event (
  event_id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_stream_id   text   NOT NULL REFERENCES agentrt.event_stream(event_stream_id),
  seq               bigint NOT NULL CHECK (seq >= 1),
  event_type        text   NOT NULL,
  -- Checked at commit: an event is written before the row it creates.
  actor_id          text   NOT NULL REFERENCES agentrt.actor(actor_id) DEFERRABLE INITIALLY DEFERRED,
  mission_id        text   REFERENCES agentrt.mission(mission_id) DEFERRABLE INITIALLY DEFERRED,
  plan_node_id      text   REFERENCES agentrt.plan_node(plan_node_id) DEFERRABLE INITIALLY DEFERRED,
  task_id           text   REFERENCES agentrt.task(task_id) DEFERRABLE INITIALLY DEFERRED,
  attempt_id        text   REFERENCES agentrt.attempt(attempt_id) DEFERRABLE INITIALLY DEFERRED,
  cause_event_id    bigint REFERENCES agentrt.event(event_id),
  body_json         jsonb  NOT NULL CHECK (jsonb_typeof(body_json) = 'object'),
  prev_sha256       text   NOT NULL CHECK (prev_sha256 = '' OR prev_sha256 ~ '^[0-9a-f]{64}$'),
  sha256            text   NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  txid              bigint NOT NULL DEFAULT txid_current(),
  at                timestamptz NOT NULL,
  UNIQUE (event_stream_id, seq),
  CHECK ((seq = 1) = (prev_sha256 = ''))
);
CREATE INDEX event_task_idx    ON agentrt.event (task_id, event_id) WHERE task_id IS NOT NULL;
CREATE INDEX event_mission_idx ON agentrt.event (mission_id, event_id) WHERE mission_id IS NOT NULL;
CREATE INDEX event_plan_idx    ON agentrt.event (plan_node_id, event_id) WHERE plan_node_id IS NOT NULL;
CREATE INDEX event_type_at_idx ON agentrt.event (event_type, at);
CREATE INDEX event_txid_idx    ON agentrt.event (txid);

ALTER TABLE agentrt.plan_node
  ADD FOREIGN KEY (state_event_id) REFERENCES agentrt.event(event_id);

CREATE TABLE agentrt.plan_node_binding (
  plan_node_binding_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  plan_node_id         text NOT NULL REFERENCES agentrt.plan_node(plan_node_id),
  bound_kind           text NOT NULL CHECK (bound_kind IN ('approval','question')),
  bound_ref            text NOT NULL,
  event_id             bigint NOT NULL REFERENCES agentrt.event(event_id),
  UNIQUE (plan_node_id, bound_kind, bound_ref)
);

CREATE TABLE agentrt.outbox (
  outbox_id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id      bigint NOT NULL REFERENCES agentrt.event(event_id),
  topic         text   NOT NULL CHECK (topic IN ('wake','stop','cooldown','events') OR topic ~ '^mbox:.+$'),
  payload_json  jsonb  NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  delivered_at  timestamptz,
  tries         int NOT NULL DEFAULT 0 CHECK (tries >= 0),
  UNIQUE (event_id, topic)
);
CREATE INDEX outbox_pending_idx ON agentrt.outbox (outbox_id) WHERE delivered_at IS NULL;

CREATE TABLE agentrt.message (
  message_id        text PRIMARY KEY CHECK (message_id ~ '^m_[0-9A-HJKMNP-TV-Z]{26}$'),
  from_actor_id     text NOT NULL REFERENCES agentrt.actor(actor_id),
  to_actor_id       text NOT NULL REFERENCES agentrt.actor(actor_id),
  mission_id        text REFERENCES agentrt.mission(mission_id),
  task_id           text REFERENCES agentrt.task(task_id),
  message_kind      text NOT NULL CHECK (message_kind IN ('ask','answer','help','handover','note','wake')),
  body_artifact_id  text NOT NULL REFERENCES agentrt.artifact(artifact_id),
  event_id          bigint NOT NULL REFERENCES agentrt.event(event_id),
  created_at        timestamptz NOT NULL DEFAULT now(),
  acked_at          timestamptz
);
CREATE INDEX message_unacked_idx ON agentrt.message (to_actor_id, created_at) WHERE acked_at IS NULL;

CREATE TABLE agentrt.verification (
  verification_id      text PRIMARY KEY CHECK (verification_id ~ '^v_[0-9A-HJKMNP-TV-Z]{26}$'),
  task_id              text NOT NULL REFERENCES agentrt.task(task_id),
  attempt_id           text NOT NULL REFERENCES agentrt.attempt(attempt_id),
  method               text NOT NULL CHECK (method IN ('deterministic','review','gate')),
  check_ref            text,
  env                  text CHECK (env IN ('dev','stg','prod')),
  verifier_attempt_id  text REFERENCES agentrt.attempt(attempt_id),
  verdict              text NOT NULL CHECK (verdict IN ('pass','fail','none')),
  evidence_artifact_id text NOT NULL REFERENCES agentrt.artifact(artifact_id),
  subject_commit       text CHECK (subject_commit ~ '^[0-9a-f]{40}$'),
  deploy_ref_json      jsonb,
  spec_sha256          text CHECK (spec_sha256 ~ '^[0-9a-f]{64}$'),
  accepts_attempt      boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CHECK (method <> 'gate' OR (check_ref IS NOT NULL AND subject_commit IS NOT NULL AND env IS NOT NULL
                              AND deploy_ref_json IS NOT NULL AND spec_sha256 IS NOT NULL)),
  CHECK (NOT accepts_attempt OR verdict = 'pass')
);
CREATE UNIQUE INDEX verification_one_accept ON agentrt.verification (task_id) WHERE accepts_attempt;
CREATE INDEX verification_gate_idx ON agentrt.verification (env, check_ref, subject_commit) WHERE method = 'gate';

CREATE TABLE agentrt.control (
  control_id           text PRIMARY KEY,
  scope                text NOT NULL CHECK (scope IN ('global','mission','task','host')),
  scope_ref            text,
  control_kind         text NOT NULL CHECK (control_kind IN ('stop','pause','drain')),
  reason               text NOT NULL,
  set_by_actor_id      text NOT NULL REFERENCES agentrt.actor(actor_id),
  set_at               timestamptz NOT NULL DEFAULT now(),
  cleared_at           timestamptz,
  cleared_by_actor_id  text REFERENCES agentrt.actor(actor_id),
  CHECK ((scope = 'global') = (scope_ref IS NULL)),
  CHECK ((cleared_at IS NULL) = (cleared_by_actor_id IS NULL))
);
CREATE INDEX control_active_idx ON agentrt.control (scope, scope_ref) WHERE cleared_at IS NULL;

CREATE TABLE agentrt.idempotency_key (
  idempotency_key_id text PRIMARY KEY,
  route              text NOT NULL,
  request_sha256     text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
  response_json      jsonb NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz NOT NULL,
  CHECK (expires_at > created_at)
);
CREATE INDEX idempotency_key_expires_idx ON agentrt.idempotency_key (expires_at);

-- Capability and quota: capability first, quota second (Owner 2026-10-13).
CREATE TABLE agentrt.job (
  job_id        text PRIMARY KEY,
  tenant        text NOT NULL CHECK (tenant IN ('dev','business')),
  rule_id       text NOT NULL,
  rule_version  int  NOT NULL,
  is_locked     boolean NOT NULL DEFAULT false,
  FOREIGN KEY (rule_id, rule_version) REFERENCES agentrt.rule(rule_id, rule_version)
);

CREATE TABLE agentrt.job_candidate (
  job_candidate_id   text PRIMARY KEY,
  job_id             text NOT NULL REFERENCES agentrt.job(job_id),
  executor_actor_id  text NOT NULL REFERENCES agentrt.actor(actor_id),
  model_provider     text NOT NULL,
  model_pattern      text NOT NULL DEFAULT '*',
  status             text NOT NULL CHECK (status IN ('qualifying','active','probation','demoted','retired')),
  rule_id            text,
  rule_version       int,
  qualified_at       timestamptz,
  exam_artifact_id   text REFERENCES agentrt.artifact(artifact_id),
  adapters_json      jsonb NOT NULL DEFAULT '[]',
  version            int NOT NULL DEFAULT 0,
  UNIQUE (job_id, executor_actor_id, model_provider, model_pattern),
  FOREIGN KEY (rule_id, rule_version) REFERENCES agentrt.rule(rule_id, rule_version)
);

CREATE TABLE agentrt.capability_fact (
  capability_fact_id text PRIMARY KEY,
  executor_actor_id  text NOT NULL REFERENCES agentrt.actor(actor_id),
  model_provider     text,
  fact_kind          text NOT NULL CHECK (fact_kind IN ('cannot','must_not','only','prefer')),
  subject            text NOT NULL,
  scope_json         jsonb NOT NULL DEFAULT '{}',
  source_kind        text NOT NULL CHECK (source_kind IN ('measured','owner')),
  source             text NOT NULL,
  rule_id            text,
  rule_version       int,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  retired_at         timestamptz,
  FOREIGN KEY (rule_id, rule_version) REFERENCES agentrt.rule(rule_id, rule_version)
);

CREATE TABLE agentrt.capability_score (
  capability_score_id     text PRIMARY KEY,
  executor_actor_id       text NOT NULL REFERENCES agentrt.actor(actor_id),
  model_provider          text NOT NULL,
  model                   text NOT NULL,
  tool_version            text NOT NULL,
  job_id                  text NOT NULL REFERENCES agentrt.job(job_id),
  task_class              text NOT NULL,
  core                    boolean NOT NULL,
  source                  text NOT NULL CHECK (source IN ('exam','benchmark','in_job')),
  window_start            timestamptz NOT NULL,
  window_end              timestamptz NOT NULL,
  attempts                int NOT NULL CHECK (attempts >= 0),
  first_pass              int NOT NULL CHECK (first_pass >= 0),
  accepted                int NOT NULL CHECK (accepted >= 0),
  rework                  int NOT NULL CHECK (rework >= 0),
  review_misses           int NOT NULL DEFAULT 0,
  review_false_alarms     int NOT NULL DEFAULT 0,
  median_wall_s           int,
  score                   numeric NOT NULL CHECK (score BETWEEN 0 AND 1),
  computed_from_event_id  bigint NOT NULL REFERENCES agentrt.event(event_id),
  computed_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (executor_actor_id, model, tool_version, job_id, task_class, core, source, window_end),
  CHECK (window_end > window_start),
  CHECK (first_pass <= attempts AND accepted <= attempts)
);

CREATE TABLE agentrt.vendor_pool (
  vendor_pool_id   text PRIMARY KEY,
  tool             text NOT NULL,
  window_kind      text NOT NULL CHECK (window_kind IN ('short','weekly','monthly')),
  resets_at        timestamptz,
  used_fraction    numeric CHECK (used_fraction BETWEEN 0 AND 1),
  measured_at      timestamptz,
  measure_source   text CHECK (measure_source IN ('reading','exit_text','owner')),
  cooldown_until   timestamptz,
  version          int NOT NULL DEFAULT 0
);

CREATE TABLE agentrt.budget_ledger (
  budget_ledger_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  attempt_id       text REFERENCES agentrt.attempt(attempt_id),
  vendor_pool_id   text REFERENCES agentrt.vendor_pool(vendor_pool_id),
  unit             text NOT NULL CHECK (unit IN ('wall_s','turns','tokens','usd','gpu_s')),
  amount           numeric NOT NULL CHECK (amount >= 0),
  measured         boolean NOT NULL,
  at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX budget_ledger_attempt_idx ON agentrt.budget_ledger (attempt_id);
