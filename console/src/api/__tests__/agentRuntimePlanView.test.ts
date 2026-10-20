import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import goView from '../__fixtures__/agentrt-plan-view.go.json'
import imported from '../__fixtures__/agentrt-plan-imported.testdb.json'
import moved from '../__fixtures__/agentrt-plan-moved.testdb.json'
import { parseRuntimeProgress } from '../agentRuntime'
import { PLAN_ROOT, fetchImportRequest, fetchRuntimePlan, parsePlanRequest, runtimeRound } from '../agentRuntimePlan'
import { fromPlanWithRequest, importHead, requestOf, type PlanView } from '../agentRuntimePlanView'

// The fixtures are the platform's own answers on a test database: the round-2 ledger fixture
// (planimport/testdata/ledger.md) imported by store.ImportPlan, then GET /plan?root=R2, GET
// /events?stream=plan:R2, GET /artifacts/<request> and GET /progress through agentrtapi; "moved" after a
// step was added and another dropped. agentrt-plan-view.go.json holds planimport.FromPlanWithRequest's
// outputs on those answers and on edited copies of them (inputs), with and without the request.

const request = parsePlanRequest(imported.request, PLAN_ROOT)
const docs: Record<string, unknown> = { imported: imported.plan, moved: moved.plan, ...goView.inputs }
type GoView = { items: { key: string; name: string; steps: { id: string; t: string; state: string; owner: boolean; star: boolean; when?: string }[] | null }[] | null }
/** Go writes no items as null and an empty when not at all. */
function normal(v: GoView): PlanView {
  return { items: (v.items ?? []).map(i => ({ key: i.key, name: i.name, steps: (i.steps ?? []).map(s => ({ ...s, when: s.when ?? '' })) })) }
}
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => vi.unstubAllGlobals())

describe('the plan view, as the Go view gives it', () => {
  it.each(goView.cases.map(c => [c.name, c] as const))('%s', (_, c) => {
    const run = () => fromPlanWithRequest(docs[c.plan], c.root, c.request ? request : null)
    if ('error' in c && c.error) {
      // A value Go cannot decode is refused before anything is checked; the reasons after that are Go's own.
      if (c.error.startsWith('the plan does not read: ')) expect(run).toThrow(/^the plan does not read: /)
      else expect(run).toThrow(new Error(c.error))
      return
    }
    expect(run()).toEqual(normal((c as { view: GoView }).view))
  })

  it('covers every branch of when: the ledger\'s date for an imported state, the state event\'s time otherwise', () => {
    const cases = Object.fromEntries(goView.cases.map(c => [c.name, c]))
    expect(cases['mutated, with the request'].view?.items).toBeTruthy()
    const steps = normal(cases['imported, with the request'].view as GoView).items.flatMap(i => i.steps)
    expect(steps.find(s => s.id === '2.1-1')).toMatchObject({ state: 'done', when: '2026-10-14 01:09' })
    expect(steps.find(s => s.id === '2.1-3')).toMatchObject({ state: 'doing', when: '2026-10-14 11:06' })
    const after = normal(cases['moved on, with the request'].view as GoView).items.flatMap(i => i.steps)
    expect(after.find(s => s.id === '2.3-2')?.state).toBe('dropped')
    expect(after.find(s => s.id === '2.3-2')?.when).toMatch(/^2026-10-19T/)
  })
})

