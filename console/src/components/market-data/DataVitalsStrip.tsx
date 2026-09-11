import { useQuery } from '@tanstack/react-query'
import { fetchCoverageDimensions } from '@/api/marketDataDimensions'
import { DenseTag, Skeleton } from '@bifrost/ui'
import {
  fetchCoverageContracts,
  fetchCoverageDbSummary,
  fetchMarketStatus,
  fetchUniverseCount,
  isProxyError,
  type CoverageContractsResponse,
  type CoverageDbSummary,
  type MarketStatusResponse,
  type UniverseCountResponse,
} from '@/api/marketDataPlugin'
import { fetchMarketDataStatus } from '@/api/network'
import type { MarketDataWorkerInfo } from '@/api/satelliteBusTypes'
import {
  PENDING,
  countByKind,
  freshnessToday,
  judgeVital,
  vitalFill,
  vitalTagVariant,
  type Arrival,
  type VitalKind,
} from '@/components/market-data/dataVitalsModel'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

const REFETCH_MS = 60_000

function freshnessLastRun(
  rows: Array<{ dimension?: string; last_run_at?: string | null }> | undefined,
  dimension: string,
): string | undefined {
  const row = rows?.find(f => (f.dimension ?? '').toLowerCase() === dimension.toLowerCase())
  const last = row?.last_run_at?.trim()
  return last || undefined
}

function workerNextRun(workers: MarketDataWorkerInfo[] | undefined, pool: string): string | undefined {
  const w = workers?.find(x => (x.pool ?? '').toLowerCase() === pool.toLowerCase())
  const next = w?.next_run_at?.trim()
  return next || undefined
}

function universeCount(data: UniverseCountResponse | undefined, errored: boolean): number | null {
  if (errored || data == null) return null
  const n = data.total_tickers
  if (n == null || Number.isNaN(n)) return null
  return n
}

function stockDailyRows(data: CoverageDbSummary | undefined, errored: boolean): number | null {
  if (errored || data == null) return null
  const rows = data.counts?.stock_daily
  if (rows == null) return null
  return rows
}

function optionCounts(
  data: CoverageContractsResponse | undefined,
  errored: boolean,
): { underlyings: number | null; contracts: number | null } {
  if (errored || data == null) return { underlyings: null, contracts: null }
  const rows = data.rows ?? []
  const underlyings = data.count ?? rows.length
  const contracts = rows.reduce((sum, r) => sum + (r.contract_count ?? 0), 0)
  if (underlyings === 0 && contracts === 0) return { underlyings: null, contracts: null }
  return { underlyings, contracts }
}

function activeDimensions(data: MarketStatusResponse | undefined, errored: boolean): {
  ok: number
  total: number
} {
  if (errored || data == null) return { ok: 0, total: 0 }
  const items = data.freshness_summary ?? []
  const ok = items.filter(i => {
    const s = (i.status ?? '').toLowerCase()
    return s === 'ok' || s === 'success' || s === 'active'
  }).length
  return { ok, total: items.length }
}

/** A card judges on whichever of its reads has answered; it waits while none has. */
function arrivalOf(reads: Array<{ data: unknown; failed: boolean }>): Arrival {
  if (reads.some(r => r.data != null && !r.failed)) return 'arrived'
  if (reads.every(r => r.failed)) return 'failed'
  return 'pending'
}

