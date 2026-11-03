import { describe, expect, it } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import signoffFixture from '@/api/__fixtures__/plan-view-signoff.generated.json'
import type { SignoffWait } from '@/api/agentRuntimePlan'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { moduleRows, type ModuleRow } from '../planModuleModel'
import { stepRows } from '../stepStateModel'
import type { SignoffWaitsRead } from '../useRuntimePlan'
import { workTabHref, workViewOf } from '../workViewModel'
import {
  dependencyWaits, dependencyWords, lastReadRows, mainlineLabels, mainlineSentence, moduleClass, othersSentence, roundHeader, signoffBreakdown, signoffCount,
  signoffGaps, signoffWords, summaryLamp,
} from '../workSummaryModel'
import type { RoundsRead } from '../planRoundModel'

/*
 * R5:5.5-2: Work's one sentence and its counts are computed from plan-view and GET /plan, never written by hand (the
 * Owner 2026-11-01: Work had no answer to "where are we, what do I do"). Claims: ~/agent-work/modview/WORK_V36_2_CLAIMS.md.
 */

/** The PROD R4 answer (2026-10-27), with step states set as a test needs them. */
function prodView(states: Record<string, PlanSnapshot['nodes'][number]['state']> = {}): PlanSnapshot {
  const raw = structuredClone(prod) as typeof prod & { nodes: { plan_node_id: string; state: string | null }[] }
  for (const n of raw.nodes) if (n.plan_node_id in states) n.state = states[n.plan_node_id]
  return parsePlanSnapshot(raw, 'R4')
}
const row = (o: Partial<ModuleRow>): ModuleRow => ({ id: 'R:x', label: 'x', lane: 'Support', title: '', current: false, href: '', cells: [],
  total: 3, done: 0, active: 0, running: 0, queued: 0, held: 0, idle: 0, yours: 0, nowUnknown: 0, stopped: 0, unknown: 0, dropped: 0, toGo: 3, waits: [], ...o })
const waitsOk = (waits: SignoffWait[] = [], notStarted: SignoffWait[] = [], acceptanceRoots: string[] = []): SignoffWaitsRead => ({ read: 'ok', waits, notStarted, acceptanceRoots })
const wait = (node: string, label: string, state = 'active', refs = ['owner-ok']): SignoffWait => ({ node, label, title: `step ${label}`, state, refs, named: false })
const roundsOk: RoundsRead = { state: 'ok', rounds: [{ id: 'R4', title: '' }, { id: 'R5', title: '' }], newest: 'R5', ignored: [], at: 0, failed: '' }
const read = (view: PlanSnapshot | undefined, o: { failed?: string; root?: string | null; rounds?: RoundsRead } = {}) =>
  ({ view, failed: o.failed ?? null, root: o.root === undefined ? view?.root ?? 'R4' : o.root, rounds: o.rounds ?? roundsOk })

