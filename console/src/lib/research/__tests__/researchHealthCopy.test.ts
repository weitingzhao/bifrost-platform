import { describe, expect, it } from 'vitest'
import { statusOfWord } from '@bifrost/ui/city/world'
import {
  layerWord,
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
    // 2026-11-23 (箭10, Market Data slice): the sentence speaks the engine's seven words too (degraded is fail here).
    expect(copy.summary).toContain('Product ok')
    expect(copy.summary).toContain('Batch unprobed')
    expect(copy.summary).toMatch(/Flex fail/)
    expect(copy.summary).not.toMatch(/ops_dagster\.runs/)
    expect(copy.tagLabel).toBe('warn')
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
    expect(copy.tagLabel).toBe('ok')
    expect(copy.summary).toContain('Product ok')
    expect(copy.summary).toContain('Batch ok')
    expect(copy.summary).toMatch(/Flex fail|Market fail/)
  })

  it('unreachable fails hard', () => {
    const copy = buildResearchVerdictCopy({
      reachable: false,
      statusError: 'down',
    })
    expect(copy.tagLabel).toBe('fail')
    expect(copy.lamp).toBe('fail')
  })
})

// 2026-11-23 UI / Console: Research lanes and layers printed raw HEALTHY / DEGRADED / FAIL in capitals next to the
// engine words on the Data page, and the Console carried its own word table. Layer words now come from the engine's
// statusOfWord; the one exception is the Console's own degraded (a stale / empty layer or failed run), which is fail.
describe('layerWord', () => {
  it('reads the engine table, with the Console-derived degraded as fail', () => {
    expect(layerWord('healthy')).toBe('ok')
    expect(layerWord('due')).toBe('boundary')
    expect(layerWord('draining')).toBe('warn')
    expect(layerWord('caution')).toBe('warn')
    expect(layerWord('missed')).toBe('fail')
    expect(layerWord('degraded')).toBe('fail')
    expect(layerWord('unknown')).toBe('unknown')
  })
  // A plugin's quality check says PASS / FAIL; the engine reads them, the Console keeps no table of its own.
  it('reads a plugin quality verdict through the engine', () => {
    expect(statusOfWord('PASS')).toBe('ok')
    expect(statusOfWord('FAIL')).toBe('fail')
    expect(statusOfWord('computing')).toBe('unknown')
  })
})

// 2026-11-23 箭10 on the Market Data slice: a layer the Console derives as degraded is labelled fail with the reason in
// the sentence (stale / empty / run failed), so `degraded` keeps one meaning city-wide (a source's own warn).
describe('fail reasons in the sentence', () => {
  it('names why each derived layer failed and never says degraded', () => {
    const copy = buildResearchVerdictCopy({
      reachable: true,
      productOverall: 'empty',
      batchVerdict: 'degraded',
      marketVerdict: 'stale',
      flexVerdict: 'healthy',
    })
    expect(copy.summary).toContain('Product fail · empty')
    expect(copy.summary).toContain('Batch fail · run failed')
    expect(copy.summary).toContain('Market fail · stale')
    expect(copy.summary).not.toMatch(/degraded/)
    expect(copy.layers.find(l => l.id === 'product')!.meta).toBe('feature tables empty')
  })
})
