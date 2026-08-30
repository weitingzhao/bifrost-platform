/**
 * Human-readable Research Engine health copy — shared by page strip + sidebar tooltip.
 * Never put SQL / table names in the OpsVerdictStrip summary.
 */

export type HealthLayerVerdict =
  | 'healthy'
  | 'due'
  | 'draining'
  | 'caution'
  | 'missed'
  | 'degraded'
  | 'unknown'

export type ResearchHealthCopyInput = {
  loading?: boolean
  reachable?: boolean
  statusError?: string
  marketVerdict?: string | null
  flexVerdict?: string | null
  batchVerdict?: string | null
  batchDetail?: string | null
  productOverall?: string | null
  /** Multi-schedule summary from orchestration/status (ops_dagster). */
  schedulesTotal?: number | null
  schedulesRunning?: number | null
  schedulesStopped?: number | null
  recentFailures?: Array<{ name?: string | null }> | null
}

export type ResearchHealthLayerView = {
  id: 'feedstock' | 'batch' | 'product'
  label: string
  verdict: HealthLayerVerdict
  meta: string
}

export type ResearchVerdictCopy = {
  lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
  summary: string
  layers: ResearchHealthLayerView[]
  /** Short tooltip for sidebar (same semantics as summary). */
  navSummary: string
}

const RANK: Record<string, number> = {
  healthy: 0,
  due: 1,
  draining: 1,
  caution: 2,
  unknown: 2,
  missed: 3,
  degraded: 3,
}

function norm(v: string | null | undefined): HealthLayerVerdict {
  const x = (v ?? '').toLowerCase().trim()
  if (
    x === 'healthy' ||
    x === 'due' ||
    x === 'draining' ||
    x === 'caution' ||
    x === 'missed' ||
    x === 'degraded' ||
    x === 'unknown'
  ) {
    return x
  }
  if (x === 'ok' || x === 'fresh') return 'healthy'
  if (x === 'empty' || x === 'stale') return 'degraded'
  return 'unknown'
}

function worse(a: HealthLayerVerdict, b: HealthLayerVerdict): HealthLayerVerdict {
  return (RANK[b] ?? 2) > (RANK[a] ?? 2) ? b : a
}

function productFromOverall(overall: string | null | undefined): HealthLayerVerdict {
  const o = (overall ?? '').toLowerCase()
  if (o === 'ok') return 'healthy'
  if (o === 'degraded' || o === 'empty') return 'degraded'
  return 'unknown'
}

/** Dense multi-schedule line for Batch layer / HusbandryStrip (English UI). */
export function formatSchedulesSummary(input: {
  schedulesTotal?: number | null
  schedulesRunning?: number | null
  schedulesStopped?: number | null
  recentFailures?: Array<{ name?: string | null }> | null
}): string | null {
  const total = input.schedulesTotal
  if (total == null || total <= 0) return null
  const stopped = input.schedulesStopped ?? 0
  const parts: string[] = [`${total} schedules`]
  if (stopped > 0) {
    parts.push(`${stopped} stopped`)
  } else if (input.schedulesRunning != null) {
    parts.push(`${input.schedulesRunning} running`)
  }
  const failName = input.recentFailures?.[0]?.name
  if (failName) parts.push(`last fail ${failName}`)
  return parts.join(' · ')
}

function batchSlaMeta(verdict: HealthLayerVerdict, detail: string | null | undefined): string {
  const d = (detail ?? '').toLowerCase()
  if (d.includes('permission denied')) return 'runs table permission denied'
  if (d.includes('schema missing')) return 'ops_dagster schema missing'
  if (d.includes('table missing')) return 'Dagster runs table not created yet'
  if (d.includes('not found') || d.includes('may not have started')) return 'orchestration unprobed'
  if (verdict === 'healthy') return 'trading_day within SLA'
  if (verdict === 'missed') return 'batch overdue vs 22:30 ET SLA'
  if (verdict === 'degraded') return 'last trading_day run failed'
  if (verdict === 'due') return 'trading_day run in progress'
  if (verdict === 'caution') return 'no recent trading_day run (within grace)'
  return 'batch status unknown'
}

function batchHumanMeta(input: ResearchHealthCopyInput, verdict: HealthLayerVerdict): string {
  const sla = batchSlaMeta(verdict, input.batchDetail)
  const sched = formatSchedulesSummary(input)
  if (sched != null) return `${sched} · ${sla}`
  return sla
}

