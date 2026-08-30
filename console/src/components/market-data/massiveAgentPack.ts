/**
 * Build a clipboard pack for AI agents from the Massive Overview surface.
 * Prefer actionable facts + interpretation notes over raw JSON dumps.
 */

import { fetchDataHusbandry, type DataHusbandrySnapshot } from '@/api/dataHusbandry'
import {
  fetchCoverageDbSummary,
  fetchCoverageInventory,
  fetchIngestQueueDashboard,
  fetchQualityScore,
  fetchReadinessFinancialsByType,
  fetchUniverseCount,
  isProxyError,
  type CoverageDbSummary,
  type CoverageInventoryResponse,
  type IngestQueueDashboardResponse,
  type MarketDataProxyError,
  type UniverseCountResponse,
} from '@/api/marketDataPlugin'
import { fetchMarketDataStatus } from '@/api/network'
import type { MarketDataFreshnessInfo, MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import { buildAnalyticsDemand } from '@/components/market-data/analyticsDemandModel'
import { computeVerdict } from '@/components/market-data/dataVitalsModel'
import { sortFreshness } from '@/components/market-data/marketDataProbeUtils'

export type MassiveAgentPackSnapshot = {
  generatedAt: string
  husbandry: DataHusbandrySnapshot | null
  husbandryError: string | null
  plugin: MarketDataStatusResponse | null
  pluginError: string | null
  qualitySummary: string | null
  queue: IngestQueueDashboardResponse | null
  universe: UniverseCountResponse | null
  dbSummary: CoverageDbSummary | null
  inventory: CoverageInventoryResponse | null
  incomeStatementSymbols: number | null
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function freshLast(
  rows: Array<{ dimension?: string; last_run_at?: string | null }> | undefined,
  dimension: string,
): string | undefined {
  const row = rows?.find(f => (f.dimension ?? '').toLowerCase() === dimension.toLowerCase())
  const last = row?.last_run_at?.trim()
  return last || undefined
}

function unwrapOk<T>(data: T | MarketDataProxyError | null | undefined): T | null {
  if (data == null || isProxyError(data)) return null
  return data
}

export async function gatherMassiveAgentSnapshot(): Promise<MassiveAgentPackSnapshot> {
  const generatedAt = new Date().toISOString()
  const [
    husbandryRes,
    pluginRes,
    qualityRes,
    queueRes,
    universeRes,
    dbRes,
    invRes,
    finRes,
  ] = await Promise.allSettled([
    fetchDataHusbandry(),
    fetchMarketDataStatus(),
    fetchQualityScore(),
    fetchIngestQueueDashboard(),
    fetchUniverseCount(),
    fetchCoverageDbSummary(),
    fetchCoverageInventory(),
    fetchReadinessFinancialsByType(),
  ])

  const husbandry =
    husbandryRes.status === 'fulfilled' ? husbandryRes.value : null
  const husbandryError =
    husbandryRes.status === 'rejected'
      ? (husbandryRes.reason as Error)?.message ?? 'fetch failed'
      : null

  const plugin = pluginRes.status === 'fulfilled' ? pluginRes.value : null
  const pluginError =
    pluginRes.status === 'rejected'
      ? (pluginRes.reason as Error)?.message ?? 'fetch failed'
      : plugin?.error ?? null

  const quality = qualityRes.status === 'fulfilled' ? unwrapOk(qualityRes.value) : null
  const queue = queueRes.status === 'fulfilled' ? unwrapOk(queueRes.value) : null
  const universe = universeRes.status === 'fulfilled' ? unwrapOk(universeRes.value) : null
  const dbSummary = dbRes.status === 'fulfilled' ? unwrapOk(dbRes.value) : null
  const inventory = invRes.status === 'fulfilled' ? unwrapOk(invRes.value) : null
  const financials = finRes.status === 'fulfilled' ? unwrapOk(finRes.value) : null

  return {
    generatedAt,
    husbandry,
    husbandryError,
    plugin,
    pluginError,
    qualitySummary:
      quality?.summary ?? (quality?.ok === true ? 'PASS' : quality != null ? 'FAIL' : null),
    queue,
    universe,
    dbSummary,
    inventory,
    incomeStatementSymbols: financials?.counts?.income_statement_symbols ?? null,
  }
}

export function buildMassiveAgentPack(snap: MassiveAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }

  push(
    '# Massive / Market Data Plugin — Agent repair pack',
    `Generated: ${snap.generatedAt}`,
    'Source: Ops Console → Subcontractors → Massive → Overview (Copy for Agent)',
    '',
    '## Goal',
    'Diagnose and fix red/yellow Massive Overview signals below.',
    'Prefer durable husbandry fixes (freshness evidence, probes, enqueue) over one-off page refreshes.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up.',
    'Ground truth = freshness / coverage / signal-health — not K8s Job Complete. void ≠ fail.',
    '',
  )

  push('## Data husbandry')
  if (snap.husbandry != null) {
    push(`overall: ${snap.husbandry.overall} — ${snap.husbandry.detail}`)
    for (const lane of snap.husbandry.lanes ?? []) {
      const src = lane.source ? ` · src=${lane.source}` : ''
      push(`- ${lane.id}: ${lane.verdict} — ${lane.detail}${src}`)
    }
    if (snap.husbandry.note) push(`note: ${snap.husbandry.note}`)
  } else {
    push(`unavailable: ${snap.husbandryError ?? 'no snapshot'}`)
  }
  push('')

  push('## Plugin reach (platform-api probe)')
  if (snap.plugin != null) {
    push(
      `reachability: ${snap.plugin.reachability ?? snap.plugin.reachable ?? '—'}`,
      `summary: ${snap.plugin.summary ?? '—'}`,
      `health_reachability: ${snap.plugin.health_reachability ?? '—'}`,
      `freshness_reachability: ${snap.plugin.freshness_reachability ?? '—'}`,
      `quality: ${snap.qualitySummary ?? '—'}`,
      `autonomy: ${snap.plugin.autonomy ?? '—'}`,
    )
    if (snap.pluginError) push(`error: ${snap.pluginError}`)
    const workers = snap.plugin.workers ?? []
    if (workers.length > 0) {
      push('workers:')
      for (const w of workers) {
        push(
          `  - pool=${w.pool ?? '?'} status=${w.status ?? '—'} done=${w.jobs_done ?? 0} fail=${w.jobs_failed ?? 0}` +
            (w.next_run_at ? ` next=${w.next_run_at}` : ''),
        )
      }
    }
    const deps = snap.plugin.deployments ?? []
    if (deps.length > 0) {
      push('deployments:')
      for (const d of deps) {
        push(
          `  - ${d.name ?? '?'}: ready=${d.ready ?? '—'} reach=${d.reachability ?? '—'}` +
            (d.detail ? ` detail=${d.detail}` : ''),
        )
      }
    }
  } else {
    push(`unavailable: ${snap.pluginError ?? 'no probe'}`)
  }
  push('')

  push('## Queue dashboard (ingest husbandry)')
  if (snap.queue != null) {
    const hus = snap.queue.husbandry
    push(
      `husbandry: ${hus?.verdict ?? '—'} — ${hus?.detail ?? '—'}`,
      `schedule: ${snap.queue.schedule?.verdict ?? '—'} (on_plan=${snap.queue.schedule?.on_plan ?? 0} due=${snap.queue.schedule?.due ?? 0} missed=${snap.queue.schedule?.missed ?? 0})`,
      `queue: ${snap.queue.queue?.verdict ?? '—'} pending=${snap.queue.queue?.pending ?? 0} running=${snap.queue.queue?.running ?? 0}`,
      `throughput_15m: done=${snap.queue.throughput?.done_last_15m ?? 0} fail=${snap.queue.throughput?.failed_last_15m ?? 0} eta_min=${snap.queue.throughput?.eta_minutes_at_current_rate ?? '—'}`,
    )
    const missed = (snap.queue.schedule?.slots ?? []).filter(s => s.adherence === 'missed')
    if (missed.length > 0) {
      push('missed slots:')
      for (const s of missed.slice(0, 12)) {
        push(`  - ${s.slot}: ${s.detail ?? s.adherence}`)
      }
    }
  } else {
    push('unavailable: queue-dashboard fetch failed or proxy error')
  }
  push('')

  const dbFresh = snap.dbSummary?.freshness
  const probeFresh = sortFreshness(
    (snap.plugin?.freshness ?? []) as MarketDataFreshnessInfo[],
  )
  const tickerLast =
    freshLast(dbFresh, 'ticker_sync') ??
    freshLast(
      probeFresh.map(f => ({ dimension: f.dimension, last_run_at: f.last_run_at })),
      'ticker_sync',
    )
  const stockLast =
    freshLast(dbFresh, 'stock_daily') ??
    freshLast(
      probeFresh.map(f => ({ dimension: f.dimension, last_run_at: f.last_run_at })),
      'stock_daily',
    )
  const optionLast =
    freshLast(dbFresh, 'option_contract') ??
    freshLast(
      probeFresh.map(f => ({ dimension: f.dimension, last_run_at: f.last_run_at })),
      'option_contract',
    )
  const uniVerdict = computeVerdict(tickerLast)
  const stockVerdict = computeVerdict(stockLast)
  const optionVerdict = computeVerdict(optionLast)

  push('## Stock summary (today = UTC date of last_run_at)')
  push(
    `Universe: ${uniVerdict.text} — tickers=${snap.universe?.total_tickers ?? '—'} · ticker_sync=${tickerLast ?? 'none'}`,
    `Stock Daily: ${stockVerdict.text} — rows=${snap.dbSummary?.counts?.stock_daily ?? '—'} · stock_daily=${stockLast ?? 'none'}`,
    `Option Contracts: ${optionVerdict.text} — contracts=${snap.dbSummary?.counts?.option_contract ?? '—'} · option_contract=${optionLast ?? 'none'}`,
    'Note: Missing on Universe means ticker_sync did not run today — not that the ticker table is empty.',
    '',
  )

  const demand = buildAnalyticsDemand({
    freshness: probeFresh,
    inventory: snap.inventory,
    incomeStatementSymbols: snap.incomeStatementSymbols,
  })
  push(
    '## Analytics demand (Research feedstock)',
    `ready=${demand.ready} thin=${demand.thin} blocked=${demand.blocked} unknown=${demand.unknown}`,
    `feeds: snapshot=${snap.inventory?.option?.snapshot_symbols ?? '—'} oi=${snap.inventory?.option?.oi_symbols ?? '—'} stock_daily_symbols=${snap.inventory?.stock_daily?.symbols ?? '—'} income=${snap.incomeStatementSymbols ?? '—'}`,
  )
  if (snap.inventory == null) {
    push(
      'WARNING: coverage/inventory unavailable — UI shows Snapshot/OI/Stock daily as "—" and products become blocked.',
    )
  }
  for (const row of demand.rows) {
    const inputs = row.inputs
      .map(i => `${i.label}=${i.count ?? '—'}(${i.freshnessVerdict ?? i.lastRunAt ?? 'no fresh'})`)
      .join(', ')
    push(`- ${row.title}: ${row.level} — needs ${row.needs} · ${inputs}`)
  }
  push(
    'Note: blocked = all required feedstock counts are null/0. Fix inventory/proxy or enqueue feedstock; do not treat as Research engine crash.',
    '',
  )

  push('## Freshness dimensions (plugin probe)')
  const notOk = probeFresh.filter(f => {
    const v = (f.verdict ?? '').toLowerCase()
    return v !== 'ok' && v !== 'success' && v !== 'active'
  })
  push(`ok=${probeFresh.length - notOk.length}/${probeFresh.length}`)
  if (notOk.length > 0) {
    push('not ok:')
    for (const f of notOk) {
      push(
        `  - ${f.dimension}: verdict=${f.verdict} last=${f.last_run_at ?? '—'} age_h=${f.age_hours ?? '—'}`,
      )
    }
  } else if (probeFresh.length === 0) {
    push('(no freshness rows on probe)')
  }
  push('')

  push(
    '## Suggested investigation order',
    '1. If Analytics Demand blocked with Snapshot/OI/Stock daily = — → fix /market/coverage/inventory (or proxy) first.',
    '2. If Market batch missed → check queue-dashboard missed slots (trim needs job_trim freshness; EOD needs job evidence).',
    '3. If Market batch draining → let workers drain OR triage fail kinds; do not Operate on mere due.',
    '4. If Universe Missing → check Cron/reference ticker_sync last_run_at (UTC day), not ticker count.',
    '5. If freshness not-ok on ratios/short_* → check source_void / vendor void (void ≠ fail) before gap-heal panic.',
    '6. Research OLAP degraded → Dagster research_trading_day + husbandry_gate (Flex source=secret; Market not missed/degraded).',
    '',
    '## Owner ask',
    'Propose the smallest durable fix, verify with the same endpoints this pack used, then report before/after lane verdicts.',
  )

  return lines.join('\n')
}
