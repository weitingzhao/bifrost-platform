import { describe, expect, it } from 'vitest'
import collector from '../__fixtures__/plan-view-collector.generated.json'
import handler from '../__fixtures__/plan-view-handler.json'
import signoff from '../__fixtures__/plan-view-signoff.generated.json'
import { parsePlanSnapshot, type ExternalWork } from '../planSnapshot'

// R5:5.5-12 §7, mixed versions. plan-view-collector.generated.json is what Go's
// PlanView writes for a collector's work in every place a row goes (running:
// external_work and working; submitted today: finished_today; beside a live
// attempt: tasks[].external_work and working[].external_work) and an MCP
// registrant's (store TestPlanViewCollectorWorkFixture, byte for byte). The
// P-c1 reader reads it, and reads it again with what P-c2 adds
// (coverage.sessions); an answer without any of the new fields reads as it
// always did.

type Json = Record<string, unknown>
const identityKeys = ['host', 'vendor', 'session', 'epoch', 'sidebar'] as const
const sessions = { linked: 1, unlinked: 2, refused: 0, unsupported: 1, unknown: 0, scope: 'Sessions heard from in the last 72 hours, by host, vendor, session and epoch' }

function clone(): Json { return structuredClone(collector) as unknown as Json }
function rows(value: Json, list: 'external_work' | 'working' | 'finished_today' | 'tasks'): Json[] { return value[list] as Json[] }
/** Every external work row wherever it sits: the top list, working and finished today, and each task's history (in tasks and in working). */
function everyRow(value: Json): Json[] {
  const out = [...rows(value, 'external_work')]
  for (const list of ['working', 'finished_today', 'tasks'] as const) for (const row of rows(value, list)) {
    if ('external_work_id' in row) out.push(row)
    else out.push(...((row.external_work as Json[] | undefined) ?? []))
  }
  return out
}
function withoutIdentity(value: Json): Json {
  for (const row of everyRow(value)) for (const k of identityKeys) delete row[k]
  return value
}
/** The task beside a live attempt, as working holds it. */
const workingTask = (value: Json) => rows(value, 'working').find(r => !('external_work_id' in r))!
const collectorRow = (list: ExternalWork[]) => list.find(r => r.registrant === 'thread:collector@h1')

describe('P-c1 reader, P-c2 answer', () => {
  it('reads a collector row with its host and identity, the MCP row without', () => {
    const view = parsePlanSnapshot(collector, 'R4')
    expect(collectorRow(view.external_work)).toMatchObject({
      host: 'h1', vendor: 'claude', session: '0b8e2f4a-1111-2222-3333-444455556666', epoch: 1,
      sidebar: 'local_0b8e2f4a-aaaa-bbbb-cccc-ddddeeeeffff', source_run_key: 'claude:0b8e2f4a-1111-2222-3333-444455556666:1', status: 'Running',
    })
    const mcp = view.external_work.find(r => r.registrant === 'thread:local_mcp')!
    for (const k of identityKeys) expect(mcp[k]).toBeUndefined()
    expect(view.working).toHaveLength(3)
    expect(view.coverage.sessions).toBeUndefined()
  })
  it('reads the identity in every place a row goes', () => {
    const view = parsePlanSnapshot(collector, 'R4') as unknown as Json
    const rowsOf = everyRow(view)
    expect(rowsOf).toHaveLength(9)
    for (const row of rowsOf) {
      const mine = row.registrant === 'thread:collector@h1'
      for (const k of ['host', 'vendor', 'session', 'epoch'] as const) expect(row[k] !== undefined).toBe(mine)
    }
    const finished = rows(view, 'finished_today')[0]
    expect(finished).toMatchObject({ host: 'h1', vendor: 'claude', session: '7c1d9e3b-1111-2222-3333-444455556666', epoch: 2, status: 'Submitted' })
    const history = { host: 'h1', vendor: 'claude', session: '9e5f0a1c-1111-2222-3333-444455556666', epoch: 1, task_id: 't_fixture_live' }
    expect((rows(view, 'tasks')[0].external_work as Json[])[0]).toMatchObject(history)
    expect((workingTask(view).external_work as Json[])[0]).toMatchObject(history)
  })
  it('reads the session counts P-c2 adds to coverage', () => {
    const value = clone()
    ;(value.coverage as Json).sessions = sessions
    expect(parsePlanSnapshot(value, 'R4').coverage.sessions).toEqual(sessions)
  })
})

describe('P-c2 reader, the new fields left out', () => {
  it('reads the answers from before as before', () => {
    for (const [value, root] of [[handler, 'R4'], [signoff, 'R4']] as const) {
      const view = parsePlanSnapshot(value, root)
      expect(view.coverage.sessions).toBeUndefined()
      for (const row of view.external_work) for (const k of identityKeys) expect(row[k]).toBeUndefined()
    }
  })
  it('reads the collector answer without its new fields as the same answer less them', () => {
    const full = parsePlanSnapshot(collector, 'R4')
    const bare = parsePlanSnapshot(withoutIdentity(clone()), 'R4')
    const expected = structuredClone(full) as unknown as Json
    withoutIdentity(expected)
    expect(bare).toEqual(expected)
  })
})

describe('the new fields are closed', () => {
  it.each([
    ['a collector row without its vendor', (v: Json) => { delete rows(v, 'external_work')[0].vendor }],
    ['a collector row without its host', (v: Json) => { delete rows(v, 'external_work')[0].host }],
    ['a working row without its epoch', (v: Json) => { delete rows(v, 'working')[0].epoch }],
    ['a finished row without its session', (v: Json) => { delete rows(v, 'finished_today')[0].session }],
    ['a task history row without its vendor', (v: Json) => { delete (rows(v, 'tasks')[0].external_work as Json[])[0].vendor }],
    ['a working task history row without its vendor', (v: Json) => { delete (workingTask(v).external_work as Json[])[0].vendor }],
    ['a sidebar on a row with no identity', (v: Json) => { rows(v, 'external_work')[1].sidebar = 'local_x' }],
    ['a host alone', (v: Json) => { rows(v, 'external_work')[1].host = 'h2' }],
    ['a vendor not in the set', (v: Json) => { rows(v, 'external_work')[0].vendor = 'gemini' }],
    ['epoch 0', (v: Json) => { rows(v, 'external_work')[0].epoch = 0 }],
    ['a fractional epoch', (v: Json) => { rows(v, 'external_work')[0].epoch = 1.5 }],
    ['a numeric session', (v: Json) => { rows(v, 'external_work')[0].session = 7 }],
    ['a negative session count', (v: Json) => { (v.coverage as Json).sessions = { ...sessions, unlinked: -1 } }],
    ['a session count missing', (v: Json) => { const { refused: _, ...rest } = sessions; (v.coverage as Json).sessions = rest }],
    ['an unknown session count', (v: Json) => { (v.coverage as Json).sessions = { ...sessions, linked_elsewhere: 1 } }],
    ['a null sessions', (v: Json) => { (v.coverage as Json).sessions = null }],
  ])('rejects %s', (_name, change) => {
    const value = clone()
    change(value)
    expect(() => parsePlanSnapshot(value, 'R4')).toThrow(/Plan view/)
  })
})