describe('the summary sentence', () => {
  it('says where the mainline stands and counts the other items by their own state, each counted directly', () => {
    const view = prodView({ 'R4:4.1-1': 'done', 'R4:4.1-2': 'done', 'R4:4.1-3': 'active', 'R4:4.5-1': 'active' })
    // Read at the snapshot's time: both active steps have no task, so nothing runs on either (R5:5.5-10).
    const rows = stepRows(view, { failed: null, now: Date.parse(view.as_of) })
    expect(mainlineSentence(view, rows)).toEqual({ text: 'The mainline stands at 4.1 · runtime steps 2 of 6 done, 1 idle.', unknown: false })
    // In the runtime's own terms: "no step started" is the controller's record, not a claim that no work was done.
    expect(othersSentence(rows, view.mainline.current?.plan_node_id ?? null)).toBe('By runtime step state, of the 9 other items, 1 has an active step with nothing running and 8 have no step started.')
    // A read over 60 s old, or rows not judged at all, say the active step's work now is Unknown: never idle.
    const old = stepRows(view, { failed: null, now: Date.parse(view.as_of) + 61_000 })
    expect(mainlineSentence(view, old).text).toBe('The mainline stands at 4.1 · runtime steps 2 of 6 done, 1 active, Unknown now.')
    expect(othersSentence(old, view.mainline.current?.plan_node_id ?? null)).toBe('By runtime step state, of the 9 other items, 1 has an active step whose work now is Unknown and 8 have no step started.')
    expect(mainlineSentence(view, moduleRows(view)).text).toBe('The mainline stands at 4.1 · runtime steps 2 of 6 done, 1 active, Unknown now.')
    expect(mainlineLabels(view)).toEqual(['4.1', '4.2', '4.4', '4.10'])
    expect(roundHeader(view)).toBe('Round 4 · 10 items · plan v1')
  })

  it('names every class an item can be in, in a fixed order, and only the ones that hold any', () => {
    const rows = [
      row({ id: 'cur' }),
      row({ id: 'a', active: 2, running: 1, idle: 1, done: 1, toGo: 2 }), row({ id: 'b', active: 1, queued: 1, stopped: 1, toGo: 3 }),
      row({ id: 'c' }), row({ id: 'd' }),
      row({ id: 'e', done: 3, toGo: 0 }),
      row({ id: 'f', done: 1, toGo: 2 }),
      row({ id: 'g', stopped: 1, toGo: 3 }),
      row({ id: 'h', unknown: 2, toGo: 3 }),
      row({ id: 'i', total: 0, toGo: 0 }),
      // Active with nothing running, the Owner's, held, and Unknown now beside an Owner step, a held and an idle one: never
      // folded into in progress, and Unknown now said before any of them.
      row({ id: 'j', active: 1, idle: 1 }), row({ id: 'k', active: 1, yours: 1 }), row({ id: 'l', active: 4, idle: 1, held: 1, yours: 1, nowUnknown: 1 }),
      row({ id: 'm', active: 2, held: 1, yours: 1 }),
    ]
    expect(rows.slice(1).map(moduleClass)).toEqual(['in-progress', 'in-progress', 'not-started', 'not-started', 'all-done', 'partly', 'stopped', 'unknown', 'no-steps',
      'idle', 'yours', 'active-unknown', 'held'])
    expect(othersSentence(rows, 'cur')).toBe('By runtime step state, of the 13 other items, 2 have steps in progress, 1 has an active step held, '
      + '1 has an Owner step active, 1 has an active step with nothing running, 1 has an active step whose work now is Unknown, 2 have no step started, 1 has every step done, '
      + '1 has steps done and none active, 1 has a stopped step, 1 has a step with no state and 1 has no steps.')
    expect(othersSentence([row({ id: 'cur' })], 'cur')).toBe('')
  })

  it('says every mainline item is done or verified when the plan view has no current one, by its basis', () => {
    const view = prodView()
    const done = { ...view, mainline: { ...view.mainline, current: null } }
    expect(mainlineSentence(done, moduleRows(done)).text).toBe('Every mainline item has every runtime step done.')
    const pv2 = { ...done, mainline: { ...done.mainline, basis: 'Versioned acceptance observations' } }
    expect(mainlineSentence(pv2, moduleRows(pv2)).text).toBe('Every mainline item is verified accepted.')
    expect(othersSentence(moduleRows(done), null)).toBe('By runtime step state, of the 10 items, 10 have no step started.')
  })

  it('is Unknown, with why, when the plan view has no mainline', () => {
    const view = prodView()
    const none = { ...view, mainline: { ...view.mainline, path: [], current: null } }
    expect(mainlineSentence(none, moduleRows(none))).toEqual({ text: 'Mainline Unknown · the plan view has no adopted mainline.', unknown: true })
  })

  it('counts dependency waits as Blocked and Unknown, only for modules in the view document, and Unknown without one', () => {
    const view = prodView()
    expect(dependencyWaits(view, moduleRows(view))).toEqual({ state: 'ok', total: 13, blocked: 0, unverified: 13, unread: 0 })
    expect(dependencyWords(dependencyWaits(view, moduleRows(view)))).toBe('13 dependency waits')
    expect(dependencyWaits({ ...view, artifact_id: '' }, moduleRows(view))).toEqual({ state: 'unknown', reason: 'no adopted plan view' })
    expect(roundHeader({ ...view, artifact_id: '' })).toBe('Round 4 · 10 items · no adopted plan view')
    // The view document did not parse (R5:5.5-1 review): no module has node.view; the server's warning is the reason.
    const warning = 'Plan root R4: view document unavailable: unexpected end of JSON input'
    const unread = { ...view, waiting_on: [], warnings: [warning], nodes: view.nodes.map(n => ({ ...n, view: undefined })) }
    expect(dependencyWords(dependencyWaits(unread, moduleRows(unread)))).toBe(`Dependencies Unknown · ${warning}`)
    // One module not in the document: its waits are not known, so "no waits" is not said for the round.
    const one = { ...view, waiting_on: [], nodes: view.nodes.map(n => n.plan_node_id === 'R4:4.6' ? { ...n, view: undefined } : n) }
    expect(dependencyWords(dependencyWaits(one, moduleRows(one)))).toBe('No dependency waits recorded · 1 module not in the adopted plan view, Unknown')
  })
})

