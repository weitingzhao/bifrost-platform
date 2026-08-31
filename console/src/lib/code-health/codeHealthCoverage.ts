/**
 * Code Health coverage map — Domain ↔ repos (and intentional exclusions).
 *
 * Must stay aligned with scan.sh KNOWN_REPOS + add_metric domains.
 * UI shows this so operators do not have to open the script to answer
 * “which repos does this plane actually measure?”
 *
 * Contract (v10): every **git repo in the multi-root workspace** is either in
 * CODE_HEALTH_COVERAGE or documented in CODE_HEALTH_EXCLUSIONS. No silent gaps.
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'

export type CodeHealthCoveredRepo = {
  repo: string
  /** Short label used in metric names, e.g. (trade-api). */
  short: string
}

export type CodeHealthCoveragePlane = {
  domain: SystemDomainId
  repos: CodeHealthCoveredRepo[]
  metricsNote: string
}

/** Repos scan.sh measures — order matches Console SYSTEM_DOMAINS display preference. */
export const CODE_HEALTH_COVERAGE: CodeHealthCoveragePlane[] = [
  {
    domain: 'rocket',
    metricsNote: 'oversized · infra shell/py dup',
    repos: [
      { repo: 'bifrost-platform', short: 'platform' },
      { repo: 'bifrost-ui', short: 'ui' },
      { repo: 'bifrost-trade-infra', short: 'infra' },
    ],
  },
  {
    domain: 'satellite',
    metricsNote: 'dup · oversized · FE contract',
    repos: [
      { repo: 'bifrost-trade-frontend', short: 'frontend' },
      { repo: 'bifrost-trade-api', short: 'trade-api' },
      { repo: 'bifrost-trade-core', short: 'trade-core' },
      { repo: 'bifrost-trade-worker', short: 'trade-worker' },
    ],
  },
  {
    domain: 'research',
    metricsNote: 'dup · oversized · image tiers',
    repos: [{ repo: 'bifrost-research', short: 'research' }],
  },
  {
    domain: 'subcontractors',
    metricsNote: 'dup · oversized',
    repos: [
      { repo: 'bifrost-platform-plugin', short: 'plugin' },
      { repo: 'bifrost-platform-plugin-market-data', short: 'market-data' },
      { repo: 'bifrost-platform-plugin-flex-query', short: 'flex-query' },
    ],
  },
]

export type CodeHealthExclusion = {
  repo: string
  reason: string
}

/**
 * Not measured — must still appear here so Coverage has no silent gaps.
 * Do not list multi-root workspace git repos here unless Owner documents why.
 */
export const CODE_HEALTH_EXCLUSIONS: CodeHealthExclusion[] = [
  {
    repo: 'Research-workspace',
    reason: 'Multi-root folder but not a git repo (draft drop zone) — no scan target',
  },
  {
    repo: 'bifrost-trade-socket',
    reason: 'GitHub Archived · removed from workspace (Wave 14G-F) — not a live checkout',
  },
  {
    repo: 'bifrost-analytics',
    reason: 'Archived into bifrost-research dbt (D13); not in multi-root workspace — do not edit',
  },
]

export function coveredRepoNames(): string[] {
  return CODE_HEALTH_COVERAGE.flatMap(p => p.repos.map(r => r.repo))
}
