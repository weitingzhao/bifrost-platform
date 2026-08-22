import type { CoverageInventoryResponse } from '@/api/marketDataPlugin'
import type { MarketDataFreshnessInfo } from '@/api/satelliteBusTypes'

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
  fillPct: number
}

export type AnalyticsDemandView = {
  ready: number
  thin: number
  blocked: number
  unknown: number
  rows: AnalyticsDemandRow[]
  optionUniverse: number
  optionFeed: FeedMeter[]
  equityFeed: FeedMeter[]
}

function findFresh(
  rows: MarketDataFreshnessInfo[] | undefined,
  dimension: string,
): MarketDataFreshnessInfo | undefined {
  return rows?.find(f => (f.dimension ?? '').toLowerCase() === dimension.toLowerCase())
}

export const CS_FUND_TARGET = 5000

export function meterPct(count: number | null, target: number | null): number {
  if (count == null || target == null || target <= 0) return 0
  return Math.min(100, (count / target) * 100)
}

export function coverPct(output: number | null, input: number | null): number | null {
  if (output == null || input == null || input <= 0) return null
  return Math.min(100, Math.round((output / input) * 1000) / 10)
}

function inputOf(
  key: string,
  label: string,
  count: number | null,
  fresh: MarketDataFreshnessInfo | undefined,
  target: number | null = count,
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
  const missing = req.filter(i => i.count == null || i.count <= 0)
  if (missing.length === req.length) return 'blocked'
  if (missing.length > 0) return 'thin'
  const stale = req.some(i => {
    const v = (i.freshnessVerdict ?? '').toLowerCase()
    return v === 'stale' || v === 'fail'
  })
  if (stale || extras?.thinIf) return 'thin'
  const unknownFresh = req.some(i => i.freshnessVerdict == null && i.lastRunAt == null)
  if (unknownFresh) return 'thin'
  return 'ready'
}

function detailFor(level: DemandLevel, inputs: DemandInputStatus[], outputSymbols: number | null): string {
  const bits = inputs.map(i => {
    const n = i.count == null ? '—' : String(i.count)
    const fresh = i.freshnessVerdict ?? (i.lastRunAt != null ? i.lastRunAt : 'no freshness')
    return `${i.label} ${n} (${fresh})`
  })
  const out =
    outputSymbols != null
      ? ` · Research wrote ${outputSymbols} symbols`
      : ' · Research output not required for this verdict'
  if (level === 'blocked') return `Missing Massive inputs — ${bits.join(' · ')}${out}`
  if (level === 'thin') return `Inputs present but thin or stale — ${bits.join(' · ')}${out}`
  if (level === 'ready') return `Inputs can feed Research — ${bits.join(' · ')}${out}`
  return bits.join(' · ') + out
}

export function buildAnalyticsDemand(args: {
  freshness?: MarketDataFreshnessInfo[]
  inventory?: CoverageInventoryResponse | null
  incomeStatementSymbols?: number | null
}): AnalyticsDemandView {
  const fresh = args.freshness ?? []
  const inv = args.inventory
  const opt = inv?.option
  const stock = inv?.stock_daily
  const analytics = inv?.analytics

  const snapshotCount = opt?.snapshot_symbols ?? null
  const oiCount = opt?.oi_symbols ?? null
  const stockCount = stock?.symbols ?? null
  const income = args.incomeStatementSymbols ?? null
  const watchlist = inv?.watchlist_symbols?.length ?? 0
  const optionTarget = Math.max(watchlist, snapshotCount ?? 0, oiCount ?? 0, 1)
  const fundTarget = CS_FUND_TARGET

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
        inputOf('stock', 'Stock daily', stockCount, findFresh(fresh, 'stock_daily'), stockCount),
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
      inputs: [inputOf('stock', 'Stock daily', stockCount, findFresh(fresh, 'stock_daily'), stockCount)],
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

  const rows: AnalyticsDemandRow[] = defs.map(d => {
    const level = scoreInputs(d.inputs, { thinIf: d.thinIf })
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
      detail: detailFor(level, d.inputs, outputSymbols),
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
        label: 'Stock daily',
        count: stockCount,
        target: stockCount,
        fillPct: stockCount != null && stockCount > 0 ? 100 : 0,
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
