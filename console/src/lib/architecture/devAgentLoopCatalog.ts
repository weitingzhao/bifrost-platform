/**
 * Vision V2 — Dev Agent closed-loop contract.
 *
 * Authoritative for Ops Console → Architecture → Vision (V2 gate)
 * and Agent Briefing Dev-layer release discipline.
 */

export const DEV_AGENT_LOOP_VERSION = '2026-06-19'
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
    phase: 'Promote (optional)',
    actor: 'Promote mode Agent',
    action: 'POST deliver-prod after GET /api/v1/promote/release-gate pass + Owner sign-off',
    verify: 'Prod smoke + matrix green',
  },
]

export const DEV_AGENT_TEKTON_PIPELINES = {
  stg: 'bifrost-deliver-stg',
  prod: 'bifrost-deliver-prod',
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
    '',
    '## Pre-push script',
    `- \`${DEV_AGENT_PRE_PUSH_SCRIPT}\` — mandatory before push on trade-frontend changes`,
  ]
  return lines.join('\n')
}
