-- agentrt 0008: an attempt's end prepare_failed (TD-394; the Owner
-- 2026-10-20 21:3x 「TD-394 B」). An attempt whose setup, checkout or sibling
-- clone failed before its executor started ended as exit, which TD-381
-- counted as the executor's own failure: a setup broken on the host withdrew
-- every executor in turn, and TD-395 then blocked the task as if none could
-- do it (PROD M-22). agentd now ends such an attempt prepare_failed; TD-381
-- and TD-395 count only exit and hard_limit, and three in a row block the
-- task with a card of their own (store/preparefailed.go). Only the attempt
-- table's end_how check changes.
--
-- Upgrading: run this before the platform that takes the end. A platform
-- from before refuses prepare_failed, and agentd then ends the attempt as
-- exit, as before; an older migrator refuses a database that has this file.
-- It is numbered after TD-397's 0007 and goes to PROD with it. The ALTER
-- takes a strong lock on the attempt table while it checks the rows.
ALTER TABLE agentrt.attempt DROP CONSTRAINT attempt_end_how_check;
ALTER TABLE agentrt.attempt ADD CONSTRAINT attempt_end_how_check CHECK (end_how IN (
  'exit','hard_limit','over_capacity','cancelled','daemon_restart','host_reboot','lease_lost','prepare_failed'));
