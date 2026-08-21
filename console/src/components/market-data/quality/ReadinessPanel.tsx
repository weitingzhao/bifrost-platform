import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchMarketStatus,
  fetchQualityScore,
  fetchReadinessDateCoverage,
  fetchReadinessSnapshotCoverage,
  fetchReadinessVendorGapDetail,
  fetchUniverseCount,
  isProxyError,
  type DateCoverageResponse,
  type MarketStatusResponse,
  type QualityScoreResponse,
  type SnapshotCoverageResponse,
  type UniverseCountResponse,
  type VendorGapResponse,
} from '@/api/marketDataPlugin'
import { DateCoverageSection } from '@/components/market-data/quality/DateCoverageSection'
import { DailyChecklistSection } from '@/components/market-data/quality/DailyChecklistSection'
import { ReadinessKpiStrip } from '@/components/market-data/quality/ReadinessKpiStrip'
import { ReadinessVerdictSection } from '@/components/market-data/quality/ReadinessVerdictSection'
import { SnapshotByTypeTable } from '@/components/market-data/quality/SnapshotByTypeTable'
import { VendorGapDetailTable } from '@/components/market-data/quality/VendorGapDetailTable'
import {
  buildReadinessChecks,
  deriveSnapshotCoverage,
  filterActionableLowCoverageDates,
  partitionVendorGaps,
  type ReadinessCheckId,
} from '@/components/market-data/quality/readinessChecks'

const REFETCH_MS = 60_000

/** Dashboard window — avoid Plugin defaults (days_back=420) that can 500 under load. */
const DATE_COVERAGE_DAYS = 30
const DATE_COVERAGE_MIN_SYMBOLS = 100

function unwrap<T extends { ok?: boolean; error?: string }>(
  data: T | { ok: false; error: string } | undefined,
): { value: T | null; error: string | null } {
  if (data == null) return { value: null, error: null }
  if (isProxyError(data)) return { value: null, error: data.error }
  return { value: data as T, error: null }
}

