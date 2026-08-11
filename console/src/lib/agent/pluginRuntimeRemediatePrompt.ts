/**
 * Launch Plugin checklist Agent Fix — runtime repair (not publish / not plugin-launch).
 */

import type { IbGatewayStatusResponse, MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import type { PluginLaunchTargetId } from '@/lib/delivery/pluginLaunchEvidence'

export const PLUGIN_RUNTIME_REMEDIATE_SCOPE = 'plugin-runtime-remediate'

export interface PluginRuntimeRemediatePromptContext {
  target: PluginLaunchTargetId
  verdictTitle?: string
  verdictDetail?: string
  ibStatus?: IbGatewayStatusResponse
  marketDataStatus?: MarketDataStatusResponse
  operatorSurface?: string
}

export function buildPluginRuntimeRemediatePrompt(
  ctx: PluginRuntimeRemediatePromptContext,
): string {
  const surface = ctx.operatorSurface ?? 'Launch Desk · Launch Plugin · checklist'
  const ibHint = ctx.ibStatus?.hint ?? ''
  const snapshot =
    ctx.target === 'market-data'
      ? {
          target: 'market-data',
          reachable: ctx.marketDataStatus?.reachable ?? null,
          reachability: ctx.marketDataStatus?.reachability ?? null,
          summary: ctx.marketDataStatus?.summary ?? null,
          hint: ctx.marketDataStatus?.hint ?? null,
          verdict: { title: ctx.verdictTitle ?? null, detail: ctx.verdictDetail ?? null },
        }
      : {
          target: 'ib-gateway',
          mode: ctx.ibStatus?.mode ?? null,
          deployment: ctx.ibStatus?.deployment ?? null,
          reachability: ctx.ibStatus?.reachability ?? null,
          reachable: ctx.ibStatus?.reachable ?? null,
          summary: ctx.ibStatus?.summary ?? null,
          hint: ibHint || null,
          verdict: { title: ctx.verdictTitle ?? null, detail: ctx.verdictDetail ?? null },
        }

  const deployReady =
    ctx.target === 'ib-gateway' &&
    (ctx.ibStatus?.deployment?.ready === '1/1' ||
      String(ctx.ibStatus?.deployment?.ready ?? '').startsWith('1/'))
  const staleSnapshot =
    ctx.target === 'ib-gateway' &&
    (ibHint.toLowerCase().includes('dead tws') ||
      String(ctx.ibStatus?.summary ?? '')
        .toLowerCase()
        .includes('snapshot stale'))

  const targetLines =
    ctx.target === 'market-data'
      ? [
          'Target: **Market Data** plugin runtime (workers / API / CronJobs).',
          'Primary repair tool (always available): rollout_restart_deployment after approval.',
          'Optional: get_market_data_plugin_status if present on runner.',
          'Do NOT kubectl apply overlays unless Owner explicitly asks to republish (AI Launch Plugin).',
        ]
      : [
          'Target: **IB Gateway** plugin runtime (data/ib-gateway).',
          deployReady
            ? 'Deployment is Ready 1/1 — do NOT treat as missing install. Prefer reconnect / rollout_restart.'
            : 'Check deployment readiness before deciding reconnect vs republish.',
          staleSnapshot
            ? 'Console hint: account snapshot stale / dead TWS API client — primary action is reconnect (rollout restart ib-gateway).'
            : 'If reachability=fail with redis-ib ok, prefer reconnect over make install.',
          'Primary repair tool (always available): rollout_restart_deployment(namespace="data", kind="Deployment", name="ib-gateway") after approval.',
          'Optional if on runner: get_ib_gateway_plugin_status, ib_gateway_control(action=reconnect).',
          'Manage reconnect = repair. AI Launch Plugin / make install = publish — do not conflate.',
        ]

  return [
    'Repair Launch Plugin checklist NO-GO (runtime probe), not a publish.',
    'This is NOT plugin-launch and NOT Tekton deliver-platform / deliver-stg.',
    'D10: no place_order, no daemon scale-up.',
    '',
    `## Operator context (${surface})`,
    'The operator clicked **Agent Fix** on the Launch Plugin checklist.',
    ...targetLines,
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Workflow (strict)',
    '1. Detect — use the JSON snapshot above; optionally re-probe plugin status. Classify: deploy-not-ready vs dead TWS/snapshot-stale vs false-alarm.',
    '2. Approve — request_operator_approval before any write (reconnect / rollout_restart).',
    '3. Repair — IB with Ready deploy + stale snapshot: rollout_restart_deployment data/ib-gateway (same as plugin reconnect). Market Data: targeted rollout_restart in plugin-market-data* NS.',
    '4. Verify — wait ~30–90s; re-probe until reachability is not fail OR report TWS host still dead (needs Mac Mini TWS / human).',
    '5. If only a fresh image publish can fix it — stop and tell Owner to use **AI Launch Plugin**.',
    '',
    '## Must-not',
    '- Do not start bifrost-deliver-platform / bifrost-deliver-stg.',
    '- Do not kubectl set image as a bypass.',
    '- Do not run make install-ib-gateway for snapshot-stale when deploy is already 1/1.',
    '- Do not enable live trading / scale daemon (D10 BLOCKED).',
    '',
    'Begin with Detect, then request approval before Repair.',
  ].join('\n')
}