describe('the sign-offs', () => {
  const pv2 = () => parsePlanSnapshot(signoffFixture, 'R4')

  it('puts Ready acceptance sign-offs and started step sign-offs in ready, the rest in later, each once', () => {
    const view = pv2()
    const accepted = view.owner_waiting[0] as Extract<PlanSnapshot['owner_waiting'][number], { ref: string }>
    // GET /plan lists the same sign-off on the same step: one row, the step's; and another step not started.
    const c = signoffCount(waitsOk([wait(accepted.plan_node_id, '4.1-1', 'active', [accepted.ref])], [wait('R4:4.3-1', '4.3-1', 'planned')]), read(view))
    expect(c.ready.map(r => [r.kind, r.node])).toEqual([['step', accepted.plan_node_id]])
    // Not started (every round's GET /plan) and later (this round's plan view only) are counted apart, each with its reach.
    expect(c.notStarted.map(r => [r.kind, r.node])).toEqual([['step', 'R4:4.3-1']])
    expect(c.later.map(r => [r.kind, r.node])).toEqual([['acceptance', 'R4:4.2-1'], ['acceptance', 'R4:4.3-1']])
    expect(signoffWords(c)).toBe('1 ready · 1 not started · 2 later in Round 4')
    expect(signoffGaps(c)).toEqual([])
    expect(signoffBreakdown(c)).toEqual([
      "Ready 1: 1 step sign-off whose work has started, every round (GET /plan); 0 acceptance sign-offs Round 4's plan view marks Ready.",
      'Not started 1: step sign-offs whose work has not started, every round (GET /plan).',
      "Later in Round 4 2: Round 4's plan view only (owner_later: acceptance sign-offs Blocked or Not started, Owner steps whose readiness is Unknown); other rounds' are not read here.",
    ])
  })

  it('keeps tasks awaiting the Owner apart, and an Owner step either source already lists is not a second row', () => {
    const view = prodView()
    const withTask = { ...view, owner_waiting: [{ task_id: 't_1', plan_node_id: 'R4:4.1-1', reason: 'Runtime task awaits Owner', source: 'agentrt.task' }] }
    const listed = view.owner_later[0].plan_node_id
    const c = signoffCount(waitsOk([], [wait(listed, 'x', 'planned')]), read(withTask))
    expect(c.tasks).toHaveLength(1)
    expect(c.ready).toHaveLength(0)
    expect([...c.notStarted, ...c.later].filter(r => r.node === listed)).toHaveLength(1)
    expect(c.later.filter(r => r.kind === 'owner-step')).toHaveLength(view.owner_later.length - 1)
    expect(signoffWords(c)).toBe(`0 ready · 1 not started · ${view.owner_later.length - 1} later in Round 4`)
  })

  it('names the rounds whose acceptance sign-offs it does not count', () => {
    expect(signoffCount(waitsOk([], [], ['R3', 'R4']), read(pv2())).uncounted).toEqual(['R3'])
  })

  it('says which source could not be read, still lists what the other read, and never says 0 for a source not read', () => {
    const view = pv2()
    const noSteps = signoffCount({ read: 'error' }, read(view))
    expect(noSteps.ready).toHaveLength(1)
    expect(signoffGaps(noSteps)).toEqual(['the plan could not be read (GET /plan)'])
    // Each part says its own source: ready takes both, so "+"; not started is GET /plan's alone, so Unknown.
    expect(signoffWords(noSteps)).toBe('1+ ready · not started Unknown · 2 later in Round 4')
    const noView = signoffCount(waitsOk([wait('R5:5.1-4', '5.1-4')]), read(undefined, { root: 'R5', failed: 'Plan view: 503' }))
    expect(noView.ready.map(r => r.node)).toEqual(['R5:5.1-4'])
    expect(signoffGaps(noView)).toEqual(["Round 5's plan view could not be read (Plan view: 503)"])
    expect(signoffWords(noView)).toBe('1+ ready · later in Round 5 Unknown')
    expect(signoffWords(signoffCount({ read: 'error' }, read(undefined, { root: 'R5', failed: 'x' })))).toBe('Unknown')
    expect(signoffGaps(signoffCount(waitsOk(), read(undefined, { root: null, rounds: { state: 'unknown', reason: 'Offline' } }))))
      .toEqual(['no round read; which rounds exist is Unknown (Offline)', 'which round to read is Unknown (Offline)'])
    expect(signoffWords(signoffCount({ read: 'loading' }, read(undefined, { root: 'R5' })))).toBe('reading…')
    expect(signoffWords(signoffCount({ read: 'loading' }, read(view)))).toBe('reading…')
  })

  it('is reading, not "no round", while the rounds are being read; says no round only once the list holds none', () => {
    // R5:5.5-2 review: a cold load of #work?tab=signoffs said "the runtime holds no round" as an alert while GET /progress was in flight.
    const cold = signoffCount(waitsOk(), read(undefined, { root: null, rounds: { state: 'reading' } }))
    expect(cold.round).toBe('reading')
    expect(signoffGaps(cold)).toEqual([])
    expect(signoffWords(cold)).toBe('reading…')
    const none = signoffCount(waitsOk(), read(undefined, { root: null, rounds: { ...roundsOk, rounds: [], newest: null } }))
    expect(signoffGaps(none)).toEqual(['the runtime holds no round'])
  })

  // R5:5.5-2 review: GET /plan read for PLAN_ROOT alone (the round list not read, or holding no R<n> id) was counted as
  // every round, and a round named in the address could then turn the lamp green.
  it('counts the step sign-offs as every round only when the round list was read whole', () => {
    const r2 = { ...waitsOk([wait('R2:2.1-1', '2.1-1')]), rounds: ['R2'] }
    const noList = signoffCount(r2, read(pv2(), { rounds: { state: 'unknown', reason: "the runtime's progress answer has no plan list" } }))
    expect(noList.ready.map(r => r.node)).toContain('R2:2.1-1')
    expect(signoffGaps(noList)).toEqual(["only R2 read; which rounds exist is Unknown (the runtime's progress answer has no plan list)"])
    expect(signoffWords(noList)).toBe('2+ ready · not started Unknown · 2 later in Round 4')
    expect(signoffBreakdown(noList)[1]).toBe('Not started 0: step sign-offs whose work has not started, R2 only (GET /plan).')
    const odd = signoffCount({ ...waitsOk(), rounds: ['R4', 'R5'] }, read(pv2(), { rounds: { ...roundsOk, ignored: ['R6b'] } }))
    expect(signoffGaps(odd)).toEqual(['round R6b not read: its id is not R<n>'])
    expect(signoffWords(odd)).toBe('1+ ready · not started Unknown · 2 later in Round 4')
    const clean = { ...pv2(), owner_waiting: [], owner_later: [] }
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: signoffCount({ ...waitsOk(), rounds: ['R2'] }, read(clean, { rounds: { state: 'unknown', reason: 'x' } })) }))
      .toEqual({ tone: 'unknown', why: ['sign-offs: only R2 read; which rounds exist is Unknown (x)'] })
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: signoffCount(waitsOk(), read(clean)) }).tone).toBe('ok')
  })
})

