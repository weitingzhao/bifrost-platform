import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  SegmentControl,
  StatusLamp,
  denseTableNumCell,
} from '@bifrost/ui'
import { ExternalLink, RefreshCw } from 'lucide-react'
import {
  fetchElementaryStatus,
  fetchForecastSessions,
  fetchForecastSettlements,
  fetchOrchestrationStatus,
  fetchResearchStatus,
  fetchSignalHealth,
  isResearchProxyError,
  summarizeSettlements,
  type ForecastSessionRow,
  type ForecastSettlementRow,
  type OrchestrationStatusData,
  type SignalHealthData,
} from '@/api/researchEngine'
import { fetchAnalyticsStatus } from '@/api/analyticsPlugin'
import { fetchDataHusbandry } from '@/api/dataHusbandry'
import { OpsSection } from '@/components/layout/OpsSection'
import { PageToolbar } from '@/components/layout/PageToolbar'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { RESEARCH_ENGINE_SUMMARY } from '@/lib/architecture/researchEngineCatalog'
import { resolveOpsToolUrl } from '@/lib/architecture/opsToolRackCatalog'
import { buildResearchVerdictCopy } from '@/lib/research/researchHealthCopy'
import {
  formatReadinessRollupLine,
  openFlexManage,
  openMassiveReadiness,
} from '@/lib/research/massiveNav'
import { HusbandryStrip } from '@/components/delivery/HusbandryStrip'
import { ResearchHealthLayersStrip } from '@/components/research/ResearchHealthLayersStrip'
import {
  AgeMeterCell,
  SignalHealthAgeMeters,
} from '@/components/research/SignalHealthAgeMeters'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { stackFreshnessStatuses } from '@/lib/research/signalHealthAgeMeters'

type ManageTab = 'accuracy' | 'cost' | 'health' | 'runs'

function formatPct(n: number | null | undefined, digits = 1): string {
  if (n == null || !Number.isFinite(n)) return '—'
  return `${(n * 100).toFixed(digits)}%`
}

function formatMissPct(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '—'
  return `${n.toFixed(2)}%`
}