export function DataVitalsStrip({
  onOpenCoverage,
}: {
  onOpenCoverage?: (panel: 'readiness' | 'financials' | 'quality') => void
}) {
  // The plugin's one definition of the session (C-F1). Same queryKey the
  // coverage tab uses, so this shares its cache rather than adding a read.
  const dimsQ = useQuery({
    queryKey: ['market-data', 'coverage', 'dimensions'],
    queryFn: fetchCoverageDimensions,
    staleTime: 60_000,
  })
  // undefined while in flight, null when the plugin cannot say — see judgeVital.
  const session: string | null | undefined =
    dimsQ.data != null ? (dimsQ.data.session ?? null) : dimsQ.isError ? null : undefined

  const universeQ = useQuery({
    queryKey: ['market-data', 'vitals', 'universe'],
    queryFn: fetchUniverseCount,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const stockQ = useQuery({
    queryKey: ['market-data', 'vitals', 'stock-daily'],
    queryFn: fetchCoverageDbSummary,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const contractsQ = useQuery({
    queryKey: ['market-data', 'vitals', 'contracts'],
    queryFn: () => fetchCoverageContracts(),
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const statusQ = useQuery({
    queryKey: ['market-data', 'vitals', 'analytics'],
    queryFn: fetchMarketStatus,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })
  const probeQ = useQuery({
    queryKey: ['market-data', 'live-probe', 'status'],
    queryFn: fetchMarketDataStatus,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const universeErr =
    universeQ.isError ||
    (universeQ.data != null && isProxyError(universeQ.data)) ||
    (universeQ.data != null && !isProxyError(universeQ.data) && universeQ.data.ok === false)
  const stockErr =
    stockQ.isError ||
    (stockQ.data != null && isProxyError(stockQ.data)) ||
    (stockQ.data != null && !isProxyError(stockQ.data) && stockQ.data.ok === false)
  const contractsErr =
    contractsQ.isError ||
    (contractsQ.data != null && isProxyError(contractsQ.data)) ||
    (contractsQ.data != null && !isProxyError(contractsQ.data) && contractsQ.data.ok === false)
  const statusErr =
    statusQ.isError ||
    (statusQ.data != null && isProxyError(statusQ.data)) ||
    (statusQ.data != null && !isProxyError(statusQ.data) && statusQ.data.ok === false)

  const universeOk =
    universeQ.data != null && !isProxyError(universeQ.data) ? universeQ.data : undefined
  const stockOk = stockQ.data != null && !isProxyError(stockQ.data) ? stockQ.data : undefined
  const contractsOk =
    contractsQ.data != null && !isProxyError(contractsQ.data) ? contractsQ.data : undefined
  const statusOk = statusQ.data != null && !isProxyError(statusQ.data) ? statusQ.data : undefined
  const workers = probeQ.data?.workers
  const dbFresh = stockOk?.freshness
  const probeFresh = probeQ.data?.freshness

  const probeRead = { data: probeQ.data, failed: probeQ.isError }
  const schedule = arrivalOf([probeRead])

  const stockRows = stockDailyRows(stockOk, stockErr)
  const stockLast =
    freshnessLastRun(dbFresh, 'stock_daily') ?? freshnessLastRun(probeFresh, 'stock_daily')
  const stockVerdict = judgeVital(stockLast, workerNextRun(workers, 'stocks'), {
    inputs: arrivalOf([{ data: stockOk, failed: stockErr }, probeRead]),
    session,
    schedule,
  })

  const option = optionCounts(contractsOk, contractsErr)
  const optionLast =
    freshnessLastRun(dbFresh, 'option_contract') ??
    freshnessLastRun(dbFresh, 'option_contracts') ??
    freshnessLastRun(probeFresh, 'option_contract') ??
    freshnessLastRun(probeFresh, 'option_contracts')
  const optionVerdict = judgeVital(optionLast, workerNextRun(workers, 'options'), {
    inputs: arrivalOf([
      { data: contractsOk, failed: contractsErr },
      { data: stockOk, failed: stockErr },
      probeRead,
    ]),
    session,
    schedule,
  })

  const dims = activeDimensions(statusOk, statusErr)
  const statusArrival = arrivalOf([{ data: statusOk, failed: statusErr }])
  const today =
    statusArrival === 'arrived' && typeof session === 'string'
      ? freshnessToday(statusOk?.freshness_summary ?? [], undefined, session)
      : null
  const freshnessKind: VitalKind =
    today?.kind ??
    (statusArrival === 'pending' || session === undefined ? 'pending' : 'unknown')
  const freshnessText = today?.text ?? (freshnessKind === 'pending' ? PENDING.text : '—')

  const tickerLast =
    freshnessLastRun(dbFresh, 'ticker_sync') ?? freshnessLastRun(probeFresh, 'ticker_sync')
  const uniCount = universeCount(universeOk, universeErr)
  const uniVerdict = judgeVital(tickerLast, workerNextRun(workers, 'stocks'), {
    inputs: arrivalOf([{ data: universeOk, failed: universeErr }]),
    session,
    schedule,
  })

  const score = countByKind([stockVerdict.kind, optionVerdict.kind, freshnessKind, uniVerdict.kind])
  const total = 4
  const loading = stockQ.isLoading && stockOk == null

  return (
    <OpsSection
      title="Stock summary"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          {score.pending === total ? (
            <DenseTag variant="neutral">loading…</DenseTag>
          ) : (
            <>
              <DenseTag variant="success">today {score.ok}</DenseTag>
              <DenseTag variant="warning">scheduled {score.scheduled}</DenseTag>
              <DenseTag variant="danger">missing {score.missing}</DenseTag>
              {score.pending > 0 ? (
                <DenseTag variant="neutral" title="Still loading — not yet judged">
                  pending {score.pending}
                </DenseTag>
              ) : null}
              {score.unknown > 0 ? (
                <DenseTag variant="neutral" title="A read failed or the session is unavailable — cannot judge">
                  unknown {score.unknown}
                </DenseTag>
              ) : null}
            </>
          )}
        </div>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <div className="grid grid-cols-4 gap-1.5">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : (
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={score.ok}
            thin={score.scheduled}
            blocked={score.missing}
            unknown={score.pending + score.unknown}
            total={total}
            caption="today"
          />
          <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-4">
            <DashCard
              title="Stock Daily"
              tag={stockVerdict.text}
              tagVariant={vitalTagVariant(stockVerdict.kind)}
              value={fmtCount(stockRows)}
              rawValue={stockRows}
              unit="rows"
              caption={stockLast != null ? stockLast.slice(0, 10) : 'no last_run'}
              onClick={onOpenCoverage ? () => onOpenCoverage('quality') : undefined}
            >
              <Meter
                fillPct={vitalFill(stockVerdict.kind)}
                toneClass={toneByLevel(stockVerdict.kind)}
                label={stockVerdict.text}
              />
            </DashCard>
            <DashCard
              title="Option Contracts"
              tag={optionVerdict.text}
              tagVariant={vitalTagVariant(optionVerdict.kind)}
              value={fmtCount(option.underlyings)}
              rawValue={option.underlyings}
              unit="underlyings"
              caption={`${fmtCount(option.contracts)} contracts`}
              onClick={onOpenCoverage ? () => onOpenCoverage('readiness') : undefined}
            >
              <Meter
                fillPct={vitalFill(optionVerdict.kind)}
                toneClass={toneByLevel(optionVerdict.kind)}
                label={optionVerdict.text}
              />
            </DashCard>
            <DashCard
              title="Data Freshness"
              tag={freshnessText}
              tagVariant={vitalTagVariant(freshnessKind)}
              value={dims.total > 0 ? `${dims.ok}/${dims.total}` : '—'}
              rawValue={today?.todayCount}
              unit="active"
              onClick={onOpenCoverage ? () => onOpenCoverage('quality') : undefined}
            >
              <Meter
                fillPct={vitalFill(freshnessKind, today?.ratio)}
                toneClass={toneByLevel(freshnessKind)}
                label={freshnessText}
              />
            </DashCard>
            <DashCard
              title="Universe"
              tag={uniVerdict.text}
              tagVariant={vitalTagVariant(uniVerdict.kind)}
              value={fmtCount(uniCount)}
              rawValue={uniCount}
              unit="tickers"
              caption={tickerLast != null ? tickerLast.slice(0, 10) : 'no ticker_sync'}
              onClick={onOpenCoverage ? () => onOpenCoverage('readiness') : undefined}
            >
              <Meter
                fillPct={vitalFill(uniVerdict.kind)}
                toneClass={toneByLevel(uniVerdict.kind)}
                label={uniVerdict.text}
              />
            </DashCard>
          </div>
        </div>
      )}
    </OpsSection>
  )
}