describe('the summary lamp', () => {
  const view = prodView()
  const rows = moduleRows(view)
  const clean = { ...view, owner_later: [] }
  const lamp = (count: ReturnType<typeof signoffCount>, o: { stale?: boolean; mainlineUnknown?: boolean; rows?: ModuleRow[] } = {}) =>
    summaryLamp({ stale: o.stale ?? false, mainlineUnknown: o.mainlineUnknown ?? false, count, rows: o.rows ?? rows })
  it('is yellow for a ready sign-off, a task waiting or a stopped step, and says which', () => {
    expect(lamp(signoffCount(waitsOk([wait('R4:4.1-1', '4.1-1')]), read(clean)))).toEqual({ tone: 'attention', why: ['1 sign-off ready for you'] })
    expect(lamp(signoffCount(waitsOk(), read(clean)), { rows: [row({ stopped: 1 })] })).toEqual({ tone: 'attention', why: ['1 module in Round 4 with a stopped step'] })
  })
  it('is green only for what was read, and says so: in Round N', () => {
    expect(lamp(signoffCount(waitsOk(), read(clean)))).toEqual({ tone: 'ok',
      why: ['no step sign-off ready in any round; in Round 4, no acceptance sign-off ready, no task waiting on you, no step stopped'] })
  })
  it('is grey, with why, while anything it would need is not known', () => {
    expect(lamp(signoffCount(waitsOk(), read(view)))).toEqual({ tone: 'unknown', why: [`${view.owner_later.length} Owner steps' readiness in Round 4 is Unknown`] })
    // Another round's acceptance sign-offs are not counted here (PROD: R4 beside R5): no green for them.
    expect(lamp(signoffCount(waitsOk([], [], ['R3']), read(clean)))).toEqual({ tone: 'unknown', why: ['acceptance sign-offs of R3 are not counted here'] })
    expect(lamp(signoffCount(waitsOk(), read(clean)), { stale: true }).why).toEqual(["Round 4's plan view is old or could not be read"])
    expect(lamp(signoffCount(waitsOk(), read(clean)), { mainlineUnknown: true }).tone).toBe('unknown')
    expect(lamp(signoffCount({ read: 'error' }, read(clean))).why).toEqual(['sign-offs: the plan could not be read (GET /plan)'])
    expect(lamp(signoffCount({ read: 'loading' }, read(clean))).why).toEqual(['the sign-offs are still being read'])
  })
})

