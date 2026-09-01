import { useMemo } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { ExternalLink, GitBranch } from 'lucide-react'
import { ELEMENTARY_REPORT_URL, type ElementaryStatus } from '@/api/researchEngine'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  RESEARCH_DBT_LINEAGE,
  RESEARCH_DBT_MODELS,
  RESEARCH_ENGINE_SUMMARY,
} from '@/lib/architecture/researchEngineCatalog'

function formatWhen(iso?: string | null): string {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export function ResearchDbtCatalogTab({
  elementary,
  elementaryErr,
}: {
  elementary: ElementaryStatus | null
  elementaryErr: string | null
}) {
  const byLayer = useMemo(() => {
    const m = new Map<string, typeof RESEARCH_DBT_MODELS>()
    for (const row of RESEARCH_DBT_MODELS) {
      const list = m.get(row.layer) ?? []
      list.push(row)
      m.set(row.layer, list)
    }
    return m
  }, [])

  const reportReady = Boolean(elementary?.present)

  return (
    <>
      <OpsSection title="Open Elementary report" collapsible defaultCollapsed={false}>
        <div className="flex flex-col gap-3 rounded border border-border/60 bg-secondary/30 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0 space-y-1">
            <div className="flex items-center gap-2 text-dense-label font-medium text-foreground">
              <GitBranch className="size-4 shrink-0 text-muted-foreground" />
              Lineage DAG · Catalog · Test coverage
            </div>
            <p className="text-dense-meta text-muted-foreground">
              Elementary OSS is a full SPA — open it in a new browser tab. Status and HTML both come
              from Research API (
              <code className="font-mono">GET /analytics/elementary</code>
              {' · '}
              <code className="font-mono">/analytics/elementary/files/*</code>
              ).
            </p>
            <p className="text-dense-meta font-mono text-muted-foreground">{ELEMENTARY_REPORT_URL}</p>
            <p className="text-dense-meta text-muted-foreground">
              {elementaryErr
                ? elementaryErr
                : reportReady
                  ? `Report present${elementary?.mtime ? ` · mtime ${formatWhen(elementary.mtime)}` : ''}`
                  : 'Report pending — regenerate after the next Dagster dbt-sepa run'}
            </p>
          </div>
          <Button asChild size="default" variant={reportReady ? 'default' : 'outline'}>
            <a href={ELEMENTARY_REPORT_URL} target="_blank" rel="noreferrer">
              <ExternalLink className="size-4" />
              Open Elementary report
            </a>
          </Button>
        </div>
      </OpsSection>

      <OpsSection title="Pipeline shape" collapsible defaultCollapsed={false}>
        <ol className="flex flex-col gap-2">
          {RESEARCH_DBT_LINEAGE.map((layer, idx) => (
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
          <code className="font-mono">raw_market.stock_daily</code>. Until then those tables stay
          empty while fundamental / structure / sentiment marts remain populated. Depth grows with
          daily Massive ingest + Dagster <code className="font-mono">dbt-sepa</code> (not the retired
          CronJob <code className="font-mono">bifrost-analytics-daily</code>).
        </p>
      </OpsSection>

      <OpsSection title="Data quality" collapsible defaultCollapsed>
        <p className="text-dense-body text-muted-foreground">
          Pass/fail history, freshness, and volume anomalies live in the Elementary report. Mart
          schema YAML includes <code className="font-mono">elementary.volume_anomalies</code> /{' '}
          <code className="font-mono">elementary.freshness_anomalies</code> (warn severity until
          history accumulates). Canonical output is{' '}
          <code className="font-mono">dw_stock.mart_sepa_*</code> on{' '}
          {RESEARCH_ENGINE_SUMMARY.goldenSource}.
        </p>
      </OpsSection>
    </>
  )
}
