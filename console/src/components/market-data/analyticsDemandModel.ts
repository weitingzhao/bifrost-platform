import type { CoverageDimensions } from '@/api/marketDataDimensions'
import type { CoverageInventoryResponse } from '@/api/marketDataPlugin'
import type { MarketDataFreshnessInfo } from '@/api/satelliteBusTypes'
import { isComputing } from '@/lib/market-data/backgroundAnswer'

export type DemandLevel = 'ready' | 'thin' | 'blocked' | 'unknown'
export type CoverageJump = 'readiness' | 'financials' | 'quality'

export type DemandInputStatus = {
  key: string
  label: string
  count: number | null
  target: number | null
  freshnessVerdict: string | null
  lastRunAt: string | null
  required: boolean
}

export type AnalyticsDemandRow = {
  id: string
  title: string
  owner: string
  needs: string
  jump: CoverageJump
  inputs: DemandInputStatus[]
  level: DemandLevel
  detail: string
  outputSymbols: number | null
  outputLatest: string | null
  coverPct: number | null
}

export type FeedMeter = {
  label: string
  count: number | null
  target: number | null
  /** null when no contract declares a denominator for this feed — draw no bar. */
  fillPct: number | null
}

export type AnalyticsDemandView = {
  ready: number
  thin: number
  blocked: number
  unknown: number
  /** The inventory's first pass has not finished — counts are absent, not zero. */
  pending: boolean
  rows: AnalyticsDemandRow[]
  optionUniverse: number | null
  optionFeed: FeedMeter[]
  equityFeed: FeedMeter[]
}

function findFresh(
  rows: MarketDataFreshnessInfo[] | undefined,
  dimension: string,
): MarketDataFreshnessInfo | undefined {
  return rows?.find(f => (f.dimension ?? '').toLowerCase() === dimension.toLowerCase())
}

/**
 * Share of the declared scope this feed holds, or null when there is nothing
 * honest to divide by. A bar drawn against an unknown denominator reads as
 * "0% covered", which is a claim; no bar is the truth (blueprint C-G1).
 */
export function meterPct(count: number | null, target: number | null): number | null {
  if (count == null || target == null || target <= 0) return null
  return Math.min(100, (count / target) * 100)
}

export function coverPct(output: number | null, input: number | null): number | null {
  if (output == null || input == null || input <= 0) return null
  return Math.min(100, Math.round((output / input) * 1000) / 10)
}

/** A dataset's breadth as the contract endpoint measured it, numerator and
 *  denominator drawn from one population. */
function breadthOf(
  dimensions: CoverageDimensions | null | undefined,
  dataset: string,
): { held: number; of: number | null } | null {
  const row = dimensions?.datasets?.find(d => d.dataset === dataset)
  if (row == null) return null
  return { held: row.breadth.held, of: row.breadth.of }
}

function inputOf(
  key: string,
  label: string,
  count: number | null,
  fresh: MarketDataFreshnessInfo | undefined,
  /**
   * The scope this input is measured against. Defaults to null, not to `count`:
   * a target that is the measurement is a denominator that can never disagree
   * with its numerator, and every such bar sat at 100%.
   */
  target: number | null = null,
  required = true,
): DemandInputStatus {
  return {
    key,
    label,
    count,
    target,
    freshnessVerdict: fresh?.verdict ?? null,
    lastRunAt: fresh?.last_run_at?.slice(0, 10) ?? null,
    required,
  }
}

function scoreInputs(inputs: DemandInputStatus[], extras?: { thinIf?: boolean }): DemandLevel {
  const req = inputs.filter(i => i.required)
  if (req.length === 0) return 'unknown'
  // Unmeasured and measured-as-zero are different facts and were scored as one.
  // `null` means the source has not reported — the four-axis payload takes about
  // 220 seconds on a cold cache — and reading that as "zero rows collected"
  // turns a restart into a red. Measured 2026-09-11 01:52 UTC, eight minutes
  // after a deploy: SEPA Technical read `blocked — Stock daily` while the table
  // held 13.7M rows, because its one required input comes from the dimensions
  // payload and that payload was still on its first pass.
  const unmeasured = req.filter(i => i.count == null)
  if (unmeasured.length === req.length) return 'unknown'
  const empty = req.filter(i => (i.count ?? 0) <= 0)
  if (empty.length === req.length) return 'blocked'
  if (empty.length > 0) return 'thin'
  const stale = req.some(i => {
    const v = (i.freshnessVerdict ?? '').toLowerCase()
    return v === 'stale' || v === 'fail'
  })
  if (stale || extras?.thinIf) return 'thin'
  const unknownFresh = req.some(i => i.freshnessVerdict == null && i.lastRunAt == null)
  if (unknownFresh) return 'thin'
  // A product whose inputs are partly unmeasured is not ready, but it is not
  // blocked either — the difference is whether anyone should go looking.
  if (unmeasured.length > 0) return 'thin'
  return 'ready'
}

