import type { Reachability, Target } from '@/api/matrixTypes'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import type { PayloadReadinessRow } from '@/lib/control-room/payloadReadiness'
import { worst } from '@/lib/control-room/missionSignals'
import type {
  SocketHealthMatrixRow,
  SocketHealthRow,
} from '@/lib/satellite/socketHealthSemantics'
import type { CriticalProcessRow } from '@/pages/satellite-bus/useSatelliteBusQueries'

export type ContextSectionSignal = {
  reach: Reachability
  /** Short chip: OK / WARN / FAIL / DRIFT / OBSERVE / … */
  label: string
  /** One-line why (shown on header when not OK). */
  detail?: string
}

export function contextSignalTagVariant(
  reach: Reachability,
): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (reach) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    default:
      return 'warning'
  }
}

function labelForReach(reach: Reachability): string {
  switch (reach) {
    case 'ok':
      return 'OK'
    case 'degraded':
      return 'WARN'
    case 'fail':
      return 'FAIL'
    default:
      return 'UNPROBED'
  }
}

function finish(
  reach: Reachability,
  detail?: string,
  labelOverride?: string,
): ContextSectionSignal {
  const uiReach = reach === 'unknown' ? 'degraded' : reach
  return {
    reach: uiReach,
    label: labelOverride ?? (reach === 'unknown' ? 'UNPROBED' : labelForReach(uiReach)),
    detail: uiReach === 'ok' ? undefined : detail,
  }
}

function cellSignalToReach(signal: string): Reachability | null {
  if (signal === 'fail' || signal === 'unavailable') return 'fail'
  if (signal === 'degraded') return 'degraded'
  if (signal === 'unknown') return 'unknown'
  return null
}

/** Shared Rocket + Ground — View · Shared segment lamp (no Evidence / matrix diverge). */
export function sharedContextSignal(
  rocket: SocketHealthRow,
  payloadRows: PayloadReadinessRow[],
): ContextSectionSignal {
  const reaches: Reachability[] = [rocket.reach]
  const notes: string[] = []
  if (rocket.reach !== 'ok' && rocket.reach !== 'unknown') {
    notes.push(`gateway ${rocket.reachLabel}`)
  }
  let divergeCount = 0
  for (const row of payloadRows) {
    if (row.envDiverges) {
      divergeCount += 1
      reaches.push('degraded')
    }
    for (const cell of [row.dev, row.stg, row.prod]) {
      const r = cellSignalToReach(cell.signal)
      if (r == null) continue
      reaches.push(r)
      if (r === 'fail' || r === 'degraded') {
        notes.push(`${row.label} ${cell.signal}`)
      }
    }
  }
  if (divergeCount > 0) {
    notes.push(`${divergeCount} payload env diverge`)
  }
  const reach = worst(...reaches)
  return finish(reach, notes[0])
}

/**
 * Cross-env socket matrix — View · Compare segment lamp.
 * Pure env diverge → label DRIFT (not WARN); required fail → FAIL.
 */
export function socketMatrixContextSignal(rows: SocketHealthMatrixRow[]): ContextSectionSignal {
  const reaches: Reachability[] = []
  let divergeCount = 0
  let failRequired = 0
  let degradedRequired = 0
  for (const row of rows) {
    if (row.envDiverges) {
      divergeCount += 1
      reaches.push('degraded')
    }
    for (const cell of [row.dev, row.stg, row.prod, row.local]) {
      if (cell.required === 'policy-off') continue
      if (cell.required !== 'required') {
        if (cell.reach === 'fail') reaches.push('degraded')
        continue
      }
      if (cell.reach === 'fail') {
        failRequired += 1
        reaches.push('fail')
      } else if (cell.reach === 'degraded') {
        degradedRequired += 1
        reaches.push('degraded')
      }
    }
  }
  const reach = reaches.length === 0 ? 'ok' : worst(...reaches)
  const parts: string[] = []
  if (divergeCount > 0) parts.push(`${divergeCount} diverged`)
  if (failRequired > 0) parts.push(`${failRequired} required fail`)
  if (degradedRequired > 0) parts.push(`${degradedRequired} required degraded`)
  const detail = parts.join(' · ') || undefined

  if (reach === 'ok') return finish('ok')
  if (failRequired > 0) return finish(reach, detail, 'FAIL')
  if (degradedRequired > 0 && divergeCount === 0) return finish(reach, detail, 'WARN')
  // Diverge-only (or diverge + soft issues) — do not say WARN next to HEALTHY bus.
  if (divergeCount > 0) return finish(reach, detail, 'DRIFT')
  return finish(reach, detail)
}

