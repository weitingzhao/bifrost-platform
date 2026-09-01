/**
 * Clipboard / Agent Desk pack from the Research Engine surface.
 * Mirrors Massive Copy for Agent — actionable facts, no secrets.
 */

import { fetchDataHusbandry, type DataHusbandrySnapshot } from '@/api/dataHusbandry'
import {
  fetchElementaryStatus,
  fetchOrchestrationStatus,
  fetchResearchHealth,
  fetchResearchStatus,
  fetchSignalHealth,
  isResearchProxyError,
  type ElementaryStatus,
  type OrchestrationScheduleRow,
  type OrchestrationStatusData,
  type ResearchHealth,
  type ResearchProxyError,
  type ResearchStatus,
  type SignalHealthData,
  type SignalHealthFreshnessRow,
} from '@/api/researchEngine'
import {
  buildResearchVerdictCopy,
  type ResearchVerdictCopy,
} from '@/lib/research/researchHealthCopy'
import { SIGNAL_HEALTH_FRESH_SLA_HOURS } from '@/lib/research/signalHealthAgeMeters'

export type ResearchEngineFinding = {
  id: string
  severity: 'info' | 'warning' | 'danger'
  title: string
  detail: string
}

export type ResearchEngineAnalysis = {
  findings: ResearchEngineFinding[]
  primaryCause: string | null
  staleLabels: string[]
  needsAttention: boolean
}

export type ResearchEngineAgentPackSnapshot = {
  generatedAt: string
  husbandry: DataHusbandrySnapshot | null
  husbandryError: string | null
  status: ResearchStatus | null
  statusError: string | null
  health: ResearchHealth | null
  signalHealth: SignalHealthData | null
  signalHealthError: string | null
  orchestration: OrchestrationStatusData | null
  orchestrationError: string | null
  elementary: ElementaryStatus | null
  elementaryError: string | null
}

/** Feature table → owning Dagster job (not Cron). */
export const RESEARCH_SIGNAL_OWNERS: Record<
  string,
  { table: string; schedule: string; job: string; note: string }
