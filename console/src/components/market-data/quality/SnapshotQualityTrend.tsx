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
  type SnapshotQualityDaily,
} from '@/api/marketDataPlugin'
import { FlashValue } from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

function pct(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return '—'
  return `${n.toFixed(1)}%`
}

function dayTick(iso: string | null | undefined): string {
  if (iso == null || iso === '') return ''
  return iso.slice(5, 10)
}

function cellTone(v: number | null | undefined): string {
  if (v == null || Number.isNaN(v)) return toneByLevel('unknown')
  if (v >= 90) return toneByLevel('ok')
  if (v >= 70) return toneByLevel('scheduled')
  return toneByLevel('missing')
}

const SERIES: Array<{
  label: string
  key: keyof Pick<SnapshotQualityDaily, 'iv_pct' | 'full_greeks_pct' | 'oi_pct'>
  meaning: string
}> = [
  { label: 'IV', key: 'iv_pct', meaning: '% of that day’s contracts that have implied vol' },
  { label: 'Greeks', key: 'full_greeks_pct', meaning: '% of that day’s contracts with full Greeks' },
  { label: 'OI', key: 'oi_pct', meaning: '% of that day’s contracts that have open interest' },
]

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
  const daily = useMemo(
    () =>
      [...(data?.daily ?? [])].sort((a, b) =>
        (a.snap_day ?? '').localeCompare(b.snap_day ?? ''),
      ),
    [data],
  )
  const latest = daily[daily.length - 1]
  const firstDay = daily[0]?.snap_day
  const lastDay = latest?.snap_day

  return (
    <OpsSection
      title="Snapshot quality trend"
      description="One cell = one UTC day. Color = IV / Greeks / OI fill. Left = older."
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
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
        {lastDay != null ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {daily.length} days · {dayTick(firstDay)} → {dayTick(lastDay)}
          </span>
        ) : null}
        <span className="ml-auto flex flex-wrap items-center gap-2 text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          <span className="inline-flex items-center gap-1">
            <span className={`inline-block h-2 w-2 rounded-[2px] ${toneByLevel('ok')}`} />
            ≥90%
          </span>
          <span className="inline-flex items-center gap-1">
            <span className={`inline-block h-2 w-2 rounded-[2px] ${toneByLevel('scheduled')}`} />
            70–89%
          </span>
          <span className="inline-flex items-center gap-1">
            <span className={`inline-block h-2 w-2 rounded-[2px] ${toneByLevel('missing')}`} />
            &lt;70% / empty
          </span>
        </span>
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
        <>
          <div className="flex max-w-xl flex-col gap-1">
            {SERIES.map(series => {
              const last = latest?.[series.key]
              return (
                <div key={series.key} className="flex items-center gap-1.5">
                  <span
                    className="w-12 shrink-0 text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]"
                    title={series.meaning}
                  >
                    {series.label}
                  </span>
                  <div
                    className="grid min-w-0 flex-1 gap-px"
                    style={{ gridTemplateColumns: `repeat(${daily.length}, minmax(0, 1fr))` }}
                  >
                    {daily.map((d, i) => {
                      const v = d[series.key]
                      return (
                        <div
                          key={`${series.key}-${d.snap_day ?? i}`}
                          className={`h-5 min-w-0 rounded-[2px] ${cellTone(v)}`}
                          title={`${d.snap_day ?? '—'} · ${series.label} ${pct(v)} · ${series.meaning}`}
                        />
                      )
                    })}
                  </div>
                  <FlashValue
                    value={last}
                    className="w-12 shrink-0 text-right font-mono text-[var(--text-dense-caption)] tabular-nums"
                  >
                    {pct(last)}
                  </FlashValue>
                </div>
              )
            })}
            <div className="flex items-center gap-1.5">
              <span className="w-12 shrink-0" />
              <div
                className="grid min-w-0 flex-1 gap-px"
                style={{ gridTemplateColumns: `repeat(${daily.length}, minmax(0, 1fr))` }}
              >
                {daily.map((d, i) => (
                  <span
                    key={`tick-${d.snap_day ?? i}`}
                    className="truncate text-center font-mono text-[var(--text-dense-micro)] leading-none text-[var(--muted-foreground)]"
                    title={d.snap_day ?? undefined}
                  >
                    {i === 0 || i === daily.length - 1 || i % 2 === 0 ? dayTick(d.snap_day) : ''}
                  </span>
                ))}
              </div>
              <span className="w-12 shrink-0 text-right text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                latest
              </span>
            </div>
          </div>

          <OpsSection
            variant="flat"
            title="Daily table"
            collapsible
            defaultCollapsed
            bodyPadding="none"
            overflow="visible"
          >
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
          </OpsSection>
        </>
      )}
    </OpsSection>
  )
}
