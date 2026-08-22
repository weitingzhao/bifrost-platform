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
  SegmentControl,
} from '@bifrost/ui'
import {
  fetchFlexCoverageDbSummary,
  fetchFlexCoverageFreshness,
  fetchFlexCoverageRawPeek,
  isProxyError,
  type FlexRawPeekResponse,
  type FlexRawPeekTable,
} from '@/api/flexQueryPlugin'
import { CoverageBarRow } from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import {
  shortBrokerageTable,
  shortDay,
} from '@/components/flex-query/flexQueryStatusUtils'
import { OpsSection } from '@/components/layout/OpsSection'

type CoverageSubTab = 'quality' | 'db-summary' | 'raw-peek'

function latestKind(iso: string | null | undefined): 'ok' | 'scheduled' | 'missing' {
  if (iso == null || iso === '') return 'missing'
  const day = iso.slice(0, 10)
  const today = new Date().toISOString().slice(0, 10)
  if (day === today) return 'ok'
  const t = Date.parse(`${day}T00:00:00Z`)
  if (!Number.isFinite(t)) return 'missing'
  const age = (Date.now() - t) / 86_400_000
  if (age <= 3) return 'scheduled'
  return 'missing'
}

function formatPeekCell(value: unknown): string {
  if (value == null || value === '') return '—'
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '—'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function RawPeekSection() {
  const [table, setTable] = useState<FlexRawPeekTable>('executions_raw_flex')
  const [limit, setLimit] = useState('20')
  const limitN = Number(limit) || 20
  const peekQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'raw-peek', table, limitN],
    queryFn: () => fetchFlexCoverageRawPeek(table, limitN),
    refetchInterval: 60_000,
    retry: 1,
  })
  const raw = peekQ.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  let peek: FlexRawPeekResponse | null = null
  if (raw != null && !isProxyError(raw)) peek = raw
  const columns = peek?.columns ?? []
  const rows = peek?.rows ?? []

  return (
    <OpsSection
      title="Raw peek"
      description="Recent Golden Source rows — ingest QA"
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={table}
          onChange={v => setTable(v as FlexRawPeekTable)}
          ariaLabel="Raw peek table"
          options={[
            { value: 'executions_raw_flex', label: 'executions' },
            { value: 'transactions', label: 'transactions' },
          ]}
        />
        <SegmentControl
          size="sm"
          value={limit}
          onChange={setLimit}
          ariaLabel="Raw peek row limit"
          options={[
            { value: '20', label: '20' },
            { value: '50', label: '50' },
            { value: '100', label: '100' },
          ]}
        />
      </div>
      {peekQ.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading rows…
        </p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : rows.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No rows in {table}
        </p>
      ) : (
        <OpsSection
          variant="flat"
          title="Peek table"
          collapsible
          defaultCollapsed={false}
          bodyPadding="none"
          overflow="visible"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                {columns.map(c => (
                  <DenseTableHead key={c}>{c}</DenseTableHead>
                ))}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map((row, i) => (
                <DenseTableRow key={i}>
                  {columns.map((c, j) => (
                    <DenseTableCell
                      key={c}
                      className="font-mono text-[var(--text-dense-caption)] whitespace-nowrap"
                    >
                      {formatPeekCell(row[j])}
                    </DenseTableCell>
                  ))}
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      )}
    </OpsSection>
  )
}

