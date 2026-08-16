/**
 * Launch Plugin ambient agent — Mission Launch third lane (not Tekton).
 * Targets: IB Gateway (make install) or Market Data (kubectl apply overlays).
 */

import type { IbGatewayStatusResponse, MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import type {
  PluginLaunchEvidence,
  PluginLaunchSeat,
  PluginLaunchTargetId,
} from '@/lib/delivery/pluginLaunchEvidence'
import {
  MARKET_DATA_IMAGE_TAG,
  PLUGIN_DOGFOOD_FEATURE,
  PLUGIN_DOGFOOD_REVISION,
  marketDataApplyCmd,
  marketDataNamespace,
  marketDataVerifyCmd,
} from '@/lib/delivery/pluginLaunchEvidence'

export const PLUGIN_LAUNCH_SCOPE = 'plugin-launch'

export interface PluginLaunchPromptContext {
  target: PluginLaunchTargetId
  seat: PluginLaunchSeat
  ibStatus?: IbGatewayStatusResponse
  marketDataStatus?: MarketDataStatusResponse
  evidence?: PluginLaunchEvidence
  outcomeKind?: 'released' | 'in_progress' | 'failed' | 'idle'
  outcomeDetail?: string
  operatorSurface?: string
}

function ibPrompt(ctx: PluginLaunchPromptContext): string {
  const surface = ctx.operatorSurface ?? 'Mission Launch · Launch Plugin'
  const snapshot = {
    target: 'ib-gateway',
    mode: ctx.ibStatus?.mode ?? null,
    deployment: ctx.ibStatus?.deployment ?? null,
    reachability: ctx.ibStatus?.reachability ?? null,
    summary: ctx.ibStatus?.summary ?? null,
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
    'Publish the Bifrost Platform IB Gateway plugin via the Launch Plugin lane.',
    'This is NOT a Tekton deliver-platform / deliver-stg pipeline.',
    'Executor (after Owner approval): cd bifrost-platform-plugin && make install-ib-gateway,',
    'then make verify-ib-gateway-program. Never kubectl set image as a bypass.',
    'D10: market-data / on-demand STK quotes only — no place_order, no daemon scale-up.',
    'Gallery observes runtime; this lane publishes.',
    '',
    `## Operator context (${surface})`,
    'The operator clicked **AI Launch Plugin** with target **IB Gateway**.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Workflow (strict)',
    '1. Detect — GET /api/v1/plugins/ib-gateway/status. Report mode, deployment.ready, reachability.',
    '2. Approve — request_operator_approval: "Publish IB Gateway plugin (make install-ib-gateway)?"',
    '3. Install — on approval: cd bifrost-platform-plugin && make install-ib-gateway (no kubectl set image).',
    '4. Verify — make verify-ib-gateway-program; restore mode=live if needed.',
    '5. Live check — mode=live + deploy ready; remind Owner Trade Live on-demand STK.',
    '',
    '## Must-not',
    '- Do not start bifrost-deliver-platform / bifrost-deliver-stg for plugin image publish.',
    '- Do not enable live trading / scale daemon (D10 BLOCKED).',
    '',
    'Begin with Detect, then request approval before Install.',
  ].join('\n')
}

function marketDataPrompt(ctx: PluginLaunchPromptContext): string {
  const seat = ctx.seat
  const ns = marketDataNamespace(seat)
  const apply = marketDataApplyCmd(seat)
  const verify = marketDataVerifyCmd(seat)
  const surface = ctx.operatorSurface ?? 'Mission Launch · Launch Plugin'
  const snapshot = {
    target: 'market-data',
    seat,
    namespace: ns,
    expected_image: `bifrost-market-data:${MARKET_DATA_IMAGE_TAG}`,
    apply_cmd: apply,
    verify_cmd: verify,
    platform_status_summary: ctx.marketDataStatus?.summary ?? null,
    platform_reachable: ctx.marketDataStatus?.reachable ?? null,
    evidence: ctx.evidence ?? null,
    console_view: {
      outcome: ctx.outcomeKind ?? 'unknown',
      detail: ctx.outcomeDetail ?? '',
    },
  }

  return [
    'Publish the Bifrost Market Data (Polygon) plugin via the Launch Plugin lane.',
    'This is NOT Tekton deliver-platform / deliver-stg, and NOT IB Gateway install.',
    `Seat: **${seat.toUpperCase()}** · namespace **${ns}** · image **bifrost-market-data:${MARKET_DATA_IMAGE_TAG}**.`,
    'Gallery observes freshness/workers; this lane publishes workers + API + CronJobs.',
    'D10: market-data REST ingest only — no place_order.',
    '',
    `## Operator context (${surface})`,
    'The operator clicked **AI Launch Plugin** with target **Market Data**.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Workflow (strict)',
    `1. Detect — kubectl -n ${ns} get deploy,cronjob; confirm current images. Platform GET /api/v1/plugins/market-data/status probes DEV NS only — for STG/PROD trust kubectl.`,
    `2. Approve — request_operator_approval: "Publish Market Data plugin to ${seat.toUpperCase()} (${ns}) at image ${MARKET_DATA_IMAGE_TAG}?"`,
    '3. Install/Apply — on approval run exactly:',
    `   ${apply}`,
    `   Ensure bifrost-market-data:${MARKET_DATA_IMAGE_TAG} is present on cluster nodes before apply (import if needed).`,
    '4. Verify — run:',
    `   ${verify}`,
    `   Acceptance: market-data-api 1/1 (STG/PROD), workers Ready, image tag ${MARKET_DATA_IMAGE_TAG}, expand CronJobs present (max-pain, atm-iv-pcr, stock-snapshot).`,
    '5. Live check — health ok; remind Owner Subcontractors → Market Data manage page for Coverage/Analytics.',
    '',
    '## Must-not',
    '- Do not apply archived overlays (k8s/overlays/_archived); single NS plugin-market-data.',
    '- Do not confuse IB Gateway make install with Market Data apply.',
    '- Do not enable live trading (D10 BLOCKED).',
    '',
    'Begin with Detect, then request approval before Apply.',
  ].join('\n')
}

export function buildPluginLaunchPrompt(ctx: PluginLaunchPromptContext): string {
  if (ctx.target === 'market-data') return marketDataPrompt(ctx)
  return ibPrompt(ctx)
}
