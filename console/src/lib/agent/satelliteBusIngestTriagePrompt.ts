import type { Signal } from '@/lib/control-room/missionSignals'
import type { Reachability, SatelliteBusIngestService } from '@/api/types'

export const SATELLITE_BUS_INGEST_TRIAGE_SCOPE = 'satellite-bus-ingest-triage'

export function ingestActiveLabel(svc: SatelliteBusIngestService): string {
  if (svc.display_active != null && svc.display_active.trim() !== '') {
    return svc.display_active
  }
  return svc.process_active ?? 'unknown'
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
    if (status === 'inactive' || svc.reachability === 'fail') {
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
    `Triage Satellite Bus market ingest display vs actual runtime for **${input.env.toUpperCase()}** (${input.namespace}).`,
    '',
    '## Reported summary',
    `- Bus reachability: ${input.busReachability ?? 'unknown'}`,
    `- Market ingest: ${input.ingestHeadline}`,
    `- Monitor socket: ${input.socketHeadline}`,
    '',
    '## Agent workflow',
    '1. verify_mission_snapshot + GET /api/v1/satellite/bus-deep?env=' + input.env + ' (platform MCP).',
    '2. Cross-check ingest.services display_active/runtime_status vs monitor.socket.* vs GET plugins/ib-gateway/status.',
    '3. kubectl -n data get deploy ib-gateway; kubectl -n ' +
      input.namespace +
      ' get deploy massive-ws daemon — confirm replicas/intent.',
    '4. Classify each ingest row: **policy-off** (daemon scale 0, ws-disabled REST-only) | **managed-ok** (platform-ib-gateway / k8s heartbeat) | **real-degraded** | **false-alarm** (systemctl inactive on api-ops pod).',
    '5. Safe L1 actuation (operator token): rollout_restart deployment/ib-gateway -n data; rollout_restart deployment/massive-ws -n ' +
      input.namespace +
      ' when ws should be live.',
    '6. **D10 BLOCKED** — do NOT scale daemon, enable live trading, or remove daemon-scale-zero / observe-safe guards.',
    '',
    '## Close criteria',
    '- Ingest rows show semantic labels (managed@platform-ib-gateway, policy-off, ws-disabled) not bare inactive for expected STG topology.',
    '- monitor.socket.platform_ib_gateway reachability is not fail when plugin healthy.',
    '- Document any config gap (missing redis_ib block) before closing.',
  ].join('\n')
}