function detailFor(
  level: DemandLevel | 'pending',
  inputs: DemandInputStatus[],
  outputSymbols: number | null,
): string {
  const bits = inputs.map(i => {
    const n = i.count == null ? '—' : String(i.count)
    const fresh = i.freshnessVerdict ?? (i.lastRunAt != null ? i.lastRunAt : 'no freshness')
    return `${i.label} ${n} (${fresh})`
  })
  const out =
    outputSymbols != null
      ? ` · Research wrote ${outputSymbols} symbols`
      : ' · Research output not required for this verdict'
  if (level === 'pending') return `Inventory is still being counted — ${bits.join(' · ')}`
  if (level === 'blocked') return `Missing Massive inputs — ${bits.join(' · ')}${out}`
  if (level === 'thin') return `Inputs present but thin or stale — ${bits.join(' · ')}${out}`
  if (level === 'ready') return `Inputs can feed Research — ${bits.join(' · ')}${out}`
  return bits.join(' · ') + out
}

export function buildAnalyticsDemand(args: {
  freshness?: MarketDataFreshnessInfo[]
  inventory?: CoverageInventoryResponse | null
  incomeStatementSymbols?: number | null
  /** `GET /market/coverage/dimensions` → the contract table's denominators. */
  denominators?: CoverageDimensions['denominators'] | null
  /** The same read, whole: per-dataset breadth already scoped to its own denominator. */
  dimensions?: CoverageDimensions | null
}): AnalyticsDemandView {
  const fresh = args.freshness ?? []
  const inv = args.inventory
  const opt = inv?.option
  const stock = inv?.stock_daily
  const analytics = inv?.analytics

  const snapshotCount = opt?.snapshot_symbols ?? null
  const oiCount = opt?.oi_symbols ?? null
  const income = args.incomeStatementSymbols ?? null
  // Denominators come from the contract table, never from the page. The option
  // feeds used to divide by `max(watchlist, snapshot, oi, 1)`, which is at least
  // as large as what it measured, so the meters read 100% however few symbols
  // were collected; the fundamentals target was a hand-typed 5,000.
  const denominators = args.denominators ?? args.dimensions?.denominators ?? null
  const optionTarget = denominators?.universe?.total ?? null
  const wholeMarket = denominators?.['whole-market'] ?? null
  const fundTarget = wholeMarket
  // stock_daily holds every symbol it has ever seen -- 20,695 against 5,317
  // active tickers, because delisted names keep their history. Divided by the
  // whole-market scope that reads 389%, and clamped it reads a full bar. The
  // contract endpoint already scopes the numerator to its own denominator, so
  // take both from there rather than pairing two different populations.
  const stockBreadth = breadthOf(args.dimensions, 'raw_market.stock_daily')
  const stockHeld = stockBreadth?.held ?? null
  const stockScope = stockBreadth?.of ?? null

  const defs: Array<{
    id: string
    title: string
    owner: string
    needs: string
    jump: CoverageJump
    inputs: DemandInputStatus[]
    output: { symbols?: number; latest?: string | null } | null
    thinIf?: boolean
  }> = [
    {
      id: 'max-pain',
      title: 'Max Pain',
      owner: 'Research volatility',
      needs: 'market.option_open_interest',
      jump: 'readiness',
      inputs: [
        inputOf('oi', 'OI', oiCount, findFresh(fresh, 'option_open_interest'), optionTarget),
      ],
      output: analytics?.max_pain ?? null,
    },
    {
      id: 'atm-iv',
      title: 'ATM IV',
      owner: 'Research volatility',
      needs: 'option snapshot + underlying',
      jump: 'readiness',
      inputs: [
        inputOf(
          'snapshot',
          'Snapshot',
          snapshotCount,
          findFresh(fresh, 'option_snapshot'),
          optionTarget,
        ),
        inputOf('stock', 'Stock daily', stockHeld, findFresh(fresh, 'stock_daily'), stockScope),
      ],
      output: analytics?.atm_iv ?? null,
    },
    {
      id: 'pcr',
      title: 'PCR',
      owner: 'Research volatility',
      needs: 'snapshot volume + OI',
      jump: 'readiness',
      inputs: [
        inputOf(
          'snapshot',
          'Snapshot',
          snapshotCount,
          findFresh(fresh, 'option_snapshot'),
          optionTarget,
        ),
        inputOf('oi', 'OI', oiCount, findFresh(fresh, 'option_open_interest'), optionTarget),
      ],
      output: analytics?.pcr ?? null,
    },
    {
      id: 'iv-percentile',
      title: 'IV Percentile',
      owner: 'Research volatility',
      needs: 'ATM IV history (from snapshots)',
      jump: 'readiness',
      inputs: [
        inputOf(
          'snapshot',
          'Snapshot',
          snapshotCount,
          findFresh(fresh, 'option_snapshot'),
          optionTarget,
        ),
      ],
      output: analytics?.iv_percentile ?? null,
    },
    {
      id: 'sepa-technical',
      title: 'SEPA Technical',
      owner: 'Research dbt',
      needs: 'market stock daily bars',
      jump: 'quality',
      inputs: [
        inputOf('stock', 'Stock daily', stockHeld, findFresh(fresh, 'stock_daily'), stockScope),
      ],
      output: null,
    },
    {
      id: 'sepa-fundamental',
      title: 'SEPA Fundamental',
      owner: 'Research dbt',
      needs: 'income statements (CS universe)',
      jump: 'financials',
      inputs: [
        inputOf(
          'financials',
          'Income statements',
          income,
          findFresh(fresh, 'financials'),
          fundTarget,
        ),
      ],
      output: null,
      thinIf: income != null && income > 0 && income < 500,
    },
  ]

  // The inventory is computed behind a cache and takes about 150 seconds. While
  // its first pass runs, every count is absent — which the scorer would read as
  // "zero rows collected" and mark all six products blocked. Absent is unknown.
  const pending = isComputing(inv) && stock == null && opt == null
  const rows: AnalyticsDemandRow[] = defs.map(d => {
    const level = pending ? 'unknown' : scoreInputs(d.inputs, { thinIf: d.thinIf })
    const outputSymbols = d.output?.symbols ?? null
    const primaryIn = d.inputs[0]?.count ?? null
    return {
      id: d.id,
      title: d.title,
      owner: d.owner,
      needs: d.needs,
      jump: d.jump,
      inputs: d.inputs,
      level,
      detail: detailFor(pending ? 'pending' : level, d.inputs, outputSymbols),
      outputSymbols,
      outputLatest: d.output?.latest ?? null,
      coverPct: coverPct(outputSymbols, primaryIn),
    }
  })

  return {
    ready: rows.filter(r => r.level === 'ready').length,
    thin: rows.filter(r => r.level === 'thin').length,
    blocked: rows.filter(r => r.level === 'blocked').length,
    unknown: rows.filter(r => r.level === 'unknown').length,
    pending,
    rows,
    optionUniverse: optionTarget,
    optionFeed: [
      {
        label: 'Snapshot',
        count: snapshotCount,
        target: optionTarget,
        fillPct: meterPct(snapshotCount, optionTarget),
      },
      {
        label: 'OI',
        count: oiCount,
        target: optionTarget,
        fillPct: meterPct(oiCount, optionTarget),
      },
    ],
    equityFeed: [
      {
        // Was "greater than zero fills the bar" — an existence flag drawn as a
        // coverage meter. Now both halves come from the same population.
        label: 'Stock daily',
        count: stockHeld,
        target: stockScope,
        fillPct: meterPct(stockHeld, stockScope),
      },
      {
        label: 'Income',
        count: income,
        target: fundTarget,
        fillPct: meterPct(income, fundTarget),
      },
    ],
  }
}
