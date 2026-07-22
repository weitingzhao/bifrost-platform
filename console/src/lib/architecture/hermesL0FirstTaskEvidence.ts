/**
 * Agent dry-run of Hermes L0 first-task (Wave C Phase C2 prep).
 * Hermes session itself still needs Owner LLM key (C1) before stream ⑥ can close.
 */

export const HERMES_L0_FIRST_TASK_EVIDENCE = {
  taskId: 'hermes-mission-health-l0',
  autonomy: 'L0',
  agentDryRunAt: '2026-07-22T17:36:07Z',
  hermesSessionStatus: 'blocked_llm_key' as const,
  toolsInvoked: [
    'get_agent_bridge',
    'verify_mission_snapshot',
    'get_connectivity_matrix',
    'get_hermes_readiness',
    'get_hermes_first_task',
  ] as const,
  reportEnglish: {
    status:
      'Mission matrix NOMINAL for trade_dev/stg/prod (12/12). post_fix_verification.passed=false because verify_payload overall=DATA_LAYER (dev-local thin-client postgres/redis TCP refused). No PROBE_DRIFT. No HTTP_FAIL on trade envs.',
    datastore:
      'dev/stg/prod: NOMINAL (CNPG + redis). dev-local: DATA_LAYER — expected when Mac thin-client does not expose :30432/:30379; not a K3s trade datastore outage.',
    payloadMatrix:
      'probe_drift_count=0 · data_layer_count=1 (dev-local) · http_fail_count=0 · nominal_count=3',
    hermesBridge:
      'Nous Hermes gateway running v0.17.0 on .50:9119; llm_key_configured=false; platform MCP agent tools=29; legacy hermes_mcp :8782 unavailable (non-blocking for Nous path).',
    recommendedNextStep:
      'Owner: configure LLM key on Mac Mini .50 (~/.hermes/ or ANTHROPIC_API_KEY/OPENROUTER_API_KEY), re-probe GET /api/v1/agent/hermes/readiness → ready:true, then re-run first-task via Hermes (not Cursor) and paste report into Control Room / Briefing for stream ⑥ sign-off. Optional: ignore or retarget dev-local datastore probes if thin-client is intentional.',
  },
  ownerPasteHint:
    'Copy reportEnglish.* into Control Room or Briefing after Hermes live session (or cite this dry-run until LLM key is set).',
} as const
