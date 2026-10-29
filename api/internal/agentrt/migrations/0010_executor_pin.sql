-- agentrt 0010: an executor pin (W-31 round 4, core block C-0;
-- bifrost-trade-infra agent-config/work/multi-agent/
-- DESIGN-coordinator-landing-v3-2026-10-29.md §2.2, §2.4, §7.1; F1 A, taken
-- by the coordinating thread on 2026-10-28 for the Owner). A task may be
-- pinned to the one external host, the coordinator's Mac: only a proposal by
-- the proposer seat a signed standing envelope names sets a pin (the store
-- checks that), and the database keeps what it can of the rest: a pin is
-- only external:coordinator on an implementation with a branch effect; it is
-- set only with the task_proposed event that records it and never changes
-- after; a pinned task is leased only on an external host, and an external
-- host leases nothing else; there is at most one external host, and a host
-- keeps its kind.
--
-- Upgrading (§7.3): run this, then roll out the platform that reads pins
-- (ordinary claims skip pinned tasks; presence does not report the external
-- host offline), check that no platform-api or workers Pod of an older
-- commit is left, and only then sign an envelope with external_hosts, which
-- is what lets a pin be set at all. An older platform still starts (it
-- reports the schema ahead) and an older migrator refuses a database that
-- has this file. An older platform never sets a pin and never registers an
-- external host; should it meet a pinned task as the oldest ready one, the
-- attempt guard below refuses its lease and that claim fails, so it fails
-- closed.
--
-- Locks, in file order: ALTER TABLE takes ACCESS EXCLUSIVE on host, then
-- task. Their ordinary CREATE INDEX takes SHARE and CREATE TRIGGER takes
-- SHARE ROW EXCLUSIVE (already covered by those stronger ALTER locks).
-- CREATE TRIGGER on attempt also takes SHARE ROW EXCLUSIVE, conflicting
-- with attempt writers. All locks remain until this file's transaction
-- ends. Small tables do not bound lock waits: migrate.apply sets LOCAL
-- lock_timeout = 2s and statement_timeout = 30s before running the file.
-- A timeout rolls back the whole file and its migration record; retry in
-- a quieter window. Record actual wait/hold time in STG before PROD.
-- The one DROP is host_host_kind_check, made again in this SAME transaction
-- with external added; never run these statements separately in autocommit.

-- Hosts: an external kind, at most one host of it, and a kind never changes.
ALTER TABLE agentrt.host DROP CONSTRAINT host_host_kind_check;
ALTER TABLE agentrt.host ADD CONSTRAINT host_host_kind_check CHECK (host_kind IN ('dev','vm','external'));
CREATE UNIQUE INDEX host_one_external ON agentrt.host (host_kind) WHERE host_kind = 'external';

CREATE FUNCTION agentrt.host_kind_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.host_kind IS DISTINCT FROM OLD.host_kind THEN
    RAISE EXCEPTION 'agentrt: host % keeps its kind %', OLD.host_id, OLD.host_kind USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER host_kind_immutable BEFORE UPDATE OF host_kind ON agentrt.host
  FOR EACH ROW EXECUTE FUNCTION agentrt.host_kind_immutable();

-- Tasks: the pin, its shape, where it comes from and that it never changes.
ALTER TABLE agentrt.task ADD COLUMN executor_pin text;
ALTER TABLE agentrt.task ADD CONSTRAINT task_executor_pin_check CHECK (
  executor_pin IS NULL OR (executor_pin = 'external:coordinator' AND role = 'implement' AND effect_class = 'branch'));

-- A new task's pin is the one its task_proposed event records (none when
-- the event names none), and only a task_proposed event creates a pinned
-- task; an update never changes the pin, whoever writes it (moving a task
-- is a plan change, cancelling and proposing again is the only way to pin
-- it elsewhere).
CREATE FUNCTION agentrt.pin_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  ev agentrt.event;
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF NEW.executor_pin IS DISTINCT FROM OLD.executor_pin THEN
      RAISE EXCEPTION 'agentrt: task % keeps its executor pin', OLD.task_id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  ev := agentrt.current_event();
  IF NEW.executor_pin IS NOT NULL AND ev.event_type <> 'task_proposed' THEN
    RAISE EXCEPTION 'agentrt: a pinned task % is made only by its task_proposed event, not by %', NEW.task_id, ev.event_type
      USING ERRCODE = 'check_violation';
  END IF;
  IF ev.event_type = 'task_proposed' AND (ev.body_json ->> 'executor_pin') IS DISTINCT FROM NEW.executor_pin THEN
    RAISE EXCEPTION 'agentrt: task % has executor pin % but its task_proposed event records %', NEW.task_id,
      COALESCE(NEW.executor_pin, 'none'), COALESCE(ev.body_json ->> 'executor_pin', 'none') USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER task_pin_immutable BEFORE INSERT OR UPDATE ON agentrt.task
  FOR EACH ROW EXECUTE FUNCTION agentrt.pin_immutable();

-- Attempts: a pinned task only on an external host, an external host only
-- on a pinned task. An attempt with no host is not on an external one.
CREATE FUNCTION agentrt.attempt_pin_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  pin  text;
  kind text;
BEGIN
  SELECT executor_pin INTO pin FROM agentrt.task WHERE task_id = NEW.task_id;
  SELECT host_kind INTO kind FROM agentrt.host WHERE host_id = NEW.host_id;
  IF (pin IS NOT NULL) <> COALESCE(kind = 'external', false) THEN
    RAISE EXCEPTION 'agentrt: task % (executor pin %) is not leased on host % (%): a pinned task runs only on the external host, which runs nothing else',
      NEW.task_id, COALESCE(pin, 'none'), COALESCE(NEW.host_id, 'none'), COALESCE(kind, 'no host') USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER attempt_pin_guard BEFORE INSERT OR UPDATE OF task_id, host_id ON agentrt.attempt
  FOR EACH ROW EXECUTE FUNCTION agentrt.attempt_pin_guard();

CREATE INDEX task_pinned_ready_idx ON agentrt.task (created_at) WHERE state = 'ready' AND executor_pin IS NOT NULL;

-- Grants, as 0004 and 0009 give their functions.
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
  f  text;
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  FOREACH f IN ARRAY ARRAY['host_kind_immutable()','pin_immutable()','attempt_pin_guard()'] LOOP
    EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.%s TO %I', f, rt);
  END LOOP;
END $$;
