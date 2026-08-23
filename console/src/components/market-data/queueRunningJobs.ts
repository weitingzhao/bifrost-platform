import type { IngestJob } from '@/api/marketDataPlugin'

/** Running longer than this is unusual for a single ingest job. */
export const RUNNING_LONG_SEC = 8 * 60
/** No heartbeat while running — past this, treat as likely stuck / dead worker. */
export const RUNNING_STALE_SEC = 20 * 60

export type RunningFreshness = 'live' | 'long' | 'stale' | 'unknown'

export function runningAgeSec(
  startedAt: string | undefined,
  nowMs: number,
): number | null {
  if (startedAt == null || startedAt === '') return null
  const t = Date.parse(startedAt)
  if (!Number.isFinite(t)) return null
  return Math.max(0, (nowMs - t) / 1000)
}

export function runningFreshness(ageSec: number | null): RunningFreshness {
  if (ageSec == null || !Number.isFinite(ageSec)) return 'unknown'
  if (ageSec >= RUNNING_STALE_SEC) return 'stale'
  if (ageSec >= RUNNING_LONG_SEC) return 'long'
  return 'live'
}

export function formatDurationSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.floor(sec)}s`
  if (sec < 3600) {
    const m = Math.floor(sec / 60)
    const s = Math.floor(sec % 60)
    return s > 0 ? `${m}m ${s}s` : `${m}m`
  }
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

export function sortRunningJobs(jobs: IngestJob[], nowMs: number): IngestJob[] {
  return [...jobs].sort((a, b) => {
    const aa = runningAgeSec(a.started_at, nowMs)
    const ba = runningAgeSec(b.started_at, nowMs)
    if (aa == null && ba == null) return 0
    if (aa == null) return 1
    if (ba == null) return -1
    return ba - aa
  })
}

export function runningJobsSummary(jobs: IngestJob[], nowMs: number) {
  let oldestSec: number | null = null
  let stale = 0
  let long = 0
  for (const job of jobs) {
    const age = runningAgeSec(job.started_at, nowMs)
    const tone = runningFreshness(age)
    if (tone === 'stale') stale += 1
    else if (tone === 'long') long += 1
    if (age != null && (oldestSec == null || age > oldestSec)) oldestSec = age
  }
  return { oldestSec, stale, long, count: jobs.length }
}

export function kindQueueCountsLabel(k: { pending: number; running: number }): {
  value: number
  valueText: string
  suffix: string | null
} {
  const ready = k.pending
  const run = k.running
  if (ready === 0 && run === 0) return { value: 0, valueText: 'idle', suffix: null }
  if (ready === 0) return { value: run, valueText: `${run} running`, suffix: null }
  if (run === 0) return { value: ready, valueText: `${ready} ready`, suffix: null }
  return { value: ready, valueText: `${ready} ready`, suffix: `${run} running` }
}

export function freshnessTagVariant(
  tone: RunningFreshness,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (tone === 'live') return 'success'
  if (tone === 'long') return 'warning'
  if (tone === 'stale') return 'danger'
  return 'neutral'
}

export function freshnessLabel(tone: RunningFreshness): string {
  if (tone === 'live') return 'live'
  if (tone === 'long') return 'long'
  if (tone === 'stale') return 'stuck?'
  return 'no start'
}

export function runningCardCaption(summary: {
  oldestSec: number | null
  stale: number
  long: number
  count: number
}): string {
  if (summary.count === 0) return 'no in-flight jobs'
  const bits = [`oldest ${formatDurationSec(summary.oldestSec)}`]
  if (summary.stale > 0) bits.push(`${summary.stale} stuck?`)
  else if (summary.long > 0) bits.push(`${summary.long} long`)
  else bits.push('all live')
  return bits.join(' · ')
}