export function ReadinessPanel() {
  const universeQ = useQuery({
    queryKey: ['market-data', 'readiness', 'universe-count'],
    queryFn: fetchUniverseCount,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const snapshotQ = useQuery({
    queryKey: ['market-data', 'readiness', 'snapshot-coverage'],
    queryFn: fetchReadinessSnapshotCoverage,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const vendorQ = useQuery({
    queryKey: ['market-data', 'readiness', 'vendor-gap'],
    queryFn: () => fetchReadinessVendorGapDetail({ limit: 200 }),
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const statusQ = useQuery({
    queryKey: ['market-data', 'readiness', 'market-status'],
    queryFn: fetchMarketStatus,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const qualityQ = useQuery({
    queryKey: ['market-data', 'readiness', 'quality-score-for-bars'],
    queryFn: fetchQualityScore,
    refetchInterval: 120_000,
    retry: 1,
  })
  const dateQ = useQuery({
    queryKey: [
      'market-data',
      'readiness',
      'date-coverage',
      DATE_COVERAGE_DAYS,
      DATE_COVERAGE_MIN_SYMBOLS,
    ],
    queryFn: () =>
      fetchReadinessDateCoverage({
        days_back: DATE_COVERAGE_DAYS,
        min_symbols: DATE_COVERAGE_MIN_SYMBOLS,
      }),
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const universe = unwrap<UniverseCountResponse>(universeQ.data)
  const snapshot = unwrap<SnapshotCoverageResponse>(snapshotQ.data)
  const vendor = unwrap<VendorGapResponse>(vendorQ.data)
  const status = unwrap<MarketStatusResponse>(statusQ.data)
  const quality = unwrap<QualityScoreResponse>(qualityQ.data)
  const dateCov = unwrap<DateCoverageResponse>(dateQ.data)

  const stockDaily = useMemo(() => {
    const item = (quality.value?.checks ?? []).find(c => c.check === 'stock_daily_coverage')
    if (item == null) return null
    return {
      symbolCount: Number(item.symbol_count ?? 0),
      watchlistGaps: Number(item.gap_count ?? 0),
    }
  }, [quality.value])

  const vendorParts = useMemo(() => {
    const rows = vendor.value?.gaps ?? []
    const parts = partitionVendorGaps(rows)
    // Prefer detail partition when we have rows; fall back to server gap_count.
    // Until Plugin excludes zero-close, detail rows are the source of truth.
    if (rows.length > 0) {
      return {
        actionableCount: parts.actionable.length,
        zeroSnapshotCount: parts.zeroSnapshot.length,
        actionableRows: parts.actionable,
      }
    }
    const serverCount = vendor.value?.gap_count ?? 0
    const zeroFromServer = Number(
      (vendor.value as VendorGapResponse & { zero_snapshot_count?: number })
        ?.zero_snapshot_count ?? 0,
    )
    return {
      actionableCount: Math.max(0, serverCount - zeroFromServer),
      zeroSnapshotCount: zeroFromServer,
      actionableRows: [] as typeof parts.actionable,
    }
  }, [vendor.value])

  const dateParts = useMemo(() => {
    const raw = dateCov.value?.low_coverage_dates ?? []
    const actionable = filterActionableLowCoverageDates(raw)
    return {
      actionableDates: actionable,
      actionableCount: actionable.length,
      thinIgnored: Math.max(0, raw.length - actionable.length),
    }
  }, [dateCov.value])

  const loading =
    universeQ.isLoading ||
    snapshotQ.isLoading ||
    vendorQ.isLoading ||
    statusQ.isLoading ||
    qualityQ.isLoading ||
    dateQ.isLoading

  const checkErrors = useMemo((): Partial<Record<ReadinessCheckId, string | null>> => {
    return {
      universe: universe.error,
      snapshot_coverage: snapshot.error,
      vendor_gap: vendor.error,
      freshness: status.error,
      bar_aggregate:
        quality.error ??
        (quality.value != null && stockDaily == null
          ? 'quality-score missing stock_daily_coverage check'
          : null),
      date_coverage: dateCov.error,
    }
  }, [
    universe.error,
    snapshot.error,
    vendor.error,
    status.error,
    quality.error,
    quality.value,
    stockDaily,
    dateCov.error,
  ])

  const probeFailureCount = Object.values(checkErrors).filter(Boolean).length

  const checks = useMemo(
    () =>
      buildReadinessChecks({
        universe: universe.value,
        snapshot: snapshot.value,
        status: status.value,
        actionableVendorGapCount: vendorParts.actionableCount,
        zeroSnapshotCount: vendorParts.zeroSnapshotCount,
        vendorSessionDate: vendor.value?.session_date ?? null,
        stockDailySymbolCount: stockDaily?.symbolCount ?? null,
        watchlistBarGaps: stockDaily?.watchlistGaps ?? null,
        actionableLowCoverageDates: dateParts.actionableCount,
        thinDaysIgnored: dateParts.thinIgnored,
        errors: checkErrors,
      }),
    [
      universe.value,
      snapshot.value,
      status.value,
      vendorParts,
      vendor.value?.session_date,
      stockDaily,
      dateParts,
      checkErrors,
    ],
  )

  const snapDerived = deriveSnapshotCoverage(snapshot.value)

  return (
    <div className="flex flex-col gap-4">
      <ReadinessVerdictSection
        checks={checks}
        loading={loading}
        probeFailureCount={probeFailureCount}
      />
      <ReadinessKpiStrip
        loading={loading}
        universeTickers={universe.value?.total_tickers ?? 0}
        snapshot={snapDerived}
        vendorGapCount={vendorParts.actionableCount}
        lowCoverageDates={dateParts.actionableCount}
      />
      <SnapshotByTypeTable
        rows={snapshot.value?.by_instrument_type ?? []}
        loading={snapshotQ.isLoading}
        error={snapshot.error}
        sessionDate={snapshot.value?.session_date ?? null}
      />
      <VendorGapDetailTable
        gaps={
          vendorParts.actionableRows.length > 0
            ? vendorParts.actionableRows
            : (vendor.value?.gaps ?? [])
        }
        gapCount={vendorParts.actionableCount}
        loading={vendorQ.isLoading}
        error={vendor.error}
        sessionDate={vendor.value?.session_date ?? null}
        zeroSnapshotCount={vendorParts.zeroSnapshotCount}
      />
      <DateCoverageSection
        dates={dateParts.actionableDates}
        count={dateParts.actionableCount}
        loading={dateQ.isLoading}
        error={dateCov.error}
        thinDaysIgnored={dateParts.thinIgnored}
      />
      <DailyChecklistSection />
    </div>
  )
}
