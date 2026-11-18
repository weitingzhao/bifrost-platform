-- 0036: seven tables nothing uses leave the database (LANE-BIZ-ALIGN,
-- OWNER_LAW #37 and #38, 2026-11-18). No production code reads or writes them:
-- the last reader left in 5594aee2, which PROD and STG were running when this
-- was written. This file HOLDS DATA DELETION: unlike 0021 and 0023 the tables
-- are not empty, so it runs only on the Owner's approval.
--
-- Measured 2026-11-18, rows on PROD / STG, last write on PROD / STG:
--   metric_reading    26,418 / 26,640   2026-11-14 00:05 / 2026-11-14 00:05 (computed_at)
--   capability_score  31,993 / 67,326   2026-11-13 14:01 / 2026-11-14 01:00 (computed_at)
--   budget_ledger      2,486 /  2,995   2026-11-13 14:00 / 2026-11-14 01:00 (at)
--   vendor_pool            4 /      4   2026-11-13 03:01 / 2026-11-13 03:00 (measured_at)
--   capability_fact        7 /      6   2026-11-08 04:29 / 2026-10-14 15:56 (recorded_at)
--   job                    5 /      5   no time column
--   job_candidate         10 /     10   qualified_at empty on every row
--
-- agentrt.event: no row is read for change, changed or deleted, and the closed
-- vocabulary (event_type_closed) stays as it is: the old events of these
-- tables' types remain readable. agentrt.outbox_retired is not touched.
--
-- The migrator runs this file in one transaction (migrate.apply): any error,
-- the refusal below included, leaves the database as it was.
--
-- DROP list (grep -niE 'drop|truncate' finds these and nothing else; no
-- TRUNCATE, no DELETE, no CASCADE):
--   DROP TABLE     agentrt.capability_score, job_candidate, job, capability_fact,
--                  budget_ledger, vendor_pool, metric_reading
--   DROP FUNCTION  agentrt.guard_metric_reading()
-- Their indexes, triggers and budget_ledger's identity sequence go with them.
-- forbid_change, forbid_delete and require_event are shared and stay.
-- Rollback (structure and rows): biz-align/ddl-0036/rollback.sql in the shared
-- notes, with the CSV exports and SHA256SUMS beside it.

-- 1. Look before acting. A row written since the measurement (a day of margin:
--    from 2026-11-15 00:00 UTC), or more rows than the larger of PROD and STG
--    had, means a writer nobody found. An empty table passes. job has no time
--    column; capability_fact's retired_at and job_candidate's qualified_at are
--    written by a change to the row, so they count as writes.
DO $$
DECLARE
  c record;
  n bigint;
  latest timestamptz;
BEGIN
  FOR c IN SELECT * FROM (VALUES
    ('metric_reading',   26640::bigint, 'computed_at'),
    ('capability_score', 67326::bigint, 'computed_at'),
    ('budget_ledger',     2995::bigint, 'at'),
    ('vendor_pool',          4::bigint, 'measured_at'),
    ('capability_fact',      7::bigint, 'GREATEST(recorded_at, retired_at)'),
    ('job',                  5::bigint, 'NULL::timestamptz'),
    ('job_candidate',       10::bigint, 'qualified_at')
  ) AS t(tbl, most, written)
  LOOP
    EXECUTE format('SELECT count(*), max(%s) FROM agentrt.%I', c.written, c.tbl) INTO n, latest;
    IF n > c.most THEN
      RAISE EXCEPTION 'agentrt 0036: agentrt.% has % rows, more than the % measured; nothing is dropped', c.tbl, n, c.most;
    END IF;
    IF latest >= timestamptz '2026-11-15 00:00:00+00' THEN
      RAISE EXCEPTION 'agentrt 0036: agentrt.% was written at %, after it was measured as stopped; nothing is dropped', c.tbl, latest;
    END IF;
  END LOOP;
END $$;

-- 2. The two shared functions, as they are without these tables' branches.
--    Every other branch is word for word what the database had (0023): for a
--    type and a table that remain, the answer is the same.
CREATE OR REPLACE FUNCTION agentrt.event_may_write(event_type text, tbl text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT (event_type = 'legacy_imported') OR CASE tbl
    WHEN 'actor'           THEN event_type IN ('actor_registered','host_registered')
    WHEN 'host'            THEN event_type IN ('host_registered','host_state_changed','host_retired','host_slots_set')
    WHEN 'plan_node'       THEN event_type IN ('plan_node_created','plan_node_retitled','plan_node_state_changed','plan_changed')
    WHEN 'mission'         THEN event_type IN ('mission_proposed','mission_state_changed','mission_amended')
    WHEN 'task'            THEN event_type IN ('task_proposed','task_state_changed','task_cancelled','route_decided','plan_changed')
    WHEN 'task_dependency' THEN event_type = 'task_dependency_added'
    WHEN 'attempt'         THEN event_type LIKE 'attempt\_%'
    WHEN 'message'         THEN event_type IN ('message_posted','message_acked')
    WHEN 'control'         THEN event_type IN ('control_set','control_cleared')
    ELSE false
  END
$$;

CREATE OR REPLACE FUNCTION agentrt.event_names_row(body jsonb, tbl text, id text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE tbl
    WHEN 'actor'           THEN COALESCE(body->'actors' ? id, false) OR body->>'actor_id' IS NOT DISTINCT FROM id
    WHEN 'host'            THEN body->>'host_id' IS NOT DISTINCT FROM id
    WHEN 'control'         THEN body->>'control_id' IS NOT DISTINCT FROM id
    WHEN 'message'         THEN body->>'message_id' IS NOT DISTINCT FROM id
    ELSE true
  END
$$;

-- 3. The tables, those that reference first, without CASCADE: a dependency
--    nobody measured is an error here and the whole file rolls back.
DROP TABLE agentrt.capability_score;
DROP TABLE agentrt.job_candidate;
DROP TABLE agentrt.job;
DROP TABLE agentrt.capability_fact;
DROP TABLE agentrt.budget_ledger;
DROP TABLE agentrt.vendor_pool;
DROP TABLE agentrt.metric_reading;

-- 4. The one function that served only them (metric_reading's insert guard).
DROP FUNCTION agentrt.guard_metric_reading();
