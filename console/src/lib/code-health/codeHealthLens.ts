/**
 * Code Health planning lens — slack / ceiling / paydown queue.
 *
 * Gate language (CI ratchet): OVER = block merge. Unchanged here.
 * Planning language: slack = baseline − value. At ceiling (slack 0) is a
 * warning that the next regression will fail CI — not a fleet NO-GO.
 *
 * No weighted composite score. Dimensions are labels only.
 */

import type {
  CodeHealthMetricDto,
  CodeHealthReportDto,
  CodeHealthResponse,
} from '@/api/codeHealth'
import type { Signal } from '@/lib/control-room/missionSignals'

/** Planning dimensions — not weighted into a score. */
export type CodeHealthDimension =
  | 'size'
  | 'duplication'
  | 'contract'
  | 'image_spread'
  | 'unknown'

export type CodeHealthMetricLens = {
  metric: CodeHealthMetricDto
  /** baseline − value; negative when OVER. Lower-is-better metrics only. */
  slack: number
  atCeiling: boolean
  over: boolean
  improved: boolean
  dimension: CodeHealthDimension
  /** Slack change vs previous report for the same metric id; null if no prior. */
  deltaSlack: number | null
}

export type CodeHealthLens = {
  reported: boolean
  note?: string
  report: CodeHealthReportDto | null
  previous: CodeHealthReportDto | null
  metrics: CodeHealthMetricLens[]
  minSlack: number | null
  atCeilingCount: number
  overCount: number
  owedCount: number
  /** Metrics that need paydown: OVER first, then ascending slack. */
  paydownQueue: CodeHealthMetricLens[]
  /**
   * Planning lamp for page + sidebar (not Observability fleet).
   * unknown = not measured · fail = OVER · degraded = at ceiling · ok = headroom.
   */
  planningLamp: Signal
  planningTag: string
  planningTitle: string
  /** True when ≥2 reports exist so Δ slack is meaningful. */
  hasTrend: boolean
  /** Sum of deltaSlack across metrics that have a prior reading. */
  totalDeltaSlack: number | null
  /** Gate vs planning posture — no composite score. */
  posture: CodeHealthPosture
  /** Per-dimension rollups (labels only). */
  dimensionSummaries: CodeHealthDimensionSummary[]
  /** First paydown item, if any. */
  nextCut: CodeHealthMetricLens | null
}

/** Gate CLEAR/BLOCKED · Planning AT CEILING/HELD — never a weighted score. */
export type CodeHealthPosture = {
  gate: 'CLEAR' | 'BLOCKED' | 'UNKNOWN'
  planning: 'AT_CEILING' | 'HELD' | 'NOT_OBSERVED'
  /** One-line for sidebar / chips / Ask pack. */
  summaryLine: string
  /** Headroom fragment, e.g. "min slack 0 · 7/7 at ceiling". */
  headroomLine: string
  /** Trend fragment. */
  trendLine: string
  /** Next-cut fragment, or empty when queue is empty. */
  nextLine: string
}

export type CodeHealthDimensionSummary = {
  dimension: CodeHealthDimension
  label: string
  metricCount: number
  atCeilingCount: number
  overCount: number
  minSlack: number | null
  /** Short chip text, e.g. "Size 3c" or "Dup min+2". */
  chipLabel: string
}

/** Primary dimensions shown in the posture strip (unknown omitted unless alone). */
const PRIMARY_DIMENSIONS: CodeHealthDimension[] = [
  'size',
  'duplication',
  'contract',
  'image_spread',
]

const DIMENSION_ORDER: CodeHealthDimension[] = [
  'size',
  'duplication',
  'contract',
  'image_spread',
  'unknown',
]

export function resolveCodeHealthDimension(metricId: string): CodeHealthDimension {
  if (metricId.includes('oversized') || metricId.includes('.size.')) return 'size'
  if (metricId.includes('duplication')) return 'duplication'
  if (metricId.includes('contract')) return 'contract'
  if (metricId.includes('image')) return 'image_spread'
  return 'unknown'
}

export function metricSlack(m: CodeHealthMetricDto): number {
  return m.baseline - m.value
}

function previousById(
  previous: CodeHealthReportDto | null,
): Map<string, CodeHealthMetricDto> {
  const map = new Map<string, CodeHealthMetricDto>()
  for (const m of previous?.metrics ?? []) map.set(m.id, m)
  return map
}