// R5:5.5-4: Work's round tab reads its own round's GET /plan alone (the earlier rounds' trees every 15 s were part of the
// load behind the PROD OOM of 2026-10-31); every count it gives is that round's and says so, and the lamp is never green
// for "any round". The Sign-offs tab keeps every round (above).
describe('the sign-offs of one round (the round tab)', () => {
  const pv2 = () => parsePlanSnapshot(signoffFixture, 'R4')
  const one = (waits: SignoffWaitsRead, view: PlanSnapshot | undefined, o: Parameters<typeof read>[1] = {}) =>
    signoffCount(waits, { ...read(view, o), scope: 'round' })
  it('counts the round\'s own step sign-offs and says the reach is that round only, other rounds on the Sign-offs tab', () => {
    const c = one({ ...waitsOk([wait('R4:4.1-1', '4.1-1')], [wait('R4:4.3-1', '4.3-1', 'planned')]), rounds: ['R4'] }, pv2())
    expect(c.scope).toBe('round')
    expect(signoffWords(c)).toBe('2 ready · 1 not started · 2 later · Round 4 only')
    expect(signoffGaps(c)).toEqual([])
    expect(signoffBreakdown(c)).toEqual([
      "Ready 2: 1 step sign-off whose work has started, Round 4 only (GET /plan?root=R4); 1 acceptance sign-off Round 4's plan view marks Ready.",
      'Not started 1: step sign-offs whose work has not started, Round 4 only (GET /plan?root=R4).',
      "Later in Round 4 2: Round 4's plan view only (owner_later: acceptance sign-offs Blocked or Not started, Owner steps whose readiness is Unknown); other rounds' are not read here.",
      "Other rounds are not read here: the Sign-offs tab reads every round's step sign-offs and lists the acceptance sign-offs of the other rounds.",
    ])
    expect(signoffBreakdown(c).join(' ')).not.toContain('every round (GET /plan)')
  })
  it('is whole once the round\'s tree answered, whatever the round list is: no other round is needed', () => {
    const c = one({ ...waitsOk(), rounds: ['R4'] }, pv2(), { rounds: { state: 'unknown', reason: "the runtime's progress answer has no plan list" } })
    expect(c.steps).toBe('ok')
    expect(signoffWords(c)).toBe('1 ready · 2 later · Round 4 only')
  })
  it('says the round\'s tree could not be read, never 0 of its sign-offs; and reading or Unknown while the round is not known', () => {
    const failed = one({ read: 'error' }, pv2())
    expect(signoffGaps(failed)).toEqual(["Round 4's plan could not be read (GET /plan?root=R4)"])
    expect(signoffWords(failed)).toBe('1+ ready · not started Unknown · 2 later · Round 4 only')
    expect(signoffWords(one({ read: 'error' }, undefined, { root: 'R5', failed: 'Plan view: 503' }))).toBe('Unknown')
    expect(signoffWords(one(waitsOk([wait('R5:5.1-4', '5.1-4')]), undefined, { root: 'R5', failed: 'Plan view: 503' }))).toBe('1+ ready · later Unknown · Round 5 only')
    expect(signoffWords(one({ read: 'loading' }, undefined, { root: null, rounds: { state: 'reading' } }))).toBe('reading…')
    expect(signoffGaps(one({ read: 'loading' }, undefined, { root: null, rounds: { state: 'unknown', reason: 'Offline' } })))
      .toEqual(['which round to read is Unknown (Offline)', 'which round to read is Unknown (Offline)'])
    expect(signoffGaps(one({ read: 'loading' }, undefined, { root: null, rounds: { ...roundsOk, rounds: [], newest: null } })))
      .toEqual(['the runtime holds no round', 'the runtime holds no round'])
  })
  it('lights green for that round only, never "any round"; yellow says the ready sign-off is that round\'s', () => {
    const clean = { ...pv2(), owner_waiting: [], owner_later: [] }
    const green = summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: one({ ...waitsOk(), rounds: ['R4'] }, clean) })
    expect(green).toEqual({ tone: 'ok', why: ['in Round 4 only, no step sign-off ready, no acceptance sign-off ready, no task waiting on you, no step stopped; other rounds are read on the Sign-offs tab'] })
    expect(green.why.join(' ')).not.toContain('any round')
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: one({ ...waitsOk([wait('R4:4.1-1', '4.1-1')]), rounds: ['R4'] }, clean) }))
      .toEqual({ tone: 'attention', why: ['1 sign-off ready for you in Round 4'] })
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: one({ read: 'error' }, clean) }))
      .toEqual({ tone: 'unknown', why: ["sign-offs: Round 4's plan could not be read (GET /plan?root=R4)"] })
  })
})

