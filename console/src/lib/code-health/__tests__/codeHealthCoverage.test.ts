import { describe, expect, it } from 'vitest'
import {
  CODE_HEALTH_COVERAGE,
  CODE_HEALTH_EXCLUSIONS,
  coveredRepoNames,
} from '@/lib/code-health/codeHealthCoverage'

/** Multi-root workspace git repos (see bifrost-trade.code-workspace). */
const WORKSPACE_GIT_REPOS = [
  'bifrost-ui',
  'bifrost-trade-infra',
  'bifrost-platform',
  'bifrost-platform-plugin',
  'bifrost-platform-plugin-market-data',
  'bifrost-platform-plugin-flex-query',
  'bifrost-trade-frontend',
  'bifrost-trade-core',
  'bifrost-trade-worker',
  'bifrost-trade-api',
  'bifrost-research',
] as const

describe('codeHealthCoverage', () => {
  it('covers eleven product/governance repos across four domains', () => {
    expect(CODE_HEALTH_COVERAGE.map(p => p.domain)).toEqual([
      'rocket',
      'satellite',
      'research',
      'subcontractors',
    ])
    expect(coveredRepoNames()).toHaveLength(11)
    expect(coveredRepoNames()).toContain('bifrost-trade-api')
    expect(coveredRepoNames()).toContain('bifrost-ui')
    expect(coveredRepoNames()).toContain('bifrost-trade-infra')
  })

  it('covers every multi-root workspace git repo (no silent gaps)', () => {
    const covered = new Set(coveredRepoNames())
    for (const repo of WORKSPACE_GIT_REPOS) {
      expect(covered.has(repo), `${repo} must be in CODE_HEALTH_COVERAGE`).toBe(true)
    }
  })

  it('documents non-git / archived exclusions without overlapping coverage', () => {
    const names = CODE_HEALTH_EXCLUSIONS.map(e => e.repo)
    expect(names).toContain('Research-workspace')
    expect(names).toContain('bifrost-trade-socket')
    expect(names).toContain('bifrost-analytics')
    expect(names).not.toContain('bifrost-trade-infra')
    expect(coveredRepoNames().some(r => names.includes(r))).toBe(false)
  })
})
