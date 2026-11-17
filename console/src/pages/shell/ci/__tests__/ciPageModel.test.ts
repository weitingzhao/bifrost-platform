import { describe, expect, it } from 'vitest'
import type { CiScorecard } from '@/api/ciScorecard'
import type { TimingRun } from '@/api/ciTimings'
import {
  changeMap, ciSteps, phaseOf, stageSteps, domainCards, domainPanel, edgeSides, graphLayout, graphSize, notMeasured, ownRuns, selectiveFact, startsAfterMain, timelineGeometry, verdict,
  type DeliveryStage, type VerdictInput,
} from '../ciPageModel'
import { AS_OF, change, graph, running, sha, unitsRun } from './ciPageFixture'

/*
 * Origin of every case in this file — 2026-11-16: the Owner read Work → CI and could not tell what was running, who
 * depends on whom, or what a change pulls in; this page and its two routes replace it. Each case says what it guards.
 */

const input = (over: Partial<VerdictInput> & { units?: ReturnType<typeof unitsRun>[] } = {}): VerdictInput => {
  const { units = [unitsRun(3, 10), unitsRun(2, 20), unitsRun(1, 30)], ...rest } = over
  return { runs: ownRuns(units, AS_OF), asOf: AS_OF, deliveries: [], window: null, ...rest }
}
const stage = (label: string, ...statuses: string[]): DeliveryStage => ({ label, runs: statuses.map((status, i) => ({ name: `${label}-${i}`, commit: sha(900 + i), status, failedTasks: status === 'Failed' ? ['rollout'] : [] })) })

describe('verdict', () => {
  // Guards the rule's floor: nothing red, nothing slow, nobody holding the window is Healthy and says who holds the window.
  it('is Healthy when the last runs are green and nothing is stuck', () => {
    const v = verdict(input())
    expect(v).toMatchObject({ level: 'Healthy', waitsFor: null, reasons: [] })
    expect(v.sentence).toBe('Nothing is in CI. Of the last 3 runs 3 are green, 0 red. Nobody holds the release window.')
  })

  // Guards Blocked's first half: the newest finished CI run on main is red. A red run of a commit that is not on main is not it.
  it('is Blocked when the newest finished run on main is red, and names the failed task', () => {
    const units = [unitsRun(3, 10, { failed: 'test-api' }), unitsRun(2, 20), unitsRun(1, 30)]
    expect(verdict(input({ units })).level).toBe('Degraded')
    const v = verdict(input({ units, mainHead: sha(3) }))
    expect(v).toMatchObject({ level: 'Blocked', waitsFor: `test-api on ${sha(3).slice(0, 8)}` })
    expect(verdict(input({ units, landed: new Set([sha(3)]) })).level).toBe('Blocked')
    // A newer green run on main clears it: only the newest finished one counts.
    expect(verdict(input({ units: [unitsRun(4, 5, { full: true }), ...units], mainHead: sha(3) })).level).toBe('Degraded')
  })

  // Guards Blocked's second half: a delivery failed last and nothing newer succeeded; a failure with a newer success is history.
  it('is Blocked when a delivery stage failed last, and not when a newer delivery succeeded', () => {
    const v = verdict(input({ deliveries: [stage('STG delivery', 'Running', 'Failed', 'Succeeded')] }))
    expect(v).toMatchObject({ level: 'Blocked', waitsFor: 'STG delivery › rollout' })
    expect(v.reasons[0]).toContain('STG delivery failed last in rollout')
    expect(verdict(input({ deliveries: [stage('STG delivery', 'Succeeded', 'Failed')] })).level).toBe('Healthy')
  })

  // Guards Degraded's first half: any of the last 20 own-test runs red; the 21st does not count.
  it('is Degraded when one of the last 20 runs is red, and Healthy when the red one is the 21st', () => {
    const twenty = Array.from({ length: 20 }, (_, i) => unitsRun(100 + i, i + 1))
    expect(verdict(input({ units: [...twenty.slice(0, 19), unitsRun(50, 25, { failed: 'test-console' })] }))).toMatchObject({ level: 'Degraded', reasons: ['1 of the last 20 runs is red'] })
    expect(verdict(input({ units: [...twenty, unitsRun(50, 25, { failed: 'test-console' })] })).level).toBe('Healthy')
  })

  // Guards Degraded's second half: in flight longer than three times the 24 h median (175 s here), and what it waits for.
  it('is Degraded when a run has been in flight longer than 3× the 24 h median', () => {
    const fast = running(9, 5, [{ task: 'clone', status: 'Succeeded', seconds: 16, waited: 8 }, { task: 'test-api', status: 'Running', waited: 5 }])
    const slow = { ...fast, created_at: new Date(Date.parse(AS_OF) - 526_000).toISOString() }
    expect(verdict(input({ units: [fast, unitsRun(2, 20), unitsRun(1, 30)] }))).toMatchObject({ level: 'Healthy' })
    const v = verdict(input({ units: [slow, unitsRun(2, 20), unitsRun(1, 30)] }))
    expect(v).toMatchObject({ level: 'Degraded', waitsFor: `test-api on ${sha(9).slice(0, 8)}` })
    expect(v.sentence).toContain('One change is in CI, at test-api.')
  })

  // Guards "who it waits for": a held window is named whatever the level; a window that was not read is not called free.
  it('names the window holder, and says nothing about a window it did not read', () => {
    const held = verdict(input({ window: { who: 'agent-8-comm', what: 'bifrost-platform' } }))
    expect(held.waitsFor).toBe('the release window, held by agent-8-comm')
    expect(held.sentence).toContain('The release window is held by agent-8-comm (bifrost-platform).')
    expect(verdict(input({ window: undefined })).sentence).not.toContain('window')
  })
})

