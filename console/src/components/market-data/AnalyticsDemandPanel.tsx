import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, cn } from '@bifrost/ui'
import { fetchCoverageDimensions } from '@/api/marketDataDimensions'
import {
  fetchCoverageInventory,
  fetchReadinessFinancialsByType,
  isProxyError,
} from '@/api/marketDataPlugin'
import type { MarketDataFreshnessInfo } from '@/api/satelliteBusTypes'
import {
  buildAnalyticsDemand,
  meterPct,
  type AnalyticsDemandRow,
  type CoverageJump,
  type DemandLevel,
  type FeedMeter,
} from '@/components/market-data/analyticsDemandModel'
import {
  FlashValue,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { ageLabel, pollWhileComputing } from '@/lib/market-data/backgroundAnswer'
import { OpsSection } from '@/components/layout/OpsSection'

function levelVariant(level: DemandLevel): 'success' | 'warning' | 'danger' | 'neutral' {
  if (level === 'ready') return 'success'
  if (level === 'thin') return 'warning'
  if (level === 'blocked') return 'danger'
  return 'neutral'
}

function FeedRow({ meters }: { meters: FeedMeter[] }) {
  return (
    <div className="grid min-w-0 flex-1 grid-cols-2 gap-x-3 gap-y-1 xl:grid-cols-4">
      {meters.map(m => (
        <div key={m.label} className="flex min-w-0 items-center gap-1.5">
          <span className="w-[4.25rem] shrink-0 truncate text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]">
            {m.label}
          </span>
          {m.fillPct == null ? (
            <span
              className="min-w-0 flex-1 truncate text-[var(--text-dense-micro)] text-[var(--muted-foreground)]"
              title={`${m.label}: no denominator declared for this feed`}
            >
              no scope
            </span>
          ) : (
            <Meter
              fillPct={m.fillPct}
              toneClass={
                m.count == null || m.count <= 0
                  ? 'bg-[var(--color-danger,var(--destructive))]/70'
                  : m.fillPct >= 80
                    ? 'bg-[var(--color-success)]'
                    : m.fillPct >= 20
                      ? 'bg-[var(--color-warning)]'
                      : 'bg-[var(--color-danger,var(--destructive))]'
              }
              label={`${m.label} ${fmtCount(m.count)}${m.target != null ? ` / ${fmtCount(m.target)}` : ''}`}
            />
          )}
          <FlashValue
            value={m.count}
            className="w-[4.5rem] shrink-0 text-right font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]"
          >
            {fmtCount(m.count)}
            {m.target != null && m.target !== m.count ? `/${fmtCount(m.target)}` : ''}
          </FlashValue>
        </div>
      ))}
    </div>
  )
}

function ProductCard({
  row,
  onOpen,
}: {
  row: AnalyticsDemandRow
  onOpen?: (jump: CoverageJump) => void
}) {
  const primary = row.inputs[0]
  const showResearch = row.outputSymbols != null
  return (
    <button
      type="button"
      className={cn(
        'flex min-w-0 flex-col gap-1 rounded-md border border-[var(--border)] bg-[var(--secondary)] px-2 py-1.5 text-left',
        onOpen && 'cursor-pointer hover:border-[var(--foreground)]/20',
      )}
      onClick={() => onOpen?.(row.jump)}
      title={`${row.needs} — open Coverage → ${row.jump}`}
    >
      <div className="flex items-center justify-between gap-1">
        <div className="flex min-w-0 items-center gap-1">
          <span
            className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${toneByLevel(row.level)}`}
            aria-hidden
          />
          <span className="truncate text-[var(--text-dense-caption)] font-medium">{row.title}</span>
        </div>
        <DenseTag variant={levelVariant(row.level)}>{row.level}</DenseTag>
      </div>
      <div className="flex items-baseline justify-between gap-1">
        <FlashValue
          value={primary?.count}
          className="font-mono text-[var(--text-dense-body)] font-semibold leading-none tabular-nums"
        >
          {fmtCount(primary?.count)}
        </FlashValue>
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          {primary?.label ?? 'input'}
        </span>
      </div>
      {/* One rule for every input: a bar needs a declared scope to divide by.
          This used to be a per-key exception for 'stock', whose target was its
          own count. */}
      {row.inputs
        .map(input => ({ input, pct: meterPct(input.count, input.target) }))
        .filter(({ pct }) => pct != null)
        .map(({ input, pct }) => (
          <div key={input.key} className="flex items-center gap-1.5">
            <Meter
              fillPct={pct as number}
              toneClass={toneByLevel(row.level)}
              label={`${input.label} ${fmtCount(input.count)} / ${fmtCount(input.target)}`}
            />
          </div>
        ))}
      {showResearch ? (
        <div className="flex items-center gap-1.5">
          <Meter
            fillPct={row.coverPct ?? 0}
            toneClass="bg-[var(--color-info,theme(colors.sky.500))]"
            label={`Research wrote ${fmtCount(row.outputSymbols)}`}
          />
          <FlashValue
            value={row.outputSymbols}
            className="w-10 shrink-0 text-right font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]"
          >
            {fmtCount(row.outputSymbols)}
          </FlashValue>
        </div>
      ) : null}
    </button>
  )
}

export function AnalyticsDemandPanel({
  freshness,
  onOpenCoverage,
}: {
  freshness?: MarketDataFreshnessInfo[]
  onOpenCoverage?: (panel: CoverageJump) => void
}) {
  const inventoryQ = useQuery({
    queryKey: ['market-data', 'coverage', 'inventory'],
    queryFn: fetchCoverageInventory,
    // Computed behind a cache; poll faster while its pass is running.
    refetchInterval: pollWhileComputing(60_000),
    retry: 1,
  })
  const financialsQ = useQuery({
    queryKey: ['market-data', 'readiness', 'financials-by-type'],
    queryFn: fetchReadinessFinancialsByType,
    refetchInterval: 60_000,
    retry: 1,
  })

  // Same query key as the coverage panels: the contract table's denominators are
  // fetched once and shared, not re-derived per panel.
  const dimensionsQ = useQuery({
    queryKey: ['market-data', 'coverage', 'dimensions'],
    queryFn: fetchCoverageDimensions,
    staleTime: 60_000,
    retry: 1,
  })
  const inventory =
    inventoryQ.data != null && !isProxyError(inventoryQ.data) ? inventoryQ.data : null
  const income =
    financialsQ.data != null && !isProxyError(financialsQ.data)
      ? (financialsQ.data.counts?.income_statement_symbols ?? null)
      : null
  const view = buildAnalyticsDemand({
    freshness,
    inventory,
    incomeStatementSymbols: income,
    denominators: dimensionsQ.data?.denominators ?? null,
  })
  const total = view.rows.length
  // While the inventory's first pass runs the counts are absent, not zero, so
  // this stays a loading state rather than reporting six blocked products.
  const loading = (inventoryQ.isLoading && inventory == null) || view.pending
  const age = ageLabel(inventory)

  return (
    <OpsSection
      title="Analytics demand"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          {view.pending ? (
            <DenseTag variant="neutral">counting inventory…</DenseTag>
          ) : (
            <>
              <DenseTag variant="success">ready {view.ready}</DenseTag>
              <DenseTag variant="warning">thin {view.thin}</DenseTag>
              <DenseTag variant="danger">blocked {view.blocked}</DenseTag>
            </>
          )}
          {age != null && !view.pending ? (
            <DenseTag variant="neutral" title="Inventory is served from a cache; a full pass takes about 150 seconds">
              {age}
            </DenseTag>
          ) : null}
        </div>
      }
      actions={
        onOpenCoverage != null ? (
          <div className="flex flex-wrap gap-1">
            <Button variant="outline" size="sm" onClick={() => onOpenCoverage('readiness')}>
              Readiness
            </Button>
            <Button variant="outline" size="sm" onClick={() => onOpenCoverage('financials')}>
              Financials
            </Button>
          </div>
        ) : null
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {view.pending
            ? 'Counting the inventory — a full pass over stock_daily takes about 150 seconds.'
            : 'Loading inventory…'}
        </p>
      ) : (
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <ScoreRing
              ready={view.ready}
              thin={view.thin}
              blocked={view.blocked}
              unknown={view.unknown}
              total={total}
            />
            <FeedRow meters={[...view.optionFeed, ...view.equityFeed]} />
          </div>
          <div className="grid grid-cols-2 gap-1.5 md:grid-cols-3 xl:grid-cols-6">
            {view.rows.map(row => (
              <ProductCard key={row.id} row={row} onOpen={onOpenCoverage} />
            ))}
          </div>
        </div>
      )}
    </OpsSection>
  )
}
