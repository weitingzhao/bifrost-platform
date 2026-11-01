import { describe, expect, it } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import signoffFixture from '@/api/__fixtures__/plan-view-signoff.generated.json'
import type { SignoffWait } from '@/api/agentRuntimePlan'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { moduleRows, type ModuleRow } from '../planModuleModel'
import type { SignoffWaitsRead } from '../useRuntimePlan'
import { workTabHref, workViewOf } from '../workViewModel'
import {
  dependencyWaits, dependencyWords, mainlineLabels, mainlineSentence, moduleClass, othersSentence, roundHeader, signoffBreakdown, signoffCount,
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
  total: 3, done: 0, active: 0, stopped: 0, unknown: 0, dropped: 0, toGo: 3, waits: [], ...o })
const waitsOk = (waits: SignoffWait[] = [], notStarted: SignoffWait[] = [], acceptanceRoots: string[] = []): SignoffWaitsRead => ({ read: 'ok', waits, notStarted, acceptanceRoots })
const wait = (node: string, label: string, state = 'active', refs = ['owner-ok']): SignoffWait => ({ node, label, title: `step ${label}`, state, refs, named: false })
const roundsOk: RoundsRead = { state: 'ok', rounds: [{ id: 'R4', title: '' }, { id: 'R5', title: '' }], newest: 'R5', ignored: [], at: 0, failed: '' }
const read = (view: PlanSnapshot | undefined, o: { failed?: string; root?: string | null; rounds?: RoundsRead } = {}) =>
  ({ view, failed: o.failed ?? null, root: o.root === undefined ? view?.root ?? 'R4' : o.root, rounds: o.rounds ?? roundsOk })

describe('the summary sentence', () => {
  it('says where the mainline stands and counts the other items by their own state, each counted directly', () => {
    const view = prodView({ 'R4:4.1-1': 'done', 'R4:4.1-2': 'done', 'R4:4.1-3': 'active', 'R4:4.5-1': 'active' })
    const rows = moduleRows(view)
    expect(mainlineSentence(view, rows)).toEqual({ text: 'The mainline stands at 4.1 · runtime steps 2 of 6 done, 1 active.', unknown: false })
    // In the runtime's own terms: "no step started" is the controller's record, not a claim that no work was done.
    expect(othersSentence(rows, view.mainline.current?.plan_node_id ?? null)).toBe('By runtime step state, of the 9 other items, 1 has a step active and 8 have no step started.')
    expect(mainlineLabels(view)).toEqual(['4.1', '4.2', '4.4', '4.10'])
    expect(roundHeader(view)).toBe('Round 4 · 10 items · plan v1')
  })

  it('names every class an item can be in, in a fixed order, and only the ones that hold any', () => {
    const rows = [
      row({ id: 'cur' }),
      row({ id: 'a', active: 1, done: 1, toGo: 2 }), row({ id: 'b', active: 2, stopped: 1, toGo: 3 }),
      row({ id: 'c' }), row({ id: 'd' }),
      row({ id: 'e', done: 3, toGo: 0 }),
      row({ id: 'f', done: 1, toGo: 2 }),
      row({ id: 'g', stopped: 1, toGo: 3 }),
      row({ id: 'h', unknown: 2, toGo: 3 }),
      row({ id: 'i', total: 0, toGo: 0 }),
    ]
    expect(rows.slice(1).map(moduleClass)).toEqual(['in-progress', 'in-progress', 'not-started', 'not-started', 'all-done', 'partly', 'stopped', 'unknown', 'no-steps'])
    expect(othersSentence(rows, 'cur')).toBe('By runtime step state, of the 9 other items, 2 have steps active, 2 have no step started, 1 has every step done, '
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
