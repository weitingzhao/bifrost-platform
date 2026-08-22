import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseTag,
  SegmentControl,
  StatusLamp,
} from '@bifrost/ui'
import { ExternalLink, GitBranch } from 'lucide-react'
import {
  ANALYTICS_REPORT_URL,
  fetchAnalyticsStatus,
  type AnalyticsStatus,
} from '@/api/analyticsPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'

type ManageTab = 'overview' | 'lineage' | 'quality'

const MODEL_INVENTORY: { layer: string; name: string; note: string }[] = [
  { layer: 'staging', name: 'stg_income_stmt', note: 'Polygon income jsonb → columns' },
  { layer: 'staging', name: 'stg_balance_sheet', note: 'Balance sheet extract' },
  { layer: 'staging', name: 'stg_cash_flow', note: 'Cash flow extract' },
  { layer: 'staging', name: 'stg_ratios', note: 'Placeholder (vendor gap)' },
  { layer: 'staging', name: 'stg_short_interest', note: 'Short interest' },
  { layer: 'staging', name: 'stg_short_volume', note: 'Short volume' },
  { layer: 'intermediate', name: 'dim_universe', note: 'CS equity universe' },
  { layer: 'intermediate', name: 'dim_trading_calendar', note: 'US holidays' },
  { layer: 'intermediate', name: 'int_stock_daily_enriched', note: 'SMA / ATR / ROC (incremental)' },
  { layer: 'intermediate', name: 'int_stock_crs', note: '252d CRS (needs depth)' },
  { layer: 'intermediate', name: 'int_financials_yoy', note: 'YoY growth' },
  { layer: 'marts', name: 'mart_sepa_fundamental_eval', note: '8 core fund conditions' },
  { layer: 'marts', name: 'mart_sepa_fundamental_ext', note: '25 extended fund conditions' },
  { layer: 'marts', name: 'mart_sepa_technical_eval', note: '11 tech conditions' },
  { layer: 'marts', name: 'mart_sepa_tier_momentum', note: 'Tier 2 momentum' },
  { layer: 'marts', name: 'mart_sepa_tier_structure', note: 'Tier 3 structure' },
  { layer: 'marts', name: 'mart_sepa_tier_sentiment', note: 'Tier 4 sentiment' },
  { layer: 'marts', name: 'mart_sepa_composite_score', note: 'Weighted composite' },
  { layer: 'marts', name: 'mart_sepa_screening_ranked', note: 'Rank / decile' },
  { layer: 'marts', name: 'mart_sepa_criteria_stats', note: 'Pre-agg pass rates' },
  { layer: 'marts', name: 'mart_sepa_screener_wide', note: 'Wide screener join' },
]

const LINEAGE_LAYERS: { id: string; title: string; detail: string }[] = [
  {
    id: 'raw',
    title: 'RAW · market.*',
    detail: 'Golden Source producer tables (stock_daily, stock_financials, ticker, holidays)',
  },
  {
    id: 'stg',
    title: 'STG · staging',
    detail: 'jsonb → scalar columns (income / balance / cash flow / short interest)',
  },
  {
    id: 'int',
    title: 'INT · intermediate',
    detail: 'Universe, calendar, enriched bars, CRS, YoY financials',
  },
  {
    id: 'mart',
    title: 'MART · sepa_*',
    detail: 'Fundamental / technical / tiers → composite → screener wide',
  },
]

function reachToVerdict(status: AnalyticsStatus | undefined, loading: boolean): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  summary: string
} {
  if (loading || !status) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: 'neutral',
      summary: 'Probing analytics CronJob and Elementary docs…',
    }
  }
  if (status.healthy) {
    return {
      lamp: 'ok',
      tagLabel: 'OK',
      tagVariant: 'success',
      summary: `${status.models_total} dbt models · Elementary report ready · docs ${status.docs_ready}/${status.docs_desired}`,
    }
  }
  if (status.report_available || status.docs_ready > 0) {
    return {
      lamp: 'degraded',
      tagLabel: 'DEGRADED',
      tagVariant: 'warning',
      summary: status.error || status.hint || 'Partial analytics surface available',
    }
  }
  return {
    lamp: 'fail',
    tagLabel: 'FAIL',
    tagVariant: 'danger',
    summary: status.error || status.hint || 'Analytics docs / CronJob unreachable',
  }
}

