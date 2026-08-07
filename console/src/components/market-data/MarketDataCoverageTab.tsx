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
  DenseTag,
  SegmentControl,
} from '@bifrost/ui'
import {
  fetchCoverageDbSummary,
  fetchCoverageWatchlist,
  isProxyError,
} from '@/api/marketDataPlugin'
import { MarketDataJsonProbeCard } from '@/components/market-data/MarketDataJsonProbeCard'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import {
  CAPABILITY_GROUP_LABELS,
  checklistEffectiveStatusLabel,
  commonFeedChecklistRows,
  groupedCommonFeedChecklistRows,
  groupedOptionFeedChecklistRows,
  groupedStockChecklistRows,
  optionFeedChecklistRows,
  shortServiceLabel,
  STOCK_CHECKLIST_ROWS,
  type ChecklistRow,
} from '@/lib/market-data/checklist'

type CoverageLens = 'stock' | 'option' | 'common'

function statusVariant(
  status: ChecklistRow['projectStatus'],
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (status === 'implemented') return 'success'
  if (status === 'partial') return 'warning'
  return 'neutral'
}

function ChecklistTable({ rows }: { rows: ChecklistRow[] }) {
  if (rows.length === 0) {
    return (
      <p className="m-0 px-3 py-4 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No checklist rows
      </p>
    )
  }
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Service</DenseTableHead>
          <DenseTableHead>Group</DenseTableHead>
          <DenseTableHead>Status</DenseTableHead>
          <DenseTableHead>Tier</DenseTableHead>
          <DenseTableHead>Verification</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(row => (
          <DenseTableRow key={row.id}>
            <DenseTableCell>
              <span className="font-semibold" title={row.service}>
                {shortServiceLabel(row)}
              </span>
              <p className="m-0 mt-0.5 max-w-md truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {row.description}
              </p>
            </DenseTableCell>
            <DenseTableCell>
              <DenseTag variant="neutral">{CAPABILITY_GROUP_LABELS[row.group]}</DenseTag>
            </DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={statusVariant(row.projectStatus)}>
                {checklistEffectiveStatusLabel(row.projectStatus)}
              </DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono text-xs">{row.tierMin}</DenseTableCell>
            <DenseTableCell className="max-w-lg text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {row.verification}
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function MarketDataCoverageTab() {
  const [lens, setLens] = useState<CoverageLens>('stock')

  const summaryQ = useQuery({
    queryKey: ['market-data', 'coverage', 'db-summary'],
    queryFn: fetchCoverageDbSummary,
    refetchInterval: 60_000,
    retry: 1,
  })
  const watchlistQ = useQuery({
    queryKey: ['market-data', 'coverage', 'watchlist'],
    queryFn: fetchCoverageWatchlist,
    refetchInterval: 60_000,
    retry: 1,
  })

  const summary = summaryQ.data
  const summaryErr = summary != null && isProxyError(summary) ? summary.error : null
  const summaryOk = summary != null && !isProxyError(summary) && summary.ok !== false

  const watchlist = watchlistQ.data
  const watchErr = watchlist != null && isProxyError(watchlist) ? watchlist.error : null
  const symbols =
    watchlist != null && !isProxyError(watchlist)
      ? (watchlist.symbols ?? [])
          .map((s) => s.symbol?.trim())
          .filter((s): s is string => Boolean(s))
      : []

  const grouped = useMemo(() => {
    if (lens === 'stock') return groupedStockChecklistRows()
    if (lens === 'option') return groupedOptionFeedChecklistRows()
    return groupedCommonFeedChecklistRows()
  }, [lens])

  const counts =
    summaryOk && summary != null && !isProxyError(summary) ? (summary.counts ?? {}) : null

  const rowCount =
    lens === 'stock'
      ? STOCK_CHECKLIST_ROWS.length
      : lens === 'option'
        ? optionFeedChecklistRows().length
        : commonFeedChecklistRows().length

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="DB coverage"
        description="Plugin GET /market/coverage/db-summary + watchlist"
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {summaryQ.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading coverage…
          </p>
        ) : summaryErr != null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {summaryErr}
          </p>
        ) : counts != null ? (
          <div className="flex flex-wrap gap-2">
            {Object.entries(counts).map(([k, v]) => (
              <DenseTag key={k} variant="neutral">
                {k}: {v ?? '—'}
              </DenseTag>
            ))}
          </div>
        ) : (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No coverage summary
          </p>
        )}
        <div className="mt-3">
          <OpsSubsectionTitle>Watchlist sample</OpsSubsectionTitle>
          {watchlistQ.isLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading…
            </p>
          ) : watchErr != null ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{watchErr}</p>
          ) : symbols.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No symbols
            </p>
          ) : (
            <p className="m-0 font-mono text-[var(--text-dense-meta)]">
              {symbols.slice(0, 24).join(' · ')}
              {symbols.length > 24 ? ` · +${symbols.length - 24}` : ''}
            </p>
          )}
        </div>
      </OpsSection>

      <OpsSection
        title="Capability checklist"
        description={`${rowCount} capabilities · adapted from Trade Massive checklist (Plugin API paths)`}
        headerExtra={
          <SegmentControl
            size="sm"
            value={lens}
            onChange={v => setLens(v as CoverageLens)}
            options={[
              { value: 'stock', label: 'Stock' },
              { value: 'option', label: 'Option' },
              { value: 'common', label: 'Common' },
            ]}
          />
        }
        bodyPadding="none"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        <div className="flex flex-col gap-3 p-3">
          {grouped.map(g => (
            <div key={g.group} className="flex flex-col gap-1">
              <OpsSubsectionTitle>{CAPABILITY_GROUP_LABELS[g.group]}</OpsSubsectionTitle>
              <ChecklistTable rows={g.rows} />
            </div>
          ))}
        </div>
      </OpsSection>

      <MarketDataJsonProbeCard
        title="JSON Probe"
        description="Read-only GET against Plugin /market/* via platform-api proxy"
        defaultPath="/market/coverage/db-summary"
      />
    </div>
  )
}
