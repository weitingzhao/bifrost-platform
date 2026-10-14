-- agentrt 0004: what the outbox relay needs from the database (design v5
-- §5.1.4). A committed outbox row wakes the relay (it also polls every two
-- seconds); each Redis rebuild takes an epoch from PostgreSQL.
CREATE SEQUENCE agentrt.redis_epoch_seq;

CREATE FUNCTION agentrt.notify_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('agrt_outbox', '');
  RETURN NULL;
END $$;
CREATE TRIGGER outbox_20_notify AFTER INSERT ON agentrt.outbox
  FOR EACH STATEMENT EXECUTE FUNCTION agentrt.notify_outbox();

DO $$
BEGIN
  EXECUTE format('GRANT USAGE ON SEQUENCE agentrt.redis_epoch_seq TO %I', current_setting('agentrt.runtime_role'));
  EXECUTE format('GRANT EXECUTE ON FUNCTION agentrt.notify_outbox() TO %I', current_setting('agentrt.runtime_role'));
END $$;
