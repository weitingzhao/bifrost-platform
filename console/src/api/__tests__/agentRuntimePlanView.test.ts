import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import goView from '../__fixtures__/agentrt-plan-view.go.json'
import imported from '../__fixtures__/agentrt-plan-imported.testdb.json'
import moved from '../__fixtures__/agentrt-plan-moved.testdb.json'
import rootOnly from '../__fixtures__/agentrt-plan-root-only.testdb.json'
import { STREAM_PAGE, planStream, streamPage } from '@/test/agentrtPlanStream'
import { parseRuntimeProgress } from '../agentRuntime'
import { IMPORT_HEAD_PAGES, PLAN_ROOT, fetchImportRequest, fetchRuntimePlan, parsePlanRequest, runtimeRound } from '../agentRuntimePlan'
import { fromPlanWithRequest, importHead, requestOf, type PlanView } from '../agentRuntimePlanView'

// The fixtures are the platform's own answers on a test database: the round-2 ledger fixture
// (planimport/testdata/ledger.md) imported by store.ImportPlan, then GET /plan?root=R2, GET
// /events?stream=plan:R2, GET /artifacts/<request> and GET /progress through agentrtapi; "moved" after a
// step was added and another dropped; "root-only" a round R2 made by store.CreatePlanNode alone (no
// import, no items). agentrt-plan-view.go.json holds planimport.FromPlanWithRequest's outputs on those
// answers and on edited copies of them (inputs), with and without the request.
//
// Regenerating them (review 416 C3): a temporary _test.go in api/internal/agentrt/agentrtapi (package
// agentrtapi_test, so it has seeded, server, get, ledger and importHead) run once with AGENTRT_TEST_PG
// set, then deleted. It (1) reads GET plan?root=R2 before anything is imported (the 404, "absent");
// (2) store.ImportPlan of ledger(t, fixtureLedger), then writes plan?root=R2, events?stream=plan:R2&
// after_seq=0, artifacts/<importHead> and progress ("imported"); (3) ProposePlanChange add_node of a step
// under R2:2.1 (applied at once) and drop_step R2:2.3-2 (asked, DecidePlanChanges with Apply), then plan
// and progress ("moved"); (4) in a fresh database, CreatePlanNode of round R2 alone, then plan, events
// and progress ("root-only"); (5) runs FromPlanWithRequest on each case of GO_CASES below, the edited
// inputs being the imported answer with the edits each case names (for "mutated": the rule, cause, state
// and event changes of steps 2.0-1 to 2.2b-6 that its view shows), and writes the cases and the inputs.
// A Go change to the view's rule or refusals shows here only once they are regenerated; a case added or
// lost on either side fails "holds every case" below.
const GO_CASES = ['imported, with the request', 'imported, no request', 'moved on, with the request', 'moved on, no request',
  'mutated, with the request', 'another root', 'listed-twice', 'step-before-item', 'wrong-id', 'no-root', 'bad-type', 'bad-kind',
  'step-no-state', 'item-under-item', 'root only, no import head']

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
  it('holds every case the Go view was run on, and only those', () => {
    expect(goView.cases.map(c => c.name)).toEqual(GO_CASES)
    expect(goView.cases.filter(c => c.root === PLAN_ROOT).every(c => docs[c.plan] !== undefined)).toBe(true)
  })

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
    // A seq is a cursor: one that is not a safe integer is refused, not read inexactly (review 416 C1).
    const note = { ...imported.events[1], event_type: 'note' }
    expect(() => importHead([{ ...note, seq: 1e30 }])).toThrow('the events do not read: events[0].seq is not a safe integer')
    expect(() => importHead([{ ...note, seq: 2 ** 53 }])).toThrow(/is not a safe integer/)
    expect(importHead([{ ...note, seq: 2 ** 53 - 1 }])).toEqual({ artifact: '', next: 2 ** 53 - 1 })
    expect(requestOf('plan-import sha256:abc')).toBeNull()
    expect(requestOf(`other ${head.artifact}`)).toBeNull()
  })

  it('reads the request of this root only', () => {
    expect(request.nodes.find(n => n.id === 'R2:2.1-1')?.evidence).toMatchObject({ marked_at: '2026-10-14 01:09' })
    expect(() => parsePlanRequest(imported.request, 'R3')).toThrow('Plan import request: invalid artifact.')
    expect(() => parsePlanRequest({ ...imported.request, format: 'other' }, PLAN_ROOT)).toThrow('Plan import request: invalid artifact.')
  })

  // The stream form of GET /events as the handler pages it: a bare array, ascending by seq, seq above
  // after_seq, at most 500 a page (review 416 B2).
  const eventsUrl = (after: number) => `/api/v1/agentrt/events?stream=plan%3AR2&after_seq=${after}`
  function serve(page: (url: string) => unknown[]) {
    const head = importHead(imported.events).artifact
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith('/api/v1/agentrt/events?')) return json(page(url))
      if (url === `/api/v1/agentrt/artifacts/${encodeURIComponent(head)}`) return new Response(JSON.stringify(imported.request))
      throw new Error(`Unexpected URL ${url}`)
    })
    vi.stubGlobal('fetch', fetch)
    return () => fetch.mock.calls.map(c => String(c[0]))
  }

  it('reads on past 500 earlier events to the page with the head, then reads the request it names once', async () => {
    const stream = planStream(STREAM_PAGE)
    const urls = serve(url => streamPage(stream, url))
    expect(await fetchImportRequest()).toEqual(request)
    expect(urls()).toEqual([eventsUrl(0), eventsUrl(STREAM_PAGE), `/api/v1/agentrt/artifacts/${encodeURIComponent(importHead(imported.events).artifact)}`])
  })

  it('a plan stream with no import head is read to its end: no request', async () => {
    const urls = serve(url => streamPage(rootOnly.events, url))
    expect(await fetchImportRequest()).toBeNull()
    expect(urls()).toEqual([eventsUrl(0), eventsUrl(rootOnly.events.at(-1)!.seq)])
  })

  it('a cursor that does not move on is an error', async () => {
    const stream = planStream(2 * STREAM_PAGE, false)
    const urls = serve(() => streamPage(stream, eventsUrl(0)))
    await expect(fetchImportRequest()).rejects.toThrow('Plan events: invalid pagination cursor')
    expect(urls()).toEqual([eventsUrl(0), eventsUrl(STREAM_PAGE)])
  })

  it(`reads at most ${IMPORT_HEAD_PAGES} pages for the head, then fails, never taking the plan for one not imported (review 416 B1)`, async () => {
    const stream = planStream((IMPORT_HEAD_PAGES + 10) * STREAM_PAGE, false)
    const urls = serve(url => streamPage(stream, url))
    await expect(fetchImportRequest()).rejects.toThrow(`Plan events: no import head in the first ${IMPORT_HEAD_PAGES} pages of plan:R2.`)
    expect(urls()).toEqual(Array.from({ length: IMPORT_HEAD_PAGES }, (_, i) => eventsUrl(i * STREAM_PAGE)))
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
    // The marks are the runtime's, not the title's: 2.1-5 is an effect step whose title has no ★ (review 416 C2).
    expect(round.items.find(i => i.key === '2.1')!.steps.find(s => s.id === '2.1-5')).toMatchObject({ t: '本机测试库可用', star: true, owner: false })
    expect(round.items.find(i => i.key === '2.1')!.steps.find(s => s.id === '2.1-3')).toMatchObject({ owner: true, star: false })
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

  it('a round with no items is an empty round with zero counts, not one that could not be read', () => {
    const round = runtimeRound(fromPlanWithRequest(rootOnly.plan, PLAN_ROOT, null), parseRuntimeProgress(rootOnly.progress).plan, PLAN_ROOT, null)
    expect(round).toEqual({ n: 2, source: 'runtime', items: [] })
    expect(rootOnly.progress.plan[0].count).toMatchObject({ done: 0, total: 0 })
  })
})
