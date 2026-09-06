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
  fetchFlexOpsCheck,
  isProxyError,
  type FlexCheckResponse,
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
  /** GET /flex/ops/check — verdict per kind, next step, actions. Optional: older plugins lack it. */
  check?: FlexCheckResponse | null
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
    checkRes,
  ] = await Promise.allSettled([
    fetchDataHusbandry(),
    fetchFlexQueryStatus(),
    fetchFlexFreshnessKpis(),
    fetchFlexIngestQueueDashboard(),
    fetchFlexConfigSummary(),
    fetchFlexCoverageFreshness(),
    fetchFlexCoverageDbSummary(),
    fetchFlexOpsCheck(),
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
    check: checkRes.status === 'fulfilled' ? unwrapOk(checkRes.value) : null,
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

  push('## Self-check (GET /flex/ops/check — the plugin\'s own verdict, no IB request)')
  if (snap.check != null) {
    const c = snap.check
    push(`verdict: ${c.verdict} (${c.timezone}, ${c.generated_at})`, `next_step: ${c.next_step}`)
    for (const k of c.kinds) {
      push(`- ${k.kind}: ${k.verdict} — ${k.headline}`)
      if (k.detail) push(`    detail: ${k.detail}`)
      if (k.next_at) push(`    next_at: ${k.next_at}`)
      if (k.job) {
        push(
          `    job: #${k.job.id ?? '?'} ${k.job.status ?? '—'} attempts=${k.job.attempts ?? '?'}/${k.job.max_attempts ?? '?'}` +
            (k.job.error_category ? ` category=${k.job.error_category}` : '') +
            (k.job.not_before ? ` not_before=${k.job.not_before}` : '') +
            (k.job.manual ? ' manual' : ''),
        )
        if (k.job.error) push(`    error: ${k.job.error}`)
      }
      for (const a of k.actions) {
        push(
          `    action ${a.id}: ${a.method} ${a.path}${a.body ? ` ${JSON.stringify(a.body)}` : ''} — ${
            a.enabled ? 'available' : `disabled: ${a.reason ?? ''}`
          }`,
        )
      }
    }
    for (const chk of c.checks) push(`- check ${chk.id}: ${chk.ok ? 'ok' : 'ATTENTION'} — ${chk.detail}`)
  } else {
    push('unavailable: plugin predates 0.6.1 or the proxy failed — fall back to the sections below')
  }
  push('')

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
    '0. Start from the Self-check verdict above: it already names the cause, the next automatic step and the action that applies.',
    '1. waiting → IB has no statement yet; the worker retries at next_at (30 min steps, 8 attempts). POST run-now skips the wait.',
    '2. throttled → IB [1018]; every request before next_at fails again. Do nothing; do not enqueue; do not press Run now.',
    '3. failed/config → expired or invalid token / bad query id: rotate in IB Account Management, sync Flex tokens (make sync-flex-tokens, sets FLEX_TOKENS_ISSUED_AT), then enqueue.',
    '4. failed (attempts exhausted) → enqueue again; check the error text for a new IB code and classify it in worker/retry.py.',
    '5. missed → Dagster research_flex_morning_schedule (06:30 America/New_York Mon–Sat) did not enqueue; the worker catch-up does it 45 min after the slot, or enqueue now.',
    '6. check worker not ok → flex-query-worker heartbeat silent: kubectl -n plugin-flex-query logs deploy/flex-query-worker; it reconnects to Postgres on its own.',
    '7. Manual runs (Trade UI Flex Refresh / Console Manual) send one request per account; fallback:true widens and can trip [1018].',
    '8. Confirm ops_jobs.flex_ingest_freshness (last_ok / new_rows) + brokerage table ages after jobs done.',
    '9. Research OLAP: husbandry_gate blocks dbt when the last Flex attempt failed or no success in 96h.',
    '',
    '## Owner ask',
    'Propose the smallest durable fix, verify with the same endpoints this pack used, then report before/after reachability + freshness verdicts.',
  )

  return lines.join('\n')
}
