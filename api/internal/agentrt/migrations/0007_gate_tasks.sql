-- agentrt 0007: a gate task checks no other task (TD-397; the Owner chose B
-- on 2026-10-20: after a release the runtime dispatches each architecture
-- row's gate tests, the deterministic executor runs them on the deployed
-- commit, and they are recorded as gate verifications;
-- CUT-td397-gate-runs-2026-10-20.md §7① 甲, the Owner 2026-10-21 「全按推荐：
-- TD-397 ① 甲、② A」). A verification or a review still
-- names the task it checks; the one exception is a gate task: role verify,
-- no subject, class gate, effect none, off the plan, made by the controller.
-- Which tasks are gate tasks of which batch is the controller's own record
-- (store/gates.go), checked again wherever a gate task is used. Nothing else
-- changes: no table, no column, no other rule.
--
-- Upgrading: run this before the platform that makes gate tasks, with the
-- new binary's migrator (an older migrator refuses a database that has this
-- file). An older platform still starts and never makes a gate task; if its
-- controller meets one that was handed in, reading its absent subject fails
-- and nothing moves, so the newer one uses it. The ALTER takes a strong lock
-- on the task table while it checks the existing rows.
DO $$
DECLARE
  name text;
  n int;
BEGIN
  SELECT count(*), min(conname) INTO n, name FROM pg_constraint
    WHERE conrelid = 'agentrt.task'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) = 'CHECK (((role = ANY (ARRAY[''verify''::text, ''review''::text])) = (subject_task_id IS NOT NULL)))';
  IF n <> 1 THEN
    RAISE EXCEPTION 'agentrt 0007: expected one CHECK tying verify and review to a subject, found %', n;
  END IF;
  EXECUTE format('ALTER TABLE agentrt.task DROP CONSTRAINT %I', name);
END $$;
ALTER TABLE agentrt.task ADD CONSTRAINT task_subject_by_role CHECK (
  (role IN ('verify','review')) = (subject_task_id IS NOT NULL)
  OR (role = 'verify' AND subject_task_id IS NULL AND task_class = 'gate' AND effect_class = 'none' AND off_plan
      AND created_by_actor_id = 'controller'));
