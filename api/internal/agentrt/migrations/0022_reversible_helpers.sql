-- 0022: two helpers for migrations that cannot destroy rows (LANE-ci-w1,
-- Owner 2026-11-15). A migration whose every statement is of a class the
-- release policy names (releasepolicy.ClassifyMigration) reaches PROD without
-- a card. That class has no DROP TABLE and no DELETE; what it has instead is
-- these two calls, which look at the rows first and raise when there is one:
--
--   SELECT agentrt.drop_table_if_empty('agentrt.some_table');
--   SELECT agentrt.assert_no_rows('agentrt.event', 'event_stream_id', 'debt:%');
--
-- The migrator runs a file in one transaction (migrate.apply), so a raise
-- here rolls the whole file back: nothing is dropped and no record of the
-- migration is written. Both are SECURITY INVOKER: a caller that does not own
-- the table cannot drop it through them. Names are parsed and quoted
-- (parse_ident, %I, regclass), never put into the statement as text; the
-- pattern is a parameter.
--
-- This file adds two functions and changes nothing else. The policy refuses a
-- later migration that replaces or drops either of them.

CREATE FUNCTION agentrt.drop_table_if_empty(tbl text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  parts text[] := parse_ident(tbl);
  rel regclass;
  has_row boolean;
BEGIN
  IF array_length(parts, 1) IS DISTINCT FROM 2 THEN
    RAISE EXCEPTION 'agentrt.drop_table_if_empty: % is not a schema-qualified table name; nothing is dropped', tbl;
  END IF;
  rel := to_regclass(format('%I.%I', parts[1], parts[2]));
  IF rel IS NULL THEN
    RAISE EXCEPTION 'agentrt.drop_table_if_empty: table % does not exist; nothing is dropped', tbl;
  END IF;
  -- The lock DROP TABLE takes, taken before the look: no row arrives between
  -- the look and the drop.
  EXECUTE format('LOCK TABLE %s IN ACCESS EXCLUSIVE MODE', rel);
  EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s)', rel) INTO has_row;
  IF has_row THEN
    RAISE EXCEPTION 'agentrt.drop_table_if_empty: table % has at least one row; nothing is dropped', tbl;
  END IF;
  -- No CASCADE: something that depends on the table is an error here.
  EXECUTE format('DROP TABLE %s', rel);
END
$$;

CREATE FUNCTION agentrt.assert_no_rows(tbl text, col text, pattern text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  parts text[] := parse_ident(tbl);
  rel regclass;
  has_row boolean;
BEGIN
  IF array_length(parts, 1) IS DISTINCT FROM 2 THEN
    RAISE EXCEPTION 'agentrt.assert_no_rows: % is not a schema-qualified table name', tbl;
  END IF;
  rel := to_regclass(format('%I.%I', parts[1], parts[2]));
  IF rel IS NULL THEN
    RAISE EXCEPTION 'agentrt.assert_no_rows: table % does not exist', tbl;
  END IF;
  EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE %I::text LIKE $1)', rel, col) INTO has_row USING pattern;
  IF has_row THEN
    RAISE EXCEPTION 'agentrt.assert_no_rows: table % has at least one row whose column % is LIKE %', tbl, col, quote_literal(pattern);
  END IF;
END
$$;
