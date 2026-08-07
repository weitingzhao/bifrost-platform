import { useQuery } from '@tanstack/react-query'
import { Skeleton, cn } from '@bifrost/ui'
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

const REFETCH_MS = 60_000
const SCHEDULED_WINDOW_MS = 6 * 60 * 60 * 1000

type Verdict = { text: string; colorClass: string }

/** Today's-data verdict: UTC date of last_run_at vs next_run within 6h. */
function computeVerdict(lastRunAt?: string, nextRunAt?: string): Verdict {
  const today = new Date().toISOString().slice(0, 10)
  const lastDate = lastRunAt?.trim().slice(0, 10)
  if (lastDate && lastDate === today) {
    return { text: 'Today OK', colorClass: 'text-success' }
  }
  if (nextRunAt?.trim()) {
    const nextMs = new Date(nextRunAt).getTime()
    if (Number.isFinite(nextMs)) {
      const delta = nextMs - Date.now()
      if (delta >= 0 && delta <= SCHEDULED_WINDOW_MS) {
        const hours = Math.max(1, Math.round(delta / (60 * 60 * 1000)))
        return { text: `Scheduled ~${hours}h`, colorClass: 'text-warning' }
      }
    }
  }
  return { text: 'Missing', colorClass: 'text-destructive' }
}

function utcToday(): string {
  return new Date().toISOString().slice(0, 10)
}

function freshnessLastRun(
  rows: Array<{ dimension?: string; last_run_at?: string | null }> | undefined,
  dimension: string,
): string | undefined {
  const row = rows?.find(
    f => (f.dimension ?? '').toLowerCase() === dimension.toLowerCase(),
  )
  const last = row?.last_run_at?.trim()
  return last || undefined
}

function workerNextRun(
  workers: MarketDataWorkerInfo[] | undefined,
  pool: string,
): string | undefined {
  const w = workers?.find(x => (x.pool ?? '').toLowerCase() === pool.toLowerCase())
  const next = w?.next_run_at?.trim()
  return next || undefined
}

function formatCount(n: number): string {
  return n.toLocaleString('en-US')
}