describe('runs', () => {
  // Guards the bar's parts: waiting is the first task's wait, tests the longest test task, the rest is before the tests;
  // a run that stood on another run's verdict is not a bar; a selective run and its packages come from the pipeline's own result.
  it('splits a run into waiting, before the tests and tests, and reads the scope from the detailed record', () => {
    const detail = { name: 'ci-1', results: [{ name: 'api-scope', value: 'incremental' }, { name: 'api-packages', value: 'internal/a internal/b' }], tasks: [] } as unknown as TimingRun
    const [newer, older] = ownRuns([unitsRun(2, 5, { api: 200, con: 20 }), unitsRun(1, 30), { ...unitsRun(7, 1), tested_by: 'ci-2' }], AS_OF, [detail])
    expect(newer).toMatchObject({ name: 'ci-2', took: 225, lag: 8, setup: 17, api: 200, con: 20, testsBy: 'api', ok: true, scope: null, packages: null })
    expect(older).toMatchObject({ name: 'ci-1', testsBy: 'con', scope: 'incremental', packages: ['internal/a', 'internal/b'] })
    expect(selectiveFact([newer, older])).toEqual({ selective: 1, known: 1, selectiveMedian: 175, fullMedian: null })
  })

  // Guards "the run in flight is lit on the step it is at": the running task is now, what finished is done, a side
  // the scope left out is skipped, and with no run in flight every step is idle.
  // Origin: 2026-11-16, the redesigned page on STG with real runs: one chip a task gave STG ten chips and PROD nine,
  // wrapped over three rows with names cut on both sides. A stage shows its phases; a phase with a task running is now.
  it('groups a delivery run\'s tasks into phases', () => {
    const task = (name: string, status: string, at: number) => ({ task: name, status, created_at: new Date(Date.UTC(2026, 10, 17, 3, 0, at)).toISOString() })
    const run = (status: string, tasks: ReturnType<typeof task>[]) => ({ name: 'r', status, created_at: '2026-11-17T03:00:00Z', tasks }) as never
    const tasks = [task('freeze-check', 'Succeeded', 0), task('clone-platform', 'Succeeded', 1), task('clone-ui', 'Succeeded', 1), task('build-platform-api', 'Running', 2),
      task('build-platform-console', 'Succeeded', 2), task('agentrt-migrate', 'Pending', 3), task('gitops-sync', 'Pending', 4), task('rollout', 'Pending', 5)]
    expect(stageSteps([run('Running', tasks)]).map(s => `${s.label}:${s.state}`)).toEqual(['check:done', 'clone:done', 'build:now', 'migrate:idle', 'sync:idle', 'rollout:idle'])
    expect(stageSteps([run('Succeeded', tasks)]).every(s => s.state === 'idle')).toBe(true)
    expect(['mirror-sync', 'check-source', 'agentrt-schema', 'promote-images', 'land', 'something-else'].map(phaseOf)).toEqual(['clone', 'check', 'migrate', 'promote', 'merge', 'something-else'])
  })

  it('lights the step of the run in flight', () => {
    const history = [unitsRun(2, 20), unitsRun(1, 30)]
    const flying = { ...running(9, 1, [{ task: 'clone', status: 'Succeeded', seconds: 16, waited: 8 }, { task: 'changes', status: 'Succeeded', seconds: 9 }, { task: 'test-api', status: 'Running', waited: 5 }]), run_modules: ' api repo ' }
    expect(ciSteps([flying, ...history], flying).map(s => `${s.label}:${s.state}`)).toEqual(['setup:done', 'api:now', 'console:skip'])
    expect(ciSteps(history).map(s => s.state)).toEqual(['idle', 'idle', 'idle'])
  })

  // Guards "Starts N s after main moves" as a live reading: merge completion to the same commit's first delivery start.
  it('measures how long after a merge its delivery starts, only for deliveries whose merge is loaded', () => {
    const run = (name: string, commit: string, start: string, end: string): TimingRun => ({ name, pipeline: 'p', status: 'True', created_at: start, start_time: start, completion_time: end, params: {}, results: [{ name: 'commit', value: commit }], tasks: [], skipped: [] })
    const lands = [run('land-1', 'a', '2026-11-16T19:00:00Z', '2026-11-16T19:00:14Z'), run('land-2', 'b', '2026-11-16T19:10:00Z', '2026-11-16T19:10:10Z')]
    const ships = [run('ship-1', 'a', '2026-11-16T19:00:25Z', '2026-11-16T19:04:00Z'), run('ship-1b', 'a', '2026-11-16T19:30:00Z', '2026-11-16T19:34:00Z'), run('ship-2', 'b', '2026-11-16T19:10:17Z', '2026-11-16T19:14:00Z'), run('ship-3', 'c', '2026-11-16T19:20:00Z', '2026-11-16T19:24:00Z')]
    expect(startsAfterMain(lands, ships)).toEqual({ seconds: 9, samples: 2 })
    expect(startsAfterMain([], ships)).toEqual({ seconds: null, samples: 0 })
  })
})

