import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import card from '@/api/__fixtures__/plan-explain-step-card.json'
import failed from '@/api/__fixtures__/plan-explain-failed.json'
import pending from '@/api/__fixtures__/plan-explain-pending.json'
import shared from '@/api/__fixtures__/plan-explain-shared-block.json'
import { splitPlanExplain, type PlanSnapshotRead } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { approvalHref, goalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { NowClock } from '../../progress/progressClock'
import { EXPLAIN_OLD_MS, flatRows, type StepWhyModel, type WhyRow } from '../explainModel'
import { LANDING_QUIET_MS } from '../liveStepModel'
import { moduleOf } from '../moduleDetailModel'
import { HEARTBEAT_UNCONFIRMED_S } from '../needsCleanupModel'
import { PlanModuleDetail } from '../PlanModuleDetail'
import { PlanModuleRows } from '../PlanModuleRows'
import { stepRows } from '../stepStateModel'
import { planNodeHref } from '../usePlanSnapshot'
import { AT, r5Raw, T3, T_OTHER, type Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

/*
 * R5:5.5-20 (Design v3.15 ItemDetail Stages, API-REQUIREMENTS-IA-v2 §4.2): the step card says why a step is not done as
 * one conclusion and structured rows. The R5 plan view of the module tests with module 5.2's steps moved on to the
 * cases of plan-explain-step-card.json: 5.2-1 the real 5.11-1 (its landing declined, nothing replacing it), 5.2-2 a
 * running step, 5.2-3 fifteen rows with three more the server left out, 5.2-5 any one of two paths with cards whose
 * question number is empty.
 */
const NOW = Date.parse(AT) + 5_000
const ago = (ms: number) => new Date(NOW - ms).toISOString()
const min = (n: number) => ago(n * 60_000)
type Rec = Record<string, unknown>
const T = (c: string) => `t_01M6FX0NQ6V8K2ZJ4R7W9E1A3${c}`
const op = (c: string, n = 1) => `land:${T(c)}:${n}`
const ALL = (min: number) => ({ all: [{ step_tasks: { state: 'accepted', min } }, { step_landed: {} }, { step_stg_settled: {} }] })
const landing = (operation: string, phase: string, observed: string, outcome = '') => ({ stream: 'landing:bifrost-platform', operation, phase, outcome, reason: '',
  attempt: 'a_1', result: 'r', main_commit: '', run: '', observed_at: observed, basis: 'note.landing' })
const attempt = (tid: string, heartbeat: number) => ({ attempt_id: `a_${tid.slice(-4)}`, attempt_no: 2, task_id: tid, mission_id: 'M-511', host_id: 'mac-mini',
  executor: 'codex', state: 'running', started_at: min(14), elapsed_s: 840, heartbeat_age_s: heartbeat, reason: '', persisted: true })

/** Module 5.2 moved on to the fixture's cases; more changes it further. */
function raw(more: (r: Raw) => void = () => {}): Raw {
  const r = r5Raw()
  const node = (nid: string, set: Rec) => Object.assign(r.nodes.find(n => n.plan_node_id === nid)!, set)
  node('R5:5.2-1', { state: 'active', version: 7, done_rule_json: ALL(1) })
  node('R5:5.2-2', { state: 'active', version: 3, done_rule_json: ALL(1) })
  node('R5:5.2-3', { state: 'active', version: 4, done_rule_json: ALL(6) })
  node('R5:5.2-5', { state: 'active', version: 2, done_rule_json: { any: [{ step_stg_settled: {} }, { step_tasks: { state: 'accepted', min: 2 } }] } })
  // The fixture's other task on 5.2-1 is moved off it: 5.2-1 holds only the declined design.
  r.tasks.find(t => t.task_id === T_OTHER)!.plan_node_id = 'R5:5.2-7'
  const task = (c: string, step: string, title: string, state: string, set: Rec = {}) => r.tasks.push({ task_id: T(c), plan_node_id: step, mission_id: 'M-511', title, state, ...set })
  task('B', 'R5:5.2-1', 'design: nervous system and brain, draft 4', 'accepted', { landing: landing(op('B'), 'failed', '2026-10-26T22:40:00Z', 'declined') })
  task('C', 'R5:5.2-2', 'nervous system: event bus, attempt 2', 'running', { attempt: attempt(T('C'), 8) })
  task('D', 'R5:5.2-3', 'replay: order flow suite', 'submitted')
  task('E', 'R5:5.2-3', 'replay: risk checks suite', 'rework')
  task('F', 'R5:5.2-3', 'replay: market data suite', 'running', { attempt: attempt(T('F'), 260) })
  task('G', 'R5:5.2-3', 'replay: settlement suite', 'blocked')
  task('H', 'R5:5.2-3', 'replay: fixtures refresh', 'accepted', { landing: landing(op('H', 2), 'failed', '2026-10-27T00:41:00Z', 'ci_red') })
  task('J', 'R5:5.2-3', 'replay: harness timeouts', 'accepted', { landing: landing(op('J'), 'main_result', min(40)) })
  task('K', 'R5:5.2-3', 'replay: seed data', 'accepted', { landing: landing(op('K', 2), 'parked', min(2)) })
  task('M', 'R5:5.2-3', 'replay: report format', 'accepted', { landing: landing(op('M'), 'awaiting_grant', min(32)) })
  task('N', 'R5:5.2-3', 'replay: dashboards', 'accepted')
  task('P', 'R5:5.2-3', 'replay: odd landing', 'accepted', { landing: landing(op('P'), 'a_phase_added_later', min(50)) })
  task('Q', 'R5:5.2-3', 'replay: alerts', 'accepted')
  task('R', 'R5:5.2-3', 'replay: old harness', 'accepted', { landing: landing(op('R'), 'failed', '2026-10-27T00:15:00Z', 'declined') })
  task('S', 'R5:5.2-5', 'joint batch: nervous system + brain', 'accepted', { landing: landing(op('S'), 'landed', min(120)) })
  task('T', 'R5:5.2-5', 'isolated run: nervous system + brain', 'ready')
  more(r)
  return r
}
const viewOf = (explain: unknown = card, more?: (r: Raw) => void): PlanSnapshotRead => splitPlanExplain({ ...raw(more), explain }, 'R5')
const cells = (v: PlanSnapshotRead, now = NOW) => stepRows(v, { failed: null, now }).flatMap(r => r.cells)
const cellOf = (v: PlanSnapshotRead, label: string, now = NOW) => cells(v, now).find(c => c.label === label)!
const why = (v: PlanSnapshotRead, label: string, now = NOW): StepWhyModel => cellOf(v, label, now).why!
const rows = (w: StepWhyModel): WhyRow[] => flatRows(w.list?.entries ?? [])
const words = (w: StepWhyModel) => [w.word, w.reason, ...rows(w).flatMap(r => [r.what, r.sub, r.record, r.push.word, r.request?.text ?? ''])].join(' | ')
/** The explain block of the fixture, changed by edit. */
const cardWith = (edit: (c: typeof card) => void) => { const c = structuredClone(card); edit(c); return c }

let client: QueryClient
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
function page(v: PlanSnapshotRead, readAgain?: () => void) {
  const opened = moduleOf(v, 'R5:5.2')!
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>
    <PlanModuleDetail view={v} item={opened.item} step="" stale={false} readAgain={readAgain} /></NowClock></PlatformAuthContext.Provider></QueryClientProvider>)
}
const stage = (label: string) => document.querySelector(`[data-step-row="${label}"]`) as HTMLElement

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
})
afterEach(() => { cleanup(); client.clear(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('the conclusion and the rows (R1, R2)', () => {
  it('1 · a declined landing with no task replacing it: Nobody is on it, two rows, the task named once', () => {
    const w = why(viewOf(), '5.2-1')
    expect(w.word).toBe('Nobody is on it.')
    expect(w.reason).toMatch(/^Its landing was declined at (\w{3} \d+ )?\d\d:\d\d and no task replaces it; STG waits for the landing\.$/)
    expect(rows(w).map(r => [r.kind, r.record])).toEqual([['Landing', 'Declined'], ['STG', 'Needs landing']])
    const [land, stg] = rows(w)
    expect(land).toMatchObject({ what: 'design: nervous system and brain, draft 4', href: goalHref('M-511', T('B')), sub: `${op('B')} · no rework record`,
      since: '2026-10-26T22:40:00Z', push: { kind: 'quiet' } })
    expect(land.push.word).toMatch(/^Quiet · \d+ h$/)
    expect(stg).toMatchObject({ what: 'Release to STG', push: { kind: 'none', word: '' } })

    page(viewOf())
    const s = stage('5.2-1')
    expect(s.querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running now')
    expect(s.querySelector('[data-why-verdict]')).toHaveTextContent(/^Nobody is on it\. Its landing was declined/)
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(2)
    expect(s.querySelector('[data-why-count]')).toHaveTextContent('In the way · 2')
    expect(s.querySelector('[data-why-done-when]')).toHaveTextContent(/^Done when: At least 1 of this step's own tasks accepted, and every accepted task of this step has landed on main/)
    // The task's name once on the card: its row's link; no Latest task line repeats it.
    expect(s.textContent!.split('design: nervous system and brain, draft 4')).toHaveLength(2)
    expect(within(s).getByRole('link', { name: 'design: nervous system and brain, draft 4' })).toHaveAttribute('href', goalHref('M-511', T('B')))
    expect(s).not.toHaveTextContent('Latest task')
    expect(s).not.toHaveTextContent(/more in the step's detail|Not done because/)
  })

  it('says who is pushing each task by its heartbeat (60 s), queued, held, or changed since the read', () => {
    const r = rows(why(viewOf(), '5.2-3'))
    const by = (c: string) => r.find(x => x.task === T(c) && x.kind === 'Task')!
    expect(by('D').push).toEqual({ kind: 'changed', word: 'Changed since read' }) // read running, submitted now
    expect(by('E').push).toEqual({ kind: 'queued', word: 'Queued' })
    expect(by('F').push).toEqual({ kind: 'unconfirmed', word: 'Unconfirmed · silent 265 s' })
    expect(by('G')).toMatchObject({ record: 'Blocked', push: { kind: 'quiet' }, since: '2026-10-26T23:02:00Z' })
    // The Live tab's rule exactly: at 60 s with the snapshot's age a heartbeat is confirmed; past it, not.
    const at = (age: number) => rows(why(viewOf(card, r2 => { (r2.tasks.find(t => t.task_id === T('C'))!.attempt as Rec).heartbeat_age_s = age }), '5.2-2'))[1].push
    expect(at(HEARTBEAT_UNCONFIRMED_S - 5)).toEqual({ kind: 'moving', word: `Moving · heartbeat ${HEARTBEAT_UNCONFIRMED_S} s` })
    expect(at(HEARTBEAT_UNCONFIRMED_S - 4).kind).toBe('unconfirmed')
  })

  it('reads a landing moving or quiet (30 min) only for the same operation, else unconfirmed; a request by its number', () => {
    const r = rows(why(viewOf(), '5.2-3'))
    const by = (c: string) => r.find(x => x.task === T(c))!
    expect(by('H')).toMatchObject({ record: 'Failed', sub: `${op('H', 2)} · result ci_red`, since: '2026-10-27T00:41:00Z', push: { kind: 'quiet' } })
    expect(by('J')).toMatchObject({ record: 'Main unsettled', push: { kind: 'quiet', word: 'Quiet · 40 min' } })
    expect(by('K').push).toEqual({ kind: 'unconfirmed', word: 'Current state unconfirmed' }) // the plan view holds another operation
    expect(by('M')).toMatchObject({ push: { kind: 'none' }, request: { text: 'Request #188 · pending', href: approvalHref('appr_188') } })
    expect(by('N')).toMatchObject({ record: 'No record', push: { kind: 'quiet' } })
    expect(by('P')).toMatchObject({ record: 'Unmapped', sub: `${op('P')} · phase a_phase_added_later, which this page does not name` })
    expect(by('Q')).toMatchObject({ record: 'Not claimed', since: '2026-10-27T01:30:00Z', push: { kind: 'quiet' } })
    // Reworked tasks landed in its place: its card is what is left, neither moving nor quiet.
    expect(by('R')).toMatchObject({ record: 'Declined', sub: `${op('R')} · rework recorded`, push: { kind: 'none' } })
    const pushJ = (observed: string, phase = 'main_result') => rows(why(viewOf(card, r2 => {
      r2.tasks.find(t => t.task_id === T('J'))!.landing = landing(op('J'), phase, observed)
    }), '5.2-3')).find(x => x.task === T('J'))!.push
    expect(pushJ(ago(LANDING_QUIET_MS + 1_000)).kind).toBe('quiet')
    expect(pushJ(min(1))).toEqual({ kind: 'moving', word: 'Moving · record 1 min ago' })
    // Changed since only by a landing record later than the read (02:44:30); an earlier one only shows the reads differ.
    expect(pushJ(ago(2_000), 'landed')).toEqual({ kind: 'changed', word: 'Changed since read' })
    expect(pushJ(min(1), 'landed')).toEqual({ kind: 'unconfirmed', word: 'The two reads differ' })
  })

  it('never says whose turn it is: Waits on you stays Needs you\'s', () => {
    const v = viewOf()
    for (const label of ['5.2-1', '5.2-2', '5.2-3', '5.2-5']) expect(words(why(v, label))).not.toMatch(/waits? on you|your turn|for you|等你/i)
  })
})

describe('every row there is (R3)', () => {
  it('2 · fifteen rows and three the server did not list: six shown, Show all 15 in place, the three named', () => {
    const w = why(viewOf(), '5.2-3')
    expect(w.list).toMatchObject({ rows: 15, omitted: 3 })
    expect(w.word).toBe('Nobody is on it.')
    // "N more" counts as the list does: 15 rows + 3 not listed, less the 5 + 1 rows of the two named.
    expect(w.reason).toBe('2 of 6 tasks are accepted; a landing failed (ci_red); 12 more in the way.')
    page(viewOf())
    const s = stage('5.2-3')
    expect(s.querySelector('[data-why-count]')).toHaveTextContent('In the way · 15 + 3 not listed')
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(6)
    const all = within(s).getByRole('button', { name: /^Show all 15/ })
    expect(all).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(all)
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(15)
    expect(within(s).getByRole('button', { name: 'Show fewer' })).toHaveAttribute('aria-expanded', 'true')
    expect(s.querySelector('[data-why-omitted]')).toHaveTextContent('3 more not listed by the server.')
    expect(s).not.toHaveTextContent(/\+\d+ more/)
    expect(within(s).getByRole('link', { name: 'Request #188 · pending' })).toHaveAttribute('href', approvalHref('appr_188'))
    expect(within(s).getByRole('link', { name: `Question q_0123456789abcdef` })).toHaveAttribute('href', questionHref('q_0123456789abcdef'))
    // Each task's name once in what is in the way; every work task of the step is there, so no Latest task repeats one.
    // (The step's Unknown state above still names what it tested, as R5:5.5-10 says it.)
    const list = s.querySelector('[data-step-why]')!
    for (const t of ['replay: order flow suite', 'replay: fixtures refresh', 'replay: old harness']) expect(list.textContent!.split(t)).toHaveLength(2)
    expect(s).not.toHaveTextContent('Latest task')
  })

  it('3 · any one of two paths, each indented with its rows; a card with no number says closed, asked or prepared', () => {
    const w = why(viewOf(), '5.2-5')
    expect(w.word).toBe('Waits.')
    expect(w.reason).toBe('Neither of its 2 paths is open yet.')
    const any = w.list!.entries[0]
    expect(any.kind).toBe('any')
    if (any.kind !== 'any') return
    const path = any.paths.map(p => flatRows(p))
    expect(path.map(p => p.map(r => r.kind))).toEqual([['STG', 'Block', 'Card', 'Block', 'Card', 'Block', 'Card'], ['Tasks', 'Task']])
    const cards = path[0].filter(r => r.kind === 'Card').map(r => r.what)
    expect(cards).toEqual(['asked, number not recorded yet', 'prepared, not asked yet', 'closed (resolved)'])
    expect(path[0][0].sub).toBe(`${op('S')} · never admitted · budget, policy, repo_cards not explained here`)
    expect(path[0][5]).toMatchObject({ what: 'ci_red', sub: 'block ci-7 · holds every new batch of the repository', since: '2026-10-26T20:00:00Z' })
    page(viewOf())
    const s = stage('5.2-5')
    expect(s.querySelector('[data-why-any]')).toHaveTextContent(/^Any one of · 2 paths/)
    expect(s.querySelector('[data-why-count]')).toHaveTextContent('In the way · 9')
    // Nine rows are over seven: folded to the first six, all of them on path 1 (R5:5.5-20 review 3, rows not entries);
    // path 1 says its seventh row is folded, path 2 is named with its two rows.
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(6)
    expect(s.querySelectorAll('[data-why-path]')).toHaveLength(1)
    expect(s.querySelector('[data-why-path-folded]')).toHaveTextContent('1 more row on this path, under Show all')
    expect(s.querySelector('[data-why-path-hidden="2"]')).toHaveTextContent('Path 2 · 2 rows, under Show all')
    expect(s).toHaveTextContent('asked, number not recorded yet')
    expect(s).toHaveTextContent('prepared, not asked yet')
    fireEvent.click(within(s).getByRole('button', { name: /^Show all 9/ }))
    expect(s.querySelectorAll('[data-why-path]')).toHaveLength(2)
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(9)
    expect(s).toHaveTextContent('closed (resolved)')
  })
})

describe('the read (R4)', () => {
  it('4 · pending, failed, older than 90 s, the step changed, no result: each its own sentence, never nothing in the way', () => {
    const pend = why(viewOf(pending), '5.2-1')
    expect([pend.word, pend.reason, pend.list]).toEqual(['Reasons pending.', 'The read has not answered yet.', null])
    const fail = why(viewOf(failed), '5.2-1')
    expect(fail).toMatchObject({ word: 'Reasons unknown.', failed: true, list: null })
    expect(fail.reason).toMatch(/^The read failed \(input_too_large at (\w{3} \d+ )?\d\d:\d\d\)\. This does not mean nothing is in the way\.$/)
    // Older than 90 s, or of another version of the step: Unconfirmed, with when and of which version the evidence is.
    const old = why(viewOf(cardWith(c => { c.read_at = ago(EXPLAIN_OLD_MS + 30_000) })), '5.2-1')
    expect(old).toMatchObject({ word: 'Unconfirmed.', confirmed: false, list: { rows: 2 } })
    expect(old.reason).toMatch(/^Reasons are from the \d\d:\d\d read of version 7, 2 min ago \(older than 90 s\), so what is in the way now is not confirmed\. Listed below as that read had it\.$/)
    const changed = why(viewOf(card, r => { r.nodes.find(n => n.plan_node_id === 'R5:5.2-1')!.version = 8 }), '5.2-1')
    expect(changed).toMatchObject({ word: 'Unconfirmed.', version: 7, stepVersion: 8 })
    expect(changed.reason).toMatch(/^Reasons are from the \d\d:\d\d read of version 7; the step changed since \(now version 8\)/)
    const none = why(viewOf(cardWith(c => { delete (c.steps as Rec)['R5:5.2-1'] })), '5.2-1')
    expect(none).toMatchObject({ word: 'No result.', list: null })
    expect(none.reason).toMatch(/^The \d\d:\d\d read has no result for this step\.$/)
    // Never "nothing in the way" as a finding ("this does not mean nothing is in the way" is its opposite), never a count of 0.
    for (const w of [pend, fail, old, changed, none]) expect(words(w)).not.toMatch(/(^|\. |\| )nothing (is )?in the way|In the way · 0/i)

    // The module's steps say it once above them too; a failed read offers Read again (the plan view's own refetch).
    const readAgain = vi.fn()
    page(viewOf(failed), readAgain)
    expect(document.querySelector('[data-why-banner="failed"]')).toHaveTextContent(/^Could not read what the steps wait on: the read failed \(input_too_large at (\w{3} \d+ )?\d\d:\d\d\)/)
    fireEvent.click(screen.getByRole('button', { name: 'Read again' }))
    expect(readAgain).toHaveBeenCalledTimes(1)
    expect(stage('5.2-1').querySelector('[data-why-verdict]')).toHaveTextContent(/^Reasons unknown\. The read failed/)
    expect(stage('5.2-1').querySelector('[data-why-list]')).toBeNull()
    cleanup()
    page(viewOf(cardWith(c => { c.read_at = ago(EXPLAIN_OLD_MS + 30_000) })))
    expect(document.querySelector('[data-why-banner="old"]')).toHaveTextContent(/^Reasons are from \d\d:\d\d, 2 min ago\./)
    expect(stage('5.2-1').querySelector('[data-why-read="unconfirmed"]')).toHaveTextContent(/^read \d\d:\d\d · 2 min old$/)
    expect(stage('5.2-1').querySelector('[data-why-confirmed="false"]')).toHaveTextContent(/^Unconfirmed\. Reasons are from the/)
    cleanup()
    page(viewOf(pending))
    expect(document.querySelector('[data-why-banner="pending"]')).toHaveTextContent(/^Reading what each step waits on…/)
    expect(screen.queryByRole('button', { name: 'Read again' })).toBeNull()
  })
})

describe('review round 1', () => {
  it('M1 · an old or other-version read: no rule of another version beside its list, and said while a running step is folded', () => {
    page(viewOf(cardWith(c => { c.read_at = ago(EXPLAIN_OLD_MS + 30_000) }), r => { r.nodes.find(n => n.plan_node_id === 'R5:5.2-1')!.version = 8 }))
    const s1 = stage('5.2-1')
    expect(s1.querySelector('[data-why-verdict]')).toHaveTextContent(/^Unconfirmed\. Reasons are from the \d\d:\d\d read of version 7, 2 min ago/)
    expect(s1.querySelector('[data-why-done-when]')).toHaveTextContent("Done when: rule of version 7 not available (this plan view holds version 8's)")
    expect(s1.querySelector('[data-why-read]')).toHaveTextContent(/^read \d\d:\d\d · 2 min old · version 7$/)
    const s2 = stage('5.2-2')
    const fin = within(s2).getByRole('button', { name: /^To finish · 4 in the way · as of \d\d:\d\d/ })
    expect(fin).toHaveAttribute('aria-expanded', 'false')
    expect(s2.querySelector('[data-why-confirmed="false"]')).toHaveTextContent(/^Unconfirmed\. Reasons are from the/)
    expect(s2.querySelector('[data-why-row]')).toBeNull()
    for (const label of ['5.2-1', '5.2-2', '5.2-3', '5.2-5']) expect(stage(label).querySelector('[data-why-verdict]')?.textContent ?? '').not.toMatch(/^(Moving|Nobody is on it|Done by its rule)\./)
  })

  it('M2 · draws "any one of" inside a path as its own group', () => {
    const nested = cardWith(c => {
      const step = c.steps['R5:5.2-1'] as unknown as { blockers: unknown[] }
      step.blockers = [{ atom: 'one_of', branches: [[{ atom: 'one_of', branches: [[{ atom: 'step_tasks', state: 'no_task' }], [{ atom: 'step_landed', state: 'no_implementation' }]] }],
        [{ atom: 'step_stg_settled', state: 'stg_needs_landing' }]] }]
    })
    page(viewOf(nested))
    const groups = stage('5.2-1').querySelectorAll('[data-why-any]')
    expect(groups).toHaveLength(2)
    expect(groups[1].closest('[data-why-path="1"]')).not.toBeNull()
    expect(groups[1].querySelectorAll('[data-why-path]')).toHaveLength(2)
    expect(stage('5.2-1').querySelector('[data-why-count]')).toHaveTextContent('In the way · 3')
  })

  it('M4 · one total of what the server did not list, in the heading and the last line', () => {
    const more = cardWith(c => {
      const step = c.steps['R5:5.2-1'] as unknown as { blockers: unknown[]; omitted?: number }
      step.blockers = [{ atom: 'step_tasks', state: 'tasks_short', have: 0, need: 1, candidates: [{ task_id: T('B'), task_state: 'accepted' }], omitted: 4 }]
    })
    page(viewOf(more))
    expect(stage('5.2-1').querySelector('[data-why-count]')).toHaveTextContent('In the way · 2 + 4 not listed')
    expect(stage('5.2-1').querySelector('[data-why-omitted]')).toHaveTextContent('4 more not listed by the server.')
  })

  it('M6 · Latest task is the newest work; when the way names it, that line goes and older work stays earlier', () => {
    const declinedT3 = { atom: 'step_landed', state: 'landing_declined', task_id: T3, op: `land:${T3}:1`, rework: { seen: null, not_asked: false, card: null } }
    const r = r5Raw()
    const t3 = r.tasks.find(t => t.task_id === T3)!
    Object.assign(t3, { state: 'accepted', landing: landing(`land:${T3}:1`, 'failed', AT, 'declined') })
    const v = splitPlanExplain({ ...r, explain: { status: 'ok', read_at: AT, steps: { 'R5:5.1-3': { node_version: r.nodes.find(n => n.plan_node_id === 'R5:5.1-3')!.version,
      kind: 'explained', rule: 'false', blockers: [declinedT3] } } } }, 'R5')
    const opened = moduleOf(v, 'R5:5.1')!
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>
      <PlanModuleDetail view={v} item={opened.item} step="" stale={false} /></NowClock></PlatformAuthContext.Provider></QueryClientProvider>)
    const s = stage('5.1-3')
    expect(s.querySelector('[data-why-row="Landing"] [data-why-what]')).toHaveTextContent('Module detail page')
    expect([...s.querySelectorAll('dt')].map(dt => dt.textContent)).not.toContain('Latest task')
    expect(s).toHaveTextContent(/Earlier own work.*First cut of the page/)
    // Review is of the newest work, the task named in the way (t_…F0WBP3).
    const review = [...s.querySelectorAll('dt')].find(dt => dt.textContent === 'Review')!.nextElementSibling!
    expect(review).toHaveTextContent(/of t_…F0WBP3/)
    expect(s.textContent!.split('Module detail page')).toHaveLength(2)
  })
})

describe('review round 3: folding counts rows inside every group', () => {
  /** A step whose blockers are these, on 5.2-1; candidates on tasks nobody else names, so none is folded into another. */
  const withBlockers = (blockers: unknown[]) => viewOf(cardWith(c => { (c.steps['R5:5.2-1'] as unknown as { blockers: unknown[] }).blockers = blockers }))
  const short = (prefix: string, n: number) => ({ atom: 'step_tasks', state: 'tasks_short', have: 0, need: n,
    candidates: Array.from({ length: n }, (_, i) => ({ task_id: `t_${prefix}${i}`, task_state: 'ready' })) })

  it('folds one "any one of" of 15 rows on two paths to its first 6 rows, its heading and path titles kept; Show all 15 shows them all', () => {
    // Path 1: a tasks line and 7 candidates (8 rows); path 2: a tasks line and 6 (7 rows).
    const v = withBlockers([{ atom: 'one_of', branches: [[short('a', 7)], [short('b', 6)]] }])
    expect(why(v, '5.2-1').list).toMatchObject({ rows: 15, omitted: 0 })
    page(v)
    const s = stage('5.2-1')
    expect(s.querySelector('[data-why-count]')).toHaveTextContent('In the way · 15')
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(6)
    expect(s.querySelector('[data-why-any]')).toHaveTextContent(/^Any one of · 2 paths/)
    const path1 = s.querySelector('[data-why-path="1"]')!
    expect(path1).toHaveTextContent(/^Path 1/)
    expect(path1.querySelectorAll('[data-why-row]')).toHaveLength(6)
    expect(path1.querySelector('[data-why-path-folded]')).toHaveTextContent('2 more rows on this path, under Show all')
    expect(s.querySelector('[data-why-path-hidden="2"]')).toHaveTextContent('Path 2 · 7 rows, under Show all')
    const all = within(s).getByRole('button', { name: /^Show all 15/ })
    expect(all).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(all)
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(15)
    expect([...s.querySelectorAll('[data-why-path]')].map(p => [p.getAttribute('data-why-path'), p.querySelectorAll('[data-why-row]').length])).toEqual([['1', 8], ['2', 7]])
    expect(s.querySelector('[data-why-path-folded], [data-why-path-hidden]')).toBeNull()
    expect(within(s).getByRole('button', { name: 'Show fewer' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('counts rows at every depth: a fold inside a nested group keeps every heading and path title above it', () => {
    // Outer path 1 holds a group of 4 + 4 rows; outer path 2 has 3: 11 rows, the sixth on the inner path 2.
    const v = withBlockers([{ atom: 'one_of', branches: [[{ atom: 'one_of', branches: [[short('c', 3)], [short('d', 3)]] }], [short('e', 2)]] }])
    page(v)
    const s = stage('5.2-1')
    expect(s.querySelector('[data-why-count]')).toHaveTextContent('In the way · 11')
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(6)
    const [outer, inner] = [...s.querySelectorAll('[data-why-any]')]
    expect(outer).toHaveTextContent(/^Any one of · 2 paths/)
    expect(inner.closest('[data-why-path="1"]')).toBe(outer.querySelector('[data-why-path="1"]'))
    expect([...inner.querySelectorAll('[data-why-path]')].map(p => p.querySelectorAll('[data-why-row]').length)).toEqual([4, 2])
    expect(inner.querySelector('[data-why-path="2"] [data-why-path-folded]')).toHaveTextContent('2 more rows on this path, under Show all')
    expect(outer.querySelector('[data-why-path-hidden="2"]')).toHaveTextContent('Path 2 · 3 rows, under Show all')
    fireEvent.click(within(s).getByRole('button', { name: /^Show all 11/ }))
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(11)
  })

  it('does not fold seven rows, in a group or not', () => {
    page(withBlockers([{ atom: 'one_of', branches: [[short('f', 3)], [short('g', 2)]] }]))
    expect(stage('5.2-1').querySelectorAll('[data-why-row]')).toHaveLength(7)
    expect(within(stage('5.2-1')).queryByRole('button', { name: /^Show all/ })).toBeNull()
  })
})

describe('review round 2: a record several tasks wait on', () => {
  it('lists the repository block two tasks wait on, its cards and what the server left out of them once, saying which tasks', () => {
    const w = why(viewOf(shared), '5.2-1')
    const r = rows(w)
    // 6 rows: each task's STG blocker, the shared block, its 3 cards; 2 not listed: the block's 2 other cards, once.
    expect(r.map(x => x.kind)).toEqual(['STG', 'Block', 'Card', 'Card', 'Card', 'STG'])
    expect(w.list).toMatchObject({ rows: 6, omitted: 2 })
    expect(r.map(x => x.task ?? '')).toEqual(['t_a', '', '', '', '', 't_b'])
    expect(r[1].sub).toBe('block shared · holds every new batch of the repository · in the way of t_a, t_b')
    expect(r.filter(x => x.kind === 'Card').map(x => x.sub)).toEqual(['p5 · question number empty', 'p4 · question number empty', 'p3 · question number empty · 2 older not listed by the server'])
    expect(r[5].sub).toBe('land:t_b:1 · never admitted · budget, policy, repo_cards not explained here · held by block shared, listed above')
    // The conclusion counts the same way: both blockers named, nothing more.
    expect([w.word, w.reason]).toEqual(['Waits.', 'STG has not admitted it (ci_red).'])

    page(viewOf(shared))
    const s1 = stage('5.2-1')
    expect(s1.querySelector('[data-why-count]')).toHaveTextContent('In the way · 6 + 2 not listed')
    expect(s1.querySelector('[data-why-omitted]')).toHaveTextContent('2 more not listed by the server.')
    expect(s1.querySelectorAll('[data-why-row]')).toHaveLength(6)
    expect(s1.querySelectorAll('[data-why-row="Block"]')).toHaveLength(1)
    for (const prep of ['p3', 'p4', 'p5']) expect([...s1.querySelectorAll('[data-why-row="Card"] [data-why-sub]')].filter(e => e.textContent!.startsWith(`${prep} `))).toHaveLength(1)
  })

  it('counts a members\' own block list and a card set that two blockers share once', () => {
    const block = { id: 'b-1', kind: 'lane_held', since: '2026-10-27T01:00:00Z', trigger_seq: 7,
      cards: [{ question_id: null, prep: 'c1', kind: 'stg_blocked', family: 'stg', ask_started: true, closed: false, closed_why: null }], cards_omitted: 1 }
    const failedCard = { question_id: null, prep: 'f1', kind: 'stg_failed', family: 'stg', ask_started: false, closed: false, closed_why: null }
    const held = (task: string) => ({ atom: 'step_stg_settled', state: 'stg_exhausted', task_id: task, op: `land:${task}:1`, cards: [failedCard], cards_omitted: 2,
      blocks: [block], blocks_omitted: 3 })
    const w = why(viewOf(cardWith(c => { (c.steps['R5:5.2-1'] as unknown as { blockers: unknown[] }).blockers = [held('t_a'), held('t_b')] })), '5.2-1')
    expect(rows(w).map(x => x.kind)).toEqual(['STG', 'Card', 'Block', 'Card', 'STG'])
    // Not listed: the failure card set's 2, the block's 1 card, the member list's 3 blocks: each once.
    expect(w.list).toMatchObject({ rows: 5, omitted: 6 })
    expect(rows(w)[2].sub).toBe('block b-1 · in the way of t_a, t_b · 3 more open blocks not listed by the server')
  })
})

describe('a step with work running (R7) and how a state was read (R6)', () => {
  it('5 · keeps the live state on top and folds what is left into one line at its bottom', () => {
    const c = cellOf(viewOf(), '5.2-2')
    expect(c.now?.kind).toBe('running')
    expect(c.why).toMatchObject({ word: 'Moving.', list: { rows: 4 } })
    page(viewOf())
    const s = stage('5.2-2')
    expect(s.querySelector('[data-step-now="running"]')).toHaveTextContent(/^In progress · running now: /)
    const fin = within(s).getByRole('button', { name: /^To finish · 4 in the way/ })
    expect(fin).toHaveAttribute('aria-expanded', 'false')
    expect(s.querySelector('[data-why-row]')).toBeNull()
    expect(s.querySelector('[data-why-verdict]')).toBeNull()
    // Its rows folded, Latest task still names the task at work (once).
    expect(s).toHaveTextContent('Latest task')
    expect(s.textContent!.split('nervous system: event bus, attempt 2')).toHaveLength(2)
    // At the bottom: after the live state, the step's work and its review.
    const review = [...s.querySelectorAll('dt')].find(dt => dt.textContent === 'Review')!
    expect(review.compareDocumentPosition(fin) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(fin)
    expect(s.querySelectorAll('[data-why-row]')).toHaveLength(4)
    expect(s.querySelector('[data-why-verdict]')).toHaveTextContent(/^Moving\. 0 of 1 tasks are accepted; no implementation task is on it; 1 more in the way\.$/)
    expect(s.querySelector('[data-why-push="moving"]')).toHaveTextContent('Moving · heartbeat 13 s')
  })

  it('folds how an idle step was read into four rows: Running, Queued, Waits on you, Held', () => {
    page(viewOf())
    const s = stage('5.2-1')
    expect(s).not.toHaveTextContent(/nothing running now, read now/)
    const how = within(s).getByRole('button', { name: /^How “Active · nothing running now” was read · \d\d:\d\d/ })
    expect(s.querySelector('[data-read-row]')).toBeNull()
    fireEvent.click(how)
    expect([...s.querySelectorAll('[data-read-row] dt')].map(dt => dt.textContent)).toEqual(['Running', 'Queued', 'Waits on you', 'Held'])
    expect(s.querySelector('[data-read-row="Running"] dd')).toHaveTextContent(/^Nothing: no running attempt with its heartbeat confirmed/)
  })
})

describe('the round\'s module rows', () => {
  it('give an open step one grey line with the same conclusion and a link to its step card', () => {
    render(<NowClock><PlanModuleRows view={viewOf()} selected="" /></NowClock>)
    fireEvent.click(screen.getByRole('button', { name: /^Step 5\.2-1: active, nothing running now/ }))
    const detail = screen.getByRole('region', { name: 'Step 5.2-1' })
    const line = detail.querySelector('[data-step-why="compact"]')!
    expect(line.querySelector('[data-why-verdict]')).toHaveTextContent(/^Nobody is on it\. Its landing was declined/)
    expect(line.querySelector('[data-why-row]')).toBeNull()
    expect(within(detail).getByRole('link', { name: 'In the way · 2 on the step card' })).toHaveAttribute('href', planNodeHref('R5', 'R5:5.2-1'))
  })
})

describe('a phone\'s width (R8)', () => {
  it('6 · every row wraps: no fixed width beyond the kind label, no sideways scroll, long ids break', () => {
    page(viewOf())
    fireEvent.click(within(stage('5.2-3')).getByRole('button', { name: /^Show all 15/ }))
    const block = [...document.querySelectorAll<HTMLElement>('[data-step-why]')]
    expect(block.length).toBeGreaterThan(2)
    const all = block.flatMap(b => [b, ...b.querySelectorAll<HTMLElement>('*')])
    for (const el of all) {
      const cls = el.getAttribute('class') ?? ''
      expect(cls).not.toMatch(/overflow-x|(^|\s)(w|min-w)-\[(?!0)|(^|\s)w-(\d{2,}|screen)|(^|\s)min-w-(?!0)\w/)
      // Only the short pills and times keep to one line.
      if (/whitespace-nowrap/.test(cls)) expect(el.matches('[data-why-push], [data-why-since], dt')).toBe(true)
    }
    for (const row of document.querySelectorAll<HTMLElement>('[data-why-row]')) {
      expect(row.className).toMatch(/grid-cols-\[3\.625rem_minmax\(0,1fr\)\]/)
      expect(row.className).toMatch(/(^|\s)min-w-0(\s|$)/)
      expect(row.children[1].className).toMatch(/flex-wrap/)
      expect(row.querySelector('[data-why-sub]')!.className).toMatch(/\[overflow-wrap:anywhere\]/)
    }
  })
})
