/**
 * Launch Plugin ambient agent — Mission Launch third lane (not Tekton).
 * Executor: cd bifrost-platform-plugin && make install-ib-gateway + verify-ib-gateway-program.
 * Approvals + checklist: Operator Dock (request_operator_approval / request_operator_manual_steps).
 */

import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { PluginLaunchEvidence } from '@/lib/delivery/pluginLaunchEvidence'
import {
  PLUGIN_DOGFOOD_FEATURE,
  PLUGIN_DOGFOOD_REVISION,
} from '@/lib/delivery/pluginLaunchEvidence'

export const PLUGIN_LAUNCH_SCOPE = 'plugin-launch'

export const PLUGIN_LAUNCH_AGENT_PROMPT = [
  'Publish the Bifrost Platform IB Gateway plugin via the Launch Plugin lane.',
  'This is NOT a Tekton deliver-platform / deliver-stg pipeline.',
  'Executor (after Owner approval): cd bifrost-platform-plugin && make install-ib-gateway,',
  'then make verify-ib-gateway-program. Never kubectl set image as a bypass.',
  'D10: market-data / on-demand STK quotes only — no place_order, no daemon scale-up.',
  'Gallery observes runtime; this lane publishes.',
].join(' ')

export interface PluginLaunchPromptContext {
  status?: IbGatewayStatusResponse
  evidence?: PluginLaunchEvidence
  outcomeKind?: 'released' | 'in_progress' | 'failed' | 'idle'
  outcomeDetail?: string
  operatorSurface?: string
}

export function buildPluginLaunchPrompt(ctx: PluginLaunchPromptContext): string {
  const surface = ctx.operatorSurface ?? 'Mission Launch · Launch Plugin'
  const snapshot = {
    mode: ctx.status?.mode ?? null,
    deployment: ctx.status?.deployment ?? null,
    reachability: ctx.status?.reachability ?? null,
    summary: ctx.status?.summary ?? null,
    evidence: ctx.evidence ?? null,
    console_view: {
      outcome: ctx.outcomeKind ?? 'unknown',
      detail: ctx.outcomeDetail ?? '',
    },
    dogfood: {
      revision: PLUGIN_DOGFOOD_REVISION,
      feature: PLUGIN_DOGFOOD_FEATURE,
      note: 'accounts_snapshot empty does NOT fail publish acceptance',
    },
  }

  return [
    PLUGIN_LAUNCH_AGENT_PROMPT,
    '',
    `## Operator context (${surface})`,
    'The operator clicked **AI Launch Plugin**. Approvals and install checklist live in Operator Dock.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Workflow (strict)',
    '1. Detect — call GET /api/v1/plugins/ib-gateway/status (or summarize from snapshot). Report mode, deployment.ready, reachability.',
    '2. Approve — request_operator_approval: "Publish IB Gateway plugin (make install-ib-gateway) for dogfood on-demand STK?"',
    '3. Install — on approval, request_operator_manual_steps with checklist:',
    '   - cd bifrost-platform-plugin && make install-ib-gateway',
    '   - Do NOT kubectl set image',
    '4. Verify — after install ack, request_operator_manual_steps:',
    '   - make verify-ib-gateway-program',
    '   - If cluster was live: POST /api/v1/plugins/ib-gateway/control/mode {"mode":"live"} with operator token',
    '5. Live check — confirm mode=live + deployment ready; remind Owner: Trade Live on-demand symbols > default 5.',
    '   Ghost TWS / accounts_snapshot empty are NOT publish failure conditions.',
    '',
    '## Must-not',
    '- Do not start bifrost-deliver-platform / bifrost-deliver-stg for plugin image publish.',
    '- Do not enable live trading / scale daemon (D10 BLOCKED).',
    '- Do not treat Plugin Gallery reconnect as publish.',
    '',
    'Begin with Detect, then request approval before Install.',
  ].join('\n')
}