function blockReasonText(daemon: SatelliteBusDeepResponse['monitor']['daemon']): string | undefined {
  const reasons = daemon?.block_reasons
  if (Array.isArray(reasons) && reasons.length > 0) {
    return String(reasons[0])
  }
  return undefined
}

/**
 * D10 / observe-safe: daemon intentionally stopped (graceful shutdown) must not
 * paint Operate · Evidence as FAIL while Bus Health stays HEALTHY.
 */
export function isDaemonExpectedOff(
  daemon: SatelliteBusDeepResponse['monitor']['daemon'] | undefined,
): boolean {
  if (daemon == null) return false
  const hb = daemon.heartbeat
  if (hb == null || typeof hb !== 'object') return false
  const alive = (hb as { daemon_alive?: unknown }).daemon_alive
  const graceful = (hb as { graceful_shutdown_at?: unknown }).graceful_shutdown_at
  return alive === false && graceful != null && String(graceful).trim() !== ''
}

function isStandbyCriticalProcess(p: CriticalProcessRow): boolean {
  const blob = `${p.status} ${p.ready} ${p.name} ${p.label}`.toLowerCase()
  return (
    blob.includes('scaled to zero') ||
    blob.includes('standby') ||
    p.status.trim().toLowerCase() === 'not deployed'
  )
}

/**
 * Evidence · raw probes for selected NS (Operate fold).
 * Observe / D10 trading-arm yellow → OBSERVE (not WARN) when not hard-fail.
 * Does not feed View · Shared / Compare lamps.
 */
export function evidenceContextSignal(
  bus: SatelliteBusDeepResponse | undefined,
  tradeApiTargets: Target[],
  criticalProcesses: CriticalProcessRow[],
): ContextSectionSignal {
  if (bus == null) return finish('unknown', 'No bus-deep probe')
  const reaches: Reachability[] = []
  const notes: string[] = []
  let hardFail = false
  let observeOnly = false

  const push = (r: Reachability | undefined, note: string) => {
    if (r == null || r === 'ok') return
    reaches.push(r)
    notes.push(note)
    if (r === 'fail') hardFail = true
  }

  const daemon = bus.monitor.daemon
  const daemonExpectedOff = isDaemonExpectedOff(daemon)
  if (daemonExpectedOff) {
    reaches.push('degraded')
    notes.push('daemon expected off (D10 observe / graceful shutdown)')
    observeOnly = true
  } else {
    push(daemon?.reachability, `daemon ${daemon?.reachability}`)
    const self = (daemon?.self_check ?? '').toLowerCase()
    if (self === 'degraded' || self === 'blocked' || self === 'fail') {
      const r = self === 'blocked' || self === 'fail' ? 'fail' : 'degraded'
      reaches.push(r)
      notes.push(`self_check ${self}`)
      if (r === 'fail') hardFail = true
      else observeOnly = true
    }
    const lamp = (daemon?.lamp ?? '').toLowerCase()
    if (lamp === 'yellow' || lamp === 'red') {
      const r = lamp === 'red' ? 'fail' : 'degraded'
      reaches.push(r)
      notes.push(`lamp ${lamp}`)
      if (r === 'fail') hardFail = true
      else observeOnly = true
    }
    const br = blockReasonText(daemon)
    if (br != null) {
      reaches.push('degraded')
      notes.push(br)
      observeOnly = true
    }
  }
  push(bus.monitor.celery?.reachability, `celery ${bus.monitor.celery?.reachability}`)
  push(bus.monitor.account_sync?.reachability, `account_sync ${bus.monitor.account_sync?.reachability}`)
  push(bus.ops?.reachability, `ops ${bus.ops?.reachability}`)

  for (const t of tradeApiTargets) {
    if (t.reachability === 'fail' || t.reachability === 'degraded') {
      reaches.push(t.reachability)
      notes.push(`${t.id} ${t.reachability}`)
      if (t.reachability === 'fail') hardFail = true
    }
  }
  for (const p of criticalProcesses) {
    if (p.reachability !== 'fail' && p.reachability !== 'degraded') continue
    if (isStandbyCriticalProcess(p)) {
      reaches.push('degraded')
      notes.push(`${p.label} standby (D10)`)
      observeOnly = true
      continue
    }
    reaches.push(p.reachability)
    notes.push(`${p.label} ${p.reachability}`)
    if (p.reachability === 'fail') hardFail = true
  }

  const reach = reaches.length === 0 ? 'ok' : worst(...reaches)
  if (reach === 'ok') return finish('ok')
  if (hardFail) return finish(reach, notes[0], 'FAIL')
  if (observeOnly && reach === 'degraded') return finish(reach, notes[0], 'OBSERVE')
  return finish(reach, notes[0])
}
