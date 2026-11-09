import { afterEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/agentrt-delivery.go.json'
import timelineWire from '@/api/__fixtures__/agentrt-plan-timeline.go.json'
import { fetchTaskDelivery, parseDelivery, taskStages, type DeliveryRead } from '@/api/deliveryStages'
import { parsePlanTimeline } from '@/api/planTimeline'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
afterEach(() => vi.unstubAllGlobals())

// The answers of store.ReadDelivery on the migrated database, as GET /delivery writes them, for a round where one
// task went to PROD and another reached main (store/console_delivery_fixture_test.go generates the fixture).
const byName = (name: string) => wire.cases.find(c => c.name === name)!.response
const prodSeen = byName('on PROD by deploy_seen, cycles read and empty')
const prodGate = byName("on PROD by the release cycle's gate")
const prodUnknown = byName('PROD unknown: the release cycles could not be read')
const onMain = byName('on main, waiting for STG')
const timeline = parsePlanTimeline(timelineWire.cases.find(c => c.name === 'round')!.response, 'R2')
const clone = <T,>(v: T): T => structuredClone(v)
const ok = (v: unknown): DeliveryRead => ({ state: 'ok', data: parseDelivery(v) })
const stageOf = (doc: typeof prodSeen, name: string) => doc.stages.find(s => s.stage === name)!

describe('GET /delivery, closed', () => {
  it("parses every Go answer as it is", () => {
    for (const c of wire.cases) expect(parseDelivery(c.response), c.name).toEqual(c.response)
  })

  it('refuses unknown keys, at the answer and on a stage', () => {
    expect(() => parseDelivery({ ...clone(prodSeen), extra: 1 })).toThrow(/at delivery$/)
    const stage = clone(prodSeen); Object.assign(stage.stages[4]!, { stg_at: stage.stages[4]!.left_at })
    expect(() => parseDelivery(stage)).toThrow(/stages\[4\]$/)
  })

  it('refuses a stage or state Go does not name, stages out of order or missing, and a bad time', () => {
    const name = clone(prodSeen); name.stages[4]!.stage = 'on_stg'
    expect(() => parseDelivery(name)).toThrow(/stage$/)
    const state = clone(prodSeen); state.stages[6]!.state = 'reached'
    expect(() => parseDelivery(state)).toThrow(/state$/)
    const order = clone(prodSeen); order.stages.reverse()
    expect(() => parseDelivery(order)).toThrow(/stages$/)
    const short = clone(prodSeen); short.stages.pop()
    expect(() => parseDelivery(short)).toThrow(/stages$/)
    const time = clone(prodSeen); time.stages[3]!.entered_at = 'soon'
    expect(() => parseDelivery(time)).toThrow(/entered_at$/)
    const missing = clone(prodSeen) as Record<string, unknown>; delete missing.warnings
    expect(() => parseDelivery(missing)).toThrow(/warnings$/)
  })
})

describe("a task's stages", () => {
  it('uses current release evidence and keeps unsettled workloads Unknown', () => {
    const current = byName('on PROD by current release and live workloads')
    const unsettled = byName('PROD unknown: current workloads unsettled')
    expect(taskStages(current.task_id, ok(current)).at(-1)).toEqual({stage: 'on_prod', state: 'reached', at: stageOf(current, 'on_prod').entered_at})
    expect(taskStages(unsettled.task_id, ok(unsettled)).at(-1)).toMatchObject({stage: 'on_prod', state: 'unknown'})
  })

  it('accepts verified STG coverage without losing the separate PROD arrival', async () => {
    const covered = clone(prodSeen)
    stageOf(covered, 'stg').source = 'stg_coverage'
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(covered), { status: 200 })))
    const read = await fetchTaskDelivery(covered.task_id)
    expect(read.state).toBe('ok')
    expect(taskStages(covered.task_id, read).slice(4)).toEqual([
      { stage: 'on_stg', state: 'reached', at: stageOf(covered, 'stg').left_at },
      { stage: 'on_prod', state: 'reached', at: stageOf(covered, 'on_prod').entered_at },
    ])
    stageOf(covered, 'stg').source = 'unverified_coverage'
    expect(() => parseDelivery(covered)).toThrow(/stages\[4\].source$/)
  })

  it('reads STG and PROD from their own facts, each with its time', () => {
    const task = prodSeen.task_id
    expect(taskStages(task, ok(prodSeen), timeline)).toEqual([
      { stage: 'planned', state: 'reached', at: timeline.steps[3]!.events.find(e => e.kind === 'created')!.at },
      { stage: 'handed_in', state: 'reached', at: stageOf(prodSeen, 'handed_in').entered_at },
      { stage: 'verified', state: 'reached', at: timeline.steps[3]!.events.find(e => e.kind === 'accepted' && e.task_id === task)!.at },
      { stage: 'on_main', state: 'reached', at: stageOf(prodSeen, 'on_main').entered_at },
      { stage: 'on_stg', state: 'reached', at: stageOf(prodSeen, 'stg').left_at },
      { stage: 'on_prod', state: 'reached', at: stageOf(prodSeen, 'on_prod').entered_at },
    ])
    // STG verified before PROD saw the revision: two times, not one.
    expect(stageOf(prodSeen, 'stg').left_at).not.toBe(stageOf(prodSeen, 'on_prod').entered_at)
    // By the release gate, PROD is when the gate passed.
    expect(taskStages(task, ok(prodGate)).at(-1)).toEqual({ stage: 'on_prod', state: 'reached', at: stageOf(prodGate, 'on_prod').entered_at })
  })

  it('leaves a stage not reached empty and an unreadable one Unknown', () => {
    expect(taskStages(onMain.task_id, ok(onMain), timeline).map(s => [s.stage, s.state])).toEqual([
      ['planned', 'reached'], ['handed_in', 'reached'], ['verified', 'reached'], ['on_main', 'reached'], ['on_stg', 'not_reached'], ['on_prod', 'not_reached']])
    expect(taskStages(prodUnknown.task_id, ok(prodUnknown)).at(-1)).toEqual({ stage: 'on_prod', state: 'unknown', reason: 'no release cycles given' })
    // Reached with no time is Unknown, never a time from elsewhere.
    const timeless = clone(prodSeen); delete (timeless.stages[4] as { left_at?: string }).left_at
    expect(taskStages(prodSeen.task_id, ok(timeless))[4]).toEqual({ stage: 'on_stg', state: 'unknown', reason: 'stg is done with no left_at' })
    // A task that handed nothing in, an answer that could not be read, and one of another task.
    expect(taskStages(prodSeen.task_id, { state: 'none' }).slice(1).map(s => s.state)).toEqual(['not_reached', 'unknown', 'not_reached', 'not_reached', 'not_reached'])
    expect(taskStages(prodSeen.task_id, { state: 'unknown', reason: 'Delivery: HTTP 503' })[1]).toEqual({ stage: 'handed_in', state: 'unknown', reason: 'Delivery: HTTP 503' })
    expect(taskStages(onMain.task_id, ok(prodSeen))[3]).toMatchObject({ stage: 'on_main', state: 'unknown' })
    // Without the plan timeline, planned and verified are Unknown.
    expect(taskStages(prodSeen.task_id, ok(prodSeen)).filter(s => s.state === 'unknown').map(s => s.stage)).toEqual(['planned', 'verified'])
  })

  it('reads a task by its id; 404 is no change handed in, another failure throws', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(prodSeen), { status: 200 }))
    vi.stubGlobal('fetch', fetcher)
    expect(await fetchTaskDelivery(prodSeen.task_id)).toEqual({ state: 'ok', data: prodSeen })
    expect(String((fetcher.mock.calls[0] as unknown[])[0])).toBe(`/api/v1/agentrt/delivery?task=${prodSeen.task_id}`)
    fetcher.mockImplementation(async () => new Response(JSON.stringify({ error: 'not_found', message: 'change not found' }), { status: 404 }))
    expect(await fetchTaskDelivery(prodSeen.task_id)).toEqual({ state: 'none' })
    fetcher.mockImplementation(async () => new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 }))
    await expect(fetchTaskDelivery(prodSeen.task_id)).rejects.toThrow(/Delivery: unavailable/)
  })
})