function formatWhen(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function formatNum(n: number | null | undefined, digits = 2): string {
  if (n == null || !Number.isFinite(n)) return '—'
  return n.toLocaleString('en-US', {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  })
}

function providerCounts(rows: ForecastSessionRow[]): Map<string, number> {
  const m = new Map<string, number>()
  for (const r of rows) {
    const key = (r.llm_provider || 'unknown').trim() || 'unknown'
    m.set(key, (m.get(key) ?? 0) + 1)
  }
  return m
}

function sumOptional(rows: ForecastSessionRow[], key: 'token_usage' | 'token_cost_usd'): number | null {
  let total = 0
  let any = false
  for (const r of rows) {
    const v = r[key]
    if (typeof v === 'number' && Number.isFinite(v)) {
      total += v
      any = true
    }
  }
  return any ? total : null
}

function laneLamp(verdict: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  const v = (verdict ?? '').toLowerCase()
  if (v === 'healthy' || v === 'ok') return 'ok'
  if (v === 'due' || v === 'draining' || v === 'caution') return 'degraded'
  if (v === 'missed' || v === 'degraded') return 'fail'
  return 'unknown'
}

export function ResearchEnginePage({
  onNavigate,
}: {
  onNavigate?: (tabId: string) => void
} = {}) {
  const [tab, setTab] = useState<ManageTab>('health')
  const marketProbe = useMarketDataLiveProbe()

  const statusQ = useQuery({
    queryKey: ['research-engine-status'],
    queryFn: fetchResearchStatus,
    refetchInterval: 30_000,
  })
  const husbandryQ = useQuery({
    queryKey: ['data-husbandry'],
    queryFn: fetchDataHusbandry,
    refetchInterval: 30_000,
    retry: 1,
  })
  const signalHealthQ = useQuery({
    queryKey: ['research-engine-signal-health'],
    queryFn: fetchSignalHealth,
    refetchInterval: 60_000,
    retry: 1,
  })
  const orchestrationQ = useQuery({
    queryKey: ['research-engine-orchestration'],
    queryFn: fetchOrchestrationStatus,
    refetchInterval: 60_000,
    retry: 1,
  })
  const settlementsQ = useQuery({
    queryKey: ['research-engine-settlements'],
    queryFn: () => fetchForecastSettlements(50),
    refetchInterval: 60_000,
    retry: 1,
  })
  const sessionsQ = useQuery({
    queryKey: ['research-engine-sessions'],
    queryFn: () => fetchForecastSessions(50),
    refetchInterval: 60_000,
    retry: 1,
  })
  const elementaryQ = useQuery({
    queryKey: ['research-engine-elementary'],
    queryFn: fetchElementaryStatus,
    refetchInterval: 60_000,
    retry: 1,
  })
  const analyticsQ = useQuery({
    queryKey: ['analytics-pipeline-status'],
    queryFn: fetchAnalyticsStatus,
    refetchInterval: 60_000,
    retry: 1,
  })

  const settlementRows = useMemo<ForecastSettlementRow[]>(() => {
    if (settlementsQ.data != null && !isResearchProxyError(settlementsQ.data)) {
      return settlementsQ.data.rows
    }
    return []
  }, [settlementsQ.data])

  const sessionRows = useMemo<ForecastSessionRow[]>(() => {
    if (sessionsQ.data != null && !isResearchProxyError(sessionsQ.data)) {
      return sessionsQ.data.rows
    }
    return []
  }, [sessionsQ.data])

  const settlementErr =
    settlementsQ.data != null && isResearchProxyError(settlementsQ.data)
      ? settlementsQ.data.error
      : settlementsQ.error
        ? String(settlementsQ.error)
        : null
  const sessionErr =
    sessionsQ.data != null && isResearchProxyError(sessionsQ.data)
      ? sessionsQ.data.error
      : sessionsQ.error
        ? String(sessionsQ.error)
        : null
  const elementary =
    elementaryQ.data != null && !isResearchProxyError(elementaryQ.data) ? elementaryQ.data : null
  const elementaryErr =
    elementaryQ.data != null && isResearchProxyError(elementaryQ.data)
      ? elementaryQ.data.error
      : null

  const researchLane = husbandryQ.data?.lanes.find(l => l.id === 'research_olap')
  const marketLane = husbandryQ.data?.lanes.find(l => l.id === 'market_batch')
  const flexLane = husbandryQ.data?.lanes.find(l => l.id === 'flex_batch')

  const signalHealth: SignalHealthData | null =
    signalHealthQ.data != null && !isResearchProxyError(signalHealthQ.data)
      ? signalHealthQ.data.data
      : null
  const signalHealthErr =
    signalHealthQ.data != null && isResearchProxyError(signalHealthQ.data)
      ? signalHealthQ.data.error
      : null

  const readinessLine = formatReadinessRollupLine(
    marketProbe.status?.readiness_rollup ?? null,
  )
  const freshnessStack = useMemo(
    () => stackFreshnessStatuses(signalHealth?.freshness ?? []),
    [signalHealth?.freshness],
  )

  const orchestration: OrchestrationStatusData | null =
    orchestrationQ.data != null && !isResearchProxyError(orchestrationQ.data)
      ? orchestrationQ.data.data
      : null
  const orchestrationErr =
    orchestrationQ.data != null && isResearchProxyError(orchestrationQ.data)
      ? orchestrationQ.data.error
      : null

  const accuracy = useMemo(() => summarizeSettlements(settlementRows), [settlementRows])
  const providers = useMemo(() => providerCounts(sessionRows), [sessionRows])
  const tokenUsage = useMemo(() => sumOptional(sessionRows, 'token_usage'), [sessionRows])
  const tokenCost = useMemo(() => sumOptional(sessionRows, 'token_cost_usd'), [sessionRows])

  const loading = statusQ.isLoading || husbandryQ.isLoading

  const verdict = useMemo(
    () =>
      buildResearchVerdictCopy({
        loading,
        reachable: statusQ.data?.reachable,
        statusError: statusQ.data?.error || statusQ.data?.hint,
        marketVerdict: marketLane?.verdict,
        flexVerdict: flexLane?.verdict,
        batchVerdict: orchestration?.verdict,
        batchDetail: orchestration?.detail ?? orchestrationErr,
        productOverall: signalHealth?.overall,
        schedulesTotal: orchestration?.schedules_total,
        schedulesRunning: orchestration?.schedules_running,
        schedulesStopped: orchestration?.schedules_stopped,
        recentFailures: orchestration?.recent_failures,
      }),
    [
      loading,
      statusQ.data,
      marketLane?.verdict,
      flexLane?.verdict,
      orchestration?.verdict,
      orchestration?.detail,
      orchestration?.schedules_total,
      orchestration?.schedules_running,
      orchestration?.schedules_stopped,
      orchestration?.recent_failures,
      orchestrationErr,
      signalHealth?.overall,
    ],
  )

  const dagsterUrl = resolveOpsToolUrl('dagster')

  const refreshAll = () => {
    void statusQ.refetch()
    void husbandryQ.refetch()
    void signalHealthQ.refetch()
    void orchestrationQ.refetch()
    void settlementsQ.refetch()
    void sessionsQ.refetch()
    void elementaryQ.refetch()
    void analyticsQ.refetch()
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <HusbandryStrip />
      <OpsVerdictStrip
        lamp={verdict.lamp as OpsVerdictLamp}
        title="Research Engine"
        summary={verdict.summary}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant as OpsVerdictTagVariant}
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={
              statusQ.isFetching ||
              husbandryQ.isFetching ||
              signalHealthQ.isFetching ||
              orchestrationQ.isFetching ||
              settlementsQ.isFetching ||
              sessionsQ.isFetching
            }
            onClick={refreshAll}
          >
            <RefreshCw className="size-3.5" />
            Refresh
          </Button>
        }
        meta={
          <span className="text-dense-meta text-muted-foreground">
            NS {RESEARCH_ENGINE_SUMMARY.namespace} · :{RESEARCH_ENGINE_SUMMARY.apiPort} · D10
            BLOCKED
          </span>
        }
      />

      <ResearchHealthLayersStrip
        layers={verdict.layers}
        onSelectLayer={() => setTab('health')}
      />

      <PageToolbar align="between">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => setTab(v as ManageTab)}
          ariaLabel="Research engine governance tabs"
          options={[
            { value: 'accuracy', label: 'Accuracy' },
            { value: 'cost', label: 'Token cost' },
            { value: 'health', label: 'Pipeline health' },
            { value: 'runs', label: 'Runs' },
          ]}
        />
        <span className="text-dense-meta text-muted-foreground font-mono">
          {RESEARCH_ENGINE_SUMMARY.platformProxy}
        </span>
      </PageToolbar>

      {tab === 'accuracy' && (
        <>
          <OpsSection title="Forecast accuracy" collapsible defaultCollapsed={false}>
            <p className="mb-3 text-dense-meta text-muted-foreground">
              Path Hit / Close Miss from{' '}
              <code className="font-mono">GET /research/backtest/settlement</code> via platform
              proxy. Aggregates computed in Console (proxy is GET-only).
            </p>
            <div className="grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="Sessions settled"
                value={String(accuracy.sessionsSettled)}
                detail="Last 50 settlement rows"
              />
              <Metric
                label="Path hit rate"
                value={formatPct(accuracy.pathHitRate)}
                detail={`${accuracy.pathHitSessions} / ${accuracy.sessionsSettled} sessions`}
              />
              <Metric
                label="Avg close miss"
                value={formatMissPct(accuracy.avgCloseMissPct)}
                detail="Mean |expected − actual| %"
              />
              <Metric
                label="Median close miss"
                value={formatMissPct(accuracy.medianCloseMissPct)}
                detail="Median close_miss_pct"
              />
            </div>
            {settlementErr && (
              <p className="mt-2 text-dense-meta text-destructive">{settlementErr}</p>
            )}
          </OpsSection>

          <OpsSection
            title="Recent settlements"
            description="research.forecast_settlement"
            bodyPadding="none"
            overflow="visible"
            collapsible
            defaultCollapsed={false}
          >
            {settlementsQ.isLoading ? (
              <p className="m-0 px-3 py-3 text-dense-meta text-muted-foreground">
                Loading settlements…
              </p>
            ) : settlementErr ? (
              <p className="m-0 px-3 py-3 text-dense-meta text-destructive">{settlementErr}</p>
            ) : settlementRows.length === 0 ? (
              <p className="m-0 px-3 py-3 text-dense-meta text-muted-foreground">
                No settlements yet. Run forecast settlement against actual closes to populate Path
                Hit / Close Miss metrics.
              </p>
            ) : (
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Date</DenseTableHead>
                    <DenseTableHead>Symbol</DenseTableHead>
                    <DenseTableHead>Path hit</DenseTableHead>
                    <DenseTableHead className="text-right">Close miss %</DenseTableHead>
                    <DenseTableHead className="text-right">Expected</DenseTableHead>
                    <DenseTableHead className="text-right">Actual</DenseTableHead>
                    <DenseTableHead>Session</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {settlementRows.map(r => (
                    <DenseTableRow key={r.settlement_id}>
                      <DenseTableCell className="font-mono text-xs">{r.trade_date}</DenseTableCell>
                      <DenseTableCell className="font-mono text-xs font-medium">
                        {r.symbol}
                      </DenseTableCell>
                      <DenseTableCell>
                        {r.path_hit == null ? (
                          <DenseTag variant="neutral">—</DenseTag>
                        ) : r.path_hit ? (
                          <DenseTag variant="success">HIT</DenseTag>
                        ) : (
                          <DenseTag variant="warning">MISS</DenseTag>
                        )}
                        <span className="ml-1 text-dense-meta text-muted-foreground">
                          {r.path_hit_count ?? 0}/{r.path_total ?? 0}
                        </span>
                      </DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>
                        {formatMissPct(r.close_miss_pct)}
                      </DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>
                        {formatNum(r.expected_close)}
                      </DenseTableCell>
                      <DenseTableCell className={denseTableNumCell}>
                        {formatNum(r.actual_close)}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-xs text-muted-foreground">
                        {r.session_id.slice(0, 12)}
                        {r.session_id.length > 12 ? '…' : ''}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            )}
          </OpsSection>
        </>
      )}

      {tab === 'cost' && (
        <>
          <OpsSection title="Token usage / AI cost" collapsible defaultCollapsed={false}>
            <p className="mb-3 text-dense-meta text-muted-foreground">
              Derived from forecast session metadata when present. Token counts / USD are not yet
              persisted on <code className="font-mono">research.forecast_session</code> — KPIs
              show placeholders until Wave 4 metadata lands.
            </p>
            <div className="grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="Sessions sampled"
                value={String(sessionRows.length)}
                detail="Last 50 forecast sessions"
              />
              <Metric
                label="Token usage"
                value={tokenUsage != null ? formatNum(tokenUsage, 0) : '—'}
                detail={
                  tokenUsage == null
                    ? 'Not persisted — placeholder'
                    : 'Sum of session token_usage'
                }
              />
              <Metric
                label="Est. cost (USD)"
                value={tokenCost != null ? `$${formatNum(tokenCost, 4)}` : '—'}
                detail={
                  tokenCost == null
                    ? 'Not persisted — placeholder'
                    : 'Sum of session token_cost_usd'
                }
              />
              <Metric
                label="LLM providers"
                value={providers.size > 0 ? String(providers.size) : '—'}
                detail={
                  providers.size > 0
                    ? [...providers.entries()]
                        .map(([k, n]) => `${k}×${n}`)
                        .join(' · ')
                    : 'No sessions'
                }
              />
            </div>
            {sessionErr && (
              <p className="mt-2 text-dense-meta text-destructive">{sessionErr}</p>
            )}
          </OpsSection>

          <OpsSection title="Provider mix" collapsible defaultCollapsed={false}>
            {sessionRows.length === 0 ? (
              <p className="text-dense-meta text-muted-foreground">
                No forecast sessions to attribute. Heuristic provider counts as zero cost until an
                external LLM is configured.
              </p>
            ) : (
              <ul className="grid gap-1 sm:grid-cols-2">
                {[...providers.entries()].map(([name, count]) => (
                  <li
                    key={name}
                    className="rounded border border-border/60 bg-secondary/40 px-2 py-1 text-dense-meta"
                  >
                    <span className="font-mono text-foreground">{name}</span>
                    <span className="text-muted-foreground"> — {count} sessions</span>
                  </li>
                ))}
              </ul>
            )}
          </OpsSection>
        </>
      )}

      {tab === 'health' && (
        <>
          <OpsSection
            title="Feedstock (upstream)"
            collapsible
            defaultCollapsed={false}
            actions={
              <div className="flex flex-wrap items-center gap-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 gap-1 px-2 text-dense-caption"
                  onClick={() => openMassiveReadiness(onNavigate)}
                >
                  <ExternalLink size={12} aria-hidden />
                  Open Massive
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 gap-1 px-2 text-dense-caption"
                  onClick={() => openFlexManage(onNavigate)}
                >
                  <ExternalLink size={12} aria-hidden />
                  Open Flex
                </Button>
              </div>
            }
          >
            <p className="mb-2 text-dense-meta text-muted-foreground">
              Husbandry = enqueue gate (Massive / Flex lanes). readiness_rollup = Massive coverage /
              gaps. void ≠ fail — does not paint the Research sidebar icon. Detail: Massive → Coverage
              → Readiness.
            </p>
            <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
              {[marketLane, flexLane].map(lane =>
                lane == null ? null : (
                  <li
                    key={lane.id}
                    className="inline-flex items-center gap-1.5 rounded border border-border/60 bg-card px-2 py-1"
                    title={lane.detail}
                  >
                    <StatusLamp value={laneLamp(lane.verdict)} kind="reach" />
                    <DenseTag variant={lane.verdict === 'healthy' ? 'success' : lane.verdict === 'degraded' || lane.verdict === 'missed' ? 'danger' : 'warning'}>
                      {lane.verdict.toUpperCase()}
                    </DenseTag>
                    <span className="text-dense-caption font-medium">{lane.label}</span>
                    <span className="text-dense-micro text-muted-foreground">{lane.detail}</span>
                  </li>
                ),
              )}
            </ul>
            <p className="mt-2 m-0 text-dense-caption text-muted-foreground">
              {marketProbe.isLoading
                ? 'Massive readiness: probing…'
                : readinessLine != null
                  ? `Massive readiness: ${readinessLine}`
                  : marketProbe.status?.hint ||
                    marketProbe.status?.error ||
                    'Massive readiness: rollup unavailable'}
            </p>
            {husbandryQ.isLoading ? (
              <p className="mt-2 text-dense-meta text-muted-foreground">Loading husbandry…</p>
            ) : null}
          </OpsSection>

          <OpsSection
            title="Batch compute (Dagster)"
            collapsible
            defaultCollapsed={false}
            actions={
              <Button variant="outline" size="sm" className="h-7 gap-1 px-2 text-dense-caption" asChild>
                <a href={dagsterUrl} target="_blank" rel="noopener noreferrer">
                  <ExternalLink size={12} aria-hidden />
                  Open Dagster
                </a>
              </Button>
            }
          >
            <div className="grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="Orchestration"
                value={
                  <span className="inline-flex items-center gap-1.5">
                    <StatusLamp
                      value={laneLamp(orchestration?.verdict)}
                      kind="reach"
                    />
                    {orchestration?.verdict?.toUpperCase() ?? '—'}
                  </span>
                }
                detail={orchestrationErr || orchestration?.detail || 'GET /research/orchestration/status'}
              />
              <Metric
                label="Schedules"
                value={
                  orchestration?.schedules_total != null
                    ? `${orchestration.schedules_total} · ${orchestration.schedules_running ?? 0} run · ${orchestration.schedules_stopped ?? 0} stop`
                    : '—'
                }
                detail={
                  orchestration?.recent_failures?.[0]?.name
                    ? `Last fail ${orchestration.recent_failures[0].name}`
                    : orchestration?.schedules_detail ||
                      'Whitelist husbandry schedules in ops_dagster'
                }
              />
              <Metric
                label="Trading-day SLA"
                value={orchestration?.job_name ?? 'research_trading_day'}
                detail={
                  orchestration?.overdue ? 'Overdue vs 22:30 ET SLA' : 'Schedule Mon–Fri 22:30 ET'
                }
              />
              <Metric
                label="Last trading-day run"
                value={orchestration?.last_run_status ?? '—'}
                detail={formatWhen(orchestration?.last_run_ended_at)}
              />
            </div>
            {orchestration?.recent_failures != null && orchestration.recent_failures.length > 0 ? (
              <ul className="mt-2 m-0 list-none space-y-1 p-0 text-dense-caption text-muted-foreground">
                {orchestration.recent_failures.map(f => (
                  <li key={f.name} className="font-mono">
                    FAIL {f.name}
                    {f.last_run_status ? ` · ${f.last_run_status}` : ''}
                    {f.last_run_ended_at ? ` · ${formatWhen(f.last_run_ended_at)}` : ''}
                  </li>
                ))}
              </ul>
            ) : null}
            <p className="mt-2 m-0 text-dense-micro text-muted-foreground">
              research_olap: {researchLane?.verdict?.toUpperCase() ?? '—'}
              {researchLane?.detail ? ` · ${researchLane.detail}` : ''}
            </p>
          </OpsSection>

          <OpsSection title="Product asof (signal-health)" collapsible defaultCollapsed={false}>
            <div className="mb-2 grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="Overall"
                value={signalHealth?.overall?.toUpperCase() ?? '—'}
                detail={
                  signalHealthErr ||
                  (freshnessStack.total > 0
                    ? `${freshnessStack.fresh} fresh / ${freshnessStack.stale} stale · ${formatWhen(signalHealth?.as_of)}`
                    : formatWhen(signalHealth?.as_of))
                }
              />
              <Metric
                label="Research API"
                value={
                  <span className="inline-flex items-center gap-1.5">
                    <StatusLamp
                      value={statusQ.data?.reachable ? 'ok' : statusQ.isLoading ? 'unknown' : 'fail'}
                      kind="reach"
                    />
                    {statusQ.data?.reachable ? 'Reachable' : 'Down'}
                  </span>
                }
                detail={statusQ.data?.hint || statusQ.data?.error || 'GET /api/v1/research/status'}
              />
              <Metric
                label="Elementary"
                value={elementary?.present ? 'Report present' : 'Pending'}
                detail={
                  elementaryErr ||
                  (elementary?.mtime ? `mtime ${formatWhen(elementary.mtime)}` : 'GET /analytics/elementary')
                }
              />
              <Metric
                label="dbt Analytics"
                value={
                  <span className="inline-flex items-center gap-1.5">
                    <StatusLamp
                      value={
                        analyticsQ.data?.healthy
                          ? 'ok'
                          : analyticsQ.isLoading
                            ? 'unknown'
                            : analyticsQ.data?.report_available
                              ? 'degraded'
                              : 'fail'
                      }
                      kind="reach"
                    />
                    {analyticsQ.data?.models_total ?? 21} models
                  </span>
                }
                detail={`Cron last: ${formatWhen(analyticsQ.data?.last_schedule)}`}
              />
            </div>
            {signalHealthErr ? (
              <p className="text-dense-meta text-destructive">{signalHealthErr}</p>
            ) : signalHealthQ.isLoading ? (
              <p className="text-dense-meta text-muted-foreground">Loading signal-health…</p>
            ) : (signalHealth?.freshness?.length ?? 0) === 0 ? (
              <p className="text-dense-meta text-muted-foreground">No freshness rows.</p>
            ) : (
              <>
                <SignalHealthAgeMeters rows={signalHealth!.freshness} />
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Label</DenseTableHead>
                      <DenseTableHead>Status</DenseTableHead>
                      <DenseTableHead>Age vs 36h</DenseTableHead>
                      <DenseTableHead>Max computed</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {signalHealth!.freshness.map(row => (
                      <DenseTableRow key={`${row.label}-${row.table ?? ''}`}>
                        <DenseTableCell className="font-mono text-xs">{row.label}</DenseTableCell>
                        <DenseTableCell>
                          <DenseTag
                            variant={
                              row.status === 'fresh'
                                ? 'success'
                                : row.status === 'stale' || row.status === 'missing'
                                  ? 'danger'
                                  : 'warning'
                            }
                          >
                            {row.status}
                          </DenseTag>
                        </DenseTableCell>
                        <DenseTableCell>
                          <AgeMeterCell ageHours={row.age_hours} status={row.status} />
                        </DenseTableCell>
                        <DenseTableCell className="text-dense-meta text-muted-foreground">
                          {formatWhen(row.max_computed_at)}
                        </DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </>
            )}
          </OpsSection>

          <OpsSection title="Engine surface map" collapsible defaultCollapsed>
            <ul className="grid gap-1 sm:grid-cols-2">
              {RESEARCH_ENGINE_SUMMARY.engines.map(name => (
                <li
                  key={name}
                  className="rounded border border-border/60 bg-secondary/40 px-2 py-1 text-dense-meta"
                >
                  <span className="font-mono text-foreground">{name}</span>
                </li>
              ))}
            </ul>
            <p className="mt-3 text-dense-meta text-muted-foreground">
              Schemas: {RESEARCH_ENGINE_SUMMARY.schemas.join(' · ')} on{' '}
              {RESEARCH_ENGINE_SUMMARY.goldenSource}. Adjacent dbt surface:{' '}
              <span className="font-medium text-foreground">Plugin → Analytics</span>.
            </p>
          </OpsSection>
        </>
      )}

      {tab === 'runs' && (
        <OpsSection
          title="Recent forecast sessions"
          description="research.forecast_session — run management (Dagster UI linkage planned)"
          bodyPadding="none"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
        >
          {sessionsQ.isLoading ? (
            <p className="m-0 px-3 py-3 text-dense-meta text-muted-foreground">
              Loading forecast sessions…
            </p>
          ) : sessionErr ? (
            <p className="m-0 px-3 py-3 text-dense-meta text-destructive">{sessionErr}</p>
          ) : sessionRows.length === 0 ? (
            <p className="m-0 px-3 py-3 text-dense-meta text-muted-foreground">
              No forecast sessions yet. Sessions appear after AI Forecast / terrain compute writes
              to Golden Source.
            </p>
          ) : (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Date</DenseTableHead>
                  <DenseTableHead>Symbol</DenseTableHead>
                  <DenseTableHead>Regime</DenseTableHead>
                  <DenseTableHead>Provider</DenseTableHead>
                  <DenseTableHead className="text-right">Spot</DenseTableHead>
                  <DenseTableHead className="text-right">Expected close</DenseTableHead>
                  <DenseTableHead>Computed</DenseTableHead>
                  <DenseTableHead>Session</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {sessionRows.map(r => (
                  <DenseTableRow key={r.session_id}>
                    <DenseTableCell className="font-mono text-xs">{r.trade_date}</DenseTableCell>
                    <DenseTableCell className="font-mono text-xs font-medium">
                      {r.symbol}
                    </DenseTableCell>
                    <DenseTableCell>
                      {r.regime ? (
                        <DenseTag variant="neutral">{r.regime}</DenseTag>
                      ) : (
                        '—'
                      )}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">
                      {r.llm_provider ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className={denseTableNumCell}>
                      {formatNum(r.spot)}
                    </DenseTableCell>
                    <DenseTableCell className={denseTableNumCell}>
                      {formatNum(r.expected_close)}
                    </DenseTableCell>
                    <DenseTableCell className="text-dense-meta text-muted-foreground">
                      {formatWhen(r.computed_at)}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs text-muted-foreground">
                      {r.session_id.slice(0, 12)}
                      {r.session_id.length > 12 ? '…' : ''}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </OpsSection>
      )}
    </div>
  )
}

function Metric({
  label,
  value,
  detail,
}: {
  label: string
  value: ReactNode
  detail?: string
}) {
  return (
    <div className="rounded border border-border/60 bg-secondary/30 px-3 py-2">
      <div className="text-dense-meta text-muted-foreground">{label}</div>
      <div className="text-dense-body font-medium text-foreground">{value}</div>
      {detail ? <div className="text-dense-meta text-muted-foreground">{detail}</div> : null}
    </div>
  )
}
