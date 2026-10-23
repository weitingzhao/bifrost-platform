-- agentrt 0009: business issues, the data of business intake (W-31 BI-1
-- slice 1; bifrost-trade-infra agent-config/work/crew/
-- DESIGN-crew-intake-2026-10-22.md §4, §5.2, §5.3, §7.2; the Owner chose
-- I-D1…I-D8 A and X, Y A on 2026-10-23). Seven tables: who is served
-- (person), the external agents that bring issues (intake_client), the issue
-- itself, all of its text (issue_text: kept out of artifacts, so the generic
-- artifact and event reads never reach a person's words), the asks to the
-- person served and their answers (issue_ask), the evidence the gates read
-- (issue_check) and the work taken for it (issue_work). The history of an
-- issue is its event stream issue:BI-n; an event holds ids, enums and hashes,
-- never text.
--
-- Guards (the 0002 way): every write of a state table (person,
-- intake_client, issue, issue_ask, issue_work) carries an event of its
-- transaction, of a type allowed for the table, naming the row; issue_text
-- and issue_check are append only and written only with an event naming
-- them. The one-time import (legacy_imported) writes none of the seven
-- tables (§4.4: an import goes through the ordinary issue events).
-- only_liveness lets an intake client's served cursor move forward without
-- an event (R2-A5). A mission that ends ends its issue work in the same
-- statement, whichever binary moved it (R2-A6); a mission whose work id
-- starts BI- commits only with the issue work take_issue wrote for it (§4.4).
-- Three 0002 functions are replaced with the same signatures, their old
-- branches word for word, so the 0002 triggers that call them stay as they
-- are and keep their owner and grants: only_liveness, event_may_write,
-- event_names_row.
--
-- Upgrading: run this before the platform that writes issues (an older
-- migrator refuses a database that has it). An older platform still starts
-- (it reports the schema ahead) and keeps working: it writes no issue, the
-- vocabulary only grows, its mission moves end issue work through the same
-- trigger, and a POST /missions with a BI- work id fails at commit. The
-- ALTERs take strong locks on actor and event while they check the rows.

-- Who: an external agent is an actor of its own kind (§5.2); its tool stays
-- NULL (only an executor has one).
ALTER TABLE agentrt.actor DROP CONSTRAINT actor_actor_kind_check;
ALTER TABLE agentrt.actor ADD CONSTRAINT actor_actor_kind_check CHECK (actor_kind IN (
  'host','executor','controller','owner','seat','thread','agent'));

-- The closed vocabulary (0006's list and the issue types, §4.3).
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
  'note','wait_limit_passed','legacy_imported'));

-- Whose words, and whom an issue serves. Not a login: the name stays in the
-- database, never in a repository.
CREATE TABLE agentrt.person (
  person_id     text PRIMARY KEY CHECK (person_id ~ '^person:[a-z0-9-]{1,32}$'),
  display_name  text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
  created_at    timestamptz NOT NULL DEFAULT now(),
  retired_at    timestamptz
);

-- One external agent: the token it holds (intake:<name>), the one person it
-- may speak for (NULL: only its own proposals), its products and daily
-- limits, how far it was served (a liveness pair, forward only, no event)
-- and how far it acknowledged (with intake_acked, forward only, never past
-- served).
CREATE TABLE agentrt.intake_client (
  intake_client_id text PRIMARY KEY REFERENCES agentrt.actor(actor_id) CHECK (intake_client_id ~ '^agent:[a-z0-9-]{1,32}$'),
  token_name       text NOT NULL UNIQUE CHECK (token_name = 'intake:' || substr(intake_client_id, 7)),
  on_behalf_of     text REFERENCES agentrt.person(person_id),
  products         text[] NOT NULL CHECK (cardinality(products) >= 1),
  daily_submits    int NOT NULL DEFAULT 10 CHECK (daily_submits >= 0),
  daily_adds       int NOT NULL DEFAULT 20 CHECK (daily_adds >= 0),
  daily_proposals  int NOT NULL DEFAULT 3 CHECK (daily_proposals >= 0),
  served_txid      bigint,
  served_event     bigint,
  acked_txid       bigint,
  acked_event      bigint,
  acked_at         timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  revoked_at       timestamptz,
  CHECK ((served_txid IS NULL) = (served_event IS NULL)),
  CHECK ((acked_txid IS NULL) = (acked_event IS NULL) AND (acked_txid IS NULL) = (acked_at IS NULL)),
  CHECK (acked_txid IS NULL OR (served_txid IS NOT NULL AND (acked_txid, acked_event) <= (served_txid, served_event)))
);

-- The issue. Its state only says where it is and whom it waits on; whether
-- it may be taken or closed is computed from the evidence of its current
-- brief and design (§4.3). product is an opaque key the platform compares
-- and never interprets. dedupe_key is the submitting agent's own key for
-- "the same thing again" (§2.1), compared within one person's issues.
CREATE TABLE agentrt.issue (
  issue_id            text PRIMARY KEY CHECK (issue_id ~ '^BI-[1-9][0-9]*$'),
  product             text NOT NULL CHECK (product ~ '^[a-z][a-z0-9-]{0,31}$'),
  kind                text NOT NULL CHECK (kind IN ('complaint','question','request','proposal')),
  title               text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 80),
  served_person       text REFERENCES agentrt.person(person_id),
  asks_person         text NOT NULL REFERENCES agentrt.person(person_id),
  state               text NOT NULL CHECK (state IN ('clarifying','brief_confirmed','classified','designing','in_review',
                        'owner_deciding','active','awaiting_user','parked','closed')),
  waiting_on          text NOT NULL CHECK (waiting_on IN ('crew','user','owner')),
  conclusion          text CHECK (conclusion IN ('resolved','partial','already_possible','declined','merged')),
  priority            text CHECK (priority IN ('p1','p2','p3','later')),
  risk_class          text CHECK (risk_class IN ('small','medium','large')),
  brief_text_id       bigint,
  brief_confirmed_ask bigint,
  design_text_id      bigint,
  business_owner      text REFERENCES agentrt.actor(actor_id),
  build_owner         text REFERENCES agentrt.actor(actor_id),
  duplicate_of        text REFERENCES agentrt.issue(issue_id),
  dedupe_key          text CHECK (char_length(dedupe_key) BETWEEN 1 AND 200),
  next_step           text,
  next_by             text,
  next_due_at         timestamptz,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  version             int NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK ((state = 'closed') = (conclusion IS NOT NULL)),
  -- IS NOT DISTINCT FROM: a NULL conclusion must not let duplicate_of stand.
  CHECK ((conclusion IS NOT DISTINCT FROM 'merged') = (duplicate_of IS NOT NULL)),
  CHECK (duplicate_of IS DISTINCT FROM issue_id),
  CHECK (asks_person = COALESCE(served_person, 'person:owner')),
  CHECK (kind <> 'proposal' OR served_person IS NULL),
  -- An agent's own (self) issue is a proposal or a question (§2.1).
  CHECK (served_person IS NOT NULL OR kind IN ('proposal','question'))
);
CREATE INDEX issue_product_idx ON agentrt.issue (product, state);
CREATE INDEX issue_served_idx  ON agentrt.issue (served_person, product) WHERE served_person IS NOT NULL;

