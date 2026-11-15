-- agentrt 0022: the action record (Owner 2026-11-15: a card is for changing a
-- law; every other action is done by the line that owns it and recorded, for
-- the Owner's review afterwards). One row is one action taken, or refused,
-- with who did it, what, why, what undoes it and whether that was ready, on
-- which law it rested, how it ended, what was checked after and what was not.
--
-- The evidence ledger (0017) was considered and does not fit: it is keyed by
-- a commit and a content hash and its result column is passed/failed, while an
-- action has neither a commit nor a pass/fail, and a refusal is a row too.
--
-- Only the platform writes. The table is append only (0002's forbid_change). Nothing
-- decides by a record; it is read by seat, by day and by law.
--
-- Upgrading: run this before the platform that writes records. An older
-- platform still starts (it reports the schema ahead). A newer platform on a
-- database without this file does its actions as before and logs that the
-- record was not written. The file creates one table and takes no lock on an
-- existing one. Way back: DROP TABLE agentrt.action_record (it holds only
-- records, which nothing else reads).

CREATE TABLE agentrt.action_record (
  record_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  seat text NOT NULL CHECK (seat <> '' AND length(seat) <= 128),
  action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]{0,99}$'),
  target text NOT NULL DEFAULT '' CHECK (length(target) <= 500),
  reason text NOT NULL CHECK (reason <> '' AND length(reason) <= 4000),
  regret text NOT NULL CHECK (length(regret) <= 2000),
  -- ready: prepared and verified by the machine; declared: the caller wrote
  -- what undoes it, nobody verified it; undeclared: the caller wrote nothing
  -- (a state of the first phase, which only records); not_ready: needed and
  -- not prepared (the action is then refused).
  regret_state text NOT NULL CHECK (regret_state IN ('ready','declared','undeclared','not_ready')),
  law text NOT NULL CHECK (law <> '' AND length(law) <= 200),
  outcome text NOT NULL CHECK (outcome IN ('done','refused','failed')),
  check_result text NOT NULL DEFAULT '' CHECK (length(check_result) <= 2000),
  not_verified text NOT NULL DEFAULT '' CHECK (length(not_verified) <= 2000),
  params_hash text CHECK (params_hash ~ '^[0-9a-f]{64}$'),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  -- No regret prepared, no action done: such a case is recorded as refused.
  CONSTRAINT action_record_no_done_without_regret CHECK (NOT (outcome = 'done' AND regret_state = 'not_ready')),
  CONSTRAINT action_record_ready_says_what CHECK (regret_state NOT IN ('ready','declared') OR regret <> '')
);
CREATE INDEX action_record_seat_idx ON agentrt.action_record (seat, record_id DESC);
CREATE INDEX action_record_law_idx ON agentrt.action_record (law, record_id DESC);
CREATE INDEX action_record_time_idx ON agentrt.action_record (recorded_at DESC);

CREATE TRIGGER action_record_append_only BEFORE UPDATE OR DELETE ON agentrt.action_record
  FOR EACH ROW EXECUTE FUNCTION agentrt.forbid_change();
CREATE TRIGGER action_record_no_truncate BEFORE TRUNCATE ON agentrt.action_record
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.forbid_change();

-- Grants, named one by one: append only, SELECT and INSERT; the reader may
-- read records (the writer redacts secrets from the free text first).
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT SELECT, INSERT ON agentrt.action_record TO %I', rt);
  GRANT SELECT ON agentrt.action_record TO agent_reader;
END $$;
