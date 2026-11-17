-- agentrt 0028: the outbox is retired (communication line, 2026-11-17).
-- agentrt.outbox carried the Redis wake signals of mailbox letters (design v5
-- §5.1.4). Its writer left the code on 2026-11-15 (relay A, 7fc1eff2): a
-- letter is the message row and its message_posted event, and the bus reads
-- the event ledger. Since then nothing writes or reads the table. Measured on
-- PROD with agent_reader at 2026-11-17 19:25Z: 2,309 rows, every one
-- delivered, the newest of 2026-11-15 05:33Z; the letters themselves are in
-- agentrt.message, so retiring the table loses no letter.
--
-- The table is renamed, not dropped, and keeps every row for seven days (the
-- Owner, 2026-11-13: an automatic decision's trail is kept seven days). A
-- later migration drops it, on its own card. Nothing else changes: the
-- guard trigger stays (a row is still only marked delivered, never changed),
-- the notify trigger fires only on an insert and nothing inserts. No row is
-- read for change, changed or removed.
--
-- Way back, any time before the drop:
--   ALTER TABLE agentrt.outbox_retired RENAME TO outbox;
--   ALTER INDEX agentrt.outbox_retired_pending_idx RENAME TO outbox_pending_idx;
-- A platform from before 7fc1eff2 writing to agentrt.outbox fails after this
-- file; none runs (PROD and STG are past it).

ALTER TABLE agentrt.outbox RENAME TO outbox_retired;
ALTER INDEX agentrt.outbox_pending_idx RENAME TO outbox_retired_pending_idx;