-- Every text of an issue: the person's words, the agent's reading and
-- proposal, replies, briefs, designs, review reports, answers, the seat's
-- check, crew notes. Append only. sha256 is the body's, checked on insert.
-- said_where is where words were said (a page or a feature, §2.1).
-- to_requester marks a text the submitting side may read: only a reply or a
-- brief put to the person; nothing internal ever is.
CREATE TABLE agentrt.issue_text (
  issue_text_id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  issue_id            text NOT NULL REFERENCES agentrt.issue(issue_id) DEFERRABLE INITIALLY DEFERRED,
  text_kind           text NOT NULL CHECK (text_kind IN ('words','reading','proposal','reply','brief','design','review','response','check','note')),
  person              text REFERENCES agentrt.person(person_id),
  channel             text NOT NULL CHECK (channel IN ('intake_local','chrome','public','thread','owner_relay','console','card')),
  said_at             timestamptz,
  said_where          text CHECK (char_length(said_where) BETWEEN 1 AND 200),
  body                text NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 1048576),
  sha256              text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  to_requester        boolean NOT NULL DEFAULT false,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  CHECK ((text_kind IN ('words','response')) = (person IS NOT NULL)),
  CHECK (said_where IS NULL OR text_kind = 'words'),
  CHECK (NOT to_requester OR text_kind IN ('reply','brief')),
  UNIQUE (issue_text_id, issue_id),
  UNIQUE (issue_text_id, issue_id, person),
  UNIQUE (issue_text_id, issue_id, sha256)
);
CREATE INDEX issue_text_issue_idx ON agentrt.issue_text (issue_id, issue_text_id);

-- Work taken for an issue: only take_issue writes a row (issue_taken), for
-- the brief current then (brief_sha256, R2-A4); ended_at is written only by
-- the mission's terminal trigger below (R2-A6).
CREATE TABLE agentrt.issue_work (
  issue_work_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  issue_id      text NOT NULL REFERENCES agentrt.issue(issue_id),
  commitment    int NOT NULL CHECK (commitment >= 1),
  mission_id    text NOT NULL REFERENCES agentrt.mission(mission_id),
  brief_sha256  text NOT NULL CHECK (brief_sha256 ~ '^[0-9a-f]{64}$'),
  created_at    timestamptz NOT NULL DEFAULT now(),
  ended_at      timestamptz,
  UNIQUE (issue_id, commitment, mission_id),
  UNIQUE (issue_id, commitment, mission_id, brief_sha256)
);
-- First batch (B2): one live work per issue, whatever the commitment.
CREATE UNIQUE INDEX issue_work_one_live ON agentrt.issue_work (issue_id) WHERE ended_at IS NULL;
CREATE INDEX issue_work_live_by_mission ON agentrt.issue_work (mission_id) WHERE ended_at IS NULL;

-- One ask to the person asked and her answer, in one row (A5): what is
-- asked (bound_text_id, and its sha256), the brief current when it was asked
-- (NULL only for a needs_info before any brief), for a trial the delivery,
-- and the answer, which is that person's words (words_text_id). Answered
-- once; superseded when the brief changes, the issue is reopened, or a new
-- ask of its purpose replaces it.
CREATE TABLE agentrt.issue_ask (
  issue_ask_id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  issue_id             text NOT NULL REFERENCES agentrt.issue(issue_id),
  person               text NOT NULL REFERENCES agentrt.person(person_id),
  purpose              text NOT NULL CHECK (purpose IN ('confirm_brief','needs_info','ready_to_try','check_answer')),
  bound_text_id        bigint NOT NULL,
  bound_sha256         text NOT NULL CHECK (bound_sha256 ~ '^[0-9a-f]{64}$'),
  brief_sha256         text CHECK (brief_sha256 ~ '^[0-9a-f]{64}$'),
  commitment           int CHECK (commitment >= 1),
  mission_id           text,
  env                  text CHECK (env IN ('dev','stg','prod')),
  commits              text[],
  verdict              text,
  words_text_id        bigint,
  said_at              timestamptz,
  answered_by_actor_id text REFERENCES agentrt.actor(actor_id),
  answer_channel       text CHECK (answer_channel IN ('intake_local','chrome','public','thread','owner_relay','console','card')),
  answered_at          timestamptz,
  superseded_at        timestamptz,
  FOREIGN KEY (bound_text_id, issue_id, bound_sha256) REFERENCES agentrt.issue_text(issue_text_id, issue_id, sha256),
  FOREIGN KEY (words_text_id, issue_id, person) REFERENCES agentrt.issue_text(issue_text_id, issue_id, person),
  FOREIGN KEY (issue_id, commitment, mission_id, brief_sha256)
    REFERENCES agentrt.issue_work(issue_id, commitment, mission_id, brief_sha256),
  CHECK ((purpose = 'ready_to_try') = (mission_id IS NOT NULL)
         AND (mission_id IS NULL) = (commitment IS NULL) AND (mission_id IS NULL) = (env IS NULL)),
  CHECK (purpose = 'ready_to_try' OR commits IS NULL),
  CHECK (purpose = 'needs_info' OR brief_sha256 IS NOT NULL),
  CHECK (purpose <> 'confirm_brief' OR brief_sha256 = bound_sha256),
  CHECK ((verdict IS NULL) = (answered_at IS NULL) AND (verdict IS NULL) = (words_text_id IS NULL)
         AND (verdict IS NULL) = (answered_by_actor_id IS NULL) AND (verdict IS NULL) = (answer_channel IS NULL)),
  CHECK (verdict IS NULL OR verdict = 'withdrawn' OR (purpose, verdict) IN (
    ('confirm_brief','confirmed'),('confirm_brief','rejected'),('needs_info','answered'),
    ('ready_to_try','confirmed'),('ready_to_try','partly'),('ready_to_try','rejected'),
    ('check_answer','confirmed'),('check_answer','rejected'))),
  UNIQUE (issue_ask_id, issue_id)
);
CREATE UNIQUE INDEX issue_ask_one_open ON agentrt.issue_ask (issue_id, purpose) WHERE verdict IS NULL AND superseded_at IS NULL;
CREATE INDEX issue_ask_issue_idx ON agentrt.issue_ask (issue_id, issue_ask_id);

