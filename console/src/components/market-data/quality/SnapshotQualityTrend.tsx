import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  Input,
  denseTableNumCell,
} from '@bifrost/ui'
import {
  fetchSnapshotQualityDetail,
  isProxyError,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

function pct(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '—'
  return `${n.toFixed(1)}%`
}

export function SnapshotQualityTrend({
  defaultSymbol = 'SPY',
}: {
  defaultSymbol?: string
}) {
  const [symbolInput, setSymbolInput] = useState(defaultSymbol)
  const [symbol, setSymbol] = useState(defaultSymbol.toUpperCase())

  const q = useQuery({
    queryKey: ['market-data', 'coverage', 'snapshot-quality-detail', symbol],
    queryFn: () => fetchSnapshotQualityDetail({ symbol, days: 14 }),
    enabled: symbol.length > 0,
    refetchInterval: 120_000,
    retry: 1,
  })

  const proxyErr = q.data != null && isProxyError(q.data) ? q.data.error : null
  const data = q.data != null && !isProxyError(q.data) ? q.data : null
  const daily = useMemo(() => data?.daily ?? [], [data])

  return (
    <OpsSection
      title="Snapshot quality trend"
      description="Plugin GET /market/coverage/snapshot-quality-detail — IV / Greeks / OI % by day"
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="shrink-0 text-xs font-medium text-[var(--muted-foreground)]">
          Symbol:
        </span>
        <Input
          className="h-7 w-28 font-mono text-xs"
          value={symbolInput}
          onChange={e => setSymbolInput(e.target.value.toUpperCase())}
          onKeyDown={e => {
            if (e.key === 'Enter') {
              const next = symbolInput.trim().toUpperCase()
              if (next) setSymbol(next)
            }
          }}
          onBlur={() => {
            const next = symbolInput.trim().toUpperCase()
            if (next) setSymbol(next)
          }}
          aria-label="Snapshot quality symbol"
        />
        {data?.latest_date ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            latest {data.latest_date}
          </span>
        ) : null}
      </div>

      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading snapshot quality…
        </p>
      ) : proxyErr != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{proxyErr}</p>
      ) : daily.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No daily snapshot quality rows for {symbol}
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Day</DenseTableHead>
              <DenseTableHead className="text-right">Contracts</DenseTableHead>
              <DenseTableHead className="text-right">IV %</DenseTableHead>
              <DenseTableHead className="text-right">Full Greeks %</DenseTableHead>
              <DenseTableHead className="text-right">OI %</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {daily.map((d, i) => (
              <DenseTableRow key={`${d.snap_day ?? i}`}>
                <DenseTableCell className="font-mono text-xs">{d.snap_day ?? '—'}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {(d.contract_count ?? 0).toLocaleString('en-US')}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>{pct(d.iv_pct)}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {pct(d.full_greeks_pct)}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>{pct(d.oi_pct)}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
