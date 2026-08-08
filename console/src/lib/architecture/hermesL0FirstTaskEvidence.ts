/**
 * Hermes L0 first-task evidence — live session 2026-08-08 (stream ⑥ close).
 * Session: Hermes dashboard 20260808_013539_3a2845 (deepseek/deepseek-chat).
 */

export const HERMES_L0_FIRST_TASK_EVIDENCE = {
  taskId: 'hermes-mission-health-l0',
  autonomy: 'L0',
  hermesSessionId: '20260808_013539_3a2845',
  hermesSessionTitle: 'Mission health read-only verification',
  liveRunAt: '2026-08-08T06:38:38Z',
  hermesSessionStatus: 'completed' as const,
  model: 'deepseek/deepseek-chat',
  toolsInvoked: [
    'get_agent_bridge',
    'get_connectivity_matrix',
    'verify_mission_snapshot',
    'verify_payload',
    'get_cluster_summary',
    'get_release_state',
  ] as const,
  reportEnglish: {
    status:
      'Hermes bridge READY. Nous gateway v0.17.0 running. Mission snapshot trade_dev/stg/prod 12/12. post_fix_verification.passed=true. No actuation.',
    datastore:
      'verify_payload overall=NOMINAL. probe_drift_count=0 · data_layer_count=0 · http_fail_count=0 · nominal_count=4 (dev / dev-local / stg / prod).',
    payloadMatrix:
      'All trade HTTP + postgres/redis ok. ops-capabilities unknown (no ops token, expected L0). ib-operator-rpc / daemon-control-write blocked by R-DV3 — correct.',
    hermesBridge:
      'Nous Hermes 0.17.0 on .50:9119; DeepSeek key is_set; mcp-server-platform 79 tools; tool_search disabled after first-task so verify_* stay first-class MCP tools.',
    recommendedNextStep: 'none — Mission healthy. Optional hygiene closed separately (gates re-run, Failed Tekton pod deleted, promtail bounce).',
  },
  caveats: [
    'First live session called verify_* via read-only GET after tool_search hid MCP schemas; subsequent sessions use tools.tool_search.enabled=false + synced mcp-platform.',
  ],
  ownerPasteHint: 'Live report is on Hermes session 20260808_013539_3a2845; stream hermes-gateway-integration ⑥ closed 2026-08-08.',
} as const