describe('domains', () => {
  const runs = ownRuns([unitsRun(4, 5), unitsRun(3, 10, { failed: 'test-api' }), unitsRun(2, 20), unitsRun(1, 30)], AS_OF)
  const changes = changeMap([change(4, { plan: 2, console: 1 }), change(3, { plan: 1 }), change(2, { foundation: 3 }), change(1, null)])

  // Guards "one card a domain + shared + console + repository files": a run counts for every domain its commit changed;
  // a run whose commit was not read is under no domain.
  it('builds one card a domain and the three places, with red count and median', () => {
    const cards = domainCards(graph(), runs, changes)
    expect(cards.map(c => c.id)).toEqual(['foundation', 'plan', 'lineage', 'owner', 'shared', 'console', 'repo'])
    expect(cards.find(c => c.id === 'plan')).toMatchObject({ label: 'plan', red: 1, median: 175, selective: 0 })
    expect(cards.find(c => c.id === 'plan')!.runs.map(r => r.name)).toEqual(['ci-3', 'ci-4'])
    expect(cards.find(c => c.id === 'console')).toMatchObject({ label: 'console (frontend)' })
    expect(cards.flatMap(c => c.runs).some(r => r.name === 'ci-1')).toBe(false)
  })

  // Guards the side panel: depends on, depended on by, with the wrong-way import marked on both ends.
  it('says what a domain depends on and who depends on it', () => {
    expect(domainPanel(graph(), 'owner')).toEqual({ dependsOn: [{ id: 'foundation', imports: 14, wrongWay: false }], dependedOnBy: [{ id: 'foundation', imports: 1, wrongWay: true }], packages: 8, shared: 1 })
    expect(domainPanel(graph(), 'nope')).toBeNull()
  })

  // Guards the drawing: the base every domain stands on is in the middle with a row above and a row below (an arrow
  // to it crosses no node), nodes do not overlap, the two arrows between the base and a domain leave by different
  // sides, and an arrow past a neighbour in its row arcs outside the row instead of running through the neighbour
  // (2026-11-16, first render: owner → coordination ran through compute).
  it('lays the base in the middle, keeps nodes apart, and arcs an arrow past a neighbour', () => {
    const wide = { ...graph(), domains: [...graph().domains, { id: 'compute', packages: 5 }, { id: 'coordination', packages: 3 }, { id: 'tenant', packages: 12 }] }
    const layout = graphLayout(wide)
    const at = (id: string) => layout.find(n => n.id === id)!
    const base = at('foundation')
    expect(layout.filter(n => n.id !== 'foundation').map(n => Math.sign(n.y - base.y))).toEqual([-1, -1, -1, 1, 1, 1])
    for (const a of layout) for (const b of layout) if (a.id < b.id) expect(a.x + a.width <= b.x || b.x + b.width <= a.x || a.y + a.height <= b.y || b.y + b.height <= a.y).toBe(true)
    expect(graphSize(layout)).toEqual({ width: base.width, height: at('tenant').y + at('tenant').height })
    expect(edgeSides(at('owner'), base)).toEqual(['bottom', 'top', 'straight'])
    expect(edgeSides(base, at('owner'))).toEqual(['top', 'bottom', 'straight'])
    expect(edgeSides(at('plan'), at('lineage'))).toEqual(['right', 'left', 'straight'])
    expect(edgeSides(at('plan'), at('owner'))).toEqual(['top', 'top', 'default'])
    expect(edgeSides(at('compute'), at('tenant'))).toEqual(['bottom', 'bottom', 'default'])
  })
})

