import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  DenseTag,
  SegmentControl,
  StatusLamp,
} from '@bifrost/ui'
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
  { layer: 'marts', name: 'sepa_fundamental_eval', note: '8 core fund conditions' },
  { layer: 'marts', name: 'sepa_fundamental_ext', note: '25 extended fund conditions' },
  { layer: 'marts', name: 'sepa_technical_eval', note: '11 tech conditions' },
  { layer: 'marts', name: 'sepa_tier_momentum', note: 'Tier 2 momentum' },
  { layer: 'marts', name: 'sepa_tier_structure', note: 'Tier 3 structure' },
  { layer: 'marts', name: 'sepa_tier_sentiment', note: 'Tier 4 sentiment' },
  { layer: 'marts', name: 'sepa_composite_score', note: 'Weighted composite' },
  { layer: 'marts', name: 'sepa_screening_ranked', note: 'Rank / decile' },
  { layer: 'marts', name: 'sepa_criteria_stats', note: 'Pre-agg pass rates' },
  { layer: 'marts', name: 'sepa_screener_wide', note: 'Wide screener join' },
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

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        lamp={verdict.lamp}
        title="Analytics Pipeline"
        summary={verdict.summary}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
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
        <a
          className="text-dense-meta text-primary underline-offset-2 hover:underline"
          href={ANALYTICS_REPORT_URL}
          target="_blank"
          rel="noreferrer"
        >
          Open report in new tab
        </a>
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
        <OpsSection title="Elementary lineage & catalog" collapsible defaultCollapsed={false}>
          <p className="mb-2 text-dense-meta text-muted-foreground">
            Interactive DAG, model catalog, and test coverage from Elementary OSS (
            <code className="font-mono">edr report</code>). Regenerated after each CronJob run.
          </p>
          <div className="overflow-hidden rounded border border-border bg-background">
            <iframe
              title="Elementary observability report"
              src={ANALYTICS_REPORT_URL}
              className="h-[min(75vh,900px)] w-full border-0"
              sandbox="allow-scripts allow-same-origin allow-popups"
            />
          </div>
        </OpsSection>
      )}

      {tab === 'quality' && (
        <>
          <OpsSection title="Observability surface" collapsible defaultCollapsed={false}>
            <p className="text-dense-body text-muted-foreground">
              Pass/fail history, freshness, and volume anomalies live inside the Elementary report
              (Lineage & Catalog tab). Schema YAML can add{' '}
              <code className="font-mono">elementary.volume_anomalies</code> /{' '}
              <code className="font-mono">freshness_anomalies</code> on marts (Wave 4).
            </p>
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