-- The evidence the gates read, each row bound to the brief (and design)
-- current when it was recorded (A6). Append only: for each kind the newest
-- row (largest issue_check_id) is what counts, never "there was once one".
-- An Owner's decision is his: written by owner (his admin token), or by the
-- runtime (controller) from a card answered not by a rule.
CREATE TABLE agentrt.issue_check (
  issue_check_id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  issue_id            text NOT NULL REFERENCES agentrt.issue(issue_id),
  check_kind          text NOT NULL CHECK (check_kind IN ('classification','priority','review','owner_decision','close_check')),
  brief_sha256        text NOT NULL CHECK (brief_sha256 ~ '^[0-9a-f]{64}$'),
  design_sha256       text CHECK (design_sha256 ~ '^[0-9a-f]{64}$'),
  result              text NOT NULL,
  category            text,
  report_text_id      bigint,
  seat_artifact_id    text REFERENCES agentrt.artifact(artifact_id),
  reviewer_tool       text,
  author_tool         text,
  review_round        int CHECK (review_round >= 1),
  question_id         text,
  answered_by         text,
  answer_channel      text,
  ask_id              bigint,
  rule_id             text,
  rule_version        int,
  created_by_actor_id text NOT NULL REFERENCES agentrt.actor(actor_id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (report_text_id, issue_id) REFERENCES agentrt.issue_text(issue_text_id, issue_id),
  FOREIGN KEY (ask_id, issue_id) REFERENCES agentrt.issue_ask(issue_ask_id, issue_id),
  CHECK (check_kind <> 'classification' OR (result IN ('small','medium','large') AND seat_artifact_id IS NOT NULL
         AND category IN ('guidance','improvement','method_change','data_not_shown','fault','out_of_scope'))),
  CHECK (check_kind <> 'priority' OR result IN ('p1','p2','p3','later','declined')),
  CHECK (check_kind <> 'review' OR (reviewer_tool IS NOT NULL AND author_tool IS NOT NULL AND reviewer_tool <> author_tool
         AND report_text_id IS NOT NULL AND design_sha256 IS NOT NULL AND result IN ('pass','fix') AND review_round IS NOT NULL)),
  CHECK (check_kind <> 'owner_decision' OR (design_sha256 IS NOT NULL
         AND ((category = 'review_dispute' AND result IN ('proceed','stop')) OR (category ~ '^D[1-9][0-9]*$' AND result ~ '^[A-Z]$')))),
  CHECK (check_kind NOT IN ('priority','owner_decision')
         OR (created_by_actor_id = 'owner' AND question_id IS NULL AND answered_by IS NULL AND answer_channel IS NULL)
         OR (created_by_actor_id = 'controller' AND question_id IS NOT NULL AND answered_by IS NOT NULL
             AND answer_channel IS NOT NULL AND answer_channel <> 'rule')),
  CHECK (check_kind <> 'close_check' OR (ask_id IS NOT NULL AND seat_artifact_id IS NOT NULL AND result IN ('supports','does_not'))),
  CHECK (category IS NULL OR check_kind IN ('classification','owner_decision'))
);
CREATE INDEX issue_check_latest_idx ON agentrt.issue_check (issue_id, check_kind, issue_check_id);

-- Links that point back at rows created after the issue: checked at commit.
ALTER TABLE agentrt.issue
  ADD FOREIGN KEY (brief_text_id, issue_id) REFERENCES agentrt.issue_text(issue_text_id, issue_id) DEFERRABLE INITIALLY DEFERRED,
  ADD FOREIGN KEY (design_text_id, issue_id) REFERENCES agentrt.issue_text(issue_text_id, issue_id) DEFERRABLE INITIALLY DEFERRED,
  ADD FOREIGN KEY (brief_confirmed_ask, issue_id) REFERENCES agentrt.issue_ask(issue_ask_id, issue_id) DEFERRABLE INITIALLY DEFERRED;

-- Liveness (0002, same signature): attempt and host as before, word for
-- word; an intake client's served cursor, alone, moves without an event,
-- forward only (R2-A5). With anything else in the same statement it is not
-- liveness and needs an event as usual (guard_intake_client then holds the
-- cursor to forward as well).
CREATE OR REPLACE FUNCTION agentrt.only_liveness(tbl text, o jsonb, n jsonb) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE live text[];
BEGIN
  IF tbl = 'attempt' THEN
    live := ARRAY['heartbeat_at','lease_expires_at'];
  ELSIF tbl = 'host' THEN
    live := ARRAY['last_poll_at'];
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

-- Which event types may write which state table (0002, same signature): the
-- old tables as before, word for word, and the import for them only; the
-- issue tables only by the issue events (§4.2, §4.6). The mission's terminal
-- trigger ends issue work under its mission_state_changed (R2-A6).
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported'
          AND tbl NOT IN ('person','intake_client','issue','issue_text','issue_ask','issue_work','issue_check')) OR CASE tbl
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
    ELSE false
  END
$$;

-- Rows the envelope keys cannot name are named in the body (0002, same
-- signature, its branches word for word). An issue by its issue_id, a text,
-- an ask or a check by its id (the store takes the id from the table's
-- sequence before the event, so the event can name it), a person and a
-- client by theirs. Issue work is named by the envelope's mission_id, which
-- require_event checks against the row (§4.6).
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

-- The sha256 of an issue text (an issue's current brief or design B, D;
-- NULL when there is none).
CREATE FUNCTION agentrt.issue_text_sha(text_id bigint) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT sha256 FROM agentrt.issue_text WHERE issue_text_id = text_id
$$;

