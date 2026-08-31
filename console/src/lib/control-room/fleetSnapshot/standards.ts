/**
 * Fleet standard helpers (probe → board chips).
 */
import type { MatrixResponse, SelfHealthResponse } from '@/api/matrixTypes'
import type { StgSmokeResponse } from '@/api/deliveryTypes'
import { tradeReadinessTargets } from '@/lib/control-room/matrixSummary'
import {
  FLEET_STANDARD_GROUP_LABEL,
  FLEET_STANDARD_GROUP_ORDER,
  type FleetCell,
  type FleetCellGate,
  type FleetCellSignal,
  type FleetGroupRollup,
  type FleetStandard,
  type FleetStandardGroup,
  type FleetStandardSource,
} from '@/lib/control-room/fleetSnapshot/types'

export function std(
  id: string,
  label: string,
  signal: FleetCellSignal,
  reason: string,
  group: FleetStandardGroup,
  required = true,
  source: FleetStandardSource = 'probe',
): FleetStandard {
  return { id, label, signal, reason, group, required, source }
}

/** Structural / path-missing unavailable cells never enter FAIL/HOLD scoring. */
export function cellCountsTowardVerdict(cell: FleetCell): boolean {
  if (cell.countsTowardVerdict != null) return cell.countsTowardVerdict
  return cell.signal !== 'unavailable'
}
/** Required standards must all be green (ok) for GO. */
export function resolveCellGate(cell: FleetCell): FleetCellGate {
  if (!cellCountsTowardVerdict(cell)) return 'N/A'
  const required = cell.standards.filter(s => s.required !== false)
  if (required.length === 0) {
    return cell.signal === 'ok' ? 'GO' : 'NO-GO'
  }
  return required.every(s => s.signal === 'ok') ? 'GO' : 'NO-GO'
}

export function signalFromStandards(standards: FleetStandard[]): FleetCellSignal {
  const required = standards.filter(s => s.required !== false)
  if (required.length === 0) return 'unknown'
  if (required.every(s => s.signal === 'ok')) return 'ok'
  if (required.some(s => s.signal === 'fail')) return 'fail'
  if (required.some(s => s.signal === 'degraded')) return 'degraded'
  if (required.some(s => s.signal === 'unavailable')) return 'unavailable'
  return 'unknown'
}

/** Compact board: one row per group (ok/total + worst signal). */
export function rollupStandards(standards: FleetStandard[]): FleetGroupRollup[] {
  const map = new Map<FleetStandardGroup, FleetStandard[]>()
  for (const s of standards) {
    const list = map.get(s.group) ?? []
    list.push(s)
    map.set(s.group, list)
  }
  const out: FleetGroupRollup[] = []
  for (const group of FLEET_STANDARD_GROUP_ORDER) {
    const members = map.get(group)
    if (members == null || members.length === 0) continue
    const required = members.filter(m => m.required !== false)
    // Optional-only groups (e.g. Rocket RELEASE on DEV/PROD = N/A) still roll up for display.
    const scored = required.length > 0 ? required : members
    const ok = scored.filter(m => m.signal === 'ok').length
    const signal: FleetCellSignal =
      required.length > 0
        ? signalFromStandards(required)
        : scored.every(m => m.signal === 'ok')
          ? 'ok'
          : scored.some(m => m.signal === 'fail')
            ? 'fail'
            : scored.some(m => m.signal === 'degraded')
              ? 'degraded'
              : scored.some(m => m.signal === 'unavailable')
                ? 'unavailable'
                : 'unknown'
    out.push({
      group,
      label: FLEET_STANDARD_GROUP_LABEL[group],
      ok,
      total: scored.length,
      signal,
    })
  }
  return out
}

/** Group standards for Detail panel sections. */
export function groupStandards(
  standards: FleetStandard[],
): Array<{ group: FleetStandardGroup; label: string; items: FleetStandard[] }> {
  const map = new Map<FleetStandardGroup, FleetStandard[]>()
  for (const s of standards) {
    const list = map.get(s.group) ?? []
    list.push(s)
    map.set(s.group, list)
  }
  return FLEET_STANDARD_GROUP_ORDER.filter(g => map.has(g)).map(group => ({
    group,
    label: FLEET_STANDARD_GROUP_LABEL[group],
    items: map.get(group)!,
  }))
}

