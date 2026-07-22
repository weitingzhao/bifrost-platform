/**
 * Post-QA Wave F4 — Owner gate pack (materials only).
 * Agent must NOT mark spine CLOSED / W1–W3 / Hermes ⑥ / Mission Signal P3 without Owner action.
 */

export type PostQaOwnerGateId = 'w1-signoff' | 'a3-closed' | 'hermes-llm' | 'w2-gateway' | 'w3-readpath'

export type PostQaOwnerGate = {
  id: PostQaOwnerGateId
  title: string
  ownerAction: string
  agentPrep: string
  unlocks: string
  status: 'ready_for_owner' | 'blocked_external' | 'done'
}

export const POST_QA_OWNER_GATE_PACK_VERSION = '2026-07-22'

export const POST_QA_OWNER_GATES: PostQaOwnerGate[] = [
  {
    id: 'w1-signoff',
    title: 'TIBM W1 Owner sign-off',
    ownerAction: 'Completed 2026-07-22 — spine trade-ib-client-migration-rollout W1 signed',
    agentPrep:
      'TIBM_W1_STG_EVIDENCE ownerSignedAt=2026-07-22 · verify PASS · daemon replicas=0 · next_task=W2',
    unlocks: 'Wave G Phase G1 (W2 celery verify) — unlocked after Gateway session',
    status: 'done',
  },
  {
    id: 'a3-closed',
    title: 'ops-ui-actuation → CLOSED',
    ownerAction:
      'Completed 2026-07-22 — Owner waived runtime-observe-act-loop residual; spine ops-ui-actuation CLOSED',
    agentPrep:
      'Checklist: Network POLICY_NOMINAL · Cluster/Delivery/Promote/MCP done · runtime deferred by Owner waiver · D10 remains BLOCKED',
    unlocks: 'North star milestone closure',
    status: 'done',
  },
  {
    id: 'hermes-llm',
    title: 'Hermes readiness green (LLM key)',
    ownerAction:
      'On Mac Mini .50 configure LLM key in ~/.hermes/ or ANTHROPIC_API_KEY / OPENROUTER_API_KEY; confirm GET /api/v1/agent/hermes/readiness ready:true and nous_hermes.mcp_tool_count > 0',
    agentPrep:
      'Last probe: ready=false · LLM_KEY_MISSING · gateway v0.17.0 OK · L0 dry-run in hermesL0FirstTaskEvidence.ts. Re-probe after Owner configures key.',
    unlocks: 'Wave G Phase G3 (Hermes real L0 first-task → stream ⑥)',
    status: 'blocked_external',
  },
  {
    id: 'w2-gateway',
    title: 'IB Gateway session for celery bars RPC',
    ownerAction:
      'Completed 2026-07-22 — Console IB Gateway Reconnect cleared ghost session; verify-trade-ib-w2-stg PASS (fetch_bars_range OK)',
    agentPrep:
      'W2 signed · celery-worker 1/1 · bifrost-core 0.3.2 · accounts_snapshot populated · daemon replicas=0 · D10 BLOCKED · next=W3',
    unlocks: 'Wave G / TIBM W3 read-path domain work',
    status: 'done',
  },
  {
    id: 'w3-readpath',
    title: 'TIBM W3 read-only API domains',
    ownerAction:
      'Completed 2026-07-22 — Owner continued W3; verify-trade-ib-w3-stg PASS; spine done:3 closed',
    agentPrep:
      'TIBM_W3_STG_EVIDENCE · bifrost-core 0.3.3 · quotes NVDA OK · redis_ib E2E · daemon replicas=0 · D10 BLOCKED',
    unlocks: 'Optional STG soak / Hermes LLM gate / future D10 unlock program',
    status: 'done',
  },
]

export function buildPostQaOwnerGateMarkdown(): string {
  const lines = [
    `# Post-QA Owner gate pack (${POST_QA_OWNER_GATE_PACK_VERSION})`,
    '',
    'Agent completed Wave F + TIBM W1–W3. **a3-closed + w2-gateway + w3-readpath done 2026-07-22** — remaining Owner gate: hermes-llm (optional). D10 BLOCKED.',
    '',
    '| Gate | Owner action | Unlocks |',
    '|------|--------------|---------|',
  ]
  for (const g of POST_QA_OWNER_GATES) {
    lines.push(`| **${g.title}** (\`${g.id}\`) | ${g.ownerAction} | ${g.unlocks} |`)
  }
  lines.push('', '## Agent prep detail', '')
  for (const g of POST_QA_OWNER_GATES) {
    lines.push(`### ${g.title}`, g.agentPrep, '')
  }
  lines.push(
    '## Hard rules',
    '- D10 remains BLOCKED — no daemon scale, no live place_order, no Monitor live control',
    '- Agent does not forge Owner sign-offs or milestone CLOSED without explicit Owner command',
  )
  return lines.join('\n')
}
