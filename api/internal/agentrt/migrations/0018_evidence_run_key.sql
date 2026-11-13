-- agentrt 0018: what makes an evidence row of a run the same row. 0017 gave a
-- run one row per result. That is too narrow twice over: a later wave's run
-- gives a row per unit it tested, and a row written with a wrong key (STG,
-- 2026-11-13: the trees beside a repository were read as their commits' ids)
-- kept the run's right row out. A row is now the same row when the run, the
-- unit, the task, the key and the result are: seeing a run again still writes
-- nothing, and the same run under another key is another row. The ledger
-- stays append only; a row with a wrong key matches no question and is left.
--
-- Upgrading: run this before the platform that writes by the new rule. Between
-- the two, a platform of 0017 fails to write evidence (its ON CONFLICT names
-- the constraint dropped here), reports it, and records pipeline_seen as
-- before. The table is small; the lock is on it alone.

ALTER TABLE agentrt.evidence DROP CONSTRAINT evidence_run_uid_result_key;
ALTER TABLE agentrt.evidence ADD CONSTRAINT evidence_run_row_key UNIQUE (run_uid, repo, unit, task, input_key, result);
