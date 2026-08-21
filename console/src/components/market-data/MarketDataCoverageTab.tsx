import { useEffect, useMemo, useState } from 'react'
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
import { DataInventoryStrip } from '@/components/market-data/DataInventoryStrip'
import { OptionCoverageSection } from '@/components/market-data/OptionCoverageSection'
import { QualityScoreSection } from '@/components/market-data/QualityScoreSection'
import { StockDepthSection } from '@/components/market-data/StockDepthSection'
import { FinancialsPanel } from '@/components/market-data/quality/FinancialsPanel'
import { ReadinessPanel } from '@/components/market-data/quality/ReadinessPanel'
import { SepaStatsSection } from '@/components/market-data/quality/SepaStatsSection'
import { SnapshotQualityTrend } from '@/components/market-data/quality/SnapshotQualityTrend'
import {
  type CoverageDetailPanel,
  writeMdSearchParams,
} from '@/components/market-data/quality/mdNavParams'
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
  OPTION_CHECKLIST_ROWS,
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

export function MarketDataCoverageTab({
  panel,
  onPanelChange,
}: {
  panel: CoverageDetailPanel
  onPanelChange: (panel: CoverageDetailPanel) => void
}) {
  const [lens, setLens] = useState<CoverageLens>('stock')

  useEffect(() => {
    writeMdSearchParams({ tab: 'coverage', panel })
  }, [panel])

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
          .map(s => s.symbol?.trim())
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

  const allRows = useMemo(() => [...STOCK_CHECKLIST_ROWS, ...OPTION_CHECKLIST_ROWS], [])
  const capTotal = allRows.length
  const capImpl = allRows.filter(r => r.projectStatus === 'implemented').length
  const capPartial = allRows.filter(r => r.projectStatus === 'partial').length
  const capPct = capTotal > 0 ? Math.round(((capImpl + capPartial * 0.5) / capTotal) * 100) : 0

  return (
    <div className="flex flex-col gap-4">
      <DataInventoryStrip />

      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--border)] pb-2">
        <SegmentControl
          size="sm"
          ariaLabel="Coverage detail panel"
          value={panel}
          onChange={v => onPanelChange(v as CoverageDetailPanel)}
          options={[
            { value: 'quality', label: 'Quality Score' },
            { value: 'readiness', label: 'Readiness' },
            { value: 'financials', label: 'Financials' },
            { value: 'db-summary', label: 'DB Summary' },
            { value: 'capability', label: 'Capability' },
          ]}
        />
        {panel === 'capability' ? (
          <SegmentControl
            size="sm"
            ariaLabel="Capability lens"
            value={lens}
            onChange={v => setLens(v as CoverageLens)}
            options={[
              { value: 'stock', label: 'Stock' },
              { value: 'option', label: 'Option' },
              { value: 'common', label: 'Common' },
            ]}
          />
        ) : null}
      </div>

      {panel === 'quality' ? (
        <>
          <QualityScoreSection />
          <SnapshotQualityTrend />
          <SepaStatsSection />
          <StockDepthSection symbols={symbols} watchlistLoading={watchlistQ.isLoading} />
          <OptionCoverageSection />
        </>
      ) : null}

      {panel === 'readiness' ? <ReadinessPanel /> : null}

      {panel === 'financials' ? <FinancialsPanel /> : null}

      {panel === 'db-summary' ? (
        <OpsSection
          title="DB summary (raw counts)"
          description="Plugin GET /market/coverage/db-summary + watchlist row counts"
          bodyPadding="default"
          overflow="visible"
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
      ) : null}

      {panel === 'capability' ? (
        <OpsSection
          title="Capability checklist"
          description={`${capTotal} Polygon features · Plugin utilization ${capPct}%`}
          bodyPadding="none"
          overflow="visible"
        >
          <div className="flex flex-col gap-3 p-3">
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-3">
                <span className="shrink-0 text-xs font-medium text-[var(--muted-foreground)]">
                  Polygon offers {capTotal} features
                </span>
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-[var(--muted)]/30">
                  <div
                    className="flex h-full"
                    style={{ width: `${Math.round(((capImpl + capPartial) / capTotal) * 100)}%` }}
                  >
                    <div
                      className="h-full bg-emerald-500"
                      style={{
                        width: `${Math.round((capImpl / (capImpl + capPartial || 1)) * 100)}%`,
                      }}
                    />
                    <div className="h-full bg-amber-500" style={{ flex: 1 }} />
                  </div>
                </div>
              </div>
              <div className="flex flex-wrap gap-3 text-xs tabular-nums">
                <span>
                  <span className="font-semibold text-emerald-500">{capImpl}</span>
                  <span className="text-[var(--muted-foreground)]"> implemented</span>
                </span>
                <span>
                  <span className="font-semibold text-amber-500">{capPartial}</span>
                  <span className="text-[var(--muted-foreground)]"> partial</span>
                </span>
                <span>
                  <span className="font-semibold text-[var(--muted-foreground)]">
                    {capTotal - capImpl - capPartial}
                  </span>
                  <span className="text-[var(--muted-foreground)]"> not implemented</span>
                </span>
              </div>
            </div>
            <OpsSubsectionTitle className="mt-1">
              {lens === 'stock' ? 'Stock' : lens === 'option' ? 'Option' : 'Common'} — {rowCount}{' '}
              capabilities
            </OpsSubsectionTitle>
            {grouped.map(g => (
              <div key={g.group} className="flex flex-col gap-1">
                <OpsSubsectionTitle>{CAPABILITY_GROUP_LABELS[g.group]}</OpsSubsectionTitle>
                <ChecklistTable rows={g.rows} />
              </div>
            ))}
          </div>
        </OpsSection>
      ) : null}

      <MarketDataJsonProbeCard
        key={`probe-${panel}`}
        title="JSON Probe"
        description="Read-only GET against Plugin /market/* via platform-api proxy"
        defaultPath={
          panel === 'readiness'
            ? '/market/readiness/snapshot-coverage'
            : panel === 'financials'
              ? '/market/readiness/financials-by-instrument-type'
              : '/market/coverage/db-summary'
        }
      />
    </div>
  )
}
