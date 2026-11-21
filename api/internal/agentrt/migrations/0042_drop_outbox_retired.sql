-- agentrt 0042: the retired outbox leaves the database (LANE-BIZ-ALIGN, 2026-11-21).
-- The Owner, in the decoupling thread on 2026-11-21: "c5 现在就删" (delete it
-- now), ahead of the seven days 0028 kept it for. This file HOLDS DATA
-- DELETION and runs only on the Owner's approval.
--
-- 0028 renamed agentrt.outbox to outbox_retired on 2026-11-17; its writer left
-- the code on 2026-11-15 (7fc1eff2). No code reads or writes the table or its
-- two functions (git grep at origin/main 3c335db2). The letters it signalled
-- are in agentrt.message, so no letter is lost.
--
-- Measured 2026-11-21 with agent_reader, rows on PROD / STG, newest created_at:
--   outbox_retired  2,309 / 3,138   2026-11-15 05:33Z / 2026-11-14 00:50Z
--   every row delivered (delivered_at set) on both.
--
-- DROP list (grep -niE 'drop|truncate' finds these and nothing else; no
-- TRUNCATE, no DELETE, no CASCADE):
--   DROP TABLE     agentrt.outbox_retired   (its indexes, triggers and identity sequence go with it)
--   DROP FUNCTION  agentrt.guard_outbox(), agentrt.notify_outbox()
-- Rollback (structure, grants and rows): biz-align/ddl-0042/rollback.sql in the
-- shared notes, with the CSV exports and SHA256SUMS beside it.
--
-- The migrator runs this file in one transaction: any error, the refusal
-- below included, leaves the database as it was.

-- 1. Look before acting. More rows than the larger of PROD and STG had, a row
--    written since the measurement (a day of margin: from 2026-11-16 00:00
--    UTC), or an undelivered row means a writer nobody found.
DO $$
DECLARE
  n bigint;
  latest timestamptz;
  pending bigint;
BEGIN
  SELECT count(*), max(created_at), count(*) FILTER (WHERE delivered_at IS NULL)
    INTO n, latest, pending FROM agentrt.outbox_retired;
  IF n > 3138 THEN
    RAISE EXCEPTION 'agentrt 0042: agentrt.outbox_retired has % rows, more than the 3138 measured; nothing is dropped', n;
  END IF;
  IF latest >= timestamptz '2026-11-16 00:00:00+00' THEN
    RAISE EXCEPTION 'agentrt 0042: agentrt.outbox_retired was written at %, after it was measured as stopped; nothing is dropped', latest;
  END IF;
  IF pending > 0 THEN
    RAISE EXCEPTION 'agentrt 0042: agentrt.outbox_retired has % undelivered rows; nothing is dropped', pending;
  END IF;
END $$;

-- 2. The table, without CASCADE: a dependency nobody measured is an error here
--    and the whole file rolls back.
DROP TABLE agentrt.outbox_retired;

-- 3. The two functions that served only it (its insert/update guard and its notify).
DROP FUNCTION agentrt.guard_outbox();
DROP FUNCTION agentrt.notify_outbox();