-- Who writes which issue event (§1, §2): an external agent (agent:) and the
-- crew's threads and the Owner submit, add and answer; the Owner, or the
-- runtime from his card, decides priority and the design's points; the
-- runtime records an answer the Owner gave on a card; only the Owner sets up
-- people and clients; a client acknowledges its own updates; every other
-- issue event is the crew's (a thread) or the Owner's.
CREATE FUNCTION agentrt.issue_writer_ok(event_type text, actor text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN event_type IN ('issue_submitted','issue_added') THEN actor LIKE 'agent:%' OR actor LIKE 'thread:%' OR actor = 'owner'
    WHEN event_type = 'issue_answered' THEN actor LIKE 'agent:%' OR actor LIKE 'thread:%' OR actor IN ('owner','controller')
    WHEN event_type IN ('issue_priority_set','issue_owner_decided') THEN actor IN ('owner','controller')
    WHEN event_type = 'intake_client_set' THEN actor = 'owner'
    WHEN event_type = 'intake_acked' THEN actor LIKE 'agent:%'
    WHEN event_type LIKE 'issue\_%' THEN actor LIKE 'thread:%' OR actor = 'owner'
    ELSE false
  END
$$;

-- Whether actor may write a text (or answer) on channel, and for person when
-- the text is someone's words: an agent only on its own channels, for the
-- person it is bound to, while not revoked; a thread as itself (the Owner's
-- words) or relaying the Owner's relay of someone else's; the Owner on the
-- Console; the runtime only what he chose on a card (§1, §5.4).
CREATE FUNCTION agentrt.intake_voice_ok(actor text, channel text, person text) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT CASE
    WHEN actor LIKE 'agent:%' THEN channel IN ('intake_local','chrome','public')
      AND EXISTS (SELECT 1 FROM agentrt.intake_client c WHERE c.intake_client_id = actor AND c.revoked_at IS NULL
                    AND (person IS NULL OR c.on_behalf_of IS NOT DISTINCT FROM person))
    WHEN actor LIKE 'thread:%' THEN channel = 'owner_relay' OR (channel = 'thread' AND (person IS NULL OR person = 'person:owner'))
    WHEN actor = 'owner' THEN channel = 'console' AND (person IS NULL OR person = 'person:owner')
    WHEN actor = 'controller' THEN channel = 'card' AND (person IS NULL OR person = 'person:owner')
    ELSE false
  END
$$;

-- The issue event of this transaction, on the issue's own stream, naming the
-- issue in its body, written by a writer allowed for its type.
CREATE FUNCTION agentrt.issue_event(issue text) RETURNS agentrt.event LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.current_event();
BEGIN
  IF NOT agentrt.issue_writer_ok(ev.event_type, ev.actor_id) THEN
    RAISE EXCEPTION 'agentrt: % does not write a % event', ev.actor_id, ev.event_type USING ERRCODE = 'check_violation';
  END IF;
  IF ev.event_stream_id IS DISTINCT FROM 'issue:' || issue OR ev.body_json->>'issue_id' IS DISTINCT FROM issue THEN
    RAISE EXCEPTION 'agentrt: event % (%) on % is not about issue %', ev.event_id, ev.event_type, ev.event_stream_id, issue
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN ev;
END $$;

-- Issues: created clarifying by issue_submitted; an agent only for its own
-- person and products; product, kind, title, people and creator never
-- change; the version moves by one with every write; the risk class only
-- rises (§4.2); the brief changes only with issue_brief_drafted, never while
-- work is live or a trial or an answer is asked, and clears its
-- confirmation; the confirmation names an ask that confirmed this very brief;
-- active is started only by issue_taken (or back from awaiting_user by the
-- answer to its trial); nothing is parked while its work is live (R2-A3);
-- only a merge sets duplicate_of, once, within one person's issues of one
-- product, neither ever taken (A7, B2).
CREATE FUNCTION agentrt.guard_issue() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev    agentrt.event := agentrt.issue_event(NEW.issue_id);
  c     agentrt.intake_client;
  tgt   agentrt.issue;
  a     agentrt.issue_ask;
  ranks text[] := ARRAY['small','medium','large'];
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF ev.event_type <> 'issue_submitted' THEN
      RAISE EXCEPTION 'agentrt: an issue is created by issue_submitted, not %', ev.event_type USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.state <> 'clarifying' OR NEW.waiting_on <> 'crew' OR NEW.version <> 0 OR NEW.conclusion IS NOT NULL
       OR NEW.priority IS NOT NULL OR NEW.risk_class IS NOT NULL OR NEW.brief_text_id IS NOT NULL
       OR NEW.brief_confirmed_ask IS NOT NULL OR NEW.design_text_id IS NOT NULL OR NEW.duplicate_of IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: a new issue starts clarifying, waiting on the crew, at version 0, with nothing decided'
        USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.created_by_actor_id <> ev.actor_id THEN
      RAISE EXCEPTION 'agentrt: issue % is created by the writer of its event', NEW.issue_id USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.created_by_actor_id LIKE 'agent:%' THEN
      SELECT * INTO c FROM agentrt.intake_client WHERE intake_client_id = NEW.created_by_actor_id;
      IF NOT FOUND OR c.revoked_at IS NOT NULL OR NOT (NEW.product = ANY (c.products))
         OR (NEW.served_person IS NOT NULL AND NEW.served_person IS DISTINCT FROM c.on_behalf_of) THEN
        RAISE EXCEPTION 'agentrt: % submits only for the person it is bound to, in its products, while not revoked',
          NEW.created_by_actor_id USING ERRCODE = 'check_violation';
      END IF;
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'agentrt: issue % moves from version % to %; one at a time', OLD.issue_id, OLD.version, NEW.version
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.product <> OLD.product OR NEW.kind <> OLD.kind OR NEW.title <> OLD.title
     OR NEW.served_person IS DISTINCT FROM OLD.served_person OR NEW.asks_person <> OLD.asks_person
     OR NEW.created_by_actor_id <> OLD.created_by_actor_id OR NEW.created_at <> OLD.created_at
     OR NEW.dedupe_key IS DISTINCT FROM OLD.dedupe_key THEN
    RAISE EXCEPTION 'agentrt: issue % keeps its product, kind, title, people, creator and dedupe key', OLD.issue_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.risk_class IS DISTINCT FROM OLD.risk_class THEN
    IF NEW.risk_class IS NULL OR array_position(ranks, NEW.risk_class) < COALESCE(array_position(ranks, OLD.risk_class), 0) THEN
      RAISE EXCEPTION 'agentrt: the risk class of % only rises (% to %)', OLD.issue_id, OLD.risk_class, NEW.risk_class
        USING ERRCODE = 'check_violation';
    END IF;
    IF ev.event_type NOT IN ('issue_classified','issue_merged') THEN
      RAISE EXCEPTION 'agentrt: a risk class is set by a classification or a merge, not %', ev.event_type USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.priority IS DISTINCT FROM OLD.priority AND ev.event_type <> 'issue_priority_set' THEN
    RAISE EXCEPTION 'agentrt: a priority is set by issue_priority_set, not %', ev.event_type USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.brief_text_id IS DISTINCT FROM OLD.brief_text_id THEN
    IF ev.event_type <> 'issue_brief_drafted' OR NEW.brief_text_id IS NULL OR NEW.brief_confirmed_ask IS NOT NULL
       OR NOT EXISTS (SELECT 1 FROM agentrt.issue_text t WHERE t.issue_text_id = NEW.brief_text_id
                        AND t.issue_id = NEW.issue_id AND t.text_kind = 'brief') THEN
      RAISE EXCEPTION 'agentrt: the brief of % changes only to a brief of its own, by issue_brief_drafted, unconfirmed', OLD.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
    IF EXISTS (SELECT 1 FROM agentrt.issue_work w WHERE w.issue_id = NEW.issue_id AND w.ended_at IS NULL)
       OR EXISTS (SELECT 1 FROM agentrt.issue_ask k WHERE k.issue_id = NEW.issue_id AND k.purpose IN ('ready_to_try','check_answer')
                    AND k.verdict IS NULL AND k.superseded_at IS NULL) THEN
      RAISE EXCEPTION 'agentrt: the brief of % does not change while its work is live or a trial or an answer is asked', OLD.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.design_text_id IS DISTINCT FROM OLD.design_text_id THEN
    IF ev.event_type <> 'issue_design_put' OR NEW.design_text_id IS NULL
       OR NOT EXISTS (SELECT 1 FROM agentrt.issue_text t WHERE t.issue_text_id = NEW.design_text_id
                        AND t.issue_id = NEW.issue_id AND t.text_kind = 'design') THEN
      RAISE EXCEPTION 'agentrt: the design of % changes only to a design of its own, by issue_design_put', OLD.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.brief_confirmed_ask IS DISTINCT FROM OLD.brief_confirmed_ask THEN
    IF NEW.brief_confirmed_ask IS NULL THEN
      IF ev.event_type <> 'issue_brief_drafted' THEN
        RAISE EXCEPTION 'agentrt: a brief''s confirmation is cleared only by a new brief' USING ERRCODE = 'check_violation';
      END IF;
    ELSE
      SELECT * INTO a FROM agentrt.issue_ask WHERE issue_ask_id = NEW.brief_confirmed_ask AND issue_id = NEW.issue_id;
      IF ev.event_type <> 'issue_answered' OR NOT FOUND OR a.purpose <> 'confirm_brief' OR a.verdict IS DISTINCT FROM 'confirmed'
         OR a.superseded_at IS NOT NULL OR a.bound_sha256 IS DISTINCT FROM agentrt.issue_text_sha(NEW.brief_text_id) THEN
        RAISE EXCEPTION 'agentrt: issue % names as its brief''s confirmation an ask that did not confirm this brief', OLD.issue_id
          USING ERRCODE = 'check_violation';
      END IF;
    END IF;
  END IF;
  IF NEW.state IS DISTINCT FROM OLD.state THEN
    IF NEW.state = 'active' AND NOT (ev.event_type = 'issue_taken'
                                     OR (ev.event_type = 'issue_answered' AND OLD.state = 'awaiting_user')) THEN
      RAISE EXCEPTION 'agentrt: issue % becomes active only by take_issue (issue_taken), not %', OLD.issue_id, ev.event_type
        USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.state = 'parked' AND EXISTS (SELECT 1 FROM agentrt.issue_work w WHERE w.issue_id = NEW.issue_id AND w.ended_at IS NULL) THEN
      RAISE EXCEPTION 'agentrt: issue % is not parked while its work is live; the work ends first', OLD.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF ev.event_type = 'issue_taken' AND NEW.state <> 'active' THEN
    RAISE EXCEPTION 'agentrt: a taken issue is active' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.conclusion IS DISTINCT FROM OLD.conclusion THEN
    IF OLD.conclusion IS NOT NULL AND NEW.conclusion IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: issue % is reopened before it is closed again', OLD.issue_id USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.conclusion = 'merged' OR (NEW.conclusion = 'merged') <> (ev.event_type = 'issue_merged')
       OR (NEW.conclusion IS DISTINCT FROM 'merged' AND ev.event_type <> 'issue_state_changed') THEN
      RAISE EXCEPTION 'agentrt: issue % is closed by issue_state_changed and merged by issue_merged, and stays merged', OLD.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.duplicate_of IS DISTINCT FROM OLD.duplicate_of THEN
    IF OLD.duplicate_of IS NOT NULL OR ev.event_type <> 'issue_merged' THEN
      RAISE EXCEPTION 'agentrt: only a merge sets duplicate_of, once' USING ERRCODE = 'check_violation';
    END IF;
    SELECT * INTO tgt FROM agentrt.issue WHERE issue_id = NEW.duplicate_of;
    IF tgt.product IS DISTINCT FROM NEW.product OR tgt.served_person IS DISTINCT FROM NEW.served_person
       OR (NEW.served_person IS NULL AND tgt.created_by_actor_id IS DISTINCT FROM NEW.created_by_actor_id) THEN
      RAISE EXCEPTION 'agentrt: % and % are not one person''s issues of one product: they are not merged', OLD.issue_id, NEW.duplicate_of
        USING ERRCODE = 'check_violation';
    END IF;
    IF tgt.state = 'closed' THEN
      RAISE EXCEPTION 'agentrt: % is merged into an open issue, and % is closed', OLD.issue_id, NEW.duplicate_of
        USING ERRCODE = 'check_violation';
    END IF;
    IF EXISTS (SELECT 1 FROM agentrt.issue_work w WHERE w.issue_id IN (NEW.issue_id, NEW.duplicate_of))
       OR EXISTS (SELECT 1 FROM agentrt.issue_ask k WHERE k.issue_id IN (NEW.issue_id, NEW.duplicate_of)
                    AND k.purpose IN ('ready_to_try','check_answer') AND k.verdict IS NULL AND k.superseded_at IS NULL) THEN
      RAISE EXCEPTION 'agentrt: only issues no work was taken for, with no trial or answer asked, are merged (first batch)'
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- Which text kinds each issue event writes.
CREATE FUNCTION agentrt.issue_text_kind_ok(event_type text, text_kind text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE event_type
    WHEN 'issue_submitted'     THEN text_kind IN ('words','reading','proposal')
    WHEN 'issue_added'         THEN text_kind IN ('words','reading','proposal')
    WHEN 'issue_brief_drafted' THEN text_kind = 'brief'
    WHEN 'issue_asked'         THEN text_kind = 'reply'
    WHEN 'issue_replied'       THEN text_kind = 'reply'
    WHEN 'issue_answered'      THEN text_kind IN ('response','note')
    WHEN 'issue_classified'    THEN text_kind = 'note'
    WHEN 'issue_design_put'    THEN text_kind = 'design'
    WHEN 'issue_reviewed'      THEN text_kind IN ('review','check')
    WHEN 'issue_taken'         THEN text_kind = 'note'
    WHEN 'issue_state_changed' THEN text_kind = 'note'
    ELSE false
  END
$$;

-- Texts: written by their event's writer, of a kind its type writes, on a
-- channel that writer speaks on, someone's words only as §1 allows; the
-- sha256 is the body's.
CREATE FUNCTION agentrt.guard_issue_text() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.issue_event(NEW.issue_id);
BEGIN
  IF NEW.created_by_actor_id <> ev.actor_id THEN
    RAISE EXCEPTION 'agentrt: a text is written by the writer of its event' USING ERRCODE = 'check_violation';
  END IF;
  IF NOT agentrt.issue_text_kind_ok(ev.event_type, NEW.text_kind) THEN
    RAISE EXCEPTION 'agentrt: a % event writes no % text', ev.event_type, NEW.text_kind USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.sha256 <> encode(sha256(convert_to(NEW.body, 'UTF8')), 'hex') THEN
    RAISE EXCEPTION 'agentrt: a text''s sha256 is its body''s' USING ERRCODE = 'check_violation';
  END IF;
  IF NOT agentrt.intake_voice_ok(ev.actor_id, NEW.channel, NEW.person) THEN
    RAISE EXCEPTION 'agentrt: % does not write % on channel % for %', ev.actor_id, NEW.text_kind, NEW.channel, COALESCE(NEW.person, 'nobody')
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Asks: asked of the issue's asks_person, for its current brief (none only
-- for a needs_info before there is a brief), a brief's confirmation only by
-- issue_brief_drafted and every other ask by issue_asked. What an ask asks
-- never changes. It is answered once, while open, by issue_answered, by a
-- writer allowed to speak for the person, for the brief still current; an
-- agent's own proposal is confirmed and tried by the Owner (I-D6); a
-- withdrawal comes only before the issue is taken. It is superseded once.
CREATE FUNCTION agentrt.guard_issue_ask() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev agentrt.event := agentrt.issue_event(NEW.issue_id);
  i  agentrt.issue;
  b  text;
BEGIN
  SELECT * INTO i FROM agentrt.issue WHERE issue_id = NEW.issue_id;
  b := agentrt.issue_text_sha(i.brief_text_id);
  IF TG_OP = 'INSERT' THEN
    IF ev.event_type NOT IN ('issue_brief_drafted','issue_asked') OR (NEW.purpose = 'confirm_brief') <> (ev.event_type = 'issue_brief_drafted') THEN
      RAISE EXCEPTION 'agentrt: a brief''s confirmation is asked by issue_brief_drafted and every other ask by issue_asked, not %',
        ev.event_type USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.person <> i.asks_person THEN
      RAISE EXCEPTION 'agentrt: issue % asks %, not %', NEW.issue_id, i.asks_person, NEW.person USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.verdict IS NOT NULL OR NEW.said_at IS NOT NULL OR NEW.superseded_at IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: a new ask is open' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.brief_sha256 IS NULL THEN
      IF NEW.purpose <> 'needs_info' OR b IS NOT NULL THEN
        RAISE EXCEPTION 'agentrt: an ask of % names its current brief' , NEW.issue_id USING ERRCODE = 'check_violation';
      END IF;
    ELSIF NEW.brief_sha256 IS DISTINCT FROM b THEN
      RAISE EXCEPTION 'agentrt: an ask of % is asked for its current brief' , NEW.issue_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.person <> OLD.person OR NEW.purpose <> OLD.purpose OR NEW.bound_text_id <> OLD.bound_text_id
     OR NEW.bound_sha256 <> OLD.bound_sha256 OR NEW.brief_sha256 IS DISTINCT FROM OLD.brief_sha256
     OR NEW.commitment IS DISTINCT FROM OLD.commitment OR NEW.mission_id IS DISTINCT FROM OLD.mission_id
     OR NEW.env IS DISTINCT FROM OLD.env OR NEW.commits IS DISTINCT FROM OLD.commits THEN
    RAISE EXCEPTION 'agentrt: what ask % asks never changes', OLD.issue_ask_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.superseded_at IS DISTINCT FROM OLD.superseded_at THEN
    IF OLD.superseded_at IS NOT NULL OR NEW.superseded_at IS NULL
       OR ev.event_type NOT IN ('issue_brief_drafted','issue_asked','issue_state_changed')
       OR (NEW.verdict, NEW.words_text_id, NEW.answered_at) IS DISTINCT FROM (OLD.verdict, OLD.words_text_id, OLD.answered_at) THEN
      RAISE EXCEPTION 'agentrt: ask % is superseded once, by itself', OLD.issue_ask_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.verdict, NEW.words_text_id, NEW.said_at, NEW.answered_by_actor_id, NEW.answer_channel, NEW.answered_at)
     IS NOT DISTINCT FROM (OLD.verdict, OLD.words_text_id, OLD.said_at, OLD.answered_by_actor_id, OLD.answer_channel, OLD.answered_at) THEN
    RETURN NEW;
  END IF;
  IF ev.event_type <> 'issue_answered' OR OLD.verdict IS NOT NULL OR OLD.superseded_at IS NOT NULL OR NEW.verdict IS NULL THEN
    RAISE EXCEPTION 'agentrt: ask % is answered once, while open, by issue_answered', OLD.issue_ask_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.answered_by_actor_id IS DISTINCT FROM (CASE WHEN NEW.answer_channel = 'card' THEN 'owner' ELSE ev.actor_id END)
     OR NOT agentrt.intake_voice_ok(ev.actor_id, NEW.answer_channel, NEW.person) THEN
    RAISE EXCEPTION 'agentrt: % does not answer for % on channel %', ev.actor_id, NEW.person, NEW.answer_channel
      USING ERRCODE = 'check_violation';
  END IF;
  IF i.served_person IS NULL AND NEW.purpose IN ('confirm_brief','ready_to_try') AND NEW.answered_by_actor_id <> 'owner' THEN
    RAISE EXCEPTION 'agentrt: an agent''s own proposal is confirmed and tried by the Owner himself' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.brief_sha256 IS DISTINCT FROM b THEN
    RAISE EXCEPTION 'agentrt: ask % was asked of another brief than the current one', OLD.issue_ask_id USING ERRCODE = 'check_violation';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM agentrt.issue_text t WHERE t.issue_text_id = NEW.words_text_id AND t.text_kind = 'response') THEN
    RAISE EXCEPTION 'agentrt: an answer is the person''s response' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.verdict = 'withdrawn' AND EXISTS (SELECT 1 FROM agentrt.issue_work w WHERE w.issue_id = NEW.issue_id) THEN
    RAISE EXCEPTION 'agentrt: % is withdrawn only before it is taken', NEW.issue_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Work: inserted only by issue_taken, for the current brief, of a live
