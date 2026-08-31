import { describe, expect, it } from 'vitest'
import {
  CODE_HEALTH_COVERAGE,
  CODE_HEALTH_EXCLUSIONS,
  coveredRepoNames,
} from '@/lib/code-health/codeHealthCoverage'

describe('codeHealthCoverage', () => {
  it('covers ten product repos across four domains', () => {
    expect(CODE_HEALTH_COVERAGE.map(p => p.domain)).toEqual([
      'rocket',
      'satellite',
      'research',
      'subcontractors',
    ])
    expect(coveredRepoNames()).toHaveLength(10)
    expect(coveredRepoNames()).toContain('bifrost-trade-api')
    expect(coveredRepoNames()).toContain('bifrost-ui')
  })

  it('documents intentional exclusions including half-retired socket', () => {
    const names = CODE_HEALTH_EXCLUSIONS.map(e => e.repo)
    expect(names).toContain('bifrost-trade-socket')
    expect(names).toContain('bifrost-trade-infra')
    expect(coveredRepoNames().some(r => names.includes(r))).toBe(false)
  })
})
