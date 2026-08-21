import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
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
import { FieldFillRateTable } from '@/components/market-data/quality/FieldFillRateTable'
import {
  FinancialsOverviewStrip,
  type FinancialsCounts,
} from '@/components/market-data/quality/FinancialsOverviewStrip'
import { ReferenceQualitySection } from '@/components/market-data/quality/ReferenceQualitySection'
import { SepaGapsSection } from '@/components/market-data/quality/SepaGapsSection'

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