function feedstockMeta(market: HealthLayerVerdict, flex: HealthLayerVerdict): string {
  const parts: string[] = []
  if (market !== 'healthy' && market !== 'unknown') {
    parts.push(`Market ${market.toUpperCase()}`)
  }
  if (flex !== 'healthy' && flex !== 'unknown') {
    parts.push(`Flex ${flex.toUpperCase()}`)
  }
  if (parts.length === 0) {
    if (market === 'unknown' && flex === 'unknown') return 'upstream unprobed'
    return 'Massive / Flex ok'
  }
  return `${parts.join(' · ')} (upstream)`
}

function layerPhrase(label: string, verdict: HealthLayerVerdict): string {
  if (verdict === 'healthy') return `${label} OK`
  if (verdict === 'unknown') return `${label} unprobed`
  return `${label} ${verdict.toUpperCase()}`
}

function toLamp(v: HealthLayerVerdict): ResearchVerdictCopy['lamp'] {
  if (v === 'healthy') return 'ok'
  if (v === 'due' || v === 'draining' || v === 'caution' || v === 'unknown') return 'degraded'
  return 'fail'
}

function toTag(v: HealthLayerVerdict): {
  tagLabel: string
  tagVariant: ResearchVerdictCopy['tagVariant']
} {
  if (v === 'healthy') return { tagLabel: 'HEALTHY', tagVariant: 'success' }
  if (v === 'due' || v === 'draining' || v === 'caution' || v === 'unknown') {
    return { tagLabel: v === 'unknown' ? 'UNKNOWN' : v.toUpperCase(), tagVariant: 'warning' }
  }
  return { tagLabel: v.toUpperCase(), tagVariant: 'danger' }
}

/** Rollup for OpsVerdictStrip / nav — Product + Batch only (not Market/Flex alone). */
function researchOlapRollup(
  product: HealthLayerVerdict,
  batch: HealthLayerVerdict,
): HealthLayerVerdict {
  if (product === 'healthy' && batch === 'unknown') {
    return 'caution'
  }
  return worse(product, batch)
}

export function buildResearchHealthLayers(
  input: ResearchHealthCopyInput,
): ResearchHealthLayerView[] {
  const market = norm(input.marketVerdict)
  const flex = norm(input.flexVerdict)
  const feedstock = worse(market, flex)
  const batch = norm(input.batchVerdict)
  const product = productFromOverall(input.productOverall)

  return [
    {
      id: 'feedstock',
      label: 'Feedstock',
      verdict: feedstock,
      meta: feedstockMeta(market, flex),
    },
    {
      id: 'batch',
      label: 'Batch',
      verdict: batch,
      meta: batchHumanMeta(input, batch),
    },
    {
      id: 'product',
      label: 'Product',
      verdict: product,
      meta:
        product === 'healthy'
          ? 'signal-health tables fresh'
          : product === 'degraded'
            ? 'feature tables stale or missing'
            : 'signal-health unprobed',
    },
  ]
}

export function buildResearchVerdictCopy(input: ResearchHealthCopyInput): ResearchVerdictCopy {
  if (input.loading) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: 'neutral',
      summary: 'Probing Research API and husbandry layers…',
      navSummary: 'Probing…',
      layers: buildResearchHealthLayers(input),
    }
  }
  if (input.reachable === false) {
    return {
      lamp: 'fail',
      tagLabel: 'UNREACHABLE',
      tagVariant: 'danger',
      summary: input.statusError || 'Research API unreachable',
      navSummary: 'API unreachable',
      layers: buildResearchHealthLayers(input),
    }
  }

  const layers = buildResearchHealthLayers(input)
  const product = layers.find(l => l.id === 'product')!.verdict
  const batch = layers.find(l => l.id === 'batch')!.verdict
  const feedstock = layers.find(l => l.id === 'feedstock')!
  const rollup = researchOlapRollup(product, batch)
  const { tagLabel, tagVariant } = toTag(rollup)

  const human = [
    layerPhrase('Product', product),
    layerPhrase('Batch', batch),
    feedstock.verdict !== 'healthy' && feedstock.verdict !== 'unknown'
      ? feedstock.meta
      : null,
  ]
    .filter(Boolean)
    .join(' · ')

  return {
    lamp: toLamp(rollup),
    tagLabel,
    tagVariant,
    summary: human,
    navSummary: human,
    layers,
  }
}

export function layerVerdictToLamp(
  verdict: HealthLayerVerdict,
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  return toLamp(verdict)
}