function VitalCell({
  label,
  loading,
  value,
  detail,
  verdict,
}: {
  label: string
  loading: boolean
  value: string
  detail?: string
  verdict?: Verdict
}) {
  return (
    <div className="min-w-0 flex-1">
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{label}</p>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-24" />
      ) : (
        <>
          <p className="m-0 mt-0.5 font-semibold tabular-nums text-[var(--text-dense-body)] text-[var(--foreground)]">
            {value}
          </p>
          {verdict ? (
            <p
              className={cn(
                'm-0 mt-0.5 truncate text-[var(--text-dense-caption)] font-medium',
                verdict.colorClass,
              )}
            >
              {verdict.text}
            </p>
          ) : detail ? (
            <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {detail}
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

function universeDisplay(data: UniverseCountResponse | undefined, errored: boolean): string {
  if (errored || data == null) return '—'
  const n = data.total_tickers
  if (n == null || Number.isNaN(n)) return '—'
  return `${formatCount(n)} tickers`
}

function stockDailyDisplay(
  data: CoverageDbSummary | undefined,
  errored: boolean,
): { value: string } {
  if (errored || data == null) return { value: '—' }
  const rows = data.counts?.stock_daily
  if (rows == null) return { value: '—' }
  return { value: `${formatCount(rows)} rows` }
}

function optionContractsDisplay(
  data: CoverageContractsResponse | undefined,
  errored: boolean,
): string {
  if (errored || data == null) return '—'
  const rows = data.rows ?? []
  const underlyings = data.count ?? rows.length
  const contracts = rows.reduce((sum, r) => sum + (r.contract_count ?? 0), 0)
  if (underlyings === 0 && contracts === 0) return '—'
  return `${formatCount(underlyings)} underlyings · ${formatCount(contracts)} contracts`
}

function analyticsDisplay(
  data: MarketStatusResponse | undefined,
  errored: boolean,
): string {
  if (errored || data == null) return '—'
  const items = data.freshness_summary ?? []
  if (items.length === 0) return '—'
  const ok = items.filter(i => {
    const s = (i.status ?? '').toLowerCase()
    return s === 'ok' || s === 'success' || s === 'active'
  }).length
  return `${ok}/${items.length} dimensions active`
}

/** Data Freshness cell: count dimensions with last_run today; worst overall verdict. */
function freshnessTodayVerdict(data: MarketStatusResponse | undefined): Verdict | undefined {
  if (data == null) return undefined
  const items = data.freshness_summary ?? []
  if (items.length === 0) return undefined
  const today = utcToday()
  const todayCount = items.filter(i => {
    const d = i.last_run_at?.trim().slice(0, 10)
    return d === today
  }).length
  const total = items.length
  const text = `${todayCount}/${total} today`
  if (todayCount === total) {
    return { text, colorClass: 'text-success' }
  }
  if (todayCount === 0) {
    return { text, colorClass: 'text-destructive' }
  }
  return { text, colorClass: 'text-warning' }
}

/** Universe: ticker_sync last_run; stocks pool next_run as schedule fallback. */
function universeVerdict(lastRunAt?: string, nextRunAt?: string): Verdict {
  const today = utcToday()
  const lastDate = lastRunAt?.trim().slice(0, 10)
  if (lastDate && lastDate === today) {
    return { text: 'Today OK', colorClass: 'text-success' }
  }
  // ticker_sync typically shares stocks pool CronJob cadence
  if (nextRunAt?.trim()) {
    const nextMs = new Date(nextRunAt).getTime()
    if (Number.isFinite(nextMs)) {
      const delta = nextMs - Date.now()
      if (delta >= 0 && delta <= SCHEDULED_WINDOW_MS) {
        const hours = Math.max(1, Math.round(delta / (60 * 60 * 1000)))
        return { text: `Scheduled ~${hours}h`, colorClass: 'text-warning' }
      }
    }
  }
  return { text: 'Missing', colorClass: 'text-destructive' }
}

export function DataVitalsStrip() {
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
  // Share cache with Overview live probe for workers.next_run_at
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

  const stock = stockDailyDisplay(stockOk, stockErr)
  const stockLast =
    freshnessLastRun(dbFresh, 'stock_daily') ?? freshnessLastRun(probeFresh, 'stock_daily')
  const stockVerdict =
    stockOk != null || probeQ.data != null
      ? computeVerdict(stockLast, workerNextRun(workers, 'stocks'))
      : undefined
  const optionLast =
    freshnessLastRun(dbFresh, 'option_contract') ??
    freshnessLastRun(dbFresh, 'option_contracts') ??
    freshnessLastRun(probeFresh, 'option_contract') ??
    freshnessLastRun(probeFresh, 'option_contracts')
  const optionVerdict =
    contractsOk != null || stockOk != null || probeQ.data != null
      ? computeVerdict(optionLast, workerNextRun(workers, 'options'))
      : undefined
  const dataFreshVerdict = !statusErr ? freshnessTodayVerdict(statusOk) : undefined
  const tickerLast =
    freshnessLastRun(dbFresh, 'ticker_sync') ?? freshnessLastRun(probeFresh, 'ticker_sync')
  const uniVerdict =
    !universeErr && universeOk != null
      ? universeVerdict(tickerLast, workerNextRun(workers, 'stocks'))
      : undefined

  return (
    <div
      className="flex flex-wrap items-center gap-6 rounded-md bg-[var(--secondary)] px-4 py-3"
      role="region"
      aria-label="Data vitals"
    >
      <VitalCell
        label="Stock Daily"
        loading={stockQ.isLoading}
        value={stock.value}
        verdict={stockVerdict}
      />
      <VitalCell
        label="Option Contracts"
        loading={contractsQ.isLoading || stockQ.isLoading}
        value={optionContractsDisplay(contractsOk, contractsErr)}
        verdict={optionVerdict}
      />
      <VitalCell
        label="Data Freshness"
        loading={statusQ.isLoading}
        value={analyticsDisplay(statusOk, statusErr)}
        verdict={dataFreshVerdict}
      />
      <VitalCell
        label="Universe"
        loading={universeQ.isLoading}
        value={universeDisplay(universeOk, universeErr)}
        verdict={uniVerdict}
      />
    </div>
  )
}
