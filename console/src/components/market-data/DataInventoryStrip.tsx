import { useQuery } from '@tanstack/react-query'
import { DenseTag, Skeleton } from '@bifrost/ui'
import {
  fetchCoverageInventory,
  isProxyError,
  type CoverageInventoryMetric,
  type CoverageInventoryResponse,
} from '@/api/marketDataPlugin'
import { DashCard, Meter } from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

const REFETCH_MS = 60_000

function shortDate(iso: string | null | undefined): string {
  if (!iso) return '—'
  return iso.trim().slice(0, 10)
}

function formatRange(min: string | null | undefined, max: string | null | undefined): string {
  const a = shortDate(min)
  const b = shortDate(max)
  if (a !== '—' && b !== '—') return `${a} — ${b}`
  if (a !== '—') return a
  if (b !== '—') return b
  return '—'
}

function analyticsActive(analytics: CoverageInventoryResponse['analytics'] | undefined): {
  active: number
  symbols: number
  latest: string
} {
  if (analytics == null) return { active: 0, symbols: 0, latest: '—' }
  const metrics: Array<CoverageInventoryMetric | null | undefined> = [
    analytics.max_pain,
    analytics.atm_iv,
    analytics.pcr,
    analytics.iv_percentile,
  ]
  const live = metrics.filter(m => m != null && (m.symbols ?? 0) > 0)
  const symbols = live.length > 0 ? Math.max(...live.map(m => m?.symbols ?? 0)) : 0
  const latests = live
    .map(m => m?.latest?.trim().slice(0, 10))
    .filter((d): d is string => Boolean(d))
    .sort()
  return { active: live.length, symbols, latest: latests[latests.length - 1] ?? '—' }
}

function scopeLabel(data: CoverageInventoryResponse | undefined): string {
  const n = data?.watchlist_symbols?.length ?? 0
  const scope = (data?.scope ?? 'watchlist').trim() || 'watchlist'
  if (scope === 'watchlist') return `Watchlist ${fmtCount(n)}`
  if (scope === 'option_contract_underlyings') return `Underlyings ${fmtCount(n)}`
  if (scope === 'empty') return 'No symbols'
  return `${scope} ${fmtCount(n)}`
}

export function DataInventoryStrip() {
  const inventoryQ = useQuery({
    queryKey: ['market-data', 'coverage', 'inventory'],
    queryFn: fetchCoverageInventory,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const errored =
    inventoryQ.isError ||
    (inventoryQ.data != null && isProxyError(inventoryQ.data)) ||
    (inventoryQ.data != null && !isProxyError(inventoryQ.data) && inventoryQ.data.ok === false)

  const data =
    inventoryQ.data != null && !isProxyError(inventoryQ.data) && inventoryQ.data.ok !== false
      ? inventoryQ.data
      : undefined

  const errorMsg =
    inventoryQ.data != null && isProxyError(inventoryQ.data)
      ? inventoryQ.data.error
      : inventoryQ.data != null && !isProxyError(inventoryQ.data) && inventoryQ.data.ok === false
        ? inventoryQ.data.error?.trim() || 'Inventory request failed'
        : inventoryQ.error instanceof Error
          ? inventoryQ.error.message
          : null

  const stockSymbols = data?.stock_daily?.symbols ?? null
  const stockRows = data?.stock_daily?.total_rows ?? null
  const underlyings = data?.option?.underlyings ?? null
  const contracts = data?.option?.total_contracts ?? null
  const snap = data?.option?.snapshot_symbols ?? null
  const oi = data?.option?.oi_symbols ?? null
  const optionTarget = Math.max(data?.watchlist_symbols?.length ?? 0, underlyings ?? 0, 1)
  const analytics = analyticsActive(data?.analytics)
  const loading = inventoryQ.isLoading && data == null

  return (
    <OpsSection
      title="Data inventory"
      headerExtra={
        data != null ? (
          <DenseTag variant="neutral" title="Ingest policy scope">
            {scopeLabel(data)}
          </DenseTag>
        ) : null
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible={false}
    >
      {errored ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {errorMsg ?? 'Failed to load inventory'}
        </p>
      ) : loading ? (
        <div className="grid grid-cols-4 gap-1.5">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-1.5 xl:grid-cols-4" role="region" aria-label="Data inventory">
          <DashCard
            title="Stock Day"
            value={fmtCount(stockSymbols)}
            rawValue={stockSymbols}
            unit="symbols"
            caption={`${fmtCount(stockRows)} rows · ${formatRange(data?.stock_daily?.min_date, data?.stock_daily?.max_date)}`}
          >
            <Meter
              fillPct={stockSymbols != null && stockSymbols > 0 ? 100 : 0}
              toneClass={toneByLevel(stockSymbols != null && stockSymbols > 0 ? 'ok' : 'missing')}
              label="stock daily symbols"
            />
          </DashCard>
          <DashCard
            title="Option"
            value={fmtCount(underlyings)}
            rawValue={underlyings}
            unit="underlyings"
            caption={`${fmtCount(contracts)} contracts · ${fmtCount(data?.option?.total_expiries)} expiries`}
          >
            <Meter
              fillPct={underlyings != null ? Math.min(100, (underlyings / optionTarget) * 100) : 0}
              toneClass={toneByLevel(underlyings != null && underlyings > 0 ? 'ok' : 'missing')}
              label="option underlyings"
            />
          </DashCard>
          <DashCard
            title="Snapshots"
            value={fmtCount(snap)}
            rawValue={snap}
            unit="symbols"
            caption={`OI ${fmtCount(oi)} · ${shortDate(data?.option?.snapshot_latest)}`}
          >
            <Meter
              fillPct={snap != null ? Math.min(100, (snap / optionTarget) * 100) : 0}
              toneClass={toneByLevel(snap != null && snap > 0 ? 'ok' : 'missing')}
              label="snapshot symbols"
            />
          </DashCard>
          <DashCard
            title="Analytics"
            value={`${analytics.active}/4`}
            rawValue={analytics.active}
            unit="metrics"
            caption={`${fmtCount(analytics.symbols)} symbols · ${analytics.latest}`}
          >
            <Meter
              fillPct={(analytics.active / 4) * 100}
              toneClass={toneByLevel(analytics.active === 4 ? 'ok' : analytics.active > 0 ? 'scheduled' : 'missing')}
              label="analytics metrics"
            />
          </DashCard>
        </div>
      )}
    </OpsSection>
  )
}
