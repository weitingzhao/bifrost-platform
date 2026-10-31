import { describe, expect, it } from 'vitest'
import {
  buildResearchVerdictCopy,
  formatSchedulesSummary,
} from '@/lib/research/researchHealthCopy'

describe('formatSchedulesSummary', () => {
  it('formats running and stopped with last fail', () => {
    expect(
      formatSchedulesSummary({
        schedulesTotal: 26,
        schedulesRunning: 24,
        schedulesStopped: 2,
        recentFailures: [{ name: 'market_fundamentals_rotate_schedule' }],
      }),
    ).toBe('26 schedules · 2 stopped · last fail market_fundamentals_rotate_schedule')
  })

  it('omits when total missing', () => {
    expect(formatSchedulesSummary({})).toBeNull()
  })
})

describe('buildResearchVerdictCopy', () => {
  it('humanizes Product ok + Batch unprobed + Flex degraded without SQL jargon', () => {
    const copy = buildResearchVerdictCopy({
      reachable: true,
      marketVerdict: 'due',
      flexVerdict: 'degraded',
      batchVerdict: 'unknown',
      batchDetail: 'ops_dagster.runs not found — Dagster instance may not have started',
      productOverall: 'ok',
    })
    expect(copy.summary).toContain('Product OK')
    expect(copy.summary).toContain('Batch unprobed')
    expect(copy.summary).toMatch(/Flex DEGRADED/i)
    expect(copy.summary).not.toMatch(/ops_dagster\.runs/)
    expect(copy.tagLabel).toBe('CAUTION')
    expect(copy.layers).toHaveLength(3)
    expect(copy.layers.find(l => l.id === 'product')?.verdict).toBe('healthy')
    expect(copy.layers.find(l => l.id === 'batch')?.meta).toBe('orchestration unprobed')
  })

  it('batch layer includes multi-schedule rollup', () => {
    const copy = buildResearchVerdictCopy({
      reachable: true,
      batchVerdict: 'healthy',
      productOverall: 'ok',
      schedulesTotal: 26,
      schedulesRunning: 26,
      schedulesStopped: 0,
    })
    expect(copy.layers.find(l => l.id === 'batch')?.meta).toContain('26 schedules')
    expect(copy.layers.find(l => l.id === 'batch')?.meta).toContain('trading_day within SLA')
  })

  it('does not paint Product fail from Flex alone in rollup tag when Product+Batch healthy', () => {
    const copy = buildResearchVerdictCopy({
      reachable: true,
      marketVerdict: 'missed',
      flexVerdict: 'degraded',
      batchVerdict: 'healthy',
      productOverall: 'ok',
    })
    expect(copy.tagLabel).toBe('HEALTHY')
    expect(copy.summary).toContain('Product OK')
    expect(copy.summary).toContain('Batch OK')
    expect(copy.summary).toMatch(/Flex DEGRADED|Market MISSED/i)
  })

  it('unreachable fails hard', () => {
    const copy = buildResearchVerdictCopy({
      reachable: false,
      statusError: 'down',
    })
    expect(copy.tagLabel).toBe('UNREACHABLE')
    expect(copy.lamp).toBe('fail')
  })
})
