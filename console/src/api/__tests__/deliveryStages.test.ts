import { afterEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/agentrt-delivery.go.json'
import { commitStages, deliverySettled, fetchCommitDelivery, parseDelivery, type DeliveryRead } from '@/api/deliveryStages'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
afterEach(() => vi.unstubAllGlobals())

// The answers of store.ReadDelivery on the migrated database, as GET /delivery writes them, for commits external works
// handed in. The merge into main is not recorded, so On main is Unknown with Go's reason and the strip leaves it out.
const byName = (name: string) => wire.cases.find(c => c.name === name)!.response
const waiting = byName('external work: CI green, waiting for STG')
const failed = byName('external work: STG delivery failed')
const onProd = byName('external work: on PROD by its recorded runs')
const clone = <T,>(v: T): T => structuredClone(v)
const ok = (v: unknown): DeliveryRead => ({ state: 'ok', data: parseDelivery(v) })
const stageOf = (doc: typeof onProd, name: string) => doc.stages.find(s => s.stage === name)!

describe('GET /delivery, closed', () => {
  it("parses every Go answer as it is", () => {
    for (const c of wire.cases) expect(parseDelivery(c.response), c.name).toEqual(c.response)
  })

  it('refuses unknown keys, at the answer and on a stage', () => {
    expect(() => parseDelivery({ ...clone(onProd), extra: 1 })).toThrow(/at delivery$/)
    const stage = clone(onProd); Object.assign(stage.stages[4]!, { stg_at: stage.stages[4]!.left_at })
    expect(() => parseDelivery(stage)).toThrow(/stages\[4\]$/)
  })

  it('refuses a stage or state Go does not name, stages out of order or missing, and a bad time', () => {
    const name = clone(onProd); name.stages[4]!.stage = 'on_stg'
    expect(() => parseDelivery(name)).toThrow(/stage$/)
    const state = clone(onProd); state.stages[6]!.state = 'reached'
    expect(() => parseDelivery(state)).toThrow(/state$/)
    const order = clone(onProd); order.stages.reverse()
    expect(() => parseDelivery(order)).toThrow(/stages$/)
    const short = clone(onProd); short.stages.pop()
    expect(() => parseDelivery(short)).toThrow(/stages$/)
    const time = clone(onProd); time.stages[3]!.entered_at = 'soon'
    expect(() => parseDelivery(time)).toThrow(/entered_at$/)
    const missing = clone(onProd) as Record<string, unknown>; delete missing.warnings
    expect(() => parseDelivery(missing)).toThrow(/warnings$/)
  })
})

// A commit only an external work names (the main lane): GET /delivery?commit= answers from the pipeline runs the
// platform recorded of that commit. The merge into main is not recorded, so On main is Unknown with Go's reason.
describe("an external work's commit", () => {
  it('names its external work and no task, attempt or operation', () => {
    for (const doc of [waiting, failed, onProd]) {
      expect(doc).toMatchObject({ external_work_id: expect.stringMatching(/^ew_[0-9a-f]{64}$/), attempt_id: '', operation: '' })
      expect(parseDelivery(doc).warnings.join(' ')).toContain('exact commit')
    }
  })

  it('reads each stage from its own fact, with its time, and what is not recorded as Unknown and why', () => {
    expect(commitStages(ok(onProd))).toEqual([
      { stage: 'handed_in', state: 'reached', at: stageOf(onProd, 'handed_in').entered_at },
      { stage: 'ci', state: 'reached', at: stageOf(onProd, 'landing_ci').left_at },
      { stage: 'on_stg', state: 'reached', at: stageOf(onProd, 'stg').left_at },
      { stage: 'on_prod', state: 'reached', at: stageOf(onProd, 'on_prod').entered_at },
    ])
    expect(stageOf(onProd, 'on_main').reason).toMatch(/does not record/)
    expect(stageOf(onProd, 'owner_wait')).toMatchObject({ state: 'unknown', reason: expect.stringMatching(/Gitea/) })
    expect(commitStages(ok(waiting)).map(s => [s.stage, s.state])).toEqual([
      ['handed_in', 'reached'], ['ci', 'reached'], ['on_stg', 'not_reached'], ['on_prod', 'not_reached']])
  })

  it('says why a stage stopped, and is settled only once on PROD', () => {
    expect(commitStages(ok(failed))[2]).toEqual({ stage: 'on_stg', state: 'not_reached', reason: 'STG delivery failed: bifrost-deliver-platform-0' })
    expect([onProd, waiting, failed].map(doc => deliverySettled(ok(doc)))).toEqual([true, false, false])
    expect([deliverySettled(undefined), deliverySettled({ state: 'none' }), deliverySettled({ state: 'unknown', reason: 'x' })]).toEqual([false, false, false])
    // A commit nothing names, and an answer that could not be read.
    expect(commitStages({ state: 'none' }).map(s => s.state)).toEqual(Array(4).fill('not_reached'))
    expect(commitStages({ state: 'unknown', reason: 'Delivery: HTTP 503' })[1]).toEqual({ stage: 'ci', state: 'unknown', reason: 'Delivery: HTTP 503' })
  })

  it('keeps PROD waiting only when what PROD runs was read; else Unknown with why', () => {
    const waits = byName('external work: on STG, PROD does not run it yet')
    const unknown = byName('external work: on STG, what PROD runs is unknown')
    expect(commitStages(ok(waits)).slice(2).map(s => [s.stage, s.state])).toEqual([['on_stg', 'reached'], ['on_prod', 'not_reached']])
    expect(stageOf(waits, 'prod_release')).toMatchObject({ state: 'current', waits_on: 'PROD release' })
    expect(commitStages(ok(unknown)).at(-1)).toEqual({ stage: 'on_prod', state: 'unknown', reason: stageOf(unknown, 'on_prod').reason })
    expect(stageOf(unknown, 'prod_release')).toMatchObject({ state: 'unknown', reason: expect.stringMatching(/^what PROD runs now is unknown: /) })
    expect([waits, unknown].map(doc => deliverySettled(ok(doc)))).toEqual([false, false])
  })

  it('is read by its commit; 404 is none', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(onProd), { status: 200 }))
    vi.stubGlobal('fetch', fetcher)
    expect(await fetchCommitDelivery(onProd.commit)).toEqual({ state: 'ok', data: onProd })
    expect(String((fetcher.mock.calls[0] as unknown[])[0])).toBe(`/api/v1/agentrt/delivery?commit=${onProd.commit}`)
    fetcher.mockImplementation(async () => new Response(JSON.stringify({ error: 'not_found', message: 'change not found' }), { status: 404 }))
    expect(await fetchCommitDelivery(onProd.commit)).toEqual({ state: 'none' })
  })
})
