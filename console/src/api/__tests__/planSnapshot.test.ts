import { describe, expect, it, vi, afterEach } from 'vitest'
import fixture from '../__fixtures__/plan-view-handler.json'
import legacy from '../__fixtures__/plan-view-handler-legacy.json'
import page2 from '../__fixtures__/plan-view-handler-page2.json'
import signoff from '../__fixtures__/plan-view-signoff.generated.json'
import { fetchPlanSnapshot, parsePlanSnapshot } from '../planSnapshot'

vi.mock('../client', () => ({ authedFetch: vi.fn() }))
const { authedFetch } = await import('../client')
afterEach(() => vi.clearAllMocks())
describe('generated sign-off readiness contract', () => {
  it('accepts Not started only in owner_later, with an empty candidate list', () => {
    const view = parsePlanSnapshot(signoff, 'R4')
    expect(view.owner_waiting).toHaveLength(1)
    expect(view.owner_waiting[0]).toMatchObject({ readiness: 'Ready' })
    expect(view.owner_later).toHaveLength(2)
    expect(view.owner_later.find(row => row.readiness === 'Not started')).toMatchObject({
      plan_node_id: 'R4:4.3-1', candidate_evidence: [],
      reason: 'Work for this step has not started; no candidate evidence yet',
    })
  })
  it.each(['Not started', 'Blocked', 'Unknown'])('rejects %s in owner_waiting', readiness => {
    const value = structuredClone(signoff)
    value.owner_waiting[0].readiness = readiness
    expect(() => parsePlanSnapshot(value, 'R4')).toThrow(/readiness/)
  })
  it.each(['Ready', 'Unknown', 'Not Started'])('rejects %s in a later sign-off', readiness => {
    const value = structuredClone(signoff)
    value.owner_later[0].readiness = readiness
    expect(() => parsePlanSnapshot(value, 'R4')).toThrow(/readiness/)
  })
})
describe('PV1 snapshot contract captured from the HTTP handler', () => {
  it('reads the adopted plan, external work and stable history boundary', () => {
    const view = parsePlanSnapshot(fixture, 'R4')
    expect(view.items).toHaveLength(12)
    expect(view.mainline.current?.plan_node_id).toBe('R4:4.1')
    expect(view.working).toHaveLength(1)
    expect(view.owner_later).toHaveLength(1)
    expect(view.owner_waiting).toEqual([])
    expect(view.freshness.owner_waiting).toMatch(/Partial/)
    expect(parsePlanSnapshot(page2, 'R4').recent_as_of).toBe(view.recent_as_of)
  })
  it('reads legacy nodes and real runtime attempts without manufacturing classification', () => {
    const view = parsePlanSnapshot(legacy, 'R2')
    expect(view.plan_version).toBe(0)
    expect(view.mainline.current).toBeNull()
    expect(view.working).toHaveLength(1)
    expect(view.nodes.every(n => n.lane_kind === 'Unclassified')).toBe(true)
  })
  it('reads what a wait names: dependencies, the landing slot holder, the executor', () => {
    const value = structuredClone(legacy)
    const { attempt: _attempt, external_work: _external, ...base } = value.tasks[0]
    const wait = (task_id: string, waiting: Record<string, unknown>) => ({ ...base, task_id, waiting })
    const tasks = [
      wait('t_01M6SZ4Z5SJKN0H2DN8KMX1BV1', { on: 'dependencies', reason: 'waits for 2 dependencies to be accepted', since: '2026-11-07T08:00:00Z',
        depends_on: [{ task_id: 't_01M6SZ4Z5SJKN0H2DN8KMX1BV2', title: 'store', state: 'running' }, { task_id: 't_01M6SZ4Z5SJKN0H2DN8KMX1BV3', title: 'console', state: 'ready' }] }),
      wait('t_01M6SZ4Z5SJKN0H2DN8KMX1BV4', { on: 'landing', reason: 'landing land:2 is in phase claimed', since: '2026-11-07T08:00:00Z',
        slot_holder: { operation: 'land:3', task_id: 't_01M6SZ4Z5SJKN0H2DN8KMX1BV5', title: 'other root', phase: 'main_intent', since: '2026-11-07T08:01:00Z' } }),
      wait('t_01M6SZ4Z5SJKN0H2DN8KMX1BV6', { on: 'queued', reason: 'waits for a host', executor: { executor: 'external:coordinator' } }),
      wait('t_01M6SZ4Z5SJKN0H2DN8KMX1BV7', { on: 'host', reason: 'the host stopped answering', executor: { executor: 'exec:codex', host_id: 'h1' } }),
      wait('t_01M6SZ4Z5SJKN0H2DN8KMX1BV8', { on: 'queued', reason: 'waits for a host; the store names no executor or pool it requires' }),
    ]
    const view = parsePlanSnapshot({ ...value, tasks: [...value.tasks, ...tasks] }, 'R2')
    const waits = view.tasks.slice(-5).map(t => t.waiting)
    expect(waits[0]?.depends_on?.map(d => d.title)).toEqual(['store', 'console'])
    expect(waits[1]?.slot_holder).toMatchObject({ operation: 'land:3', phase: 'main_intent', title: 'other root' })
    expect(waits[2]?.executor).toEqual({ executor: 'external:coordinator' })
    expect(waits[3]?.executor).toEqual({ executor: 'exec:codex', host_id: 'h1' })
    expect(waits[4]?.executor).toBeUndefined()
    for (const change of [
      (w: Record<string, unknown>) => { w.slot_holder = { ...(w.slot_holder as object), fabricated: true } },
      (w: Record<string, unknown>) => { w.depends_on = [{ task_id: 't_01M6SZ4Z5SJKN0H2DN8KMX1BV2' }] },
    ]) {
      const bad = structuredClone(tasks)
      change(bad[0].waiting)
      change(bad[1].waiting)
      expect(() => parsePlanSnapshot({ ...value, tasks: [...value.tasks, ...bad] }, 'R2')).toThrow(/Plan view/)
    }
  })
  it.each([
    ['version', (x: Record<string, unknown>) => { x.schema = 'agentrt.plan_view_response/v2' }],
    ['external version', (x: Record<string, unknown>) => { x.working = [{ ...fixture.working[0], v: 2 }] }],
    ['missing list', (x: Record<string, unknown>) => { delete x.working }],
    ['null list', (x: Record<string, unknown>) => { x.owner_waiting = null }],
    ['prototype-named field', (x: Record<string, unknown>) => { Object.defineProperty(x, 'constructor', { value: 'unexpected', enumerable: true }) }],
    ['unknown field', (x: Record<string, unknown>) => { x.new_field = true }],
    ['malformed count', (x: Record<string, unknown>) => { x.coverage = { ...fixture.coverage, unregistered_work: '0' } }],
    ['wrong time', (x: Record<string, unknown>) => { x.as_of = 'not a date' }],
    ['unknown nested shape', (x: Record<string, unknown>) => { x.mainline = { ...fixture.mainline, fabricated: true } }],
    ['missing attempt', (x: Record<string, unknown>) => { x.working = [{ ...legacy.tasks[0], plan_node_id: 'R4:4.1-1', attempt: undefined }] }],
    ['dangling current path', (x: Record<string, unknown>) => { x.next = [{ ...fixture.next[0], plan_node_id: 'absent' }] }],
  ])('rejects %s instead of inventing zero', (_name, change) => {
    const value = structuredClone(fixture) as Record<string, unknown>
    change(value)
    expect(() => parsePlanSnapshot(value, 'R4')).toThrow(/Plan view/)
  })
  it('refuses a response belonging to another root', () => expect(() => parsePlanSnapshot(fixture, 'R3')).toThrow())
  it('fetches one snapshot with an encoded root and cursor, forwards cancellation', async () => {
    vi.mocked(authedFetch).mockResolvedValue(new Response(JSON.stringify(fixture)))
    const signal = new AbortController().signal
    await fetchPlanSnapshot('R4', signal, 'cursor+/=')
    expect(authedFetch).toHaveBeenCalledWith('Plan view', '/api/v1/agentrt/plan-view?root=R4&recent_cursor=cursor%2B%2F%3D', { signal })
  })
})
