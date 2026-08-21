import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { fetchQualityScore, isProxyError } from '@/api/marketDataPlugin'
import { DataVitalsStrip } from '@/components/market-data/DataVitalsStrip'
import {
  MarketDataFreshnessTable,
  MarketDataWorkersTable,
} from '@/components/market-data/MarketDataProbeTables'
import { sortFreshness, workerReady } from '@/components/market-data/marketDataProbeUtils'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import type { MarketDataLiveProbeState } from '@/hooks/useMarketDataLiveProbe'

function reachToVerdict(reach: MarketDataLiveProbeState['probeReach']): {
  lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
} {
  switch (reach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

export function MarketDataOverviewTab({
  marketProbe,
  onOpenCoverageReadiness,
}: {
  marketProbe: MarketDataLiveProbeState
  onOpenCoverageReadiness?: () => void
}) {
  const mdReach = marketProbe.isLoading ? 'unknown' : marketProbe.probeReach
  const marketVerdict = reachToVerdict(mdReach)

  const deployments = marketProbe.status?.deployments ?? []
  const workers = marketProbe.status?.workers ?? []
  const freshness = useMemo(
    () => sortFreshness(marketProbe.status?.freshness ?? []),
    [marketProbe.status?.freshness],
  )
  const freshnessOk = freshness.filter(f => f.verdict === 'ok').length
  const freshnessAttention = freshness.filter(f => f.verdict !== 'ok')
  const freshnessAllOk =
    freshness.length > 0 && freshnessAttention.length === 0 && !marketProbe.isLoading
  const workersAllReady =
    workers.length > 0 && workers.every(workerReady) && !marketProbe.isLoading
  const sectionHealthy =
    !marketProbe.isLoading && marketProbe.probeReach === 'ok' && freshnessAllOk && workersAllReady

  const readiness = marketProbe.status?.readiness_rollup ?? null

  const qualityQ = useQuery({
    queryKey: ['market-data', 'coverage', 'quality-score'],
    queryFn: fetchQualityScore,
    refetchInterval: 60_000,
    retry: 1,
  })
  const quality =
    qualityQ.data != null && !isProxyError(qualityQ.data) ? qualityQ.data : null
  const qualitySummary =
    quality?.summary ?? (quality?.ok === true ? 'PASS' : quality != null ? 'FAIL' : null)

  return (
    <div className="flex flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Market Data plugin verdict"
        title="MARKET DATA PLUGIN"
        lamp={marketVerdict.lamp}
        tagLabel={marketVerdict.tagLabel}
        tagVariant={marketVerdict.tagVariant}
        summary={marketProbe.summary}
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={marketProbe.isLoading}
            onClick={() => marketProbe.refetch()}
          >
            Refresh
          </Button>
        }
        meta={
          <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <span>
              freshness {freshness.length > 0 ? `${freshnessOk}/${freshness.length} ok` : '—'}
              {marketProbe.status?.autonomy != null
                ? ` · autonomy ${marketProbe.status.autonomy}`
                : ''}
              {marketProbe.status?.health_reachability != null
                ? ` · health ${marketProbe.status.health_reachability}`
                : ''}
            </span>
            {qualitySummary != null ? (
              <DenseTag
                variant={qualitySummary === 'PASS' ? 'success' : 'danger'}
                title="Coverage → Quality Score"
              >
                Quality {qualitySummary}
              </DenseTag>
            ) : null}
          </span>
        }
      />

      <DataVitalsStrip />

      <OpsSection
        title="Workers & freshness"
        description="L0 observe via platform-api GET /api/v1/plugins/market-data/status"
        leading={<StatusLamp value={mdReach} kind="reach" />}
        headerExtra={
          <DenseTag variant={marketVerdict.tagVariant}>{marketVerdict.tagLabel}</DenseTag>
        }
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={sectionHealthy}
      >
        <div className="flex flex-col gap-4">
          {freshnessAttention.length > 0 ? (
            <div
              className="rounded-md border border-[color-mix(in_srgb,var(--warning)_45%,var(--border))] bg-[color-mix(in_srgb,var(--warning)_10%,var(--secondary))] px-3 py-2"
              role="status"
            >
              <p className="m-0 text-[var(--text-dense-label)] font-semibold">Attention</p>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {freshnessAttention.length} dimension
                {freshnessAttention.length === 1 ? '' : 's'} not ok:{' '}
                {freshnessAttention.map(f => `${f.dimension} (${f.verdict})`).join(' · ')}
              </p>
            </div>
          ) : null}

          <div className="flex flex-col gap-2">
            <OpsSubsectionTitle>Freshness</OpsSubsectionTitle>
            {freshness.length === 0 ? (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No ingest_freshness rows yet — run workers / daily CronJobs, then refresh.
              </p>
            ) : (
              <MarketDataFreshnessTable rows={freshness} collapsibleWhenOk={freshnessAllOk} />
            )}
          </div>

          <div className="flex flex-col gap-2">
            <OpsSubsectionTitle>Workers</OpsSubsectionTitle>
            {deployments.length === 0 && workers.length === 0 ? (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No deployment / worker snapshot yet — apply k8s/base or check platform-api probe.
              </p>
            ) : (
              <MarketDataWorkersTable
                deployments={deployments}
                workers={workers}
                collapsibleWhenOk={workersAllReady}
              />
            )}
          </div>

          {readiness != null ? (
            <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="m-0 text-[var(--text-dense-label)] font-semibold">
                  Data readiness rollup
                </p>
                {onOpenCoverageReadiness != null ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={onOpenCoverageReadiness}
                    title="Open Coverage → Readiness panel"
                  >
                    Open Readiness
                  </Button>
                ) : null}
              </div>
              <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Universe {readiness.universe} · Snapshot {readiness.snapshot_covered}/
                {readiness.snapshot_rows} · Vendor gaps {readiness.vendor_gap_count} · as-of{' '}
                {readiness.as_of}
                {readiness.source ? ` · ${readiness.source}` : ''}
              </p>
              <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Producer dashboard: Coverage → Readiness (?tab=coverage&panel=readiness). Trade owns
                SEPA criteria / stock_readiness_daily publish — not this panel.
              </p>
            </div>
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Readiness rollup unavailable (Plugin snapshot-coverage / vendor-gap unreachable).
              </p>
              {onOpenCoverageReadiness != null ? (
                <Button variant="outline" size="sm" onClick={onOpenCoverageReadiness}>
                  Open Readiness
                </Button>
              ) : null}
            </div>
          )}
        </div>
      </OpsSection>

      <OpsSection
        title="API reachability"
        description="Plugin API is reached via platform-api proxy (/api/v1/plugins/market-data/api/market/*)."
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={marketProbe.probeReach === 'ok'}
      >
        <ul className="m-0 list-disc space-y-1 pl-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <li>
            Status probe:{' '}
            <span className="font-mono text-[var(--foreground)]">
              GET /api/v1/plugins/market-data/status
            </span>
          </li>
          <li>
            Plugin proxy:{' '}
            <span className="font-mono text-[var(--foreground)]">
              /api/v1/plugins/market-data/api/market/*
            </span>{' '}
            → market-data-api:8790 (or MARKET_DATA_API_URL)
          </li>
          <li>
            Overall reach:{' '}
            <DenseTag variant={marketVerdict.tagVariant}>{marketVerdict.tagLabel}</DenseTag>
          </li>
        </ul>
      </OpsSection>
    </div>
  )
}
