-- agentrt 0049: one transcript's owner_said records go, once (plan domain,
-- 2026-11-25). The first backfill of the Owner's history kept a database
-- password in three records of transcript 7604bfcc-2df4-49aa-877d-8e482c27ed93
-- (written as PGPASSWORD: …, which the hook's first redaction missed; the hook
-- was fixed in infra the same day). Owner, in the legislation thread, asked
-- whether to delete them: 「好，删」.
--
-- A record cannot be cut out of a stream without breaking its hash chain, so
-- the whole stream owner-said:7604bfcc-… goes (its 529 messages), the way
-- 0047 deletes a whole law stream; the hook then posts that transcript again
-- with the fixed redaction, and the platform records each message once more.
-- Nothing else is touched: the delete names that one stream and only
-- owner_said records; the append-only trigger and the stream guard are off for
-- this transaction alone and back on before it commits. Where the stream does
-- not exist (dev, stg) the file changes nothing.
--
-- Way back: none for the deleted rows (that is the point); the hook re-records
-- the messages without the password.
ALTER TABLE agentrt.event DISABLE TRIGGER event_append_only;
ALTER TABLE agentrt.event_stream DISABLE TRIGGER event_stream_guard;

DO $$
DECLARE
  stream constant text := 'owner-said:7604bfcc-2df4-49aa-877d-8e482c27ed93';
  other int;
BEGIN
  SELECT count(*) INTO other FROM agentrt.event WHERE event_stream_id = stream AND event_type <> 'owner_said';
  IF other > 0 THEN
    RAISE EXCEPTION 'agentrt 0049: stream % holds % records that are not owner_said', stream, other;
  END IF;
  DELETE FROM agentrt.event WHERE event_stream_id = stream AND event_type = 'owner_said';
  DELETE FROM agentrt.event_stream WHERE event_stream_id = stream;
END $$;

ALTER TABLE agentrt.event ENABLE TRIGGER event_append_only;
ALTER TABLE agentrt.event_stream ENABLE TRIGGER event_stream_guard;