-- mission made for this issue; afterwards only ended_at moves, once, and only
-- when its mission has ended, under that mission's mission_state_changed
-- (the trigger on agentrt.mission below).
CREATE FUNCTION agentrt.guard_issue_work() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event;
BEGIN
  IF TG_OP = 'INSERT' THEN
    ev := agentrt.issue_event(NEW.issue_id);
    IF ev.event_type <> 'issue_taken' OR NEW.ended_at IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: issue work is taken by issue_taken, live' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.brief_sha256 IS DISTINCT FROM (SELECT agentrt.issue_text_sha(brief_text_id) FROM agentrt.issue WHERE issue_id = NEW.issue_id) THEN
      RAISE EXCEPTION 'agentrt: work for % is taken for its current brief', NEW.issue_id USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM agentrt.mission m WHERE m.mission_id = NEW.mission_id AND m.work_id = NEW.issue_id
                     AND m.state NOT IN ('done','stopped','expired')) THEN
      RAISE EXCEPTION 'agentrt: the work of % is a live mission made for it (work id %)', NEW.issue_id, NEW.issue_id
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'ended_at') IS DISTINCT FROM (to_jsonb(OLD) - 'ended_at') THEN
    RAISE EXCEPTION 'agentrt: issue work % changes nothing but its end', OLD.issue_work_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.ended_at IS DISTINCT FROM OLD.ended_at THEN
    ev := agentrt.current_event();
    IF ev.event_type <> 'mission_state_changed' OR OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL
       OR NOT EXISTS (SELECT 1 FROM agentrt.mission m WHERE m.mission_id = NEW.mission_id AND m.state IN ('done','stopped','expired')) THEN
      RAISE EXCEPTION 'agentrt: the work of % ends only when its mission % ends', OLD.issue_id, OLD.mission_id
        USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- Evidence: each kind by its own event type, by its event's writer, bound to
