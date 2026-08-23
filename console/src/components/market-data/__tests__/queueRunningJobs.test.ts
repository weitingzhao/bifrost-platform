import { describe, expect, it } from 'vitest'
import type { IngestJob } from '@/api/marketDataPlugin'
import {
  formatDurationSec,
  kindQueueCountsLabel,
  runningAgeSec,
  runningCardCaption,
  runningFreshness,
  runningJobsSummary,
  sortRunningJobs,
} from '@/components/market-data/queueRunningJobs'

const now = Date.parse('2026-08-22T21:50:00Z')

function job(partial: Partial<IngestJob> & Pick<IngestJob, 'kind' | 'status'>): IngestJob {
  return { ...partial }
}

describe('queueRunningJobs', () => {
  it('ages from started_at and buckets live / long / stale', () => {
    expect(runningAgeSec('2026-08-22T21:49:20Z', now)).toBe(40)
    expect(runningFreshness(40)).toBe('live')
    expect(runningFreshness(9 * 60)).toBe('long')
    expect(runningFreshness(21 * 60)).toBe('stale')
    expect(runningFreshness(null)).toBe('unknown')
  })

  it('sorts oldest running first and summarizes stuck jobs', () => {
    const rows = sortRunningJobs(
      [
        job({
          kind: 'option_snapshot',
          status: 'running',
          started_at: '2026-08-22T21:48:00Z',
        }),
        job({
          kind: 'option_open_interest',
          status: 'running',
          started_at: '2026-08-22T21:10:00Z',
        }),
        job({ kind: 'option_contract', status: 'running' }),
      ],
      now,
    )
    expect(rows.map(j => j.kind)).toEqual([
      'option_open_interest',
      'option_snapshot',
      'option_contract',
    ])
    const summary = runningJobsSummary(rows, now)
    expect(summary.stale).toBe(1)
    expect(summary.oldestSec).toBe(40 * 60)
    expect(runningCardCaption(summary)).toBe('oldest 40m · 1 stuck?')
  })

  it('labels kind depth in ready/running words', () => {
    expect(kindQueueCountsLabel({ pending: 0, running: 16 })).toEqual({
      value: 16,
      valueText: '16 running',
      suffix: null,
    })
    expect(kindQueueCountsLabel({ pending: 4, running: 2 })).toEqual({
      value: 4,
      valueText: '4 ready',
      suffix: '2 running',
    })
    expect(formatDurationSec(90)).toBe('1m 30s')
  })
})
