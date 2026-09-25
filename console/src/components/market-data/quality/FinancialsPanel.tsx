import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  enqueueIngestSlot,
  fetchReadinessFinancialsByType,
  fetchReadinessFinancialsCoverage,
  fetchReadinessFinancialsFillRate,
  fetchReferenceOverviewCoverage,
  fetchReferenceRelatedCoverage,
  fetchUniverseCount,
  isProxyError,
  type FinancialsByTypeResponse,
  type FinancialsCoverageSymbolsResponse,
  type FinancialsFillRateResponse,
  type ReferenceCoverageResponse,
  type UniverseCountResponse,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { FieldFillRateTable } from '@/components/market-data/quality/FieldFillRateTable'
import {
  FinancialsOverviewStrip,
  type FinancialsCounts,
} from '@/components/market-data/quality/FinancialsOverviewStrip'
import { ReferenceQualitySection } from '@/components/market-data/quality/ReferenceQualitySection'
import { RefillAction } from '@/components/market-data/quality/RefillAction'
import {
  FUNDAMENTALS_REFILL,
  MARKET_FUNDAMENTALS_REFILL,
  slotMessage,
  slotNote,
} from '@/components/market-data/quality/refillModel'
import { SepaGapsSection } from '@/components/market-data/quality/SepaGapsSection'

const GAP_KEYS = [
  ['market-data', 'financials', 'sepa-gaps'],
  ['market-data', 'financials', 'by-type'],
  ['market-data', 'financials', 'coverage-symbols'],
] as const

function slotRefill(spec: typeof FUNDAMENTALS_REFILL) {
  return (
    <RefillAction
      key={spec.slot}
      label={spec.label}
      title={spec.title}
      message={slotMessage(spec, null)}
      invalidateKeys={GAP_KEYS}
      run={async () => {
        const res = await enqueueIngestSlot({ slot: spec.slot })
        if (isProxyError(res)) throw new Error(res.error)
        return { queued: res.enqueued ?? 0, deduped: res.deduped, note: slotNote(res) }
      }}
    />
  )
}

const REFETCH_MS = 60_000

function unwrap<T>(
  data: T | { ok: false; error: string } | undefined,
): { value: T | null; error: string | null } {
  if (data == null) return { value: null, error: null }
  if (isProxyError(data)) return { value: null, error: data.error }
  return { value: data as T, error: null }
}

function coverageLen(
  cov: FinancialsCoverageSymbolsResponse | null,
  key: 'short_interest' | 'short_volume',
): number {
  const v = cov?.[key]
  return Array.isArray(v) ? v.length : 0
}

export function FinancialsPanel() {
  const universeQ = useQuery({
    queryKey: ['market-data', 'financials', 'universe-count'],
    queryFn: fetchUniverseCount,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const byTypeQ = useQuery({
    queryKey: ['market-data', 'financials', 'by-type'],
    queryFn: fetchReadinessFinancialsByType,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const coverageQ = useQuery({
    queryKey: ['market-data', 'financials', 'coverage-symbols'],
    queryFn: fetchReadinessFinancialsCoverage,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const fillQ = useQuery({
    queryKey: ['market-data', 'financials', 'fill-rate'],
    queryFn: () => fetchReadinessFinancialsFillRate(),
    refetchInterval: 120_000,
    retry: 1,
  })
  const overviewQ = useQuery({
    queryKey: ['market-data', 'financials', 'overview-coverage'],
    queryFn: fetchReferenceOverviewCoverage,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const relatedQ = useQuery({
    queryKey: ['market-data', 'financials', 'related-coverage'],
    queryFn: fetchReferenceRelatedCoverage,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const universe = unwrap<UniverseCountResponse>(universeQ.data)
  const byType = unwrap<FinancialsByTypeResponse>(byTypeQ.data)
  const coverage = unwrap<FinancialsCoverageSymbolsResponse>(coverageQ.data)
  const fill = unwrap<FinancialsFillRateResponse>(fillQ.data)
  const overview = unwrap<ReferenceCoverageResponse>(overviewQ.data)
  const related = unwrap<ReferenceCoverageResponse>(relatedQ.data)

  const counts: FinancialsCounts = useMemo(
    () => ({
      ...(byType.value?.counts ?? {}),
      short_interest: coverageLen(coverage.value, 'short_interest'),
      short_volume: coverageLen(coverage.value, 'short_volume'),
    }),
    [byType.value, coverage.value],
  )

  return (
    <div className="flex flex-col gap-4">
      <FinancialsOverviewStrip
        loading={byTypeQ.isLoading || coverageQ.isLoading || universeQ.isLoading}
        universeTickers={universe.value?.total_tickers ?? 0}
        counts={counts}
        shortInterest={counts.short_interest ?? 0}
        shortVolume={counts.short_volume ?? 0}
        error={byType.error ?? coverage.error}
      />
      <OpsSection
        title="Refill"
        description="Two slots cover the six gap reports below — the plugin groups them, so each button says which. Plugin POST /market/ingest/enqueue-slot"
        headerExtra={
          <div className="flex flex-wrap items-center gap-3">
            {slotRefill(FUNDAMENTALS_REFILL)}
            {slotRefill(MARKET_FUNDAMENTALS_REFILL)}
          </div>
        }
      >
        <ul className="m-0 flex flex-col gap-1 pl-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <li>
            <span className="font-semibold">{FUNDAMENTALS_REFILL.label}</span> —{' '}
            {FUNDAMENTALS_REFILL.covers.join(' · ')} (one job per symbol, missing first)
          </li>
          <li>
            <span className="font-semibold">{MARKET_FUNDAMENTALS_REFILL.label}</span> —{' '}
            {MARKET_FUNDAMENTALS_REFILL.covers.join(' · ')} (whole market, a page at a time)
          </li>
        </ul>
      </OpsSection>
      <SepaGapsSection />
      <FieldFillRateTable
        tables={fill.value?.tables}
        loading={fillQ.isLoading}
        error={fill.error}
      />
      <ReferenceQualitySection
        overview={overview.value}
        overviewLoading={overviewQ.isLoading}
        overviewError={overview.error}
        related={related.value}
        relatedLoading={relatedQ.isLoading}
        relatedError={related.error}
      />
    </div>
  )
}
