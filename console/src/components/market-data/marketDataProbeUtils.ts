import type {
  MarketDataFreshnessInfo,
  MarketDataWorkerInfo,
} from '@/api/satelliteBusTypes'

export function sortFreshness(rows: MarketDataFreshnessInfo[]): MarketDataFreshnessInfo[] {
  const rank = (v: string) => {
    if (v === 'stale') return 0
    if (v === 'fail') return 1
    if (v === 'unknown') return 2
    return 3
  }
  return [...rows].sort((a, b) => {
    const d = rank(a.verdict) - rank(b.verdict)
    if (d !== 0) return d
    return a.dimension.localeCompare(b.dimension)
  })
}

export function workerReady(w: MarketDataWorkerInfo): boolean {
  return w.status == null || w.status === '' || w.status.toLowerCase() === 'ok'
}
