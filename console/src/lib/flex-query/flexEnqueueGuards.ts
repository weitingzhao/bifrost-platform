/**
 * Guards before Flex enqueue — avoid zombies and IB [1018] rate-limit storms.
 */

import type { FlexIngestJob, FlexQueueSummary } from '@/api/flexQueryPlugin'

export const FLEX_RATE_LIMIT_COOLDOWN_MS = 30 * 60 * 1000

export type FlexEnqueueBlockReason =
  | { code: 'running'; message: string }
  | { code: 'rate_limit'; message: string }
  | null

function jobErrorText(job: FlexIngestJob): string {
  const res = job.result
  if (res == null) return ''
  if (typeof res === 'string') return res
  if (typeof res === 'object' && res !== null && 'error' in res) {
    const err = (res as { error?: unknown }).error
    return err == null ? '' : String(err)
  }
  try {
    return JSON.stringify(res)
  } catch {
    return String(res)
  }
}

/** True when queue reports an in-flight job (may be a zombie if worker crashed). */
export function flexQueueHasRunning(counts: FlexQueueSummary | null | undefined): boolean {
  return (counts?.running ?? 0) > 0
}

/**
 * Recent failed jobs mention IB [1018] Too many requests — cool down before re-enqueue.
 */
export function flexRecentRateLimitHit(
  jobs: FlexIngestJob[] | null | undefined,
  opts?: { nowMs?: number; cooldownMs?: number },
): { hit: boolean; newestAt: string | null } {
  const nowMs = opts?.nowMs ?? Date.now()
  const cooldownMs = opts?.cooldownMs ?? FLEX_RATE_LIMIT_COOLDOWN_MS
  let newestAt: string | null = null
  let newestMs = 0
  for (const job of jobs ?? []) {
    if ((job.status ?? '').toLowerCase() !== 'failed') continue
    const text = jobErrorText(job)
    if (!text.includes('[1018]') && !/too many requests/i.test(text)) continue
    const ts = job.finished_at ?? job.started_at ?? job.created_at
    if (ts == null || ts === '') continue
    const t = Date.parse(ts)
    if (!Number.isFinite(t)) continue
    if (t > newestMs) {
      newestMs = t
      newestAt = ts
    }
  }
  if (newestMs <= 0) return { hit: false, newestAt: null }
  return { hit: nowMs - newestMs < cooldownMs, newestAt }
}

export function flexEnqueueBlockReason(input: {
  counts?: FlexQueueSummary | null
  recentJobs?: FlexIngestJob[] | null
  nowMs?: number
  cooldownMs?: number
}): FlexEnqueueBlockReason {
  if (flexQueueHasRunning(input.counts)) {
    return {
      code: 'running',
      message:
        'Queue has a running job — wait for it to finish, or let the worker reclaim stale running (>2h) before enqueueing again.',
    }
  }
  const rl = flexRecentRateLimitHit(input.recentJobs, {
    nowMs: input.nowMs,
    cooldownMs: input.cooldownMs,
  })
  if (rl.hit) {
    return {
      code: 'rate_limit',
      message: `IB Flex rate limit [1018] hit recently${rl.newestAt ? ` (${rl.newestAt})` : ''} — wait ~30m before enqueueing to avoid another storm.`,
    }
  }
  return null
}
