/**
 * Code-health signal evaluation.
 *
 * The property under test is not "does it show numbers" — it is that an
 * unmeasured domain never renders as healthy. A screenshot proves that once;
 * these tests keep it true.
 */
import { describe, expect, it } from 'vitest'
import type { CodeHealthMetricDto, CodeHealthResponse } from '@/api/codeHealth'
import { buildObservabilityViewModel } from '@/lib/observability/observabilityViewModel'
import { SIGNAL_REGISTRY } from '@/lib/observability/signalRegistry'
import type { EvaluatedSignal } from '@/lib/observability/types'

function metric(over: Partial<CodeHealthMetricDto> & { domain: string }): CodeHealthMetricDto {
  return {
    id: 'code.example',
    label: 'duplicated function names',
    repo: 'repo',
    value: 12,
    baseline: 12,
    status: 'at_baseline',
    ...over,
  }
}

function report(metrics: CodeHealthMetricDto[], extra: Partial<CodeHealthResponse['latest']> = {}): CodeHealthResponse {
  return {
    reported: true,
    latest: {
      generated_at: '2026-08-31T00:00:00Z',
      commit: 'abc1234',
      metrics,
      received_at: '2026-08-31T00:00:00Z',
      ...extra,
    },
  }
}

function signal(codeHealth: CodeHealthResponse | null | undefined, id: string): EvaluatedSignal {
  const vm = buildObservabilityViewModel({ selectedEnv: 'dev', selectedDomain: 'satellite', codeHealth })
  const found = vm.domains.flatMap(d => d.signals).find(s => s.def.id === id)
  expect(found, `missing evaluated signal ${id}`).toBeTruthy()
  return found!
}

describe('code-health signals', () => {
  it('registers one rollup per owning domain, all optional-contract evidence', () => {
    const defs = SIGNAL_REGISTRY.filter(s => s.source === 'code_health')
    expect(defs.map(d => d.domain).sort()).toEqual(['research', 'rocket', 'satellite'])
    for (const d of defs) {
      // evidence: a duplicated helper must not flip a verdict operators page on.
      expect(d.role).toBe('evidence')
      // optionalContract: absence of data must land on NOT OBSERVED.
      expect(d.optionalContract).toBe(true)
      expect(d.detailRoute).toBe('code-health')
    }
  })

  it('reports NOT OBSERVED when nothing has ever been scanned', () => {
    expect(signal({ reported: false }, 'code-health.satellite').state).toBe('not_observed')
  })

  it('reports NOT OBSERVED when the report has not loaded', () => {
    expect(signal(null, 'code-health.satellite').state).toBe('not_observed')
  })

  it('reports NOT OBSERVED — never healthy — for a domain absent from the report', () => {
    const s = signal(report([metric({ domain: 'satellite' })], { not_measured: 'bifrost-research' }), 'code-health.research')
    expect(s.state).toBe('not_observed')
    expect(s.summary).toContain('bifrost-research')
  })

  it('is healthy only when the domain actually has readings at or below baseline', () => {
    const s = signal(report([metric({ domain: 'satellite' })]), 'code-health.satellite')
    expect(s.state).toBe('healthy')
    expect(s.evidence).toContain('abc1234')
  })

  it('degrades — not criticals — when a metric is over baseline, and names it', () => {
    const s = signal(
      report([
        metric({ domain: 'satellite', status: 'over', value: 13, label: 'duplicated function names' }),
        metric({ domain: 'satellite', id: 'code.oversized', label: 'files over 800 lines' }),
      ]),
      'code-health.satellite',
    )
    expect(s.state).toBe('degraded')
    expect(s.summary).toContain('1/2 over baseline')
    expect(s.summary).toContain('duplicated function names')
  })

  it('surfaces an owed baseline lowering so improvements cannot leave slack', () => {
    const s = signal(
      report([metric({ domain: 'satellite', status: 'improved', value: 9 })]),
      'code-health.satellite',
    )
    expect(s.state).toBe('healthy')
    expect(s.summary).toContain('1 baseline lowering owed')
  })
})
