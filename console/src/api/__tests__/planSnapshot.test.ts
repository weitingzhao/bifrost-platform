import { describe, expect, it, vi, afterEach } from 'vitest'
import fixture from '../__fixtures__/plan-view-handler.json'
import legacy from '../__fixtures__/plan-view-handler-legacy.json'
import page2 from '../__fixtures__/plan-view-handler-page2.json'
import { fetchPlanSnapshot, parsePlanSnapshot } from '../planSnapshot'

vi.mock('../client', () => ({ authedFetch: vi.fn() }))
const { authedFetch } = await import('../client')
afterEach(() => vi.clearAllMocks())
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