export function FlexCoverageTab() {
  const [sub, setSub] = useState<CoverageSubTab>('quality')
  const freshQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'freshness'],
    queryFn: fetchFlexCoverageFreshness,
    refetchInterval: 60_000,
    retry: 1,
  })
  const summaryQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'db-summary'],
    queryFn: fetchFlexCoverageDbSummary,
    refetchInterval: 60_000,
    retry: 1,
  })

  const freshRaw = freshQ.data
  const freshErr = freshRaw != null && isProxyError(freshRaw) ? freshRaw.error : null
  const dims = useMemo(
    () => (freshRaw != null && !isProxyError(freshRaw) ? (freshRaw.dimensions ?? []) : []),
    [freshRaw],
  )
  const summaryRaw = summaryQ.data
  const summaryErr = summaryRaw != null && isProxyError(summaryRaw) ? summaryRaw.error : null
  const tables =
    summaryRaw != null && !isProxyError(summaryRaw) ? (summaryRaw.tables ?? []) : []
  const maxRows = Math.max(1, ...dims.map(d => d.row_count ?? 0))
  const maxTableRows = Math.max(1, ...tables.map(t => t.row_count ?? 0))

  return (
    <div className="flex flex-col gap-2">
      <SegmentControl
        size="sm"
        value={sub}
        onChange={v => setSub(v as CoverageSubTab)}
        ariaLabel="Flex coverage sub-tabs"
        options={[
          { value: 'quality', label: 'Quality' },
          { value: 'db-summary', label: 'DB Summary' },
          { value: 'raw-peek', label: 'Raw Peek' },
        ]}
      />

      {sub === 'quality' ? (
        <OpsSection
          title="Freshness"
          description="flex_ops.ingest_freshness per kind"
          bodyPadding="compact"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
        >
          {freshQ.isLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading freshness…
            </p>
          ) : freshErr != null ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{freshErr}</p>
          ) : dims.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No ingest_freshness rows yet
            </p>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2">
                {dims.map(d => {
                  const rows = d.row_count ?? 0
                  const kind = latestKind(d.latest_ts)
                  return (
                    <CoverageBarRow
                      key={d.dimension}
                      name={d.dimension}
                      fillPct={(rows / maxRows) * 100}
                      toneClass={toneByLevel(kind)}
                      meterLabel={`${d.dimension} ${fmtCount(rows)}`}
                      value={rows}
                      valueText={fmtCount(rows)}
                      suffix={
                        <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                          {shortDay(d.latest_ts)}
                        </span>
                      }
                    />
                  )
                })}
              </div>
              <OpsSection
                variant="flat"
                title="Dimension table"
                collapsible
                defaultCollapsed
                bodyPadding="none"
                overflow="visible"
              >
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Dimension</DenseTableHead>
                      <DenseTableHead>Latest</DenseTableHead>
                      <DenseTableHead>Rows</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {dims.map(d => (
                      <DenseTableRow key={d.dimension}>
                        <DenseTableCell className="font-mono text-xs">{d.dimension}</DenseTableCell>
                        <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                          {d.latest_ts ?? '—'}
                        </DenseTableCell>
                        <DenseTableCell className="font-mono text-xs">
                          {d.row_count ?? '—'}
                        </DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </OpsSection>
            </>
          )}
        </OpsSection>
      ) : null}

      {sub === 'db-summary' ? (
        <OpsSection
          title="Brokerage tables"
          description="Golden Source row counts"
          bodyPadding="compact"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
        >
          {summaryQ.isLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading summary…
            </p>
          ) : summaryErr != null ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              {summaryErr}
            </p>
          ) : tables.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No tables
            </p>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2">
                {tables.map(t => {
                  const rows = t.row_count ?? 0
                  const kind = latestKind(t.latest_ts)
                  return (
                    <CoverageBarRow
                      key={t.name}
                      name={shortBrokerageTable(t.relation ?? t.name)}
                      nameTitle={t.relation ?? t.name}
                      fillPct={(rows / maxTableRows) * 100}
                      toneClass={toneByLevel(kind)}
                      value={rows}
                      valueText={fmtCount(rows)}
                      suffix={
                        <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                          {shortDay(t.latest_ts)}
                        </span>
                      }
                    />
                  )
                })}
              </div>
              <OpsSection
                variant="flat"
                title="Table listing"
                collapsible
                defaultCollapsed
                bodyPadding="none"
                overflow="visible"
              >
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Table</DenseTableHead>
                      <DenseTableHead>Rows</DenseTableHead>
                      <DenseTableHead>Latest</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {tables.map(t => (
                      <DenseTableRow key={t.name}>
                        <DenseTableCell className="font-mono text-xs">
                          {t.relation ?? t.name}
                        </DenseTableCell>
                        <DenseTableCell className="font-mono text-xs">
                          {t.row_count ?? '—'}
                        </DenseTableCell>
                        <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                          {t.latest_ts ?? '—'}
                        </DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </OpsSection>
            </>
          )}
        </OpsSection>
      ) : null}

      {sub === 'raw-peek' ? <RawPeekSection /> : null}
    </div>
  )
}
