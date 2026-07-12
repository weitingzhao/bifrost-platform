import type { Signal } from '@/lib/control-room/missionSignals'
import { missionStatus } from '@/lib/control-room/missionSignals'

export const PROD_ENV_FIX_SCOPE = 'cluster_issues_full_auto'

export type ProdFixSignal = {
  label: string
  signal: Signal
  detail: string
}

export function buildPlatformProdFixPrompt(input: {
  prodOverall: Signal
  namespace: string
  signals: ProdFixSignal[]
}): string {
  const status = missionStatus(input.prodOverall)
  const failing = input.signals.filter(s => s.signal !== 'ok')

  return [
    `Fix Platform PROD environment readiness before any Platform release (current overall: ${status}).`,
    '',
    `Primary namespace: **${input.namespace}** (Ops Platform PROD workloads).`,
    '',
    '## Reported signals',
    ...input.signals.map(s => `- ${s.label} (${s.signal}): ${s.detail}`),
    '',
    '## Agent workflow',
    '1. Call verify_mission_snapshot (MCP) and GET cluster summary — focus failing pods in bifrost-platform-prod.',
    '2. For pod crashes: inspect logs/events, apply safe Cluster · Remediate actions (restart deployment, delete stuck pods) via platform-api actuation.',
    '3. For self-health PROD probes failing: verify platform-api/console NodePorts and HA replicas in bifrost-platform-prod.',
    '4. For PROD release gate failing: inspect gate history; fix underlying probe failures before re-running gate — do NOT bypass gates.',
    '5. For supply chain / Dockerfile CM issues: sync mirrors and refresh Kaniko ConfigMaps (Launch Rocket page).',
    '',
    failing.length > 0
      ? `Priority targets (${failing.length} non-ok): ${failing.map(f => f.label).join(', ')}.`
      : 'All listed signals degraded — treat as blocking until verify_mission_snapshot shows Prod nominal.',
    '',
    'Before closing: verify_mission_snapshot + post_fix_verification.passed must be true.',
    'Do not start bifrost-deliver-platform-prod until Prod readiness is NOMINAL.',
  ].join('\n')
}

export function buildTradeProdFixPrompt(input: {
  prodOverall: Signal
  stgNamespace: string
  prodNamespace: string
  signals: ProdFixSignal[]
}): string {
  const status = missionStatus(input.prodOverall)
  const failing = input.signals.filter(s => s.signal !== 'ok')

  return [
    `Fix Trade PROD environment readiness before Satellite deploy or promote (current overall: ${status}).`,
    '',
    `Primary namespace: **${input.prodNamespace}** · STG reference: ${input.stgNamespace}.`,
    '',
    '## Reported signals',
    ...input.signals.map(s => `- ${s.label} (${s.signal}): ${s.detail}`),
    '',
    '## Agent workflow',
    '1. verify_mission_snapshot + verify_payload (MCP) — matrix prod targets, IB socket bus, PG/Redis reachability.',
    '2. Failing pods in bifrost-prod: safe restart/delete via platform-api; check daemon observe-only guards (D10 — no live trade enable).',
    '3. IB socket / redis-ib: Satellite Bus page signals; fix ib-gateway plugin or socket workloads in prod namespace.',
    '4. Datastore fail: CNPG/Redis probes from matrix — fix data NS services before trade API rollout.',
    '5. PROD release gate: resolve smoke/matrix failures before re-run; never bypass gates.',
    '',
    failing.length > 0
      ? `Priority targets (${failing.length} non-ok): ${failing.map(f => f.label).join(', ')}.`
      : 'All listed signals degraded — treat as blocking until verify_mission_snapshot shows Trade Prod nominal.',
    '',
    'Before closing: verify_mission_snapshot + post_fix_verification.passed must be true.',
    'Do not run bifrost-deliver-prod until Prod readiness is NOMINAL.',
  ].join('\n')
}