function labelProbeId(id: string): string {
  return id
    .replace(/^platform-api-/, 'platform-api · ')
    .replace(/^platform-console-/, 'console · ')
    .replace(/^argo-/, 'argo · ')
    .replace(/-/g, ' ')
}

function rocketProbeGroup(category: string, id: string): FleetStandardGroup {
  const c = category.toLowerCase()
  const i = id.toLowerCase()
  if (c === 'argo' || c === 'gitops' || i.includes('argo')) return 'gitops'
  return 'control'
}

/**
 * Rocket self-health standards — scoped to column env.
 * - stg/prod: only probes tagged that env (+ argo apps for that env when id matches)
 * - local (viewer DEV seat): roll up Control + GitOps from seat probes (may include remote URLs)
 */
export function standardsFromSelfProbes(
  self: SelfHealthResponse | undefined,
  scope: 'local' | 'dev' | 'stg' | 'prod',
): FleetStandard[] {
  if (!self) {
    return [std('self-health', 'Platform self-health', 'unknown', 'Probing…', 'control')]
  }

  let probes = self.probes
  if (scope === 'dev' || scope === 'stg' || scope === 'prod') {
    probes = self.probes.filter(p => {
      if (p.env === scope) return true
      // Argo apps often tagged by id suffix
      if (rocketProbeGroup(p.category, p.id) === 'gitops') {
        return p.id.toLowerCase().includes(scope) || p.env === scope
      }
      return false
    })
  }
  // scope === 'local': all seat probes (local platform-api view), still grouped Control/GitOps

  if (probes.length === 0) {
    return [
      std(
        `self-health-${scope}`,
        scope === 'local' ? 'Platform self-health' : `Platform self-health (${scope})`,
        'unknown',
        `No probes for scope=${scope}`,
        'control',
      ),
    ]
  }

  return probes.map(p =>
    std(
      p.id,
      labelProbeId(p.id),
      p.status as FleetCellSignal,
      p.detail || p.status,
      rocketProbeGroup(p.category, p.id),
    ),
  )
}

function satelliteTargetGroup(id: string, category: string): FleetStandardGroup {
  const i = id.toLowerCase()
  const c = category.toLowerCase()
  if (i.includes('nginx') || c.includes('edge') || i.includes('spa')) return 'edge'
  if (
    i.includes('postgres') ||
    i.includes('redis') ||
    c === 'datastore' ||
    c.includes('data')
  ) {
    return 'datastore'
  }
  return 'api'
}

export function standardsFromMatrix(matrix: MatrixResponse): FleetStandard[] {
  const targets = tradeReadinessTargets(matrix.targets)
  if (targets.length === 0) {
    return [std('matrix', 'Trade readiness targets', 'unknown', 'No scored targets', 'api')]
  }
  return targets.map(t =>
    std(
      t.id,
      t.id,
      t.reachability as FleetCellSignal,
      t.detail || t.reachability,
      satelliteTargetGroup(t.id, t.category),
    ),
  )
}

/** Single rollup standard for STG smoke (avoid listing every URL on the board). */
export function stgSmokeStandard(stg: StgSmokeResponse): FleetStandard {
  const ok = stg.targets.filter(t => t.reachability === 'ok').length
  const total = stg.targets.length
  const anyFail = stg.targets.some(t => t.reachability === 'fail')
  const anyDeg = stg.targets.some(t => t.reachability === 'degraded')
  const signal: FleetCellSignal = anyFail
    ? 'fail'
    : anyDeg
      ? 'degraded'
      : ok === total && total > 0
        ? 'ok'
        : 'degraded'
  return std(
    'stg-smoke',
    `STG smoke ${ok}/${total}`,
    signal,
    stg.targets.map(t => `${t.id}:${t.reachability}`).join(' · ') || 'No smoke targets',
    'release',
  )
}

