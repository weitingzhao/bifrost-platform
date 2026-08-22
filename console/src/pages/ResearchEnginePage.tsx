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
import { RefreshCw } from 'lucide-react'
import {
  fetchElementaryStatus,
  fetchForecastSessions,
  fetchForecastSettlements,
  fetchResearchStatus,
  isResearchProxyError,
  summarizeSettlements,
  type ForecastSessionRow,
  type ForecastSettlementRow,
} from '@/api/researchEngine'
import { fetchAnalyticsStatus } from '@/api/analyticsPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { PageToolbar } from '@/components/layout/PageToolbar'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { RESEARCH_ENGINE_SUMMARY } from '@/lib/architecture/researchEngineCatalog'

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

function reachToVerdict(opts: {
  loading: boolean
  reachable: boolean | undefined
  sessions: number
  settlements: number
  statusError?: string
}): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  summary: string
} {
  if (opts.loading) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: 'neutral',
      summary: 'Probing Research API health and forecast surfaces…',
    }
  }
  if (!opts.reachable) {
    return {
      lamp: 'fail',
      tagLabel: 'UNREACHABLE',
      tagVariant: 'danger',
      summary: opts.statusError || 'Research API :8795 unreachable — governance KPIs unavailable',
    }
  }
  if (opts.settlements === 0 && opts.sessions === 0) {
    return {
      lamp: 'degraded',
      tagLabel: 'EMPTY',
      tagVariant: 'warning',
      summary: 'API reachable · no forecast sessions or settlements yet (mock-friendly empty)',
    }
  }
  return {
    lamp: 'ok',
    tagLabel: 'OK',
    tagVariant: 'success',
    summary: `${opts.sessions} sessions · ${opts.settlements} settlements · D10 blocked (observe only)`,
  }
}

export function ResearchEnginePage() {
  const [tab, setTab] = useState<ManageTab>('accuracy')

  const statusQ = useQuery({
    queryKey: ['research-engine-status'],
    queryFn: fetchResearchStatus,
    refetchInterval: 30_000,
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

  const accuracy = useMemo(() => summarizeSettlements(settlementRows), [settlementRows])
  const providers = useMemo(() => providerCounts(sessionRows), [sessionRows])
  const tokenUsage = useMemo(() => sumOptional(sessionRows, 'token_usage'), [sessionRows])
  const tokenCost = useMemo(() => sumOptional(sessionRows, 'token_cost_usd'), [sessionRows])

  const loading =
    statusQ.isLoading ||
    (settlementsQ.isLoading && sessionsQ.isLoading)

  const verdict = useMemo(
    () =>
      reachToVerdict({
        loading,
        reachable: statusQ.data?.reachable,
        sessions: sessionRows.length,
        settlements: settlementRows.length,
        statusError: statusQ.data?.error || statusQ.data?.hint,
      }),
    [loading, statusQ.data, sessionRows.length, settlementRows.length],
  )

  const refreshAll = () => {
    void statusQ.refetch()
    void settlementsQ.refetch()
    void sessionsQ.refetch()
    void elementaryQ.refetch()
    void analyticsQ.refetch()
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        lamp={verdict.lamp}
        title="Research Engine"
        summary={verdict.summary}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={statusQ.isFetching || settlementsQ.isFetching || sessionsQ.isFetching}
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
          <OpsSection title="Pipeline health / freshness" collapsible defaultCollapsed={false}>
            <div className="grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
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
              <Metric
                label="Orchestration"
                value="Cron / planned Dagster"
                detail="Wave 5.1 Dagster freshness not wired yet"
              />
            </div>
            {(statusQ.data?.error || elementaryErr) && (
              <p className="mt-2 text-dense-meta text-destructive">
                {statusQ.data?.error || elementaryErr}
              </p>
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
