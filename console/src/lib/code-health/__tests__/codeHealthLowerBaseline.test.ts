import { describe, expect, it } from 'vitest'
import type { CodeHealthMetricDto } from '@/api/codeHealth'
import {
  listLowerBaselineProposals,
  proposeLowerBaseline,
  resolveBaselineVar,
} from '@/lib/code-health/codeHealthLowerBaseline'

function metric(over: Partial<CodeHealthMetricDto> & { id: string }): CodeHealthMetricDto {
  return {
    label: over.label ?? over.id,
    domain: over.domain ?? 'satellite',
    repo: over.repo ?? 'bifrost-trade-frontend',
    value: over.value ?? 10,
    baseline: over.baseline ?? 12,
    status: over.status ?? 'improved',
    ...over,
  }
}

describe('proposeLowerBaseline', () => {
  it('builds a locked proposal for improved metrics', () => {
    const p = proposeLowerBaseline(
      metric({
        id: 'code.duplication.satellite',
        label: 'duplicated function names',
        value: 10,
        baseline: 12,
        status: 'improved',
        baseline_var: 'DUP_FUNCS_FRONTEND_BASELINE',
      }),
    )
    expect(p).not.toBeNull()
    expect(p!.from).toBe(12)
    expect(p!.to).toBe(10)
    expect(p!.baselineVar).toBe('DUP_FUNCS_FRONTEND_BASELINE')
    expect(p!.patch).toContain('-DUP_FUNCS_FRONTEND_BASELINE=12')
    expect(p!.patch).toContain('+DUP_FUNCS_FRONTEND_BASELINE=10')
    expect(p!.agentBrief).toContain('exactly 10')
  })

  it('rejects over / at_baseline / unknown var', () => {
    expect(
      proposeLowerBaseline(metric({ id: 'code.oversized.rocket', status: 'over', value: 35, baseline: 34 })),
    ).toBeNull()
    expect(
      proposeLowerBaseline(
        metric({ id: 'code.oversized.rocket', status: 'at_baseline', value: 34, baseline: 34 }),
      ),
    ).toBeNull()
    expect(
      proposeLowerBaseline(
        metric({ id: 'code.unknown.metric', status: 'improved', value: 1, baseline: 2 }),
      ),
    ).toBeNull()
  })

  it('falls back to catalog when baseline_var missing', () => {
    expect(resolveBaselineVar(metric({ id: 'code.oversized.research' }))).toBe(
      'OVERSIZED_RESEARCH_BASELINE',
    )
    const list = listLowerBaselineProposals([
      metric({
        id: 'code.oversized.research',
        domain: 'research',
        repo: 'bifrost-research',
        value: 3,
        baseline: 5,
        status: 'improved',
      }),
      metric({ id: 'code.oversized.rocket', status: 'at_baseline', value: 34, baseline: 34 }),
    ])
    expect(list).toHaveLength(1)
    expect(list[0]!.to).toBe(3)
  })
})