function buildMetricLens(
  m: CodeHealthMetricDto,
  prior: CodeHealthMetricDto | undefined,
): CodeHealthMetricLens {
  const slack = metricSlack(m)
  const over = m.status === 'over'
  const improved = m.status === 'improved'
  const atCeiling = m.status === 'at_baseline'
  let deltaSlack: number | null = null
  if (prior != null) {
    deltaSlack = slack - metricSlack(prior)
  }
  return {
    metric: m,
    slack,
    atCeiling,
    over,
    improved,
    dimension: resolveCodeHealthDimension(m.id),
    deltaSlack,
  }
}

function comparePaydown(a: CodeHealthMetricLens, b: CodeHealthMetricLens): number {
  if (a.over !== b.over) return a.over ? -1 : 1
  if (a.slack !== b.slack) return a.slack - b.slack
  const da = DIMENSION_ORDER.indexOf(a.dimension)
  const db = DIMENSION_ORDER.indexOf(b.dimension)
  if (da !== db) return da - db
  return a.metric.repo.localeCompare(b.metric.repo)
}

function buildDimensionSummaries(metrics: CodeHealthMetricLens[]): CodeHealthDimensionSummary[] {
  const out: CodeHealthDimensionSummary[] = []
  for (const dim of PRIMARY_DIMENSIONS) {
    const rows = metrics.filter(m => m.dimension === dim)
    if (rows.length === 0) continue
    const atCeilingCount = rows.filter(m => m.atCeiling).length
    const overCount = rows.filter(m => m.over).length
    const minSlack = Math.min(...rows.map(m => m.slack))
    const short =
      dim === 'duplication' ? 'Dup' : dim === 'image_spread' ? 'Image' : dimensionLabel(dim)
    let chipLabel: string
    if (overCount > 0) {
      chipLabel = `${short} ${overCount} OVER`
    } else if (atCeilingCount > 0) {
      chipLabel = `${short} ${atCeilingCount}c`
    } else {
      chipLabel = `${short} min+${minSlack}`
    }
    out.push({
      dimension: dim,
      label: dimensionLabel(dim),
      metricCount: rows.length,
      atCeilingCount,
      overCount,
      minSlack,
      chipLabel,
    })
  }
  return out
}

function buildPosture(args: {
  reported: boolean
  metrics: CodeHealthMetricLens[]
  overCount: number
  atCeilingCount: number
  minSlack: number | null
  hasTrend: boolean
  totalDeltaSlack: number | null
  nextCut: CodeHealthMetricLens | null
  dimensionSummaries: CodeHealthDimensionSummary[]
}): CodeHealthPosture {
  if (!args.reported || args.metrics.length === 0) {
    return {
      gate: 'UNKNOWN',
      planning: 'NOT_OBSERVED',
      summaryLine: 'Gate UNKNOWN · Planning NOT OBSERVED',
      headroomLine: 'no readings',
      trendLine: 'NO TREND',
      nextLine: '',
    }
  }

  const gate: CodeHealthPosture['gate'] = args.overCount > 0 ? 'BLOCKED' : 'CLEAR'
  const planning: CodeHealthPosture['planning'] =
    args.minSlack != null && args.minSlack <= 0 ? 'AT_CEILING' : 'HELD'

  const headroomLine =
    args.minSlack == null
      ? 'no readings'
      : `min slack ${args.minSlack} · ${args.atCeilingCount}/${args.metrics.length} at ceiling`

  const dimPart =
    args.dimensionSummaries.length > 0
      ? args.dimensionSummaries.map(d => d.chipLabel).join(' · ')
      : '—'

  let trendLine: string
  if (!args.hasTrend) {
    trendLine = 'NO TREND'
  } else if (args.totalDeltaSlack == null) {
    trendLine = 'Δslack —'
  } else if (args.totalDeltaSlack === 0) {
    trendLine = 'Δslack 0'
  } else {
    trendLine =
      args.totalDeltaSlack > 0 ? `Δslack +${args.totalDeltaSlack}` : `Δslack ${args.totalDeltaSlack}`
  }

  let nextLine = ''
  if (args.nextCut != null) {
    nextLine = `Next: ${args.nextCut.metric.label} · ${args.nextCut.metric.repo} (slack ${args.nextCut.slack})`
  }

  const planningWord = planning === 'AT_CEILING' ? 'AT CEILING' : planning
  const summaryLine = `Gate ${gate} · Planning ${planningWord} · ${headroomLine}`

  return {
    gate,
    planning,
    summaryLine,
    headroomLine: `${headroomLine} · ${dimPart}`,
    trendLine,
    nextLine,
  }
}