function formatBytes(n?: number): string {
  if (n == null || n <= 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  return `${(n / (1024 * 1024)).toFixed(2)} MiB`
}

function formatWhen(iso?: string | null): string {
  if (!iso) return 'never'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function AnalyticsPipelinePage() {
  const [tab, setTab] = useState<ManageTab>('overview')
  const statusQ = useQuery({
    queryKey: ['analytics-pipeline-status'],
    queryFn: fetchAnalyticsStatus,
    refetchInterval: 30_000,
  })
  const status = statusQ.data
  const verdict = useMemo(
    () => reachToVerdict(status, statusQ.isLoading),
    [status, statusQ.isLoading],
  )

  const byLayer = useMemo(() => {
    const m = new Map<string, typeof MODEL_INVENTORY>()
    for (const row of MODEL_INVENTORY) {
      const list = m.get(row.layer) ?? []
      list.push(row)
      m.set(row.layer, list)
    }
    return m
  }, [])

  const reportReady = Boolean(status?.report_available)

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        lamp={verdict.lamp}
        title="Analytics Pipeline"
        summary={verdict.summary}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        actions={
          reportReady ? (
            <Button asChild size="sm">
              <a href={ANALYTICS_REPORT_URL} target="_blank" rel="noreferrer">
                <ExternalLink className="size-3.5" />
                Open Elementary
              </a>
            </Button>
          ) : (
            <Button size="sm" variant="outline" disabled>
              <ExternalLink className="size-3.5" />
              Open Elementary
            </Button>
          )
        }
        meta={
          <span className="text-dense-meta text-muted-foreground">
            NS {status?.namespace ?? 'plugin-market-data'} · report{' '}
            {formatBytes(status?.report_bytes)}
          </span>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => setTab(v as ManageTab)}
          ariaLabel="Analytics pipeline tabs"
          options={[
            { value: 'overview', label: 'Overview' },
            { value: 'lineage', label: 'Lineage & Catalog' },
            { value: 'quality', label: 'Data Quality' },
          ]}
        />
      </div>

      {tab === 'overview' && (
        <>
          <OpsSection title="Run surface" collapsible defaultCollapsed={false}>
            <div className="grid gap-2 text-dense-body sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="CronJob"
                value={
                  <span className="inline-flex items-center gap-1.5">
                    <StatusLamp value={status?.cronjob_active ? 'ok' : 'unknown'} kind="reach" />
                    bifrost-analytics-daily
                  </span>
                }
                detail={`Last schedule: ${formatWhen(status?.last_schedule)}`}
              />
              <Metric
                label="Docs pod"
                value={`${status?.docs_ready ?? 0}/${status?.docs_desired ?? 0} ready`}
                detail="analytics-docs :8061"
              />
              <Metric
                label="Elementary report"
                value={status?.report_available ? 'Available' : 'Pending'}
                detail={formatBytes(status?.report_bytes)}
              />
              <Metric
                label="dbt models"
                value={String(status?.models_total ?? 21)}
                detail="staging → intermediate → marts"
              />
            </div>
            {status?.error && (
              <p className="mt-2 text-dense-meta text-destructive">{status.error}</p>
            )}
            {status?.hint && (
              <p className="mt-1 text-dense-meta text-muted-foreground">{status.hint}</p>
            )}
          </OpsSection>

          <OpsSection title="Model inventory" collapsible defaultCollapsed={false}>
            <div className="flex flex-col gap-3">
              {(['staging', 'intermediate', 'marts'] as const).map(layer => (
                <div key={layer}>
                  <div className="mb-1 flex items-center gap-2">
                    <DenseTag variant="neutral">{layer}</DenseTag>
                    <span className="text-dense-meta text-muted-foreground">
                      {(byLayer.get(layer) ?? []).length} models
                    </span>
                  </div>
                  <ul className="grid gap-1 sm:grid-cols-2">
                    {(byLayer.get(layer) ?? []).map(m => (
                      <li
                        key={m.name}
                        className="rounded border border-border/60 bg-secondary/40 px-2 py-1 text-dense-meta"
                      >
                        <span className="font-mono text-foreground">{m.name}</span>
                        <span className="text-muted-foreground"> — {m.note}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </OpsSection>

          <OpsSection title="Data depth" collapsible defaultCollapsed>
            <p className="text-dense-body text-muted-foreground">
              Technical / CRS / composite marts need ≥252 trading days of{' '}
              <code className="font-mono">market.stock_daily</code>. Until then those tables stay
              empty while fundamental / structure / sentiment marts remain populated. Depth grows
              automatically with daily Massive ingest + the analytics CronJob.
            </p>
          </OpsSection>
        </>
      )}

      {tab === 'lineage' && (
        <>
          <OpsSection title="Open full Elementary report" collapsible defaultCollapsed={false}>
            <div className="flex flex-col gap-3 rounded border border-border/60 bg-secondary/30 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0 space-y-1">
                <div className="flex items-center gap-2 text-dense-label font-medium text-foreground">
                  <GitBranch className="size-4 shrink-0 text-muted-foreground" />
                  Lineage DAG · Catalog · Test coverage
                </div>
                <p className="text-dense-meta text-muted-foreground">
                  Elementary OSS is a full SPA — open it in a new browser tab for usable layout.
                  In-page iframe is intentionally not used (no responsive shell).
                </p>
                <p className="text-dense-meta font-mono text-muted-foreground">{ANALYTICS_REPORT_URL}</p>
              </div>
              {reportReady ? (
                <Button asChild size="default">
                  <a href={ANALYTICS_REPORT_URL} target="_blank" rel="noreferrer">
                    <ExternalLink className="size-4" />
                    Open Elementary report
                  </a>
                </Button>
              ) : (
                <Button size="default" variant="outline" disabled>
                  Report pending
                </Button>
              )}
            </div>
          </OpsSection>

          <OpsSection title="Pipeline shape (text)" collapsible defaultCollapsed={false}>
            <ol className="flex flex-col gap-2">
              {LINEAGE_LAYERS.map((layer, idx) => (
                <li
                  key={layer.id}
                  className="flex gap-3 rounded border border-border/60 bg-background/60 px-3 py-2"
                >
                  <span className="text-dense-meta font-mono text-muted-foreground tabular-nums">
                    {idx + 1}
                  </span>
                  <div className="min-w-0">
                    <div className="text-dense-label font-medium text-foreground">{layer.title}</div>
                    <div className="text-dense-meta text-muted-foreground">{layer.detail}</div>
                  </div>
                </li>
              ))}
            </ol>
          </OpsSection>
        </>
      )}

      {tab === 'quality' && (
        <>
          <OpsSection title="Observability surface" collapsible defaultCollapsed={false}>
            <p className="text-dense-body text-muted-foreground">
              Pass/fail history, freshness, and volume anomalies live in the Elementary report.
              Open it from the verdict strip or Lineage tab. Mart schema YAML includes{' '}
              <code className="font-mono">elementary.volume_anomalies</code> /{' '}
              <code className="font-mono">freshness_anomalies</code> (warn severity until history
              accumulates).
            </p>
            <div className="mt-3">
              {reportReady ? (
                <Button asChild size="sm" variant="outline">
                  <a href={ANALYTICS_REPORT_URL} target="_blank" rel="noreferrer">
                    <ExternalLink className="size-3.5" />
                    Open Elementary for test results
                  </a>
                </Button>
              ) : (
                <Button size="sm" variant="outline" disabled>
                  Report pending
                </Button>
              )}
            </div>
          </OpsSection>
          <OpsSection title="Quick links" collapsible defaultCollapsed={false}>
            <ul className="list-inside list-disc text-dense-body text-muted-foreground">
              <li>
                Report URL:{' '}
                <a className="text-primary underline" href={ANALYTICS_REPORT_URL} target="_blank" rel="noreferrer">
                  {ANALYTICS_REPORT_URL}
                </a>
              </li>
              <li>
                Status API: <code className="font-mono">/api/v1/plugins/analytics/status</code>
              </li>
              <li>
                Metadata schema: <code className="font-mono">analytics_elementary</code> on Golden
                Source
              </li>
            </ul>
          </OpsSection>
        </>
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
