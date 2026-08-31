import { describe, expect, it } from 'vitest'
import type { CodeHealthMetricDto, CodeHealthResponse } from '@/api/codeHealth'
import {
  buildCodeHealthLens,
  formatDeltaSlack,
  metricSlack,
  resolveCodeHealthDimension,
} from '@/lib/code-health/codeHealthLens'

function metric(over: Partial<CodeHealthMetricDto> & { id: string }): CodeHealthMetricDto {
  return {
    label: over.label ?? over.id,
    domain: over.domain ?? 'satellite',
    repo: over.repo ?? 'bifrost-trade-frontend',
    value: over.value ?? 12,
    baseline: over.baseline ?? 12,
    status: over.status ?? 'at_baseline',
    detail: over.detail,
    ...over,
  }
}

function response(
  metrics: CodeHealthMetricDto[],
  extra?: {
    history?: CodeHealthResponse['history']
    note?: string
    commit?: string
    received_at?: string
  },
): CodeHealthResponse {
  return {
    reported: true,
    note: extra?.note,
    latest: {
      generated_at: '2026-08-31T12:00:00Z',
      commit: extra?.commit ?? 'abc1234',
      metrics,
      received_at: extra?.received_at ?? '2026-08-31T12:00:00Z',
    },
    history: extra?.history,
  }
}

describe('resolveCodeHealthDimension', () => {
  it('maps known metric id prefixes', () => {
    expect(resolveCodeHealthDimension('code.oversized.rocket')).toBe('size')
    expect(resolveCodeHealthDimension('code.duplication.satellite')).toBe('duplication')
    expect(resolveCodeHealthDimension('code.contract-coverage.satellite')).toBe('contract')
    expect(resolveCodeHealthDimension('code.image-version-spread.research')).toBe('image_spread')
    expect(resolveCodeHealthDimension('code.mystery')).toBe('unknown')
  })
})

describe('metricSlack', () => {
  it('is baseline minus value (negative when over)', () => {
    expect(metricSlack(metric({ id: 'a', value: 10, baseline: 12 }))).toBe(2)
    expect(metricSlack(metric({ id: 'b', value: 12, baseline: 12 }))).toBe(0)
    expect(metricSlack(metric({ id: 'c', value: 15, baseline: 12 }))).toBe(-3)
  })
})

describe('buildCodeHealthLens', () => {
  it('reports NOT OBSERVED when nothing has been scanned', () => {
    const lens = buildCodeHealthLens({ reported: false, note: 'never submitted' })
    expect(lens.reported).toBe(false)
    expect(lens.planningLamp).toBe('unknown')
    expect(lens.planningTag).toBe('NOT OBSERVED')
    expect(lens.minSlack).toBeNull()
  })

  it('reports unknown when response is null', () => {
    expect(buildCodeHealthLens(null).planningLamp).toBe('unknown')
  })

  it('marks all-at-baseline as at ceiling (degraded), not healthy green', () => {
    const lens = buildCodeHealthLens(
      response([
        metric({ id: 'code.oversized.rocket', domain: 'rocket', value: 34, baseline: 34 }),
        metric({
          id: 'code.duplication.satellite',
          value: 12,
          baseline: 12,
        }),
      ]),
    )
    expect(lens.overCount).toBe(0)
    expect(lens.atCeilingCount).toBe(2)
    expect(lens.minSlack).toBe(0)
    expect(lens.planningLamp).toBe('degraded')
    expect(lens.planningTag).toContain('AT CEILING')
    expect(lens.paydownQueue).toHaveLength(2)
    expect(lens.posture.gate).toBe('CLEAR')
    expect(lens.posture.planning).toBe('AT_CEILING')
    expect(lens.posture.summaryLine).toContain('Gate CLEAR')
    expect(lens.posture.summaryLine).toContain('AT CEILING')
    expect(lens.nextCut?.metric.id).toBeTruthy()
    expect(lens.dimensionSummaries.some(d => d.dimension === 'size')).toBe(true)
    expect(lens.dimensionSummaries.some(d => d.dimension === 'duplication')).toBe(true)
    expect(lens.planningTitle).toBe(lens.posture.summaryLine)
  })

  it('uses fail lamp when any metric is over baseline', () => {
    const lens = buildCodeHealthLens(
      response([
        metric({
          id: 'code.oversized.rocket',
          domain: 'rocket',
          value: 35,
          baseline: 34,
          status: 'over',
        }),
        metric({ id: 'code.duplication.satellite', value: 10, baseline: 12, status: 'improved' }),
      ]),
    )
    expect(lens.overCount).toBe(1)
    expect(lens.owedCount).toBe(1)
    expect(lens.planningLamp).toBe('fail')
    expect(lens.paydownQueue[0]?.over).toBe(true)
    expect(lens.posture.gate).toBe('BLOCKED')
    expect(lens.posture.nextLine).toContain('code.oversized.rocket')
    expect(lens.posture.nextLine).toContain('slack -1')
  })

  it('is ok when every metric has positive slack', () => {
    const lens = buildCodeHealthLens(
      response([
        metric({
          id: 'code.oversized.rocket',
          domain: 'rocket',
          value: 30,
          baseline: 34,
          status: 'improved',
        }),
      ]),
    )
    expect(lens.minSlack).toBe(4)
    expect(lens.planningLamp).toBe('ok')
    expect(lens.planningTag).toBe('HELD')
    expect(lens.paydownQueue).toHaveLength(0)
  })

  it('orders paydown queue: OVER first, then ascending slack', () => {
    const lens = buildCodeHealthLens(
      response([
        metric({
          id: 'code.duplication.satellite',
          value: 12,
          baseline: 12,
          status: 'at_baseline',
          repo: 'frontend',
        }),
        metric({
          id: 'code.oversized.rocket',
          domain: 'rocket',
          value: 40,
          baseline: 34,
          status: 'over',
          repo: 'platform',
        }),
        metric({
          id: 'code.contract-coverage.satellite',
          value: 28,
          baseline: 29,
          status: 'improved',
          repo: 'frontend',
        }),
      ]),
    )
    // only over + at_ceiling in queue; improved with slack>0 stays out
    expect(lens.paydownQueue.map(m => m.metric.id)).toEqual([
      'code.oversized.rocket',
      'code.duplication.satellite',
    ])
  })

  it('computes Δ slack against previous history report', () => {
    const older: CodeHealthMetricDto = metric({
      id: 'code.oversized.rocket',
      domain: 'rocket',
      value: 34,
      baseline: 34,
      status: 'at_baseline',
    })
    const newer: CodeHealthMetricDto = metric({
      id: 'code.oversized.rocket',
      domain: 'rocket',
      value: 33,
      baseline: 34,
      status: 'improved',
    })
    const lens = buildCodeHealthLens(
      response([newer], {
        commit: 'bbbbbbb',
        received_at: '2026-08-31T14:00:00Z',
        history: [
          {
            generated_at: '2026-08-31T14:00:00Z',
            commit: 'bbbbbbb',
            metrics: [newer],
            received_at: '2026-08-31T14:00:00Z',
          },
          {
            generated_at: '2026-08-31T12:00:00Z',
            commit: 'aaaaaaa',
            metrics: [older],
            received_at: '2026-08-31T12:00:00Z',
          },
        ],
      }),
    )
    expect(lens.hasTrend).toBe(true)
    expect(lens.metrics[0]?.deltaSlack).toBe(1)
    expect(lens.totalDeltaSlack).toBe(1)
    expect(formatDeltaSlack(1, true)).toBe('+1')
    expect(formatDeltaSlack(null, false)).toBe('—')
  })
})
