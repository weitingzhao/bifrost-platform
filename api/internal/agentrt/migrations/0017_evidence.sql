-- agentrt 0017: the evidence ledger (CI design v4 §3.3, landing wave 1 block
-- C; the Owner agreed to this table 2026-11-12). One row is one piece of
-- evidence: one task ran on one unit with the inputs named by input_key and
-- passed or failed. In this wave a repository is one unit ('repo') and the
-- task is the CI pipeline; later waves write a module or a package as unit
-- and a task name as task, into the same columns.
--
-- The key is the content that was tested, not the commit: input_key is the
-- sha256 of the canonical JSON of inputs (the pipeline, this repository's git
-- tree, the git tree of every repository cloned beside it). The same content
-- under another commit finds the same rows. commit_sha only says which commit
-- was tested then; nothing is decided by it.
--
-- Only the platform writes: it records a row when it sees a CI pipeline run
-- complete (the pass that records pipeline_seen). Nothing reads the table for
-- a decision yet. The table is append only (0002's forbid_change), and a run
-- gives at most one row per result, so a pass that sees the run again writes
-- nothing. 'invalidated' is a later full run contradicting a passed key; no
-- code writes it in this wave.
--
-- Upgrading: run this before the platform that writes evidence. An older
-- platform still starts (it reports the schema ahead) and writes no evidence.
-- A newer platform on a database without this file records pipeline_seen as
-- before and logs that the evidence was not written. The file creates one
-- table and takes no lock on an existing one.

CREATE TABLE agentrt.evidence (
  evidence_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  repo text NOT NULL CHECK (repo ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
  unit text NOT NULL CHECK (unit ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$'),
  task text NOT NULL CHECK (task ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$'),
  input_key text NOT NULL CHECK (input_key ~ '^[0-9a-f]{64}$'),
  result text NOT NULL CHECK (result IN ('passed','failed','invalidated')),
  scope text NOT NULL CHECK (scope IN ('full','incremental','console')),
  inputs jsonb NOT NULL CHECK (jsonb_typeof(inputs) = 'object'),
  statement jsonb NOT NULL CHECK (jsonb_typeof(statement) = 'object'),
  commit_sha text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
  run_name text NOT NULL CHECK (run_name <> ''),
  run_uid text NOT NULL CHECK (run_uid <> ''),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (run_uid, result)
);
CREATE INDEX evidence_key_idx ON agentrt.evidence (repo, unit, task, input_key, evidence_id DESC);
CREATE INDEX evidence_commit_idx ON agentrt.evidence (repo, commit_sha);

CREATE TRIGGER evidence_append_only BEFORE UPDATE OR DELETE ON agentrt.evidence
  FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change();
CREATE TRIGGER evidence_no_truncate BEFORE TRUNCATE ON agentrt.evidence
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change();

-- Grants, named one by one: append only, SELECT and INSERT; the reader may
-- read evidence, which holds no person's words.
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT SELECT, INSERT ON agentrt.evidence TO %I', rt);
  GRANT SELECT ON agentrt.evidence TO agent_reader;
END $$;