-- the brief and design current now (evidence for a version that is gone is
-- never written); a review's report is a review text, a classification's
-- reason and a seat check's excerpt a note and a check text; a seat's answer
-- is the report of a brief task (§2.2); a seat checks an answer given.
CREATE FUNCTION agentrt.guard_issue_check() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev agentrt.event := agentrt.issue_event(NEW.issue_id);
  i  agentrt.issue;
BEGIN
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
  IF NEW.seat_artifact_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM agentrt.attempt a JOIN agentrt.task t ON t.task_id = a.task_id
       WHERE a.report_artifact_id = NEW.seat_artifact_id AND t.role = 'brief') THEN
    RAISE EXCEPTION 'agentrt: a seat''s answer is the report of a brief task; % is not', NEW.seat_artifact_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.check_kind = 'close_check' AND NOT EXISTS (SELECT 1 FROM agentrt.issue_ask k WHERE k.issue_ask_id = NEW.ask_id
       AND k.verdict IS NOT NULL) THEN
    RAISE EXCEPTION 'agentrt: a seat checks an answer that was given' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- People: set up by the Owner (intake_client_set); a person keeps when it
-- was created.
CREATE FUNCTION agentrt.guard_person() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ev agentrt.event := agentrt.current_event();
BEGIN
  IF NOT agentrt.issue_writer_ok(ev.event_type, ev.actor_id) THEN
    RAISE EXCEPTION 'agentrt: % does not write a % event', ev.actor_id, ev.event_type USING ERRCODE = 'check_violation';
  END IF;
  IF TG_OP = 'UPDATE' AND NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'agentrt: a person keeps when it was created' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

