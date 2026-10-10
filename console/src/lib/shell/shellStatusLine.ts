import type { ChecklistSignalsResponse } from '@/api/checklist'
import type { TelemetryAlertsResponse } from '@/api/clusterTypes'
import type { SelfHealthProbe, SelfHealthProbeStatus, SelfHealthResponse } from '@/api/matrixTypes'
import { normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { findStepByItemId } from '@/lib/control-room/dailyOpsChecklistCatalog'

/** Shell status reads this path and no environment query. */
export const SHELL_STATUS_PATH = '/api/v1/self-health'

const RANK: Record<SelfHealthProbeStatus, number> = {
  ok: 0,
  unknown: 1,
  degraded: 2,
  fail: 3,
}

/** Probes for the console's own seat. Other environments are not part of the sentence. */
export function probesForViewer(health: SelfHealthResponse): SelfHealthProbe[] {
  const env = normalizeViewerEnv(health.viewer_env)
  const matched = health.probes.filter(probe => probe.env.toLowerCase() === env)
  if (matched.length > 0) return matched
  return health.probes.filter(probe => probe.env === '')
}

function worstStatus(probes: readonly SelfHealthProbe[]): SelfHealthProbeStatus {
  let worst: SelfHealthProbeStatus = 'ok'
  for (const probe of probes) {
    if (RANK[probe.status] > RANK[worst]) worst = probe.status
  }
  return worst
}

export function shellViewerHealthy(health: SelfHealthResponse | undefined): boolean | undefined {
  if (health == null) return undefined
  const probes = probesForViewer(health)
  if (probes.length === 0) return health.overall === 'ok'
  return worstStatus(probes) === 'ok'
}

/** Header and Status share this verdict: worst of checklist, firing alerts, and the seat's self-health. */
export type VerdictTone = 'loading' | 'ok' | 'degraded' | 'failing' | 'unknown'

export type VerdictCause = {
  level: 'fail' | 'degraded'
  source: 'checklist' | 'alerts' | 'self-health'
  label: string
}

export type SystemVerdict = {
  tone: VerdictTone
  sentence: string
  causes: VerdictCause[]
  /** Sources that could not be read; All clear is never claimed while one is missing. */
  unreadable: string[]
}

type Source<T> = { data: T | undefined; loading: boolean; error: string | null }

export type VerdictInput = {
  health: Source<SelfHealthResponse>
  checklist: Source<ChecklistSignalsResponse>
  alerts: Source<TelemetryAlertsResponse>
}

export function checklistItemLabel(itemId: string): string {
  const step = findStepByItemId(itemId)
  return step?.items.find(item => item.id === itemId)?.label ?? itemId
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

function checklistCauses(body: ChecklistSignalsResponse): VerdictCause[] {
  const out: VerdictCause[] = []
  for (const row of body.signals) {
    if (row.signal !== 'fail' && row.signal !== 'degraded') continue
    out.push({ level: row.signal, source: 'checklist', label: checklistItemLabel(row.item_id) })
  }
  return out
}

function alertCauses(body: TelemetryAlertsResponse): VerdictCause[] {
  let critical = 0
  let warning = 0
  for (const alert of body.alerts) {
    if (alert.state !== 'firing') continue
    const severity = (alert.labels.severity ?? '').toLowerCase()
    if (severity === 'critical') critical += 1
    else if (severity === 'warning') warning += 1
  }
  const out: VerdictCause[] = []
  if (critical > 0) out.push({ level: 'fail', source: 'alerts', label: plural(critical, 'critical alert') })
  if (warning > 0) out.push({ level: 'degraded', source: 'alerts', label: plural(warning, 'warning alert') })
  return out
}

function healthCauses(body: SelfHealthResponse): VerdictCause[] {
  const out: VerdictCause[] = []
  for (const probe of probesForViewer(body)) {
    if (probe.status !== 'fail' && probe.status !== 'degraded') continue
    out.push({ level: probe.status, source: 'self-health', label: probe.id })
  }
  return out
}

function summarize(causes: VerdictCause[]): string {
  const shown = causes.slice(0, 2).map(cause => cause.label)
  const more = causes.length - shown.length
  return more > 0 ? `${shown.join(' · ')} · +${more} more` : shown.join(' · ')
}

/**
 * Critical alert or fail → Failing; warning alert or degraded → Degraded.
 * Unknown checklist rows (observe-only, local-only, stale) do not move the verdict.
 */
export function systemVerdict(input: VerdictInput): SystemVerdict {
  const sources = [
    { name: 'self-health', source: input.health },
    { name: 'checklist', source: input.checklist },
    { name: 'alerts', source: input.alerts },
  ] as const
  if (sources.some(({ source }) => source.loading && source.data == null)) {
    return { tone: 'loading', sentence: 'Checking health…', causes: [], unreadable: [] }
  }
  const unreadable = sources
    .filter(({ source }) => source.data == null || source.error != null)
    .map(({ name }) => name)
  const causes = [
    ...(input.checklist.data != null ? checklistCauses(input.checklist.data) : []),
    ...(input.health.data != null ? healthCauses(input.health.data) : []),
    ...(input.alerts.data != null ? alertCauses(input.alerts.data) : []),
  ]
  const failing = causes.filter(cause => cause.level === 'fail')
  if (failing.length > 0) {
    return { tone: 'failing', sentence: `Failing — ${summarize(failing)}`, causes, unreadable }
  }
  const degraded = causes.filter(cause => cause.level === 'degraded')
  if (degraded.length > 0) {
    return { tone: 'degraded', sentence: `Degraded — ${summarize(degraded)}`, causes, unreadable }
  }
  if (unreadable.length > 0) {
    return {
      tone: 'unknown',
      sentence: `Unknown — ${unreadable.join(', ')} unreadable`,
      causes,
      unreadable,
    }
  }
  return { tone: 'ok', sentence: 'All clear', causes, unreadable }
}

export const VERDICT_DOT_COLOR: Record<VerdictTone, string> = {
  loading: 'var(--color-lamp-gray)',
  ok: 'var(--color-lamp-green)',
  degraded: 'var(--color-lamp-yellow)',
  failing: 'var(--color-lamp-red)',
  unknown: 'var(--color-lamp-gray)',
}
