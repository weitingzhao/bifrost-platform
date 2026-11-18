-- agentrt 0035: where a delivery's image builds ran, on its release record
-- (2026-11-18). The platform chooses the node and writes it on the run; the
-- run is kept about two days, so the record is where the choice stays. It was
-- on the record ConfigMap only, and the table replaces those.
--
-- Two added columns with a default: no row is rewritten and nothing is read for
-- change. Upgrading: run this before the platform that reads them. The ALTER's
-- lock and statement are bounded by migrate.apply (2s, 30s).
-- Way back: drop the two columns once nothing reads them.

ALTER TABLE agentrt.release_record
  ADD COLUMN build_node text NOT NULL DEFAULT '',
  ADD COLUMN build_node_why text NOT NULL DEFAULT '';