-- Intake clients: set up by the Owner for an agent actor; the token name and
-- creation never change; products are keys, each once; the served cursor
-- only moves forward, whatever else changes; the acknowledgement only by the
-- client itself (intake_acked), forward only (never past served: the
-- table's check); settings only by intake_client_set, never again once
-- revoked.
CREATE FUNCTION agentrt.guard_intake_client() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev  agentrt.event;
  cfg text[] := ARRAY['on_behalf_of','products','daily_submits','daily_adds','daily_proposals','revoked_at'];
BEGIN
  IF TG_OP = 'INSERT' OR NEW.products IS DISTINCT FROM OLD.products THEN
    IF EXISTS (SELECT 1 FROM unnest(NEW.products) p WHERE p IS NULL OR p !~ '^[a-z][a-z0-9-]{0,31}$')
       OR (SELECT count(DISTINCT p) FROM unnest(NEW.products) p) <> cardinality(NEW.products) THEN
      RAISE EXCEPTION 'agentrt: an intake client''s products are product keys, each once' USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF TG_OP = 'INSERT' THEN
    ev := agentrt.current_event();
    IF ev.event_type <> 'intake_client_set' OR NOT agentrt.issue_writer_ok(ev.event_type, ev.actor_id) THEN
      RAISE EXCEPTION 'agentrt: the Owner sets up an intake client' USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM agentrt.actor a WHERE a.actor_id = NEW.intake_client_id AND a.actor_kind = 'agent') THEN
      RAISE EXCEPTION 'agentrt: intake client % is an agent actor', NEW.intake_client_id USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.served_txid IS NOT NULL OR NEW.acked_txid IS NOT NULL OR NEW.revoked_at IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: a new intake client has been served nothing' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.served_txid, NEW.served_event) IS DISTINCT FROM (OLD.served_txid, OLD.served_event)
     AND (NEW.served_txid IS NULL OR NEW.served_event IS NULL
          OR (OLD.served_txid IS NOT NULL AND (NEW.served_txid, NEW.served_event) < (OLD.served_txid, OLD.served_event))) THEN
    RAISE EXCEPTION 'agentrt: an intake client''s served cursor only moves forward' USING ERRCODE = 'check_violation';
  END IF;
  IF (to_jsonb(NEW) - ARRAY['served_txid','served_event']) = (to_jsonb(OLD) - ARRAY['served_txid','served_event']) THEN
    RETURN NEW;
  END IF;
  ev := agentrt.current_event();
  IF NEW.token_name <> OLD.token_name OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'agentrt: intake client % keeps its token name', OLD.intake_client_id USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.acked_txid, NEW.acked_event, NEW.acked_at) IS DISTINCT FROM (OLD.acked_txid, OLD.acked_event, OLD.acked_at) THEN
    IF ev.event_type <> 'intake_acked' OR ev.actor_id <> NEW.intake_client_id THEN
      RAISE EXCEPTION 'agentrt: % acknowledges its own updates, by intake_acked', NEW.intake_client_id USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.acked_txid IS NULL OR (OLD.acked_txid IS NOT NULL AND (NEW.acked_txid, NEW.acked_event) <= (OLD.acked_txid, OLD.acked_event)) THEN
      RAISE EXCEPTION 'agentrt: an acknowledgement only moves forward' USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF (to_jsonb(NEW) - ARRAY['served_txid','served_event','acked_txid','acked_event','acked_at'])
     IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['served_txid','served_event','acked_txid','acked_event','acked_at']) THEN
    IF ev.event_type <> 'intake_client_set' OR NOT agentrt.issue_writer_ok(ev.event_type, ev.actor_id) THEN
      RAISE EXCEPTION 'agentrt: the Owner changes an intake client''s settings (intake_client_set)' USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.revoked_at IS NOT NULL THEN
      RAISE EXCEPTION 'agentrt: intake client % is revoked and stays so', OLD.intake_client_id USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- The state tables' event check and no deletion (0002's functions), and the
