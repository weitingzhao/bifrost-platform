-- agentrt 0003: grants (design v5 §5.1.1). The runtime role differs per
-- environment (platform_agentrt_<env>); the migrator sets
-- agentrt.runtime_role for this transaction, so this file names no
-- environment and its sha256 is the same everywhere.
DO $$
DECLARE
  rt text := current_setting('agentrt.runtime_role');
  t  text;
BEGIN
  IF rt !~ '^platform_agentrt_(dev|stg|prod)$' THEN
    RAISE EXCEPTION 'agentrt: runtime role % is not platform_agentrt_<env>', rt;
  END IF;
  EXECUTE format('GRANT USAGE ON SCHEMA agentrt TO %I, agent_reader', rt);
  EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA agentrt TO %I, agent_reader', rt);
  EXECUTE format('GRANT USAGE ON ALL SEQUENCES IN SCHEMA agentrt TO %I', rt);

  -- Append only: SELECT and INSERT.
  FOREACH t IN ARRAY ARRAY['event','artifact','artifact_blob','verification','budget_ledger',
                           'capability_score','rule','plan_node_binding'] LOOP
    EXECUTE format('GRANT INSERT ON agentrt.%I TO %I', t, rt);
  END LOOP;

  -- Stream heads and state tables: INSERT and UPDATE, never DELETE.
  FOREACH t IN ARRAY ARRAY['event_stream','actor','host','plan_node','mission','task','task_dependency',
                           'attempt','message','control','vendor_pool','job','job_candidate','capability_fact'] LOOP
    EXECUTE format('GRANT INSERT, UPDATE ON agentrt.%I TO %I', t, rt);
  END LOOP;

  -- Outbox: delivery bookkeeping only.
  EXECUTE format('GRANT INSERT, UPDATE (delivered_at, tries) ON agentrt.outbox TO %I', rt);

  -- Idempotency keys: written once, deleted after they expire.
  EXECUTE format('GRANT INSERT, DELETE ON agentrt.idempotency_key TO %I', rt);

  EXECUTE format('GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA agentrt TO %I', rt);
END $$;
