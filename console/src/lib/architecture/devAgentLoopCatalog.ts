/**
 * Vision V2 — Dev Agent closed-loop contract.
 *
 * Authoritative for Ops Console → Architecture → Vision (V2 gate)
 * and Agent Briefing Dev-layer release discipline.
 */

export const DEV_AGENT_LOOP_VERSION = '2026-06-26'
export const DEV_AGENT_LOOP_SOURCE = 'console/src/lib/architecture/devAgentLoopCatalog.ts'

export const DEV_AGENT_LOOP_STATEMENT =
  'Owner edits on Mac → Agent runs pre-push checks → git push → Tekton deliver-stg → STG smoke verify → report. ' +
  'Promote mode: deliver-prod only after release gate + Owner confirm.'

export type DevAgentLoopStep = {
  order: number
  phase: string
  actor: string
  action: string
  verify: string
}

export const DEV_AGENT_LOOP_STEPS: DevAgentLoopStep[] = [
  {
    order: 1,
    phase: 'Pre-push',
    actor: 'Dev Agent',
    action: 'Run scripts/agent-pre-push.sh (lint + build + check:legacy-css)',
    verify: 'Exit 0 before git push',
  },
  {
    order: 2,
    phase: 'Push',
    actor: 'Owner / Agent',
    action: 'git push to Gitea mirror branch',
    verify: 'Gitea webhook or manual Console → Delivery → Run',
  },
  {
    order: 3,
    phase: 'CI',
    actor: 'Tekton',
    action: 'POST /api/v1/delivery/pipelines/bifrost-deliver-stg/runs',
    verify: 'PipelineRun Succeeded (prepare → build → rollout → verify-stg)',
  },
  {
    order: 4,
    phase: 'Verify',
    actor: 'Dev Agent',
    action: 'GET /api/v1/delivery/stg/smoke — 9 API domains HTTP 200',
    verify: 'All stg smoke targets ok',
  },
  {
    order: 5,
    phase: 'Report',
    actor: 'Dev Agent',
    action: 'Summarize run id, image tags, smoke result to Owner',
    verify: 'Audit log + Delivery run history',
  },
  {
    order: 6,
    phase: 'Promote — Query State',
    actor: 'Promote Agent',
    action: 'GET /api/v1/promote/release-state → read next_action + available_actions',
    verify: 'consistent: true, stg_gate: pass',
  },
  {
    order: 7,
    phase: 'Promote — Deploy PROD',
    actor: 'Promote Agent',
    action: 'POST deliver-prod with same revision as STG (from release-state)',
    verify: 'PipelineRun Succeeded + prod smoke',
  },
  {
    order: 8,
    phase: 'Promote — PROD Gate',
    actor: 'Promote Agent',
    action: 'POST /api/v1/promote/release-gate?tier=platform-prod (admin)',
    verify: 'Gate pass + release-state all stages pass with same revision',
  },
]

export const DEV_AGENT_TEKTON_PIPELINES = {
  stg: 'bifrost-deliver-stg',
  prod: 'bifrost-deliver-prod',
  platformStg: 'bifrost-deliver-platform',
  platformProd: 'bifrost-deliver-platform-prod',
} as const

export const DEV_AGENT_MCP_TOOLS = {
  deployPipeline: 'start_pipeline_run',
  releaseState: 'get_release_state',
  releaseGate: 'get_release_gate',
  runReleaseGate: 'run_release_gate',
  stgSmoke: 'get_stg_smoke',
  revisions: 'get_delivery_revisions',
} as const

export const DEV_AGENT_PRE_PUSH_SCRIPT = 'bifrost-trade-frontend/scripts/agent-pre-push.sh'

export function buildDevAgentLoopLlmPack(): string {
  const lines = [
    '# Bifrost — Dev Agent Closed Loop (Vision V2)',
    `# Source: ${DEV_AGENT_LOOP_SOURCE} v${DEV_AGENT_LOOP_VERSION}`,
    '',
    DEV_AGENT_LOOP_STATEMENT,
    '',
    '## Loop steps',
    ...DEV_AGENT_LOOP_STEPS.map(s =>
      `${s.order}. **${s.phase}** (${s.actor}): ${s.action} → verify: ${s.verify}`),
    '',
    '## Tekton pipelines',
    `- STG: \`${DEV_AGENT_TEKTON_PIPELINES.stg}\``,
    `- PROD: \`${DEV_AGENT_TEKTON_PIPELINES.prod}\``,
    `- Platform STG: \`${DEV_AGENT_TEKTON_PIPELINES.platformStg}\``,
    `- Platform PROD: \`${DEV_AGENT_TEKTON_PIPELINES.platformProd}\``,
    '',
    '## MCP tools for release workflow',
    `- Deploy: \`${DEV_AGENT_MCP_TOOLS.deployPipeline}\` (name, revision?)`,
    `- Release state: \`${DEV_AGENT_MCP_TOOLS.releaseState}\` (tier?) → next_action + available_actions`,
    `- Gate status: \`${DEV_AGENT_MCP_TOOLS.releaseGate}\` (tier?)`,
    `- Run gate: \`${DEV_AGENT_MCP_TOOLS.runReleaseGate}\` (tier?) — admin only`,
    `- STG smoke: \`${DEV_AGENT_MCP_TOOLS.stgSmoke}\``,
    `- Available tags: \`${DEV_AGENT_MCP_TOOLS.revisions}\` (repos?)`,
    '',
    '## Pre-push script',
    `- \`${DEV_AGENT_PRE_PUSH_SCRIPT}\` — mandatory before push on trade-frontend changes`,
  ]
  return lines.join('\n')
}