// Guards "legible at 390 px": at phone width the drawing is 360 units wide (a 10-unit label is about 10 px on a 390 px
// screen, not 3), the tick labels are at least 44 units apart, and a failed task's name has room right of the longest bar.
it('keeps the timeline legible at phone width', () => {
  for (const maxSeconds of [60, 240, 975, 4000]) {
    const narrow = timelineGeometry(true, maxSeconds)
    expect(narrow.width).toBe(360)
    expect(narrow.step * narrow.scale).toBeGreaterThanOrEqual(44)
    expect(narrow.left + maxSeconds * narrow.scale + narrow.right).toBeLessThanOrEqual(narrow.width + 0.001)
    expect(narrow.right).toBeGreaterThanOrEqual(14 * 6 + narrow.rowH + 8) // "test-api-store" at 10 units a line, after the red dot
    expect(narrow.rowH + narrow.gap).toBeGreaterThanOrEqual(12) // a 10-unit label per row does not touch the next row's
    expect(timelineGeometry(false, maxSeconds).step * timelineGeometry(false, maxSeconds).scale).toBeGreaterThanOrEqual(44)
  }
})

describe('not measured', () => {
  const runs = ownRuns([unitsRun(3, 5), unitsRun(2, 20), unitsRun(1, 30)], AS_OF)
  const scorecard = { repos: [{ repo: 'infra', pipeline: 'p', tier: 'full', overall: 'unmeasured', items: [{ id: 'follows_change', label: 'Time follows the change', verdict: 'unmeasured', value: 'its pipeline reports no scope', samples: 0 }, { id: 'red_ratio', label: 'Red runs', verdict: 'pass', value: '8 of 150', samples: 150 }] }], provider: { overall: 'pass', items: [] } } as unknown as CiScorecard

  // Guards "ONE list of everything the page could not measure": what it computed as missing (commits not read, scopes
  // not read), the sketch's facts no read answers, the reads that failed, and what the API itself reports unmeasured.
  it('lists what the page computed as missing, what has no source, failed reads and the API\'s own unmeasured items', () => {
    const lines = notMeasured({ repo: 'r', runs, changes: changeMap([change(3, { plan: 1 }), change(2, null)]), graph: graph(), scorecard, stages: true, unread: ['The release window: could not be read (HTTP 502)'] })
    expect(lines).toContain('What changed in 1 of 3 runs: their commits are not in the repository (they never reached a kept branch) or could not be read, so they appear By time but under no domain')
    expect(lines).toContain('What changed in 1 of 3 runs: not asked (one read names at most 120 commits)')
    expect(lines.some(l => l.startsWith('Whether 3 of 3 finished runs were selective'))).toBe(true)
    expect(lines.some(l => l.startsWith('Reconcile:'))).toBe(true)
    expect(lines.some(l => l.startsWith('Queue:'))).toBe(true)
    expect(lines).toContain('The release window: could not be read (HTTP 502)')
    expect(lines).toContain('Score "Time follows the change" of infra: its pipeline reports no scope')
    expect(lines.some(l => l.includes('Red runs'))).toBe(false)
  })

  // Guards "domains exist only for a repository with an ownership table": the API's note is repeated, and the missing stages named.
  it('names the domains and the stages of a repository that has neither', () => {
    const lines = notMeasured({ repo: 'plain', runs: [], graph: { ...graph(), measured: false, note: 'this repository has no config/domain-ownership.json: its domains are not measured' }, stages: false, unread: [] })
    expect(lines).toContain('Domains of plain: this repository has no config/domain-ownership.json: its domains are not measured')
    expect(lines).toContain('Land, STG and PROD of plain: the delivery timings are read for bifrost-platform only')
  })
})
