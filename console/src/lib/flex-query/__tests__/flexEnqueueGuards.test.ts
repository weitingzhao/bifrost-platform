import { describe, expect, it } from 'vitest'
import {
  flexEnqueueBlockReason,
  flexQueueHasRunning,
  flexRecentRateLimitHit,
} from '@/lib/flex-query/flexEnqueueGuards'

describe('flexEnqueueGuards', () => {
  it('blocks when running > 0', () => {
    expect(flexQueueHasRunning({ running: 1 })).toBe(true)
    expect(flexEnqueueBlockReason({ counts: { running: 1 } })?.code).toBe('running')
  })

  it('blocks on recent [1018] failures within cooldown', () => {
    const now = Date.parse('2026-08-31T14:00:00Z')
    const jobs = [
      {
        kind: 'flex-trades',
        status: 'failed',
        finished_at: '2026-08-31T13:45:00Z',
        result: { error: 'Flex request failed: [1018] Too many requests' },
      },
    ]
    expect(flexRecentRateLimitHit(jobs, { nowMs: now }).hit).toBe(true)
    expect(flexEnqueueBlockReason({ recentJobs: jobs, nowMs: now })?.code).toBe('rate_limit')
  })

  it('allows enqueue when rate-limit failure is old', () => {
    const now = Date.parse('2026-08-31T14:00:00Z')
    const jobs = [
      {
        kind: 'flex-trades',
        status: 'failed',
        finished_at: '2026-08-31T10:00:00Z',
        result: { error: '[1018] Too many requests' },
      },
    ]
    expect(flexRecentRateLimitHit(jobs, { nowMs: now }).hit).toBe(false)
    expect(flexEnqueueBlockReason({ counts: { running: 0 }, recentJobs: jobs, nowMs: now })).toBeNull()
  })
})
