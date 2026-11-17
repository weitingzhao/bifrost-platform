-- agentrt 0027: the release record (2026-11-16, agreed with the communication
-- layer: shared notes duty/draft-ci-w1-2026-11-16-release-record-table.md and
-- its answers). One row is one finished delivery run as the recorder read it:
-- what it deployed where, from which commit of each repository, with which
-- image digests, and what it could not read.
--
-- Until now a record is a ConfigMap in the pipelines' namespace
-- (releases/record.go). A ConfigMap has no transaction: release.deploy.finished
-- cannot be announced in the transaction that writes the record, a list reads
-- every body back, and a delivery that failed is known only while Tekton still
-- keeps its run (review 162 A1). A failed run is a row here (succeeded=false),
-- so "is there a delivery after this deployment" no longer depends on what
-- Tekton retains.
--
-- Append only (0002's forbid_change), like the evidence ledger and the action
-- record. A record that is built again (the recorder's record_version rose,
-- or a commit it could not read is readable now) is a new row; the row of a
-- run with the largest release_record_id is the record of that run. Nothing
-- is pruned: a row a delivery, some sixty a day, and what was current when is
-- the point of keeping them.
--
-- This file only creates the table. Nothing writes it yet: the recorder first
-- writes both (ConfigMap and row) and compares, then reads move here, then
-- the ConfigMaps stop. Each step is its own change and each goes back by
-- reading the ConfigMaps again.
--
-- tenant: 'rocket' is the platform's own deliveries; another tenant's
-- deliveries are rows here too. env is a segment of the bus subject of the
-- announcement, hence its form (bus/relay EnvRE). event_id is the ledger
-- event that announced the record, appended in the same transaction before
-- the row; NULL for a row written before the announcement existed.
--
-- Upgrading: run this before the platform that writes records here. An older
-- platform still starts (it reports the schema ahead). A newer platform on a
-- database without this file keeps its ConfigMap records and logs that the
-- row was not written. The file creates one table and takes no lock on an
-- existing one beyond the foreign key's brief one on agentrt.event. Way back:
-- DROP TABLE agentrt.release_record once nothing reads it.

CREATE TABLE agentrt.release_record (
  release_record_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant text NOT NULL DEFAULT 'rocket' CHECK (tenant ~ '^[a-z][a-z0-9-]{0,62}$'),
  run text NOT NULL CHECK (run <> '' AND length(run) <= 253),
  pipeline text NOT NULL CHECK (pipeline <> '' AND length(pipeline) <= 253),
  lane text NOT NULL CHECK (lane <> '' AND length(lane) <= 63),
  env text NOT NULL CHECK (env ~ '^[a-z][a-z0-9-]{0,62}$'),
  -- deploys: the run changes what the environment runs (a build-only run does not).
  deploys boolean NOT NULL,
  succeeded boolean NOT NULL,
  revision text NOT NULL DEFAULT '' CHECK (length(revision) <= 255),
  tag text NOT NULL DEFAULT '' CHECK (length(tag) <= 255),
  -- from_run: the run whose build a promoting run deployed.
  from_run text NOT NULL DEFAULT '' CHECK (length(from_run) <= 253),
  started_at timestamptz NOT NULL,
  completed_at timestamptz NOT NULL,
  -- {repository: {"sha": ..., "source": result|pinned|param}}; a run names a handful.
  repos jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(repos) = 'object'),
  -- {image: "sha256:..."}
  images jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(images) = 'object'),
  -- What the recorder could not read; not empty is an incomplete record.
  missing jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(missing) = 'array'),
  record_version text NOT NULL CHECK (record_version <> '' AND length(record_version) <= 16),
  event_id bigint REFERENCES agentrt.event(event_id),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT release_record_times CHECK (completed_at >= started_at)
);
-- The current deployment of a lane and environment, and whether anything followed it.
CREATE INDEX release_record_lane_env_idx ON agentrt.release_record (tenant, lane, env, completed_at DESC);
-- The record of a run: its newest row.
CREATE INDEX release_record_run_idx ON agentrt.release_record (run, release_record_id DESC);
CREATE INDEX release_record_pipeline_idx ON agentrt.release_record (pipeline, completed_at DESC);

CREATE TRIGGER release_record_append_only BEFORE UPDATE OR DELETE ON agentrt.release_record
  FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change();
CREATE TRIGGER release_record_no_truncate BEFORE TRUNCATE ON agentrt.release_record
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change();

DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT SELECT, INSERT ON agentrt.release_record TO %I', rt);
  GRANT SELECT ON agentrt.release_record TO agent_reader;
END $$;