// Local review of R5:5.5-4 ("old read said as now"): a refresh that hangs neither fails nor answers, so GET /plan stayed a
// success and its old "0 ready" lit the round green. Each read is judged by its age on the page's clock (readAge.ts).
describe('the sign-offs from reads too old to be said as now', () => {
  const pv2 = () => parsePlanSnapshot(signoffFixture, 'R4')
  const NOW = Date.parse(pv2().as_of)
  const clean = () => ({ ...pv2(), owner_waiting: [], owner_later: [] })
  const iso = (ms: number) => new Date(ms).toISOString()
  const tree = (o: { at: number; oldest?: string; waits?: SignoffWait[]; rounds?: string[] }): SignoffWaitsRead =>
    ({ ...waitsOk(o.waits ?? []), rounds: o.rounds ?? ['R4'], at: o.at, oldest: o.oldest ?? 'R4' })
  const count = (waits: SignoffWaitsRead, view: PlanSnapshot | undefined, scope: 'round' | 'every', o: Parameters<typeof read>[1] = {}) =>
    signoffCount(waits, { ...read(view, o), scope, now: NOW, rounds: o.rounds ?? { ...roundsOk, at: NOW } })
  it('says the round\'s GET /plan Unknown once its answer is older than twice its 15 s poll: never "0 ready", never green', () => {
    const old = count(tree({ at: NOW - 31_000, waits: [wait('R4:4.1-1', '4.1-1')] }), clean(), 'round')
    const why = `GET /plan?root=R4 last answered 31 s ago (${iso(NOW - 31_000)}), older than twice its 15 s poll`
    expect(signoffGaps(old)).toEqual([why])
    // Its rows are left out of every count, and kept to be listed as last read: the ready step it listed.
    expect(old.ready).toEqual([])
    expect(old.lastRead.ready.map(r => [r.kind, r.node])).toEqual([['step', 'R4:4.1-1']])
    expect(signoffWords(old)).toBe('0+ ready · not started Unknown · 0 later · Round 4 only')
    expect(signoffBreakdown(old)).toContain(`Unknown: ${why}.`)
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: old })).toEqual({ tone: 'unknown', why: [`sign-offs: ${why}`] })
    // 30 s is within twice the poll.
    const fresh = count(tree({ at: NOW - 30_000 }), clean(), 'round')
    expect(fresh.steps).toBe('ok')
    expect(summaryLamp({ stale: false, mainlineUnknown: false, rows: [], count: fresh }).tone).toBe('ok')
  })
  it('says every round\'s GET /plan Unknown when the oldest is, naming that round', () => {
    const old = count(tree({ at: NOW - 45_000, oldest: 'R3', rounds: ['R3', 'R4'] }), clean(), 'every')
    expect(signoffGaps(old)).toEqual([`GET /plan?root=R3 last answered 45 s ago (${iso(NOW - 45_000)}), older than twice its 15 s poll`])
    expect(signoffWords(old)).toBe('0+ ready · not started Unknown · 0 later in Round 4')
    // The round list (GET /progress) older than twice its 5 s poll: which rounds exist is Unknown, the rows read still count.
    const listOld = count(tree({ at: NOW, waits: [wait('R4:4.1-1', '4.1-1')] }), clean(), 'every', { rounds: { ...roundsOk, at: NOW - 11_000 } })
    expect(signoffGaps(listOld)).toEqual([`only R4 read; which rounds exist is Unknown (GET /progress last answered 11 s ago (${iso(NOW - 11_000)}))`])
    expect(signoffWords(listOld)).toBe('1+ ready · not started Unknown · 0 later in Round 4')
  })
  it('says the round\'s plan view Unknown once it is not now, and counts none of its sign-offs', () => {
    const view = pv2()
    const old = count(tree({ at: NOW }), view, 'round', { failed: null })
    expect(old.round).toBe('ok')
    const aged = signoffCount(tree({ at: NOW + 61_000 }), { ...read(view), scope: 'round', now: NOW + 61_000, rounds: { ...roundsOk, at: NOW + 61_000 } })
    expect(signoffGaps(aged)).toEqual([`Round 4's plan view is older than 60 s; as of ${view.as_of}`])
    expect(aged.ready).toEqual([])
    expect(aged.later).toEqual([])
    // Not counted, but listed as last read: its Ready acceptance sign-off and its later ones.
    expect(aged.lastRead.ready.map(r => [r.kind, r.node])).toEqual([['acceptance', view.owner_waiting[0].plan_node_id]])
    expect(aged.lastRead.later.map(r => r.node)).toEqual(['R4:4.2-1', 'R4:4.3-1'])
    expect(signoffWords(aged)).toBe('0+ ready · later Unknown · Round 4 only')
    const failed = count(tree({ at: NOW }), view, 'round', { failed: 'Plan view: 503' })
    expect(signoffGaps(failed)).toEqual([`Round 4's plan view could not be read (Plan view: 503); as of ${view.as_of}`])
    expect(failed.ready).toEqual([])
    expect(failed.lastRead.ready).toHaveLength(1)
  })
  it('lists a failed GET /plan refresh\'s last answers as last read, counting none; with none kept, lists nothing', () => {
    const blinked = count({ read: 'error', last: { waits: [wait('R4:4.1-1', '4.1-1')], notStarted: [wait('R4:4.3-1', '4.3-1', 'planned')], at: NOW - 5_000 } }, clean(), 'round')
    expect(signoffGaps(blinked)).toEqual(["Round 4's plan could not be read (GET /plan?root=R4)"])
    expect([blinked.ready, blinked.notStarted]).toEqual([[], []])
    expect(blinked.lastRead.ready.map(r => r.node)).toEqual(['R4:4.1-1'])
    expect(blinked.lastRead.notStarted.map(r => r.node)).toEqual(['R4:4.3-1'])
    expect(signoffWords(blinked)).toBe('0+ ready · not started Unknown · 0 later · Round 4 only')
    expect(count({ read: 'error' }, clean(), 'round').lastRead).toEqual({ ready: [], notStarted: [], later: [], tasks: [] })
  })
  // Review of 13191da1: one sign-off listed twice when both sources held it as last read, or when GET /plan was old and
  // the view, now, listed it later. The 5.5-2 overlap: GET /plan's step and the view's acceptance on the same node|ref.
  it('lists one sign-off once when both sources are last read: the step row, n = 1', () => {
    const view = { ...pv2(), owner_later: [] }
    const accepted = view.owner_waiting[0] as Extract<PlanSnapshot['owner_waiting'][number], { ref: string }>
    const both = count({ read: 'error', last: { waits: [wait(accepted.plan_node_id, '4.1-1', 'active', [accepted.ref])], notStarted: [], at: NOW - 5_000 } },
      view, 'round', { failed: 'Plan view: 503' })
    expect(both.lastRead.ready.map(r => [r.kind, r.node])).toEqual([['step', accepted.plan_node_id]])
    expect(lastReadRows(both)).toBe(1)
    expect([both.ready, both.later]).toEqual([[], []])
  })
  it('lists a sign-off the view, now, lists later only there, not again as a step read before', () => {
    const view = pv2()
    const later = view.owner_later.find(w => 'ref' in w) as Extract<PlanSnapshot['owner_later'][number], { ref: string }>
    const c = count(tree({ at: NOW - 31_000, waits: [wait(later.plan_node_id, 'x', 'active', [later.ref])] }), view, 'round')
    expect(c.later.map(r => [r.kind, r.node])).toContainEqual(['acceptance', later.plan_node_id])
    expect(c.lastRead.ready).toEqual([])
    expect(lastReadRows(c)).toBe(0)
  })
  it('lists a step read before beside the same sign-off counted from a view that is now only once: counted', () => {
    const view = pv2()
    const accepted = view.owner_waiting[0] as Extract<PlanSnapshot['owner_waiting'][number], { ref: string }>
    const c = count(tree({ at: NOW - 31_000, waits: [wait(accepted.plan_node_id, '4.1-1', 'active', [accepted.ref])] }), view, 'round')
    expect(c.ready.map(r => r.kind)).toEqual(['acceptance'])
    expect(c.lastRead.ready).toEqual([])
  })
  it('says no round only from a round list that is now', () => {
    const none = { ...roundsOk, rounds: [], newest: null }
    expect(signoffGaps(count({ read: 'loading' }, undefined, 'round', { root: null, rounds: { ...none, at: NOW } }))).toEqual(['the runtime holds no round', 'the runtime holds no round'])
    expect(signoffGaps(count({ read: 'loading' }, undefined, 'round', { root: null, rounds: { ...none, at: NOW - 11_000 } }))[0])
      .toBe(`which round to read is Unknown (GET /progress last answered 11 s ago (${iso(NOW - 11_000)}))`)
  })
})

