-- 0038: the two event reads the plan facts make find their few rows through an
-- index (LANE-BIZ-ALIGN, 2026-11-19). Only adds indexes; no row changes.
--
-- Measured on PROD 2026-11-19: readPlanFacts ran once per step whose rule names
-- external evidence, 14 steps per pass of agentrt-facts-rules, and each run read
-- all ~100,000 events twice to find 6 plan_view and 47 plan_observation rows.
-- Both indexes are partial, so ordinary event writes do not maintain them.
CREATE INDEX event_plan_view_idx ON agentrt.event (event_stream_id, seq DESC)
  WHERE body_json ? 'plan_view';

CREATE INDEX event_plan_observation_idx ON agentrt.event (event_id)
  WHERE body_json ? 'plan_observation' AND actor_id = 'controller';