-- guards above; issue_text and issue_check are append only.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['person','intake_client','issue','issue_ask','issue_work'] LOOP
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.require_event()',
                   t || '_00_event', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE DELETE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_delete', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON agentrt.%I FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_delete()',
                   t || '_00_no_truncate', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON agentrt.%I FOR EACH ROW EXECUTE FUNCTION agentrt.%I()',
                   t || '_10_guard', t, 'guard_' || t);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['issue_text','issue_check'] LOOP
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

-- A mission that ends (done, stopped, expired) ends its issue work in the
-- same statement (R2-A6): the event of the transaction is the
-- mission_state_changed the move appended first, whose envelope names this
-- mission, so require_event passes; a mission no issue took touches no row.
-- Every binary moves a mission's state with the one UPDATE that fires this.
CREATE FUNCTION agentrt.end_issue_work() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE agentrt.issue_work SET ended_at = clock_timestamp() WHERE mission_id = NEW.mission_id AND ended_at IS NULL;
  RETURN NULL;
END $$;
CREATE TRIGGER mission_80_end_issue_work AFTER UPDATE OF state ON agentrt.mission FOR EACH ROW
  WHEN (NEW.state IN ('done','stopped','expired') AND OLD.state IS DISTINCT FROM NEW.state)
  EXECUTE FUNCTION agentrt.end_issue_work();

-- A work id starting BI- is a business issue's, and its mission is made only
-- by take_issue, which writes the issue work for it in the same transaction
-- (§4.4): checked at commit, so an older binary's POST /missions with such a
-- work id fails closed. The whole prefix is reserved, not only well formed
-- issue ids.
CREATE FUNCTION agentrt.check_issue_mission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.work_id LIKE 'BI-%' AND NOT EXISTS (SELECT 1 FROM agentrt.issue_work w
                                             WHERE w.mission_id = NEW.mission_id AND w.issue_id = NEW.work_id) THEN
    RAISE EXCEPTION 'agentrt: mission % has work id %, which only take_issue gives, with the issue''s work', NEW.mission_id, NEW.work_id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER mission_90_issue_work AFTER INSERT OR UPDATE OF work_id ON agentrt.mission
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agentrt.check_issue_mission();

-- Grants, named one by one: 0003's ON ALL only covered the objects there
-- were then. Append only: SELECT and INSERT; state tables: SELECT, INSERT and
-- UPDATE; never DELETE. The identity sequences, for the ids the store takes
-- before the event that names them. agent_reader gets nothing here: the
-- issue tables hold people's words, and the Console and the crew read them
-- through the issue routes (where it has pg_read_all_data it still reads
-- them; §5.3, §12).
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
  t  text;
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  FOREACH t IN ARRAY ARRAY['issue_text','issue_check'] LOOP
    EXECUTE format('GRANT SELECT, INSERT ON agentrt.%I TO %I', t, rt);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['person','intake_client','issue','issue_ask','issue_work'] LOOP
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON agentrt.%I TO %I', t, rt);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['issue_text','issue_ask','issue_check','issue_work'] LOOP
    EXECUTE format('GRANT USAGE ON SEQUENCE %s TO %I', pg_get_serial_sequence('agentrt.' || t, t || '_id'), rt);
  END LOOP;
  FOREACH t IN ARRAY ARRAY['issue_text_sha(bigint)','issue_writer_ok(text,text)','intake_voice_ok(text,text,text)',
                           'issue_event(text)','guard_issue()','issue_text_kind_ok(text,text)','guard_issue_text()',
                           'guard_issue_ask()','guard_issue_work()','guard_issue_check()','guard_person()',
                           'guard_intake_client()','end_issue_work()','check_issue_mission()'] LOOP
    EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.%s TO %I', t, rt);
  END LOOP;
END $$;
