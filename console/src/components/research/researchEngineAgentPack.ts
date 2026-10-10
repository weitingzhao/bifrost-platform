/**
 * Research Engine findings — stale feature tables mapped to their owning Dagster job.
 * Feeds the Diagnose card on the Research Engine page.
 */

import type { DataHusbandrySnapshot } from '@/api/dataHusbandry'
import type {
  ElementaryStatus,
  OrchestrationStatusData,
  ResearchHealth,
  ResearchStatus,
  SignalHealthData,
  SignalHealthFreshnessRow,
} from '@/api/researchEngine'
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
    schedule: 'research_trading_day_schedule',
    job: 'research_trading_day',
    note: 'engines.vrp is inside trading_day, after engines.volatility — the 23:10 UTC aux schedule was retired',
  },
  iv_reconstructed: {
    table: 'features.option_iv_reconstructed_daily',
    schedule: 'research_iv_solver_schedule',
    job: 'research_iv_solver_job',
    note: 'IDS iv_solver — not SVI surface (SVI writes option_surface_*)',
  },
  playbook_trigger: {
    table: 'features.stock_signal_playbook_trigger_intraday',
    schedule: 'research_intraday_schedule',
    job: 'research_intraday_job',
    note: 'emitted from terrain-intraday after spot resolve',
  },
  forecast_settlement: {
    table: 'features.stock_backtest_settlement',
    schedule: 'research_settlement_schedule',
    job: 'research_settlement_job',
    note: 'needs research_forecast_schedule sessions first',
  },
}

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
