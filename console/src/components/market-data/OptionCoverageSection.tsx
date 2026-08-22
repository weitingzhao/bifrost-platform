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
} from '@bifrost/ui'
import {
  fetchCoverageContracts,
  fetchCoverageGreeks,
  isProxyError,
  type CoverageContractRow,
  type CoverageGreeksRow,
} from '@/api/marketDataPlugin'
import {
  CoverageBarRow,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

/** Plugin Query max is 500 — request full watchlist, avoid default truncation. */
const COVERAGE_LIMIT = 500

type JoinedRow = {
  symbol: string
  contractCount: number | null
  expiries: number | null
  strikes: number | null
  minExpiry: string | null
  maxExpiry: string | null
  totalContracts: number | null
  withFullGreeks: number | null
  greeksPct: number | null
  hasGreeks: boolean
}

function formatRange(min: string | null, max: string | null): string {
  if (min && max) return `${min} — ${max}`
  if (min) return `${min} — —`
  if (max) return `— — ${max}`
  return '—'
}

function numOrNull(v: unknown): number | null {
  return typeof v === 'number' && Number.isFinite(v) ? v : null
}

function joinRows(
  contracts: CoverageContractRow[],
  greeks: CoverageGreeksRow[],
): JoinedRow[] {
  const greeksBySym = new Map<string, CoverageGreeksRow>()
  for (const g of greeks) {
    const sym = g.symbol?.trim()
    if (!sym) continue
    greeksBySym.set(sym.toUpperCase(), g)
  }

  const rows: JoinedRow[] = []
  for (const c of contracts) {
    const symbol = c.symbol?.trim()
    if (!symbol) continue
    const g = greeksBySym.get(symbol.toUpperCase())
    const contractCount = numOrNull(c.contract_count)
    const totalContracts = numOrNull(g?.total_contracts)
    const withFullGreeks = numOrNull(g?.with_full_greeks)
    const denom = totalContracts ?? contractCount
    const greeksPct =
      g != null && denom != null && denom > 0 && withFullGreeks != null
        ? (withFullGreeks / denom) * 100
        : null

    rows.push({
      symbol,
      contractCount,
      expiries: numOrNull(c.expiries),
      strikes: numOrNull(c.strikes),
      minExpiry: c.min_expiry?.trim() || null,
      maxExpiry: c.max_expiry?.trim() || null,
      totalContracts,
      withFullGreeks,
      greeksPct,
      hasGreeks: g != null,
    })
  }

  rows.sort((a, b) => {
    const ac = a.contractCount ?? -1
    const bc = b.contractCount ?? -1
    if (bc !== ac) return bc - ac
    return a.symbol.localeCompare(b.symbol)
  })
  return rows
}

function buildSummary(rows: JoinedRow[]): string {
  const n = rows.length
  const totalContracts = rows.reduce((s, r) => s + (r.contractCount ?? 0), 0)

  const mins = rows
    .map(r => r.minExpiry)
    .filter((d): d is string => Boolean(d))
    .sort()
  const maxs = rows
    .map(r => r.maxExpiry)
    .filter((d): d is string => Boolean(d))
    .sort()
  const rangeLo = mins[0]
  const rangeHi = maxs[maxs.length - 1]
  const expiryRange =
    rangeLo && rangeHi ? `${rangeLo} — ${rangeHi}` : rangeLo || rangeHi || '—'

  const withGreeks = rows.filter(r => r.hasGreeks)
  const sumFull = withGreeks.reduce((s, r) => s + (r.withFullGreeks ?? 0), 0)
  const sumTotal = withGreeks.reduce(
    (s, r) => s + (r.totalContracts ?? r.contractCount ?? 0),
    0,
  )
  const aggPct =
    sumTotal > 0 ? Math.round((sumFull / sumTotal) * 100) : null

  const parts = [
    `${n} underlyings`,
    `${totalContracts.toLocaleString('en-US')} contracts`,
    `expiry ${expiryRange}`,
  ]
  if (aggPct != null) {
    parts.push(`${aggPct}% with full Greeks`)
  } else {
    parts.push('no Greeks data')
  }
  return parts.join(' · ')
}

function GreeksStatusCell({ row }: { row: JoinedRow }) {
  if (!row.hasGreeks) {
    return <DenseTag variant="warning">No greeks</DenseTag>
  }
  if (row.greeksPct == null) {
    return <DenseTag variant="warning">No greeks</DenseTag>
  }
  const pct = Math.round(row.greeksPct)
  if (pct >= 100) {
    return <DenseTag variant="success">OK</DenseTag>
  }
  if (pct > 80) {
    return <DenseTag variant="warning">{pct}%</DenseTag>
  }
  return <DenseTag variant="danger">{pct}%</DenseTag>
}

async function loadContracts() {
  const res = await fetchCoverageContracts({ limit: COVERAGE_LIMIT })
  if (isProxyError(res)) throw new Error(res.error)
  if (res.ok === false) throw new Error(res.error?.trim() || 'Contracts request failed')
  return res.rows ?? []
}

async function loadGreeks() {
  const res = await fetchCoverageGreeks({ limit: COVERAGE_LIMIT })
  if (isProxyError(res)) throw new Error(res.error)
  if (res.ok === false) throw new Error(res.error?.trim() || 'Greeks request failed')
  return res.rows ?? []
}

export function OptionCoverageSection() {
  const contractsQ = useQuery({
    queryKey: ['market-data', 'coverage', 'contracts'],
    queryFn: loadContracts,
    refetchInterval: 90_000,
    retry: 1,
  })
  const greeksQ = useQuery({
    queryKey: ['market-data', 'coverage', 'greeks'],
    queryFn: loadGreeks,
    refetchInterval: 90_000,
    retry: 1,
  })

  const rows = useMemo(
    () => joinRows(contractsQ.data ?? [], greeksQ.data ?? []),
    [contractsQ.data, greeksQ.data],
  )
  const summary = useMemo(() => (rows.length > 0 ? buildSummary(rows) : null), [rows])
  const maxContracts = Math.max(1, ...rows.map(r => r.contractCount ?? 0))
  const greeksOk = rows.filter(r => r.greeksPct != null && r.greeksPct >= 90).length
  const greeksThin = rows.filter(
    r => r.greeksPct != null && r.greeksPct >= 70 && r.greeksPct < 90,
  ).length
  const greeksFail = rows.length - greeksOk - greeksThin

  const loading = contractsQ.isLoading || greeksQ.isLoading
  const error =
    contractsQ.isError || greeksQ.isError
      ? contractsQ.error instanceof Error
        ? contractsQ.error.message
        : greeksQ.error instanceof Error
          ? greeksQ.error.message
          : 'Failed to load option coverage'
      : null

  return (
    <OpsSection
      title="Option chain coverage"
      description="Bar = contracts vs max underlying. Color = Greeks fill."
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading option coverage…
        </p>
      ) : error != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : rows.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No option coverage rows
        </p>
      ) : (
        <>
          <div className="mb-2 flex items-center gap-2">
            <ScoreRing
              ready={greeksOk}
              thin={greeksThin}
              blocked={greeksFail}
              total={Math.max(rows.length, 1)}
              caption="greeks"
            />
            <div className="min-w-0 flex-1">
              <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {summary}
              </p>
              <div className="mt-1 flex flex-wrap gap-px">
                {rows.map(row => (
                  <span
                    key={`heat-${row.symbol}`}
                    title={`${row.symbol} · Greeks ${row.greeksPct != null ? `${Math.round(row.greeksPct)}%` : '—'}`}
                    className={`h-3.5 w-3.5 rounded-[2px] ${toneByLevel(
                      row.greeksPct == null
                        ? 'unknown'
                        : row.greeksPct >= 90
                          ? 'ok'
                          : row.greeksPct >= 70
                            ? 'scheduled'
                            : 'missing',
                    )}`}
                  />
                ))}
              </div>
            </div>
          </div>
          <div className="mb-2 grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2 xl:grid-cols-3">
            {rows.map(row => {
              const fill =
                row.contractCount != null ? (row.contractCount / maxContracts) * 100 : 0
              const kind =
                row.greeksPct == null
                  ? 'unknown'
                  : row.greeksPct >= 90
                    ? 'ok'
                    : row.greeksPct >= 70
                      ? 'scheduled'
                      : 'missing'
              return (
                <CoverageBarRow
                  key={row.symbol}
                  name={
                    <span className="font-mono text-entity-symbol">{row.symbol}</span>
                  }
                  nameTitle={`${row.symbol} · ${fmtCount(row.contractCount)} contracts`}
                  fillPct={fill}
                  toneClass={toneByLevel(kind)}
                  meterLabel={`${row.symbol} ${fmtCount(row.contractCount)} contracts · Greeks ${row.greeksPct != null ? `${Math.round(row.greeksPct)}%` : '—'}`}
                  value={row.contractCount}
                  valueText={fmtCount(row.contractCount)}
                  suffix={
                    <span className="w-8 text-right font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
                      {row.greeksPct != null ? `${Math.round(row.greeksPct)}%` : '—'}
                    </span>
                  }
                />
              )
            })}
          </div>
          <OpsSection
            variant="flat"
            title="Underlying table"
            collapsible
            defaultCollapsed
            bodyPadding="none"
            overflow="visible"
          >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Symbol</DenseTableHead>
                <DenseTableHead className="text-right">Contracts</DenseTableHead>
                <DenseTableHead className="text-right">Expiries</DenseTableHead>
                <DenseTableHead className="text-right">Strikes</DenseTableHead>
                <DenseTableHead>Expiry Range</DenseTableHead>
                <DenseTableHead className="text-right">Greeks %</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map(row => (
                <DenseTableRow key={row.symbol}>
                  <DenseTableCell>
                    <span className="font-semibold font-mono text-entity-symbol">
                      {row.symbol}
                    </span>
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.contractCount != null
                      ? row.contractCount.toLocaleString('en-US')
                      : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.expiries != null ? row.expiries.toLocaleString('en-US') : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.strikes != null ? row.strikes.toLocaleString('en-US') : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-meta)]">
                    {formatRange(row.minExpiry, row.maxExpiry)}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.greeksPct != null ? `${Math.round(row.greeksPct)}%` : '—'}
                  </DenseTableCell>
                  <DenseTableCell>
                    <GreeksStatusCell row={row} />
                  </DenseTableCell>
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
