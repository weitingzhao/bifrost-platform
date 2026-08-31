/**
 * Code Health coverage map — Domain ↔ repos (and intentional exclusions).
 *
 * Must stay aligned with scan.sh KNOWN_REPOS + add_metric domains.
 * UI shows this so operators do not have to open the script to answer
 * “which repos does this plane actually measure?”
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
    metricsNote: 'oversized files',
    repos: [
      { repo: 'bifrost-platform', short: 'platform' },
      { repo: 'bifrost-ui', short: 'ui' },
    ],
  },
  {
    domain: 'satellite',
    metricsNote: 'dup · oversized · FE API contract',
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

/** Workspace actives deliberately outside the ratchet (not a measurement gap). */
export const CODE_HEALTH_EXCLUSIONS: CodeHealthExclusion[] = [
  {
    repo: 'bifrost-trade-socket',
    reason: 'Wave 14G-F Phase 0–2 done — Plugin→redis-ib; ARCHIVED.md; workspace remove after ≥90d',
  },
  {
    repo: 'bifrost-trade-infra',
    reason: 'Governance host (YAML/scripts) — runs the scanner, not a measured product surface',
  },
  {
    repo: 'Research-workspace',
    reason: 'Non-product draft directory (not a git delivery repo)',
  },
  {
    repo: 'bifrost-analytics',
    reason: 'Archived into bifrost-research dbt (D13)',
  },
]

export function coveredRepoNames(): string[] {
  return CODE_HEALTH_COVERAGE.flatMap(p => p.repos.map(r => r.repo))
}