describe('the address of each view of Work', () => {
  it.each([
    ['', { kind: 'tab', tab: 'round' }],
    ['section=plan&root=R5&node=R5%3A5.1', { kind: 'tab', tab: 'round' }],
    ['round=R4', { kind: 'tab', tab: 'round' }],
    ['tab=signoffs', { kind: 'tab', tab: 'signoffs' }],
    ['section=signoff', { kind: 'tab', tab: 'signoffs' }],
    ['tab=live&root=R4', { kind: 'tab', tab: 'live' }],
    ['tab=changes', { kind: 'tab', tab: 'changes' }],
    ['tab=nonsense', { kind: 'tab', tab: 'round' }],
    ['section=goals', { kind: 'section', section: 'goals' }],
    ['goal=M-12&task=t_1', { kind: 'section', section: 'goals' }],
    ['section=activity', { kind: 'section', section: 'activity' }],
    ['section=code-health', { kind: 'section', section: 'code-health' }],
    ['section=commit-lineage', { kind: 'section', section: 'commit-lineage' }],
  ])('%s opens %j', (query, view) => {
    expect(workViewOf(query)).toEqual(view)
  })
  it('keeps a round the address names in each tab, and drops a module or step', () => {
    expect(workTabHref('live', 'R5', false)).toBe('#work?tab=live')
    expect(workTabHref('live', 'R4', true)).toBe('#work?tab=live&root=R4')
    expect(workTabHref('round', 'R4', true)).toBe('#work?root=R4')
    expect(workTabHref('round', 'R5', false)).toBe('#work')
  })
})