function emptyLens(note?: string): CodeHealthLens {
  const posture = buildPosture({
    reported: false,
    metrics: [],
    overCount: 0,
    atCeilingCount: 0,
    minSlack: null,
    hasTrend: false,
    totalDeltaSlack: null,
    nextCut: null,
    dimensionSummaries: [],
  })
  return {
    reported: false,
    note,
    report: null,
    previous: null,
    metrics: [],
    minSlack: null,
    atCeilingCount: 0,
    overCount: 0,
    owedCount: 0,
    paydownQueue: [],
    planningLamp: 'unknown',
    planningTag: 'NOT OBSERVED',
    planningTitle: 'Code Health: never scanned — treat as NOT OBSERVED',
    hasTrend: false,
    totalDeltaSlack: null,
    posture,
    dimensionSummaries: [],
    nextCut: null,
  }
}

/**
 * Build planning lens from a Code Health API response.
 * `history[0]` is typically the latest; previous is the next distinct report.
 */
export function buildCodeHealthLens(response: CodeHealthResponse | null | undefined): CodeHealthLens {
  if (response == null) {
    return emptyLens('Code-health report not loaded')
  }
  if (!response.reported || response.latest == null) {
    return emptyLens(response.note)
  }

  const report = response.latest
  const history = response.history ?? []
  // Prefer the first history entry that is not the same received_at as latest.
  const previous =
    history.find(h => h.received_at !== report.received_at || h.commit !== report.commit) ??
    (history.length > 1 ? history[1] : null)

  const priorMap = previousById(previous)
  const metrics = report.metrics.map(m => buildMetricLens(m, priorMap.get(m.id)))

  const overCount = metrics.filter(m => m.over).length
  const atCeilingCount = metrics.filter(m => m.atCeiling).length
  const owedCount = metrics.filter(m => m.improved).length
  const minSlack = metrics.length > 0 ? Math.min(...metrics.map(m => m.slack)) : null

  const paydownQueue = metrics
    .filter(m => m.over || m.atCeiling)
    .slice()
    .sort(comparePaydown)
  const nextCut = paydownQueue[0] ?? null
  const dimensionSummaries = buildDimensionSummaries(metrics)

  const hasTrend = previous != null
  const deltas = metrics.map(m => m.deltaSlack).filter((d): d is number => d != null)
  const totalDeltaSlack = deltas.length > 0 ? deltas.reduce((a, b) => a + b, 0) : null

  const posture = buildPosture({
    reported: true,
    metrics,
    overCount,
    atCeilingCount,
    minSlack,
    hasTrend,
    totalDeltaSlack,
    nextCut,
    dimensionSummaries,
  })

  let planningLamp: Signal
  let planningTag: string
  let planningTitle: string

  if (overCount > 0) {
    planningLamp = 'fail'
    planningTag = `${overCount} OVER BASELINE`
    planningTitle = posture.summaryLine
  } else if (minSlack != null && minSlack <= 0) {
    planningLamp = 'degraded'
    planningTag = `${atCeilingCount} AT CEILING · MIN SLACK ${minSlack}`
    planningTitle = posture.summaryLine
  } else if (metrics.length === 0) {
    planningLamp = 'unknown'
    planningTag = 'NOT OBSERVED'
    planningTitle = posture.summaryLine
  } else {
    planningLamp = 'ok'
    planningTag = 'HELD'
    planningTitle = posture.summaryLine
  }

  return {
    reported: true,
    note: response.note,
    report,
    previous,
    metrics,
    minSlack,
    atCeilingCount,
    overCount,
    owedCount,
    paydownQueue,
    planningLamp,
    planningTag,
    planningTitle,
    hasTrend,
    totalDeltaSlack,
    posture,
    dimensionSummaries,
    nextCut,
  }
}

export function dimensionLabel(d: CodeHealthDimension): string {
  switch (d) {
    case 'size':
      return 'Size'
    case 'duplication':
      return 'Duplication'
    case 'contract':
      return 'Contract'
    case 'image_spread':
      return 'Image spread'
    default:
      return 'Other'
  }
}

/** Format Δ slack for table cells. */
export function formatDeltaSlack(delta: number | null, hasTrend: boolean): string {
  if (!hasTrend || delta == null) return '—'
  if (delta === 0) return '0'
  return delta > 0 ? `+${delta}` : String(delta)
}
