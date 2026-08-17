import type { Signal } from '@/lib/control-room/missionSignals'
import type { Reachability } from '@/api/matrixTypes'
import type { SatelliteBusIngestService } from '@/api/satelliteBusTypes'

export const SATELLITE_BUS_INGEST_TRIAGE_SCOPE = 'satellite-bus-ingest-triage'

export function ingestActiveLabel(svc: SatelliteBusIngestService): string {
  if (svc.display_active != null && svc.display_active.trim() !== '') {
    return svc.display_active
  }
  return svc.process_active ?? 'unknown'
}

/** Compact engineer-facing runtime / source for Bus Status Market ingest table. */
export type IngestRuntimeView = {
  runtime: string
  source: string
  note?: string
  tone: 'ok' | 'warn' | 'fail' | 'muted'
}

export function ingestRuntimeView(svc: SatelliteBusIngestService): IngestRuntimeView {
  const status = (svc.runtime_status ?? '').toLowerCase()
  const display = (svc.display_active ?? svc.process_active ?? '').toLowerCase()

  let source = '—'
  if (
    svc.platform_gateway_managed ||
    display.includes('platform-ib-gateway') ||
    display.includes('ib-gateway')
  ) {
    source = 'ib-gateway'
  } else if (svc.runtime_externally_managed || display.includes('managed@k8s')) {
    source = 'k8s'
  } else if (svc.runtime_kind === 'kubernetes') {
    source = 'k8s'
  } else if (svc.runtime_kind === 'subprocess' || svc.runtime_kind === 'systemd') {
    source = 'local'
  } else if (svc.runtime_kind != null && svc.runtime_kind.trim() !== '') {
    source = svc.runtime_kind
  }

  if (status === 'policy-off') {
    let note: string | undefined
    if (display.includes('ws-disabled') || display.includes('rest-only')) note = 'REST-only'
    else if (display.includes('daemon scale')) note = 'daemon×0'
    return { runtime: 'policy-off', source, note, tone: 'muted' }
  }
  if (status === 'active') {
    return { runtime: 'active', source, tone: 'ok' }
  }
  if (status === 'degraded') {
    const note = display.includes('offline') ? 'offline' : undefined
    return { runtime: 'degraded', source, note, tone: 'warn' }
  }
  if (status === 'inactive') {
    const note = display.includes('offline') ? 'offline' : undefined
    return { runtime: 'inactive', source, note, tone: 'fail' }
  }

  // Fallback when API has not yet deployed runtime_status.
  if (svc.reachability === 'ok') return { runtime: 'active', source, tone: 'ok' }
  if (svc.reachability === 'degraded') return { runtime: 'degraded', source, tone: 'warn' }
  if (svc.reachability === 'fail') return { runtime: 'inactive', source, tone: 'fail' }
  return { runtime: svc.process_active ?? 'unknown', source, tone: 'muted' }
}

export function ingestDisplayTagVariant(
  runtimeStatus: string | undefined,
): 'success' | 'warning' | 'neutral' | 'danger' {
  switch ((runtimeStatus ?? '').toLowerCase()) {
    case 'active':
    case 'policy-off':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'inactive':
      return 'danger'
    default:
      return 'neutral'
  }
}

export type IngestSummaryStats = {
  total: number
  active: number
  managed: number
  policyOff: number
  degraded: number
  inactive: number
  headline: string
  signal: Signal
}

export function summarizeIngestServices(services: SatelliteBusIngestService[]): IngestSummaryStats {
  const total = services.length
  let active = 0
  let managed = 0
  let policyOff = 0
  let degraded = 0
  let inactive = 0

  for (const svc of services) {
    const status = (svc.runtime_status ?? '').toLowerCase()
    if (status === 'policy-off') {
      policyOff += 1
      continue
    }
    if (status === 'active') {
      active += 1
      if (svc.platform_gateway_managed || svc.runtime_externally_managed) managed += 1
      continue
    }
    if (status === 'degraded') {
      degraded += 1
      continue
    }
    if (status === 'inactive') {
      // Platform-gateway consumer marked offline in ops API while monitor may still show connected.
      if (svc.platform_gateway_managed && svc.reachability !== 'fail') {
        degraded += 1
        continue
      }
      inactive += 1
      continue
    }
    if (svc.reachability === 'fail') {
      inactive += 1
      continue
    }
    if (svc.reachability === 'degraded') degraded += 1
    else if (svc.reachability === 'ok') active += 1
  }

  const parts: string[] = []
  if (managed > 0) parts.push(`${managed} managed`)
  if (policyOff > 0) parts.push(`${policyOff} policy-off`)
  if (degraded > 0) parts.push(`${degraded} degraded`)
  if (inactive > 0) parts.push(`${inactive} inactive`)

  let signal: Signal = 'ok'
  if (inactive > 0) signal = 'fail'
  else if (degraded > 0) signal = 'degraded'
  else if (total === 0) signal = 'unknown'

  const headline =
    total === 0
      ? 'No ingest rows'
      : parts.length > 0
        ? parts.join(' · ')
        : `${active}/${total} active`

  return { total, active, managed, policyOff, degraded, inactive, headline, signal }
}

export function buildSatelliteBusIngestTriagePrompt(input: {
  env: 'dev' | 'stg' | 'prod'
  namespace: string
  ingestHeadline: string
  socketHeadline: string
  busReachability: Reachability | undefined
}): string {
  return [
    `Triage Satellite Bus Socket matrix vs Rocket IB gateway for **${input.env.toUpperCase()}** (${input.namespace}).`,
    '',
    '## Reported summary',
    `- Bus reachability: ${input.busReachability ?? 'unknown'}`,
    `- Monitor socket: ${input.socketHeadline}`,
    `- Legacy ingest rollup (secondary): ${input.ingestHeadline}`,
    '',
    '## Agent workflow',
    '1. verify_mission_snapshot + GET /api/v1/satellite/bus-deep?env=' + input.env + ' (platform MCP).',
    '2. Treat **monitor.socket** as authoritative — cross-check ib_ingestor / ib_account_agent / ib_operator / platform_ib_gateway vs GET plugins/ib-gateway/status.',
    '3. kubectl -n data get deploy ib-gateway; kubectl -n plugin-market-data get deploy polygon-ws-ingestor — confirm replicas/intent.',
    '4. Classify Rocket gateway: **ok** | **partial/observe** | **fail**; classify trade socket consumers separately (do not double-count shared Rocket into STG+PROD).',
    '5. Safe L1 actuation (operator token): rollout_restart deployment/ib-gateway -n data; Gateway reconnect via plugin when TWS slots need refresh.',
    '6. **D10 BLOCKED** — do NOT scale daemon, enable live trading, or remove daemon-scale-zero / observe-safe guards.',
    '',
    '## Close criteria',
    '- Bus Status Socket matrix and Task CC Shared Rocket agree on gateway health.',
    '- monitor.socket.platform_ib_gateway reachability is not fail when plugin healthy.',
    '- Document any config gap (missing redis_ib block) before closing.',
  ].join('\n')
}
