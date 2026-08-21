import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  denseTableNumCell,
} from '@bifrost/ui'
import {
  fetchCoverageWatchlist,
  fetchDailyChecklist,
  isProxyError,
  type DailyChecklistResponse,
  type DailyChecklistSymbolItem,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

type ChecklistRow = {
  symbol: string
  tradeDate: string
  stockDaily: number
  optionOi: number
  corpActions: number
}

function toRows(data: DailyChecklistResponse | null): ChecklistRow[] {
  if (data?.symbols == null) return []
  return Object.entries(data.symbols).map(([sym, item]: [string, DailyChecklistSymbolItem]) => ({
    symbol: item.symbol ?? sym,
    tradeDate: item.trade_date ?? data.trade_date ?? '—',
    stockDaily: item.stock_daily_rows ?? 0,
    optionOi: item.option_oi_rows ?? 0,
    corpActions: item.corporate_action_rows ?? 0,
  }))
}

export function DailyChecklistSection() {
  const watchlistQ = useQuery({
    queryKey: ['market-data', 'coverage', 'watchlist'],
    queryFn: fetchCoverageWatchlist,
    refetchInterval: 60_000,
    retry: 1,
  })

  const symbols = useMemo(() => {
    const wl = watchlistQ.data
    if (wl == null || isProxyError(wl)) return []
    return (wl.symbols ?? [])
      .map(s => s.symbol?.trim())
      .filter((s): s is string => Boolean(s))
      .slice(0, 40)
  }, [watchlistQ.data])

  const checklistQ = useQuery({
    queryKey: ['market-data', 'readiness', 'daily-checklist', symbols.join(',')],
    queryFn: () => fetchDailyChecklist({ symbols }),
    enabled: symbols.length > 0,
    refetchInterval: 60_000,
    retry: 1,
  })

  const proxyErr =
    checklistQ.data != null && isProxyError(checklistQ.data) ? checklistQ.data.error : null
  const data =
    checklistQ.data != null && !isProxyError(checklistQ.data) ? checklistQ.data : null
  const rows = toRows(data)
  const loading = watchlistQ.isLoading || (symbols.length > 0 && checklistQ.isLoading)
  const err =
    proxyErr ??
    (watchlistQ.data != null && isProxyError(watchlistQ.data)
      ? watchlistQ.data.error
      : null)
  const ok = !loading && err == null && rows.length > 0

  return (
    <OpsSection
      title="Daily symbol checklist"
      description="Plugin GET /market/daily-checklist — watchlist sample (producer evidence; not Trade publish)"
      headerExtra={
        loading || err != null ? null : (
          <DenseTag variant={ok ? 'success' : 'neutral'}>
            {rows.length} symbols
          </DenseTag>
        )
      }
      bodyPadding="none"
      overflow="visible"
      collapsible
      defaultCollapsed
    >
      {loading ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading daily checklist…
        </p>
      ) : err != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {err}
        </p>
      ) : symbols.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No watchlist symbols
        </p>
      ) : rows.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No checklist rows
          {data?.note ? ` — ${data.note}` : ''}
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Symbol</DenseTableHead>
              <DenseTableHead>Trade date</DenseTableHead>
              <DenseTableHead className="text-right">Stock daily</DenseTableHead>
              <DenseTableHead className="text-right">Option OI</DenseTableHead>
              <DenseTableHead className="text-right">Corp actions</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.map(r => (
              <DenseTableRow key={r.symbol}>
                <DenseTableCell className="font-mono text-xs font-semibold">
                  {r.symbol}
                </DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{r.tradeDate}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>{r.stockDaily}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>{r.optionOi}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>{r.corpActions}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
