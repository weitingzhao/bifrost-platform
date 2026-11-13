-- agentrt 0019: one row per unit task the platform ran (CI design v4, landing
-- stage 2 block 2-C; the Owner agreed to this table 2026-11-13). The evidence
-- ledger (0017) says what content passed or failed; this table says what each
-- run was: which part of which unit, the first try or the second, how it
-- ended, what it reported and how long each stage took. A unit cut in eight
-- parts is eight rows here and one row of evidence.
--
-- class is how the run ended, read from the TaskRun as the cluster returns it
-- (measured on four real runs, 2026-11-13, ci-speed/stage2/unit-shapes):
--   passed  the TaskRun succeeded
--   test    it failed and its fail-class result says test
--   build   it failed and its fail-class result says build
--   infra   everything else: the content was not judged (the image could not
--           be pulled, the pod was lost, a timeout, a command that died
--           before it wrote its results). Such a run carries no results at
--           all, so every column a result fills may be NULL.
-- reason and message are Tekton's own words for the run's condition, kept as
-- they came. Only passed, test and build rows may lead to a row of evidence.
--
-- Only the platform writes, when it sees a unit task complete. The table is
-- append only (0002's forbid_change) and a TaskRun gives one row: seeing the
-- run again writes nothing.
--
-- Upgrading: run this before the platform that writes unit runs. An older
-- platform still starts (it reports the schema ahead). The file creates one
-- table and takes no lock on an existing one.

CREATE TABLE agentrt.unit_run (
  unit_run_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  plan text NOT NULL CHECK (plan ~ '^[0-9a-f]{16,64}$'),
  repo text NOT NULL CHECK (repo ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
  unit text NOT NULL CHECK (unit ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$'),
  task text NOT NULL CHECK (task ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$'),
  input_key text NOT NULL CHECK (input_key ~ '^[0-9a-f]{64}$'),
  commit_sha text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
  shard_index integer NOT NULL CHECK (shard_index >= 0),
  shard_total integer NOT NULL CHECK (shard_total >= 1),
  attempt integer NOT NULL CHECK (attempt >= 1),
  class text NOT NULL CHECK (class IN ('passed','test','build','infra')),
  reason text NOT NULL,
  message text NOT NULL DEFAULT '',
  exit_code integer,
  failed_tests text[] NOT NULL DEFAULT '{}',
  failed_tests_cut boolean NOT NULL DEFAULT false,
  tests_expected integer CHECK (tests_expected >= 0),
  tests_ran integer CHECK (tests_ran >= 0),
  tests_skipped integer CHECK (tests_skipped >= 0),
  tests_list_sha text CHECK (tests_list_sha ~ '^[0-9a-f]{64}$'),
  queued_seconds integer CHECK (queued_seconds >= 0),
  fetch_seconds integer CHECK (fetch_seconds >= 0),
  run_seconds integer CHECK (run_seconds >= 0),
  node text,
  run_name text NOT NULL CHECK (run_name <> ''),
  run_uid text NOT NULL CHECK (run_uid <> ''),
  started_at timestamptz,
  completed_at timestamptz,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT unit_run_shard_in_range CHECK (shard_index < shard_total),
  CONSTRAINT unit_run_run_uid_key UNIQUE (run_uid)
);
CREATE INDEX unit_run_key_idx ON agentrt.unit_run (repo, unit, task, input_key, unit_run_id DESC);
CREATE INDEX unit_run_plan_idx ON agentrt.unit_run (plan);

CREATE TRIGGER unit_run_append_only BEFORE UPDATE OR DELETE ON agentrt.unit_run
  FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change();
CREATE TRIGGER unit_run_no_truncate BEFORE TRUNCATE ON agentrt.unit_run
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change();

-- Grants, named one by one: append only, SELECT and INSERT; the reader may
-- read unit runs, which hold no person's words.
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT SELECT, INSERT ON agentrt.unit_run TO %I', rt);
  GRANT SELECT ON agentrt.unit_run TO agent_reader;
END $$;
