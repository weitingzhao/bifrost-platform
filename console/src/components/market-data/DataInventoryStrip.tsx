import { useQuery } from '@tanstack/react-query'
import { DenseTag, Skeleton } from '@bifrost/ui'
import {
  fetchCoverageInventory,
  isProxyError,
  type CoverageInventoryMetric,
  type CoverageInventoryOption,
  type CoverageInventoryResponse,
  type CoverageInventoryStockDaily,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

const REFETCH_MS = 60_000

function formatCount(n: number): string {
  return n.toLocaleString('en-US')
}

function shortDate(iso: string | null | undefined): string {
  if (!iso) return '—'
  // Prefer YYYY-MM-DD; tolerate longer ISO timestamps.
  return iso.trim().slice(0, 10)
}

function formatRange(min: string | null | undefined, max: string | null | undefined): string {
  const a = shortDate(min)
  const b = shortDate(max)
  if (a !== '—' && b !== '—') return `${a} — ${b}`
  if (a !== '—') return `${a} — —`
  if (b !== '—') return `— — ${b}`
  return '—'
}

function InventoryCell({
  label,
  loading,
  value,
  detail,
  muted,
}: {
  label: string
  loading: boolean
  value: string
  detail?: string
  muted?: boolean
}) {
  return (
    <div className="min-w-0 flex-1">
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{label}</p>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-24" />
      ) : (
        <>
          <p
            className={
              muted
                ? 'm-0 mt-0.5 font-semibold tabular-nums text-[var(--text-dense-body)] text-[var(--muted-foreground)]'
                : 'm-0 mt-0.5 font-semibold tabular-nums text-[var(--text-dense-body)] text-[var(--foreground)]'
            }
          >
            {value}
          </p>
          {detail ? (
            <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {detail}
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

function stockDailyDisplay(data: CoverageInventoryStockDaily | null | undefined): {
  value: string
  detail: string
} {
  if (data == null) return { value: '—', detail: 'No stock daily data' }
  const symbols = data.symbols ?? 0
  const rows = data.total_rows ?? 0
  return {
    value: `${formatCount(symbols)} symbols · ${formatCount(rows)} rows`,
    detail: formatRange(data.min_date, data.max_date),
  }
}

function optionDisplay(data: CoverageInventoryOption | null | undefined): {
  value: string
  detail: string
} {
  if (data == null) return { value: '—', detail: 'No option contracts' }
  const underlyings = data.underlyings ?? 0
  const contracts = data.total_contracts ?? 0
  const expiries = data.total_expiries ?? 0
  return {
    value: `${formatCount(underlyings)} underlyings · ${formatCount(contracts)} contracts`,
    detail: `${formatCount(expiries)} expiries`,
  }
}

function snapshotDisplay(data: CoverageInventoryOption | null | undefined): {
  value: string
  detail: string
} {
  if (data == null) return { value: '—', detail: 'No snapshots' }
  const symbols = data.snapshot_symbols ?? 0
  const oi = data.oi_symbols ?? 0
  return {
    value: `${formatCount(symbols)} snapshot · ${formatCount(oi)} OI symbols`,
    detail: `Snap ${shortDate(data.snapshot_latest)} · OI ${shortDate(data.oi_latest)}`,
  }
}

function analyticsDisplay(
  analytics: CoverageInventoryResponse['analytics'] | undefined,
): { value: string; detail: string } {
  if (analytics == null) return { value: '—', detail: 'No analytics' }
  const metrics: Array<[string, CoverageInventoryMetric | null | undefined]> = [
    ['max_pain', analytics.max_pain],
    ['atm_iv', analytics.atm_iv],
    ['pcr', analytics.pcr],
    ['iv_percentile', analytics.iv_percentile],
  ]
  const active = metrics.filter(([, m]) => m != null && (m.symbols ?? 0) > 0)
  if (active.length === 0) return { value: '4 metrics · 0 symbols', detail: 'No analytics rows yet' }

  const symbolCounts = active.map(([, m]) => m?.symbols ?? 0)
  const maxSymbols = Math.max(...symbolCounts)
  const latests = active
    .map(([, m]) => m?.latest?.trim().slice(0, 10))
    .filter((d): d is string => Boolean(d))
    .sort()
  const latest = latests[latests.length - 1] ?? '—'

  return {
    value: `${active.length}/4 metrics · ${formatCount(maxSymbols)} symbols`,
    detail: `Latest ${latest}`,
  }
}

function scopeLabel(data: CoverageInventoryResponse | undefined): string {
  const n = data?.watchlist_symbols?.length ?? 0
  const scope = (data?.scope ?? 'watchlist').trim() || 'watchlist'
  if (scope === 'watchlist') return `Watchlist (${formatCount(n)} symbols)`
  if (scope === 'option_contract_underlyings') {
    return `Option underlyings (${formatCount(n)} symbols)`
  }
  if (scope === 'empty') return 'No symbols tracked'
  return `${scope} (${formatCount(n)} symbols)`
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

  const stock = stockDailyDisplay(data?.stock_daily)
  const option = optionDisplay(data?.option)
  const snapshot = snapshotDisplay(data?.option)
  const analytics = analyticsDisplay(data?.analytics)

  return (
    <OpsSection
      title="DATA INVENTORY"
      description="Breadth × depth of Plugin market.* + market_analytics.* — watchlist-bound option scope"
      headerExtra={
        data != null ? (
          <DenseTag variant="neutral" title="Ingest policy scope">
            Scope: {scopeLabel(data)}
          </DenseTag>
        ) : null
      }
      bodyPadding="default"
      overflow="visible"
      collapsible={false}
    >
      {errored ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {errorMsg ?? 'Failed to load inventory'}
        </p>
      ) : (
        <div
          className="flex flex-wrap items-start gap-6"
          role="region"
          aria-label="Data inventory"
        >
          <InventoryCell
            label="Stock Day"
            loading={inventoryQ.isLoading}
            value={stock.value}
            detail={stock.detail}
          />
          <InventoryCell
            label="Option"
            loading={inventoryQ.isLoading}
            value={option.value}
            detail={option.detail}
          />
          <InventoryCell
            label="Snapshots"
            loading={inventoryQ.isLoading}
            value={snapshot.value}
            detail={snapshot.detail}
          />
          <InventoryCell
            label="Analytics"
            loading={inventoryQ.isLoading}
            value={analytics.value}
            detail={analytics.detail}
          />
          <InventoryCell
            label="Stock Min"
            loading={inventoryQ.isLoading}
            value="—"
            detail="Not tracked"
            muted
          />
        </div>
      )}
    </OpsSection>
  )
}