describe('the import head and its request', () => {
  it('finds the request the plan stream\'s legacy_imported event names', () => {
    const head = importHead(imported.events)
    expect(head).toEqual({ artifact: requestOf(imported.events[0].body_json.source ?? ''), next: 0 })
    expect(head.artifact).toMatch(/^sha256:[0-9a-f]{64}$/)
    // A page without it: read on after its last seq; an empty page: never imported.
    const rest = imported.events.slice(1)
    expect(importHead(rest)).toEqual({ artifact: '', next: rest.at(-1)!.seq })
    expect(importHead([])).toEqual({ artifact: '', next: 0 })
    // Only a legacy_imported event is the head, whatever else names a request before it.
    const other = { ...imported.events[1], body_json: { source: `plan-import sha256:${'0'.repeat(64)}` } }
    expect(importHead([other, imported.events[0]])).toEqual({ artifact: head.artifact, next: 0 })
    expect(() => importHead({ events: [] })).toThrow(/^the events do not read/)
    expect(requestOf('plan-import sha256:abc')).toBeNull()
    expect(requestOf(`other ${head.artifact}`)).toBeNull()
  })

  it('reads the request of this root only', () => {
    expect(request.nodes.find(n => n.id === 'R2:2.1-1')?.evidence).toMatchObject({ marked_at: '2026-10-14 01:09' })
    expect(() => parsePlanRequest(imported.request, 'R3')).toThrow('Plan import request: invalid artifact.')
    expect(() => parsePlanRequest({ ...imported.request, format: 'other' }, PLAN_ROOT)).toThrow('Plan import request: invalid artifact.')
  })

  it('pages the plan stream for the head, then reads the request it names once', async () => {
    const head = importHead(imported.events).artifact
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('after_seq=0')) return json(imported.events.slice(1, 3))
      if (url.includes(`after_seq=${imported.events[2].seq}`)) return json([imported.events[0]])
      if (url.endsWith(`/artifacts/${encodeURIComponent(head)}`)) return new Response(JSON.stringify(imported.request))
      throw new Error(`Unexpected URL ${url}`)
    })
    vi.stubGlobal('fetch', fetch)
    expect(await fetchImportRequest()).toEqual(request)
    expect(fetch.mock.calls.map(c => String(c[0]))).toEqual([
      '/api/v1/agentrt/events?stream=plan%3AR2&after_seq=0', `/api/v1/agentrt/events?stream=plan%3AR2&after_seq=${imported.events[2].seq}`,
      `/api/v1/agentrt/artifacts/${encodeURIComponent(head)}`])
    vi.stubGlobal('fetch', vi.fn(async () => json([])))
    expect(await fetchImportRequest()).toBeNull()
  })
})

describe('GET /plan?root=', () => {
  it('is not found only on the route\'s own 404', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(imported.absent.body, imported.absent.status)))
    expect(await fetchRuntimePlan()).toEqual({ found: false })
    vi.stubGlobal('fetch', vi.fn(async () => new Response('404 page not found', { status: 404 })))
    await expect(fetchRuntimePlan()).rejects.toThrow('Plan: HTTP 404')
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'unavailable', message: 'the runtime is not connected' }, 503)))
    await expect(fetchRuntimePlan()).rejects.toThrow('Plan: unavailable')
    vi.stubGlobal('fetch', vi.fn(async () => json(imported.plan)))
    expect(await fetchRuntimePlan()).toEqual({ found: true, doc: imported.plan })
  })
})

describe('the round from the runtime', () => {
  it.each([['imported', imported], ['moved on (a step added, one dropped)', moved]] as const)('counts each item as GET /progress does: %s', (_, f) => {
    const progress = parseRuntimeProgress(f.progress)
    const round = runtimeRound(fromPlanWithRequest(f.plan, PLAN_ROOT, request), progress.plan, PLAN_ROOT, request)
    const counted = f.progress.plan[0]
    expect(round).toMatchObject({ n: 2, source: 'runtime' })
    expect(round.items.map(i => [i.key, i.done, i.total, i.state])).toEqual(counted.items.map(i => [i.label, i.count.done, i.count.total,
      ({ planned: 'todo', active: 'doing', done: 'done' } as Record<string, string>)[i.count.state]]))
    expect(round.items.reduce((n, i) => n + (i.total ?? 0), 0)).toBe(counted.count.total)
    expect(round.items.reduce((n, i) => n + (i.done ?? 0), 0)).toBe(counted.count.done)
  })

  it('a dropped step keeps its place but leaves the total; each imported step keeps its ledger line', () => {
    const round = runtimeRound(fromPlanWithRequest(moved.plan, PLAN_ROOT, request), parseRuntimeProgress(moved.progress).plan, PLAN_ROOT, request)
    const item = round.items.find(i => i.key === '2.3')!
    expect(item.steps.map(s => s.state)).toEqual(['done', 'dropped'])
    expect([item.done, item.total, item.state]).toEqual([1, 1, 'done'])
    expect(round.items.find(i => i.key === '2.1')!.steps.at(-1)).toMatchObject({ id: '2.1-6', t: 'A step added after the import', line: undefined })
    expect(item.steps[0].line).toContain('手机上问参谋长一句')
  })

  it('is not shown when the counts do not list the plan\'s items', () => {
    const view = fromPlanWithRequest(imported.plan, PLAN_ROOT, request)
    const plan = parseRuntimeProgress(imported.progress).plan
    expect(() => runtimeRound(view, [], PLAN_ROOT, request)).toThrow("The runtime's progress does not count R2.")
    const fewer = [{ ...imported.progress.plan[0], items: imported.progress.plan[0].items.slice(1) }]
    expect(() => runtimeRound(view, fewer, PLAN_ROOT, request)).toThrow("The plan and the runtime's counts of R2 list different items; read again.")
    expect(runtimeRound(view, plan, PLAN_ROOT, null).items[0].steps[0].line).toBeUndefined()
  })
})