> = {
  scan: {
    table: 'features.stock_signal_scan_daily',
    schedule: 'research_trading_day_schedule',
    job: 'research_trading_day',
    note: 'engines.scan is inside trading_day (not a separate schedule)',
  },
  canonical_pnl: {
    table: 'features.stock_signal_canonical_pnl_daily',
    schedule: 'research_canonical_pnl_schedule',
    job: 'research_canonical_pnl_job',
    note: 'EXCLUDED from research_trading_day — 23:40 UTC Mon–Fri',
  },
  vrp: {
    table: 'features.stock_signal_vrp_daily',
    schedule: 'research_vrp_schedule',
    job: 'research_vrp_job',
    note: 'research aux schedule',
  },
  iv_reconstructed: {
    table: 'features.option_iv_reconstructed_daily',
    schedule: 'research_vol_surface_svi_schedule',
    job: 'research_vol_surface_svi_job',
    note: 'research aux schedule',
  },
  playbook_trigger: {
    table: 'features.stock_signal_playbook_trigger_intraday',
    schedule: 'research_intraday_schedule',
    job: 'research_intraday_job',
    note: 'intraday aux',
  },
  forecast_settlement: {
    table: 'features.stock_backtest_settlement',
    schedule: 'research_settlement_schedule',
    job: 'research_settlement_job',
    note: 'research aux schedule',
  },
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function rejectMsg(reason: unknown): string {
  return (reason as Error)?.message ?? 'fetch failed'
}

function unwrapOk<T>(data: T | ResearchProxyError | null | undefined): T | null {
  if (data == null || isResearchProxyError(data)) return null
  return data
}

function proxyErr(
  data: TOrProxy | undefined,
  rejected: string | null,
): string | null {
  if (rejected) return rejected
  if (data != null && isResearchProxyError(data)) return data.error
  return null
}

type TOrProxy = ResearchProxyError | { ok?: boolean; data?: unknown; present?: boolean }

function laneVerdict(husbandry: DataHusbandrySnapshot | null, id: string): string | null {
  return husbandry?.lanes.find(l => l.id === id)?.verdict ?? null
}

function staleRows(rows: SignalHealthFreshnessRow[] | undefined): SignalHealthFreshnessRow[] {
  return (rows ?? []).filter(r => {
    const s = (r.status ?? '').toLowerCase()
    return s === 'stale' || s === 'missing' || s === 'empty'
  })
}

function ageHours(row: SignalHealthFreshnessRow): number | null {
  return typeof row.age_hours === 'number' && Number.isFinite(row.age_hours) ? row.age_hours : null
}

function looksLikeWeekendSlaGap(
  batchVerdict: string | null | undefined,
  stale: SignalHealthFreshnessRow[],
): boolean {
  if ((batchVerdict ?? '').toLowerCase() !== 'healthy') return false
  if (stale.length === 0) return false
  return stale.every(r => {
    const h = ageHours(r)
    return h != null && h > SIGNAL_HEALTH_FRESH_SLA_HOURS && h <= 72
  })
}

export function analyzeResearchEngine(snap: ResearchEngineAgentPackSnapshot): ResearchEngineAnalysis {
  const findings: ResearchEngineFinding[] = []
  const stale = staleRows(snap.signalHealth?.freshness)
  const staleLabels = stale.map(r => r.label)

  if (snap.status?.reachable === false || snap.statusError) {
    findings.push({
      id: 'api-unreachable',
      severity: 'danger',
      title: 'Research API unreachable',
      detail: snap.statusError || snap.status?.error || snap.status?.hint || 'GET /api/v1/research/status failed',
    })
  }

  const market = laneVerdict(snap.husbandry, 'market_batch')
  const flex = laneVerdict(snap.husbandry, 'flex_batch')
  if (market && market !== 'healthy' && market !== 'ok') {
    findings.push({
      id: 'feedstock-market',
      severity: market === 'missed' || market === 'degraded' ? 'danger' : 'warning',
      title: `Market batch ${market}`,
      detail: snap.husbandry?.lanes.find(l => l.id === 'market_batch')?.detail ?? market,
    })
  }
  if (flex && flex !== 'healthy' && flex !== 'ok') {
    findings.push({
      id: 'feedstock-flex',
      severity: flex === 'missed' || flex === 'degraded' ? 'danger' : 'warning',
      title: `IB Flex ${flex}`,
      detail: snap.husbandry?.lanes.find(l => l.id === 'flex_batch')?.detail ?? flex,
    })
  }

  const batch = snap.orchestration?.verdict ?? null
  if (batch && batch !== 'healthy' && batch !== 'ok') {
    findings.push({
      id: 'batch-sla',
      severity: batch === 'missed' || batch === 'degraded' ? 'danger' : 'warning',
      title: `Batch ${batch}`,
      detail: snap.orchestration?.detail ?? snap.orchestrationError ?? batch,
    })
  }

  for (const row of stale) {
    const owner = RESEARCH_SIGNAL_OWNERS[row.label]
    const age =
      ageHours(row) != null ? `${ageHours(row)!.toFixed(1)}h` : 'unknown age'
    findings.push({
      id: `stale-${row.label}`,
      severity: row.status === 'missing' ? 'danger' : 'warning',
      title: `${row.label} ${row.status}`,
      detail: [
        `age=${age} vs ${SIGNAL_HEALTH_FRESH_SLA_HOURS}h SLA`,
        `computed=${row.max_computed_at ?? '—'}`,
        owner
          ? `owner=${owner.schedule} / ${owner.job} (${owner.note})`
          : 'owner=unknown schedule',
        owner ? `table=${owner.table}` : row.table ? `table=${row.table}` : null,
      ]
        .filter(Boolean)
        .join(' · '),
    })
  }

  if (looksLikeWeekendSlaGap(batch, stale)) {
    findings.push({
      id: 'weekend-36h-sla',
      severity: 'info',
      title: '36h SLA vs Mon–Fri batch',
      detail:
        'Batch HEALTHY + Product stale in the 36–72h window usually means Friday 22:30 ET aged past Monday noon. Wait for tonight research_trading_day (22:30 ET) and research_canonical_pnl (23:40 UTC). Do not treat as engine crash.',
    })
  }

  if (snap.elementary != null && !snap.elementary.present) {
    findings.push({
      id: 'elementary-pending',
      severity: 'info',
      title: 'Elementary report pending',
      detail:
        snap.elementaryError ||
        `${snap.elementary.path ?? '/report/elementary_report.html'} not present on this Research API. Cluster PVC is served by research-api /analytics/elementary/files — local :8795 without the file is expected Pending.`,
    })
  }

  const fails = snap.orchestration?.recent_failures ?? []
  for (const f of fails.slice(0, 3)) {
    findings.push({
      id: `sched-fail-${f.name}`,
      severity: 'danger',
      title: `Schedule last run failed: ${f.name}`,
      detail: `${f.job_name} · ${f.last_run_status ?? 'FAIL'} · ${f.last_run_ended_at ?? '—'}`,
    })
  }

  const productBad = (snap.signalHealth?.overall ?? '').toLowerCase() === 'degraded'
  const feedstockBad = findings.some(f => f.id.startsWith('feedstock-'))
  const primaryCause =
    snap.status?.reachable === false || snap.statusError
      ? 'Research API unreachable'
      : feedstockBad
        ? 'Upstream feedstock (Massive / Flex) not healthy'
        : batch && batch !== 'healthy' && batch !== 'ok'
          ? `Batch ${batch}`
          : staleLabels.includes('scan') && staleLabels.includes('canonical_pnl') && !feedstockBad
            ? 'Product asof stale (scan + canonical_pnl) — check trading_day scan asset and canonical_pnl schedule separately'
            : staleLabels.length > 0
              ? `Product asof stale (${staleLabels.join(', ')})`
              : productBad
                ? 'Product asof degraded'
                : null

  return {
    findings,
    primaryCause,
    staleLabels,
    needsAttention: findings.some(f => f.severity !== 'info') || primaryCause != null,
  }
}

export function researchEngineVerdictFromSnap(snap: ResearchEngineAgentPackSnapshot): ResearchVerdictCopy {
  return buildResearchVerdictCopy({
    reachable: snap.status?.reachable,
    statusError: snap.status?.error || snap.status?.hint || snap.statusError || undefined,
    marketVerdict: laneVerdict(snap.husbandry, 'market_batch'),
    flexVerdict: laneVerdict(snap.husbandry, 'flex_batch'),
    batchVerdict: snap.orchestration?.verdict,
    batchDetail: snap.orchestration?.detail ?? snap.orchestrationError,
    productOverall: snap.signalHealth?.overall,
    schedulesTotal: snap.orchestration?.schedules_total,
    schedulesRunning: snap.orchestration?.schedules_running,
    schedulesStopped: snap.orchestration?.schedules_stopped,
    recentFailures: snap.orchestration?.recent_failures,
  })
}

export async function gatherResearchEngineSnapshot(): Promise<ResearchEngineAgentPackSnapshot> {
  const generatedAt = new Date().toISOString()
  const [husbandryRes, statusRes, healthRes, signalRes, orchRes, elemRes] = await Promise.allSettled([
    fetchDataHusbandry(),
    fetchResearchStatus(),
    fetchResearchHealth(),
    fetchSignalHealth(),
    fetchOrchestrationStatus(),
    fetchElementaryStatus(),
  ])

  const husbandry = husbandryRes.status === 'fulfilled' ? husbandryRes.value : null
  const status = statusRes.status === 'fulfilled' ? statusRes.value : null
  const healthRaw = healthRes.status === 'fulfilled' ? healthRes.value : null
  const signalRaw = signalRes.status === 'fulfilled' ? signalRes.value : null
  const orchRaw = orchRes.status === 'fulfilled' ? orchRes.value : null
  const elemRaw = elemRes.status === 'fulfilled' ? elemRes.value : null

  return {
    generatedAt,
    husbandry,
    husbandryError: husbandryRes.status === 'rejected' ? rejectMsg(husbandryRes.reason) : null,
    status,
    statusError: statusRes.status === 'rejected' ? rejectMsg(statusRes.reason) : null,
    health: unwrapOk(healthRaw),
    signalHealth: unwrapOk(signalRaw)?.data ?? null,
    signalHealthError:
      signalRes.status === 'rejected' ? rejectMsg(signalRes.reason) : proxyErr(signalRaw ?? undefined, null),
    orchestration: unwrapOk(orchRaw)?.data ?? null,
    orchestrationError:
      orchRes.status === 'rejected' ? rejectMsg(orchRes.reason) : proxyErr(orchRaw ?? undefined, null),
    elementary: unwrapOk(elemRaw),
    elementaryError:
      elemRes.status === 'rejected' ? rejectMsg(elemRes.reason) : proxyErr(elemRaw ?? undefined, null),
  }
}

function formatSchedule(row: OrchestrationScheduleRow): string {
  return (
    `  - ${row.name}: status=${row.status} last=${row.last_run_status ?? '—'} ` +
    `ended=${row.last_run_ended_at ?? '—'} job=${row.job_name}`
  )
}

export function buildResearchEngineAgentPack(snap: ResearchEngineAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }
  const analysis = analyzeResearchEngine(snap)
  const verdict = researchEngineVerdictFromSnap(snap)

  push(
    '# Research Engine — Agent repair pack',
    `Generated: ${snap.generatedAt}`,
    'Source: Ops Console → Satellite → Research Engine (Copy for Agent)',
    '',
    '## Goal',
    'Diagnose and fix Research Engine DEGRADED / Product asof signals below.',
    'Ground truth = signal-health asof (36h weekday / 72h Sat–Mon-before-22:00 UTC), not K8s Job Complete and not Cron Complete.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up.',
    'Do not re-apply bifrost-analytics CronJob in plugin-market-data.',
    'Do not recreate leftover analytics-docs (nginx). Elementary is served by research-api /analytics/elementary/files.',
    'Do not unsuspend husbandry CronJobs — Dagster owns the batch.',
    '',
    '## Console verdict',
    `${verdict.tagLabel} — ${verdict.summary}`,
  )
  for (const layer of verdict.layers) {
    push(`- ${layer.label}: ${layer.verdict} — ${layer.meta}`)
  }
  if (analysis.primaryCause) push(`primary_cause: ${analysis.primaryCause}`)
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

  push('## Research API')
  if (snap.status != null) {
    push(
      `reachable: ${snap.status.reachable}`,
      `version: ${snap.health?.version ?? '—'}`,
      `startup_ok: ${snap.health?.startup_ok ?? '—'}`,
    )
    if (snap.status.error) push(`error: ${snap.status.error}`)
    if (snap.status.hint) push(`hint: ${snap.status.hint}`)
  } else {
    push(`unavailable: ${snap.statusError ?? 'no status'}`)
  }
  push('')

  push('## Findings')
  if (analysis.findings.length === 0) {
    push('(none — layers look healthy)')
  } else {
    for (const f of analysis.findings) {
      push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
  }
  push('')

  push('## Product asof (GET /research/signal-health)')
  if (snap.signalHealth != null) {
    push(
      `overall: ${snap.signalHealth.overall}`,
      `as_of: ${snap.signalHealth.as_of ?? '—'}`,
      `sla_hours: ${snap.signalHealth.sla_hours ?? SIGNAL_HEALTH_FRESH_SLA_HOURS}`,
    )
    for (const row of snap.signalHealth.freshness ?? []) {
      const owner = RESEARCH_SIGNAL_OWNERS[row.label]
      push(
        `- ${row.label}: status=${row.status} age_h=${row.age_hours ?? '—'} ` +
          `computed=${row.max_computed_at ?? '—'} rows=${row.row_count ?? '—'}` +
          (owner ? ` · schedule=${owner.schedule}` : ''),
      )
    }
  } else {
    push(`unavailable: ${snap.signalHealthError ?? 'no signal-health'}`)
  }
  push('')

  push('## Batch compute (GET /research/orchestration/status)')
  if (snap.orchestration != null) {
    const o = snap.orchestration
    push(
      `verdict: ${o.verdict} — ${o.detail}`,
      `trading_day: last=${o.last_run_status ?? '—'} ended=${o.last_run_ended_at ?? '—'} overdue=${o.overdue}`,
      `schedules: total=${o.schedules_total ?? '—'} running=${o.schedules_running ?? '—'} stopped=${o.schedules_stopped ?? '—'}`,
    )
    const interesting = (o.schedules ?? []).filter(s =>
      [
        'research_trading_day_schedule',
        'research_canonical_pnl_schedule',
        'research_vrp_schedule',
        'research_settlement_schedule',
      ].includes(s.name),
    )
    if (interesting.length > 0) {
      push('key schedules:')
      for (const s of interesting) push(formatSchedule(s))
    }
    if ((o.recent_failures ?? []).length > 0) {
      push('recent_failures:')
      for (const f of o.recent_failures ?? []) push(formatSchedule(f))
    }
  } else {
    push(`unavailable: ${snap.orchestrationError ?? 'no orchestration'}`)
  }
  push('')

  push('## Elementary')
  if (snap.elementary != null) {
    push(
      `present: ${snap.elementary.present}`,
      `path: ${snap.elementary.path ?? '—'}`,
      `mtime: ${snap.elementary.mtime ?? '—'}`,
      'open: /api/v1/research/analytics/elementary/files/elementary_report.html',
    )
  } else {
    push(`unavailable: ${snap.elementaryError ?? 'no elementary status'}`)
  }
  push('')

  push(
    '## Suggested investigation order',
    '1. If Market/Flex lanes are not healthy → fix feedstock first (Massive / IB Flex Copy for Agent). husbandry_gate will skip OLAP.',
    '2. If Batch HEALTHY and only Product stale on Monday after 22:00 UTC (weekday 36h SLA) → wait for tonight 22:30 ET trading_day; do not roll images or unsuspend Cron. Before 22:00 UTC Monday the API uses 72h weekend SLA.',
    '3. If scan stale after a SUCCESS trading_day → inspect Dagster run for engines.scan (table features.stock_signal_scan_daily). Schedule success ≠ every asset wrote.',
    '4. If canonical_pnl stale → check research_canonical_pnl_schedule / research_canonical_pnl_job. It is NOT part of research_trading_day.',
    '5. If Batch missed/failed → Dagster UI research_trading_day last run; keep Cron suspended.',
    '6. If Elementary Pending on local Vite → expected without /report mount. Verify cluster: kubectl -n research exec deploy/research-api + GET /analytics/elementary.',
    '7. Do not apply leftover analytics-docs or plugin-market-data analytics CronJob.',
    '',
    '## Owner ask',
    'Propose the smallest durable fix, verify with the same endpoints this pack used (signal-health + orchestration/status), then report before/after Product / Batch verdicts.',
  )

  return lines.join('\n')
}

export function buildResearchEngineDiagnosePrefill(snap: ResearchEngineAgentPackSnapshot): string {
  const analysis = analyzeResearchEngine(snap)
  const verdict = researchEngineVerdictFromSnap(snap)
  const lines = [
    'Research Engine — assisted diagnose (read-only). D10 BLOCKED — no live trading.',
    `Verdict: ${verdict.tagLabel} — ${verdict.summary}`,
  ]
  if (analysis.primaryCause) lines.push(`Primary cause: ${analysis.primaryCause}`)
  if (analysis.findings.length > 0) {
    lines.push('', 'Findings:')
    for (const f of analysis.findings) {
      lines.push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
  }
  lines.push(
    '',
    'Plan:',
    '1. Confirm feedstock (Massive / Flex) healthy before touching Dagster.',
    '2. Distinguish weekend 36h SLA aging vs a failed engines.scan / canonical_pnl_job.',
    '3. Do not recreate analytics-docs or unsuspend husbandry CronJobs.',
    '4. Re-check GET /research/signal-health after the next trading_day / canonical_pnl run.',
  )
  return lines.join('\n')
}
