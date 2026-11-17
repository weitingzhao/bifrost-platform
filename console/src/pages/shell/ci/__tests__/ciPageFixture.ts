import type { CiChange, CiDomainGraph } from '@/api/ciDomains'
import type { UnitsRun, UnitsRunTask } from '@/api/units'

/*
 * Invented runs in the shape GET /api/v1/units/runs answers (api/internal/agentrt/landingci.RunView) and an invented
 * graph in the shape GET /api/v1/ci/domains answers (api/internal/cidomains.Graph), for the tests of the CI page.
 */

export const AS_OF = '2026-11-16T20:00:00Z'
/** A commit id whose first eight characters (what the page shows) are the number. */
export const sha = (n: number) => n.toString(16).padStart(8, '0').padEnd(40, '0')

/** A run created `minutesAgo` before AS_OF: clone waited 8 s, then scope, then the two test tasks. */
export function unitsRun(n: number, minutesAgo: number, over: Partial<UnitsRun> & { api?: number; con?: number; failed?: string } = {}): UnitsRun {
  const { api = 60, con = 150, failed, ...rest } = over
  const status = failed ? 'Failed' : 'Succeeded'
  const task = (name: string, seconds: number): UnitsRunTask => ({ task: name, status: name === failed ? 'Failed' : 'Succeeded', seconds, waited: name === 'clone' ? 8 : 5 })
  return {
    name: `ci-${n}`, pipeline: 'ci-pipeline', revision: sha(n), status, trigger: 'gitea-webhook', full: false,
    created_at: new Date(Date.parse(AS_OF) - minutesAgo * 60_000).toISOString().replace('.000Z', 'Z'),
    seconds: 8 + 17 + Math.max(api, con), asked: true, run_modules: '*',
    tasks: [task('changes', 9), task('clone', 16), task('test-api', api), task('test-console', con)],
    ...rest,
  }
}

export function running(n: number, minutesAgo: number, tasks: UnitsRunTask[]): UnitsRun {
  return { ...unitsRun(n, minutesAgo), status: 'Running', seconds: undefined, tasks }
}

export const graph = (): CiDomainGraph => ({
  version: 1, repo: 'r', commit: sha(999), measured: true, note: '',
  domains: [{ id: 'foundation', packages: 60 }, { id: 'plan', packages: 5 }, { id: 'lineage', packages: 5 }, { id: 'owner', packages: 8 }],
  edges: [
    { from: 'foundation', to: 'owner', imports: 1, wrong_way: true },
    { from: 'plan', to: 'foundation', imports: 2, wrong_way: false },
    { from: 'plan', to: 'lineage', imports: 1, wrong_way: false },
    { from: 'lineage', to: 'foundation', imports: 9, wrong_way: false },
    { from: 'owner', to: 'foundation', imports: 14, wrong_way: false },
  ],
  shared_packages: ['svc/both'],
})

export const change = (n: number, domains: Record<string, number> | null): CiChange => ({ commit: sha(n), read: domains != null, domains: domains ?? {} })
