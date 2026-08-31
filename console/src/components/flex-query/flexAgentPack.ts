/**
 * Clipboard pack for AI agents from the IB Flex Overview surface.
 * Mirrors Massive Copy for Agent — actionable facts, no secrets.
 */

import { fetchDataHusbandry, type DataHusbandrySnapshot } from '@/api/dataHusbandry'
import {
  fetchFlexConfigSummary,
  fetchFlexCoverageDbSummary,
  fetchFlexCoverageFreshness,
  fetchFlexFreshnessKpis,
  fetchFlexIngestQueueDashboard,
  isProxyError,
  type FlexConfigSummary,
  type FlexCoverageDbSummary,
  type FlexFreshnessKpis,
  type FlexFreshnessResponse,
  type FlexQueueDashboardResponse,
  type FlexQueryProxyError,
} from '@/api/flexQueryPlugin'
import { fetchFlexQueryStatus } from '@/api/network'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import {
  analyzeFlexProbe,
  buildFlexDiagnosePrefill,
  type FlexRemediationAnalysis,
} from '@/lib/flex-query/flexQueryRemediation'

export type FlexAgentPackSnapshot = {
  generatedAt: string
  husbandry: DataHusbandrySnapshot | null
  husbandryError: string | null
  plugin: MarketDataStatusResponse | null
  pluginError: string | null
  analysis: FlexRemediationAnalysis
  kpis: FlexFreshnessKpis | null
  queue: FlexQueueDashboardResponse | null
  config: FlexConfigSummary | null
  coverageFreshness: FlexFreshnessResponse | null
  dbSummary: FlexCoverageDbSummary | null
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function unwrapOk<T>(data: T | FlexQueryProxyError | null | undefined): T | null {
  if (data == null || isProxyError(data)) return null
  return data
}

export async function gatherFlexAgentSnapshot(): Promise<FlexAgentPackSnapshot> {
  const generatedAt = new Date().toISOString()
  const [
    husbandryRes,
    pluginRes,
    kpisRes,
    queueRes,
    configRes,
    freshRes,
    dbRes,
  ] = await Promise.allSettled([
    fetchDataHusbandry(),
    fetchFlexQueryStatus(),
    fetchFlexFreshnessKpis(),
    fetchFlexIngestQueueDashboard(),
    fetchFlexConfigSummary(),
    fetchFlexCoverageFreshness(),
    fetchFlexCoverageDbSummary(),
  ])

  const husbandry = husbandryRes.status === 'fulfilled' ? husbandryRes.value : null
  const husbandryError =
    husbandryRes.status === 'rejected'
      ? ((husbandryRes.reason as Error)?.message ?? 'fetch failed')
      : null

  const plugin = pluginRes.status === 'fulfilled' ? pluginRes.value : null
  const pluginError =
    pluginRes.status === 'rejected'
      ? ((pluginRes.reason as Error)?.message ?? 'fetch failed')
      : plugin?.error ?? null

  const analysis = analyzeFlexProbe(plugin ?? undefined)

  return {
    generatedAt,
    husbandry,
    husbandryError,
    plugin,
    pluginError,
    analysis,
    kpis: kpisRes.status === 'fulfilled' ? unwrapOk(kpisRes.value) : null,
    queue: queueRes.status === 'fulfilled' ? unwrapOk(queueRes.value) : null,
    config: configRes.status === 'fulfilled' ? unwrapOk(configRes.value) : null,
    coverageFreshness: freshRes.status === 'fulfilled' ? unwrapOk(freshRes.value) : null,
    dbSummary: dbRes.status === 'fulfilled' ? unwrapOk(dbRes.value) : null,
  }
}

export function buildFlexAgentPack(snap: FlexAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }

  push(
    '# IB Flex Query Plugin — Agent repair pack',
    `Generated: ${snap.generatedAt}`,
    'Source: Ops Console → Subcontractors → IB Flex → Overview (Copy for Agent)',
    '',
    '## Goal',
    'Diagnose and fix red/yellow IB Flex Overview signals below.',
    'Prefer durable husbandry fixes (freshness evidence, enqueue, worker health) over one-off page refreshes.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up.',
    'Ground truth = ops_jobs.flex_ingest_freshness + brokerage tables — not K8s Job Complete.',
    'Token source must be secret (K8s bifrost-flex-tokens); enqueue fail-closed when source=none.',
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
    )
    if (snap.pluginError) push(`error: ${snap.pluginError}`)
    if (snap.analysis.primaryCause) {
      push(`primary_cause: ${snap.analysis.primaryCause}`)
    }
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

  push('## Remediation findings')
  if (snap.analysis.findings.length === 0) {
    push('(none — probe healthy)')
  } else {
    for (const f of snap.analysis.findings) {
      push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
    if (snap.analysis.staleKinds.length > 0) {
      push(`stale_kinds_to_enqueue: ${snap.analysis.staleKinds.join(', ')}`)
    }
  }
  push('')

  push('## Freshness KPIs (Overview cards)')
  if (snap.kpis != null) {
    const k = snap.kpis
    push(
      `last_successful_sync: ${k.last_successful_sync.age_label} (${k.last_successful_sync.at ?? 'none'})`,
      `latest_execution: ${k.latest_execution.age_label} rows=${k.latest_execution.row_count ?? '—'} (${k.latest_execution.at ?? 'none'})`,
      `latest_transaction: ${k.latest_transaction.age_label} rows=${k.latest_transaction.row_count ?? '—'} (${k.latest_transaction.at ?? 'none'})`,
      `last_run: ${k.last_run.age_label} kind=${k.last_run.kind ?? '—'} status=${k.last_run.status ?? '—'}`,
      `next_scheduled: ${k.next_scheduled_run.until_label} slot=${k.next_scheduled_run.slot ?? '—'}`,
      `last_planned: ${k.last_planned.age_label} (${k.last_planned.at ?? 'none'})`,
    )
  } else {
    push('unavailable: freshness-kpis fetch failed or proxy error')
  }
  push('')

  push('## Ingest queue dashboard')
  if (snap.queue != null) {
    const c = snap.queue.counts
    push(
      `counts: pending=${c?.pending ?? 0} running=${c?.running ?? 0} done=${c?.done ?? 0} failed=${c?.failed ?? 0}`,
      `now: ${snap.queue.now ?? '—'}`,
    )
    const late = (snap.queue.slots ?? []).filter(s => s.late || s.adherence === 'missed')
    if (late.length > 0) {
      push('late/missed slots:')
      for (const s of late.slice(0, 8)) {
        push(
          `  - ${s.slot} kind=${s.kind} adherence=${s.adherence ?? '—'} last_job=${s.last_job?.status ?? 'none'}`,
        )
      }
    }
  } else {
    push('unavailable: queue-dashboard fetch failed or proxy error')
  }
  push('')

  push('## Config (no secrets)')
  if (snap.config != null) {
    const t = snap.config.tokens
    push(
      `token_source: ${snap.config.source ?? '—'}`,
      `host_token_set: ${t.host_token_set} last4=${t.host_token_last4 ?? '—'} src=${t.host_source ?? '—'}`,
      `secondary_token_set: ${t.secondary_token_set} last4=${t.secondary_token_last4 ?? '—'} src=${t.secondary_source ?? '—'}`,
      `range_days: default=${snap.config.range_days.default} init=${snap.config.range_days.init}`,
    )
    if (snap.config.query_rows.length > 0) {
      push('query_rows:')
      for (const q of snap.config.query_rows) {
        push(
          `  - ${q.purpose}: host_id=${q.query_host_id || '—'} secondary_id=${q.query_secondary_id || '—'} label=${q.query_label ?? '—'}`,
        )
      }
    }
  } else {
    push('unavailable: config/summary fetch failed or proxy error')
  }
  push('')

  push('## Coverage freshness / tables')
  if (snap.coverageFreshness?.dimensions != null && snap.coverageFreshness.dimensions.length > 0) {
    for (const d of snap.coverageFreshness.dimensions) {
      push(
        `- ${d.dimension}: latest_ts=${d.latest_ts ?? '—'} rows=${d.row_count ?? '—'} updated=${d.updated_at ?? '—'}`,
      )
    }
  } else {
    push('(no coverage freshness rows)')
  }
  if (snap.dbSummary?.tables != null && snap.dbSummary.tables.length > 0) {
    push('tables:')
    for (const t of snap.dbSummary.tables) {
      push(
        `  - ${t.name}: rows=${t.row_count ?? '—'} latest_ts=${t.latest_ts ?? '—'}`,
      )
    }
  }
  push('')

  push('## Prefill (same as Diagnose with Agent)')
  push(buildFlexDiagnosePrefill(snap.plugin ?? undefined, snap.analysis))
  push('')

  push(
    '## Suggested investigation order',
    '1. If token_source=none → sync Flex tokens (make sync-flex-tokens / Secret bifrost-flex-tokens); do not enqueue blind.',
    '2. If flex_batch healthy but Overview degraded → trust freshness/KPI cards; enqueue stale kinds or Flex Refresh on Manual.',
    '3. If worker fail count high + freshness stale → plugin-flex-query worker logs; restart flex-query-worker if stuck.',
    '4. If queue.running>0 for hours → stale running reclaim (worker startup / FLEX_STALE_RUNNING_SEC); do not spam enqueue.',
    '5. If last failures include [1018] → cool down ~30m before enqueue (Console blocks Enqueue during cooldown).',
    '6. If last_planned FAIL / Cron overdue → Dagster research_trading_day flex assets; Cron remains suspended.',
    '7. Confirm ops_jobs.flex_ingest_freshness + brokerage table ages after jobs done.',
    '8. Research OLAP may stay degraded until Flex + Market feedstock are healthy for husbandry_gate.',
    '',
    '## Owner ask',
    'Propose the smallest durable fix, verify with the same endpoints this pack used, then report before/after reachability + freshness verdicts.',
  )

  return lines.join('\n')
}
