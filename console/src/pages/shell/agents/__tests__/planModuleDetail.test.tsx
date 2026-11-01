import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import cards from '@/api/__fixtures__/question-cards.testdb.json'
import { parseRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { ApprovalItem } from '@/api/approvals'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { parseQuestion, type Question } from '@/api/questions'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { approvalHref, goalHref, NEEDS_YOU_HREF, questionHref } from '@/lib/shell/consoleRoutes'
import { NowClock } from '../../progress/progressClock'
import { PlanModuleDetail } from '../PlanModuleDetail'
import { PlanModuleRows } from '../PlanModuleRows'
import { WorkPlanView } from '../WorkPlanView'
import { actionsLeft, moduleOf, moduleScopes, needsNow, planActivity, type ShellReads } from '../moduleDetailModel'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// Runtime task ids (ULIDs, so their order is the order they were made).
const id = (c: string) => `t_01M69PWCTHGN75N160E9F0WBP${c}`
const T1 = id('1'), T1R = id('A'), T2 = id('2'), T3 = id('3'), T4 = id('4'), T5 = id('5'), T6 = id('6'), T7 = id('7'), T_OTHER = id('9')
const Q_OPEN = 'q_00000000000000a1', Q_RULE = 'q_00000000000000b2', Q_ANSWERED = 'q_00000000000000c3', Q_OTHER = 'q_00000000000000d4'
const Q_LOOSE = 'q_00000000000000e5', Q_ADD = 'q_00000000000000f6', Q_GONE = 'q_0000000000000107', Q_MISSION = 'q_0000000000000108', Q_CHANGE = 'q_0000000000000109'
const Q_BLOCKED = 'q_000000000000010a'
const AT = prod.as_of
const FAR = '2099-01-01T00:00:00Z'

type Raw = { root: string; nodes: Record<string, unknown>[]; tasks: Record<string, unknown>[]; recent_changes: Record<string, unknown>[] }

/**
 * An R5 plan-view answer in the real PROD shape: the PROD R4 response (2026-10-27) with every id, label and stream moved
 * to round 5 (R4:4.1-3 → R5:5.1-3), then advanced as the controller moves it. 5.1-1 done: its implementation T1 and the
 * review that passed it (T1R, "review <T1>", on the same step as stepevidence.go has it). 5.1-2 done by request #6:
 * work T4 whose role the runtime no longer lists and its review T7, ended before the runtime's latest 20. 5.1-3 active:
 * an older cancelled task T2, the implementation T3 back to ready in round 2 of 3 after its verification T5 failed (T5 is
 * accepted all the same: "result used: fail"). 5.1-5 is the Owner's step (as on PROD), its rule waits on a question.
 * 5.1-6 holds T6, whose role nothing read says.
 */
function r5Raw(): Raw {
  const text = JSON.stringify(prod).replace(/"R4"/g, '"R5"').replace(/R4:4\./g, 'R5:5.').replace(/"4\.(\d+(?:-\d+)?)"/g, '"5.$1"').replace(/plan:R4/g, 'plan:R5')
  const raw = JSON.parse(text) as Raw
  const node = (nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
  const set = (nid: string, state: string, rule: Record<string, unknown>, event: number | null) => Object.assign(node(nid), { state, done_rule_json: rule, state_event_id: event })
  set('R5:5.1-1', 'done', { step_tasks: { state: 'accepted', min: 1 } }, 9001)
  set('R5:5.1-2', 'done', { approval_executed: 6 }, 9002)
  set('R5:5.1-3', 'active', { step_tasks: { state: 'accepted', min: 1 } }, 9003)
  set('R5:5.1-5', 'planned', { question_answered: { question: Q_RULE, option: 'a' } }, null)
  Object.assign(node('R5:5.1'), { execution_progress: { state: 'active', done: 2, total: 6, stopped: 0, basis: 'Existing controller step states; not delivery verification' } })
  const task = (task_id: string, plan_node_id: string, mission_id: string, title: string, state: string, more: Record<string, unknown> = {}) =>
    ({ task_id, plan_node_id, mission_id, title, state, ...more })
  raw.tasks = [
    task(T1, 'R5:5.1-1', 'M-250', 'Module detail model', 'accepted', { delivery: 'Accepted · landed on main' }),
    task(T1R, 'R5:5.1-1', 'M-250', `review ${T1}`, 'accepted'),
    task(T4, 'R5:5.1-2', 'M-253', 'Release the module rows', 'accepted'),
    task(T7, 'R5:5.1-2', 'M-253', `review ${T4}`, 'accepted'),
    task(T2, 'R5:5.1-3', 'M-251', 'First cut of the page', 'cancelled'),
    task(T3, 'R5:5.1-3', 'M-251', 'Module detail page', 'ready'),
    task(T5, 'R5:5.1-3', 'M-251', `verify ${T3}`, 'accepted'),
    task(T6, 'R5:5.1-6', 'M-252', 'Draft the acceptance notes', 'accepted'),
    task(T_OTHER, 'R5:5.2-1', 'M-260', 'Release lanes', 'running'),
  ]
  const ev = (event_id: number, event_type: string, body_json: Record<string, unknown>, at: string, stream = 'plan:R5') =>
    ({ event_id, event_stream_id: stream, seq: event_id, event_type, actor_id: 'controller', at, body_json })
  raw.recent_changes = [
    ev(9003, 'plan_node_state_changed', { from: 'planned', to: 'active', cause: 'its task was leased' }, '2026-10-27T02:40:00Z'),
    ev(9100, 'plan_node_state_changed', { from: 'planned', to: 'active', cause: 'not this module' }, '2026-10-27T02:39:00Z'),
    ev(9101, 'note', { text: 'Landing claimed (dev)', landing: { v: 1, kind: 'claimed', op: `land:${T1}:1`, task: T1 } }, '2026-10-27T02:30:00Z', 'landing:bifrost-platform'),
    ev(9102, 'note', { text: 'Landed on main', landing: { v: 1, kind: 'landed', op: `land:${T1}:1` } }, '2026-10-27T02:31:00Z', 'landing:bifrost-platform'),
    ev(9103, 'plan_node_created', { label: '5.2-1', title: 'Another module', ordinal: 0, node_kind: 'step' }, '2026-10-27T02:20:00Z'),
    ev(9104, 'plan_node_created', { label: '5.1-6', title: '验收', ordinal: 5, node_kind: 'step' }, '2026-10-27T02:10:00Z'),
    // An added step names its parent, not a node (planchange.go add_node).
    ev(9105, 'plan_change_proposed', { changes: [{ op: 'add_node', parent: 'R5:5.1', kind: 'step', title: 'Retire the board' }], reason: 'Add a step to 5.1', standing: false, question_id: Q_ADD }, '2026-10-27T02:05:00Z'),
    ev(9106, 'plan_change_decided', { proposal_event_id: 9105, decision: 'applied', by: 'owner', question_id: Q_ADD }, '2026-10-27T02:06:00Z'),
    ev(9107, 'plan_changed', { change: 'add_node', decided_event_id: 9106, nodes: ['R5:5.1-6'] }, '2026-10-27T02:07:00Z'),
    // A decision whose proposal is older than the window, and the change it applied: nothing read ties them to a module.
    ev(9108, 'plan_change_decided', { proposal_event_id: 50, decision: 'applied', by: 'owner', question_id: Q_GONE }, '2026-10-27T01:50:00Z'),
    ev(9109, 'plan_changed', { change: 'retitle', decided_event_id: 9108 }, '2026-10-27T01:51:00Z'),
  ]
  return raw
}
const r5 = (): PlanSnapshot => parsePlanSnapshot(r5Raw(), 'R5')

function question(qid: string, patch: Partial<Question> & Record<string, unknown>): Question {
  // A key set to undefined is left out, as the server omits it.
  const body = Object.fromEntries(Object.entries({ ...cards.plan_question.body, id: qid, ...patch }).filter(([, v]) => v !== undefined))
  const parsed = parseQuestion(body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}
const answer = (at: string) => ({ option: 'a', by: 'owner', at, channel: 'console', approval: false })
const questions = (): Question[] => [
  question(Q_OPEN, { number: 11, task_id: T3, title: 'Keep the step table in plan order?', expires_at: FAR }),
  question(Q_RULE, { number: 12, task_id: undefined, title: 'Sign the module detail', expires_at: FAR }),
  question(Q_ANSWERED, { number: 13, task_id: T1, status: 'answered', title: 'Land the model first?', answer: answer('2026-10-27T01:00:00Z') }),
  question(Q_OTHER, { number: 15, task_id: T_OTHER, title: 'Another module', expires_at: FAR }),
  question(Q_LOOSE, { number: 16, task_id: undefined, title: 'Rename the Console?', expires_at: FAR }),
  question(Q_ADD, { number: 14, task_id: undefined, status: 'answered', title: 'Add a step to 5.1?', created_at: '2026-10-27T02:04:00Z', answer: answer('2026-10-27T02:05:30Z') }),
]
const openRequests = (): ApprovalItem[] => [
  buildPlatformApproval({ id: 'apr-7', number: 7, action: 'gitops_sync_app', summary: 'Sync the STG console', params: { task: T3 }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-8', number: 8, action: 'approve_guardrail_landing', summary: 'Land T3', params: { task: T3, op: `land:${T3}:1` }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-9', number: 9, summary: 'Not this module', params: { task: T_OTHER }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-12', number: 12, summary: 'Rotate a node', params: { node: 'n7' }, expires_at: FAR }),
]
const allRequests = (): ApprovalItem[] => [
  ...openRequests(),
  buildPlatformApproval({ id: 'apr-6', number: 6, status: 'approved', summary: 'Release 5.1-2', created_at: '2026-10-26T20:00:00Z', decided_at: '2026-10-26T21:00:00Z', expires_at: FAR }),
]
type RawProgress = { work: { missions: { tasks: Record<string, unknown>[] }[] }; recent: { tasks: Record<string, unknown>[] }; needs_you: Record<string, unknown> }
function progress(edit: (raw: RawProgress) => void = () => {}): RuntimeProgress {
  const raw = structuredClone(progressFixture) as unknown as RawProgress
  Object.assign(raw.work.missions[0].tasks[0], { task_id: T3, title: 'Module detail page', role: 'implement', round_no: 2, max_rounds: 3, state: 'ready' })
  const ended = (task_id: string, title: string, role: string, conclusion: string, ended_at: string, mission_id: string) =>
    ({ task_id, mission_id, title, role, state: 'accepted', ended_at, ended_event_id: 77, conclusion })
  raw.recent.tasks = [
    ended(T5, `verify ${T3}`, 'verify', 'failed', '2026-10-27T02:35:00Z', 'M-251'),
    ended(T1, 'Module detail model', 'implement', 'done', '2026-10-27T00:41:00Z', 'M-250'),
    ended(T1R, `review ${T1}`, 'review', 'done', '2026-10-27T00:40:00Z', 'M-250'),
    ...raw.recent.tasks,
  ]
  raw.needs_you = { count: 0, items: [], truncated: false }
  edit(raw)
  return parseRuntimeProgress(raw)
}

let client: QueryClient
let fetcher: ReturnType<typeof vi.fn>
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
/** The shell's Needs-you reads, as ConsolePage's useNeedsYou leaves them in the cache. */
function seedShell(o: { questions?: Question[]; progress?: RuntimeProgress } = {}) {
  client.setQueryData(['questions', 'all'], o.questions ?? questions())
  client.setQueryData(['approvals', 'open'], openRequests())
  client.setQueryData(['approvals', 'all'], allRequests())
  client.setQueryData(['agent-runtime-progress'], o.progress ?? progress())
}
const ok = <T,>(data: T) => ({ state: 'ok' as const, data, at: Date.parse(AT) })
const okReads = (o: Partial<ShellReads> = {}): ShellReads => ({ questions: ok(questions()), open: ok(openRequests()), all: ok(allRequests()), progress: ok(progress()), ...o })
function wrap(children: ReactNode) {
  return <QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>{children}</NowClock></PlatformAuthContext.Provider></QueryClientProvider>
}
function detail(view = r5(), node = 'R5:5.1') {
  const opened = moduleOf(view, node)!
  return render(wrap(<PlanModuleDetail view={view} item={opened.item} step={opened.step} stale={false} />))
}
function page(hash: string) {
  window.location.hash = hash
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
}
const urls = () => fetcher.mock.calls.map(([u]) => String(u))
const region = (name: string) => screen.getByRole('region', { name })
const steps = () => within(region('Steps of 5.1')).getAllByRole('listitem')
const step = (label: string) => steps().find(li => li.dataset.stepRow === label)!

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(AT))
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  fetcher = vi.fn(async (input: RequestInfo | URL) => String(input).includes('/agentrt/plan-view?root=R5')
    ? new Response(JSON.stringify(r5Raw()), { status: 200 }) : new Response('{}', { status: 599 }))
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('module detail model', () => {
  it('opens the module an address names, by item or by one of its steps, and nothing for the round', () => {
    const view = r5()
    expect(moduleOf(view, 'R5:5.1')).toMatchObject({ item: { plan_node_id: 'R5:5.1' }, step: '' })
    expect(moduleOf(view, '5.1-3')).toMatchObject({ item: { plan_node_id: 'R5:5.1' }, step: 'R5:5.1-3' })
    expect(moduleOf(view, 'R5')).toBeNull()
    expect(moduleOf(view, '')).toBeNull()
  })

  it('counts each counted item once and each Owner step with nothing counted once; a stopped Owner step apart; a landing request not', () => {
    const raw = r5Raw()
    Object.assign(raw.nodes.find(n => n.plan_node_id === 'R5:5.1-4')!, { owner_step: true, state: 'stopped' })
    Object.assign(raw.nodes.find(n => n.plan_node_id === 'R5:5.1-6')!, { owner_step: true })
    const view = parsePlanSnapshot(raw, 'R5')
    const scopes = moduleScopes(view)
    const scope = scopes.get('R5:5.1')!
    const needs = needsNow(view, scopes, scope, okReads(), Date.parse(AT))
    expect(needs.items.map(n => [n.key, n.step, n.counted])).toEqual([
      [`question-${Q_OPEN}`, 'R5:5.1-3', true], [`question-${Q_RULE}`, 'R5:5.1-5', true], ['approval-apr-7', 'R5:5.1-3', true], ['approval-apr-8', 'R5:5.1-3', false]])
    // 5.1-5 has its question counted, so it is that one item; 5.1-6 has nothing counted, so it counts once on its own.
    expect(actionsLeft(scope, needs)).toEqual({ total: 4, items: 3, notCounted: 1, ownerSteps: 2, bareOwnerSteps: 1, stopped: 1, floor: false })
    expect(needs).not.toHaveProperty('untied')
    const unread = needsNow(view, scopes, scope, okReads({ questions: { state: 'unknown', reason: 'HTTP 503' } }), Date.parse(AT))
    expect(actionsLeft(scope, unread)).toMatchObject({ total: 3, items: 1, floor: true })
    expect(unread.gaps).toEqual(['Questions Unknown · HTTP 503'])
  })

  it('ties plan changes by the nodes a proposal touches, an added node\'s parent, the nodes a change added, and counts nothing else', () => {
    const view = r5()
    const scopes = moduleScopes(view)
    const { rows } = planActivity(view, scopes.get('R5:5.1')!, scopes, new Map())
    expect(rows.map(r => [r.kind, r.text])).toEqual([
      ['Step', '5.1-3 · Not started → In progress · its task was leased'],
      ['Landing', 'Landing claimed (dev) · Module detail model'],
      ['Landing', 'Landed on main · Module detail model'],
      ['Created', '5.1-6 · 验收'],
      ['Plan change', 'Proposed · Add a step to 5.1'],
      ['Plan change', 'Applied by owner'],
      ['Plan change', 'Applied · add_node'],
    ])
    // 9108/9109 (a decision whose proposal is older than the window, and its change) name nothing of 5.1: not shown, not counted.
    expect(rows.some(r => r.key === 'plan-9108' || r.key === 'plan-9109')).toBe(false)
  })
})

describe('plan changes at the window edge', () => {
  it('shows a change whose proposal is outside the window by its decision and the nodes it added, and gives no count', () => {
    const raw = r5Raw()
    raw.recent_changes = raw.recent_changes.filter(e => ![9105, 9106, 9107, 9108, 9109].includes(e.event_id as number))
    const ev = (event_id: number, event_type: string, body_json: Record<string, unknown>, at: string) =>
      ({ event_id, event_stream_id: 'plan:R5', seq: event_id, event_type, actor_id: 'controller', at, body_json })
    // The add_node proposal (event 40) is older than the window; its decision and the change it applied are inside.
    raw.recent_changes.push(ev(9120, 'plan_change_decided', { proposal_event_id: 40, decision: 'applied', by: 'owner', question_id: Q_GONE }, '2026-10-27T02:12:00Z'),
      ev(9121, 'plan_changed', { change: 'add_node', decided_event_id: 9120, nodes: ['R5:5.1-6'] }, '2026-10-27T02:13:00Z'),
      // A proposal still waiting, in the window, that touches no node of any module (only the round).
      ev(9122, 'plan_change_proposed', { changes: [{ op: 'retitle', node: 'R5', title: 'Round 5' }], reason: 'Retitle the round', standing: false, question_id: Q_CHANGE }, '2026-10-27T02:14:00Z'))
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    const activity = region('Recent activity on 5.1')
    fireEvent.click(within(activity).getByRole('button', { name: /^Show all/ }))
    const rows = within(activity).getAllByRole('listitem').map(li => li.textContent)
    // The decision is shown with the change it applied: one group, joined by decided_event_id.
    expect(rows).toContain('2026-10-27T02:13:00ZPlan changeApplied · add_node')
    expect(rows).toContain('2026-10-27T02:12:00ZPlan changeApplied by owner')
    expect(rows.some(r => r?.includes('Retitle the round'))).toBe(false)
    expect(activity).not.toHaveTextContent(/\d+ plan changes? there/)
    expect(activity).not.toHaveTextContent('could not be tied')
  })
})

describe('plan change groups', () => {
  it('puts an event whose proposal or decision id is missing in no group: it stands on its own evidence', () => {
    const raw = r5Raw()
    const ev = (event_id: number, event_type: string, body_json: Record<string, unknown>, at: string) =>
      ({ event_id, event_stream_id: 'plan:R5', seq: event_id, event_type, actor_id: 'controller', at, body_json })
    // Two changes with no decided_event_id: one adds a node of 5.1, the other retitles a node of nothing named.
    raw.recent_changes.push(ev(9130, 'plan_changed', { change: 'add_node', nodes: ['R5:5.1-6'] }, '2026-10-27T02:21:00Z'),
      ev(9131, 'plan_changed', { change: 'retitle' }, '2026-10-27T02:22:00Z'),
      ev(9132, 'plan_change_decided', { decision: 'applied', by: 'owner' }, '2026-10-27T02:23:00Z'))
    const view = parsePlanSnapshot(raw, 'R5')
    const scopes = moduleScopes(view)
    const keys = planActivity(view, scopes.get('R5:5.1')!, scopes, new Map()).rows.map(r => r.key)
    expect(keys).toContain('plan-9130')
    expect(keys).not.toContain('plan-9131')
    expect(keys).not.toContain('plan-9132')
  })
})

describe('module detail on the page', () => {
  it('shows a normal module: steps to go, the step\'s own work now with its round, next in plan order and ETA Unknown', () => {
    seedShell()
    detail()
    expect(region('Steps to go')).toHaveTextContent('4 steps to goof 6 · 2 done · 1 in progress')
    expect(region('Steps to go')).toHaveTextContent('Execution progress from the controller\'s step states. Not acceptance, not deployment.')
    const now = region('Now')
    expect(now).toHaveTextContent('Implement · Module detail page · ready · round 2 of 3')
    expect(now).toHaveTextContent("Role Unknown (not in the runtime's lists) · First cut of the page · cancelled")
    expect(within(now).getByRole('link', { name: 'Module detail page' })).toHaveAttribute('href', goalHref('M-251', T3))
    // B1: the failed verification is a check, not the step's work.
    expect(now).not.toHaveTextContent('verify')
    expect(region('Next in plan order')).toHaveTextContent('5.1-4')
    expect(region('Next in plan order')).toHaveTextContent('Plan order only, not a commitment.')
    expect(region('ETA')).toHaveTextContent(`UnknownCapacity and duration observations are not availableas of ${AT}`)
    expect(steps().map(li => li.dataset.stepRow)).toEqual(['5.1-1', '5.1-2', '5.1-3', '5.1-4', '5.1-5', '5.1-6'])
    expect(steps().map(li => li.querySelector('[data-step-state]')?.textContent)).toEqual(['Done', 'Done', 'In progress', 'Not started', 'Not started', 'Not started'])
    expect(step('5.1-4')).toHaveTextContent('Latest taskNone')
    expect(step('5.1-4')).toHaveTextContent('ReviewNo task yet')
    expect(step('5.1-5')).toHaveTextContent('Owner')
    expect(step('5.1-4')).toHaveTextContent('Agent')
    const crumbs = within(region('Module 5.1')).getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(crumbs).getByRole('link', { name: 'Back to Work' })).toHaveAttribute('href', '#work?root=R5')
    expect(within(crumbs).getByRole('link', { name: 'Round 5' })).toHaveAttribute('href', '#work?root=R5')
  })

  it('B1: shows the step\'s own work as its latest task and the latest check\'s verdict as its review, never a failed check as Accepted', () => {
    seedShell()
    detail()
    // 5.1-3: T5 is the newest task and accepted, but it is the verification that failed T3.
    expect(step('5.1-3')).toHaveTextContent('Latest taskImplement · Module detail page · ready · round 2 of 3 · 1 earlier · 1 check')
    expect(step('5.1-3')).toHaveTextContent('ReviewVerify fail of t_…F0WBP3')
    expect(step('5.1-3')).not.toHaveTextContent('Accepted')
    expect(step('5.1-1')).toHaveTextContent('Latest taskImplement · Module detail model · accepted · Accepted · landed on main · 1 check')
    expect(step('5.1-1')).toHaveTextContent('ReviewReview pass of t_…F0WBP1')
    // A check the runtime (read) does not list has no verdict here, and says that; a task whose role nothing says is marked so.
    expect(step('5.1-2')).toHaveTextContent("Latest taskRole Unknown (not in the runtime's lists) · Release the module rows · accepted · 1 check")
    expect(step('5.1-2')).toHaveTextContent("ReviewReview of t_…F0WBP4: verdict Unknown (not among the runtime's latest 20 ended tasks)")
    expect(step('5.1-6')).toHaveTextContent("Latest taskRole Unknown (not in the runtime's lists) · Draft the acceptance notes · accepted")
    expect(step('5.1-6')).toHaveTextContent('ReviewUnknown: 1 task role not in what was read')
    fireEvent.click(within(region('Recent activity on 5.1')).getByRole('button', { name: /^Show all/ }))
    const activity = within(region('Recent activity on 5.1')).getAllByRole('listitem').map(li => li.textContent)
    expect(activity).toContain('2026-10-27T02:35:00ZCheck5.1-3 · Verify fail · Module detail page')
    expect(activity).toContain('2026-10-27T00:40:00ZCheck5.1-1 · Review pass · Module detail model')
    expect(activity).toContain('2026-10-27T00:41:00ZTask5.1-1 · Implement accepted · Module detail model')
    expect(activity.some(a => a?.includes('Accepted · verify'))).toBe(false)
  })

  it('says what was found for the Owner and what needs him now, with links to each card and request', () => {
    seedShell()
    detail()
    // What it found, not what is left: a card without evidence here is not counted, and the tile says so whether or not a list is unread.
    expect(screen.queryByText(/actions left/i)).toBeNull()
    expect(region('Module 5.1')).not.toHaveTextContent(/actions left/i)
    expect(region('Your actions found')).toHaveTextContent('Owner steps not done, plus what Needs you now counts; cards without evidence here are not counted (see Needs you).')
    expect(region('Your actions found')).not.toHaveTextContent('At least')
    expect(region('Your actions found').querySelector('[data-actions-found]')).toHaveTextContent(/^3$/)
    expect(region('Your actions found')).toHaveTextContent('33 counted under Needs you now · 1 listed, not counted1 Owner step not done, 0 with nothing counted')
    const needs = region('Needs you now')
    expect(within(needs).getByRole('link', { name: /Keep the step table in plan order/ })).toHaveAttribute('href', questionHref(Q_OPEN))
    expect(within(needs).getByRole('link', { name: /Sign the module detail/ })).toHaveAttribute('href', questionHref(Q_RULE))
    expect(within(needs).getByRole('link', { name: 'Request #7 · Sync the STG console' })).toHaveAttribute('href', approvalHref('apr-7'))
    expect(within(needs).getByRole('link', { name: 'Request #8 · Land T3' }).closest('li')).toHaveTextContent('confirmed on Needs you, not here · not counted')
    expect(needs).not.toHaveTextContent('Another module')
    expect(within(needs).getByRole('link', { name: 'Cards with no evidence here for a module of R5 are on Needs you' })).toHaveAttribute('href', NEEDS_YOU_HREF)
  })

  it('B2: a card counts for a module only on direct evidence; a shared mission is none, and evidence elsewhere keeps it out', () => {
    // M-251 has work on 5.1 (T2, T3, T5) and, here, on 5.2 too (T_OTHER): two modules share one mission.
    const raw = r5Raw()
    Object.assign(raw.tasks.find(t => t.task_id === T_OTHER)!, { mission_id: 'M-251' })
    // A proposal in the window that changes only 5.2-1.
    raw.recent_changes.push({ event_id: 9110, event_stream_id: 'plan:R5', seq: 9110, event_type: 'plan_change_proposed', actor_id: 'thread:x', at: '2026-10-27T02:20:00Z',
      body_json: { changes: [{ op: 'retitle', node: 'R5:5.2-1', title: 'Release lanes, per application' }], reason: 'Retitle 5.2-1', standing: false, question_id: Q_CHANGE } })
    const view = parsePlanSnapshot(raw, 'R5')
    const extra = [question(Q_MISSION, { number: 17, task_id: undefined, title: 'Mission: module detail follow-up', expires_at: FAR }),
      question(Q_CHANGE, { number: 18, task_id: undefined, title: 'Plan change to R5: retitle 5.2-1', expires_at: FAR }),
      question(Q_BLOCKED, { number: 19, task_id: T_OTHER, title: 'Blocked in M-251: Release lanes', expires_at: FAR })]
    const card = (question_id: string, kind: string, missions: string[], title: string, task_id?: string) =>
      ({ id: `question:${question_id}`, kind, title, ...(task_id ? { task_id, mission_id: 'M-251' } : {}), action: { where: 'console', question_id }, blocks: { missions } })
    seedShell({ questions: [...questions(), ...extra], progress: progress(r => {
      r.needs_you = { count: 3, items: [card(Q_MISSION, 'mission', ['M-251'], 'Mission M-251: follow-up'),
        card(Q_CHANGE, 'plan_change', ['M-251'], 'Plan change to R5: retitle 5.2-1'), card(Q_BLOCKED, 'blocked_task', ['M-251'], 'Blocked in M-251', T_OTHER)], truncated: false }
    }) })
    detail(view, 'R5:5.1')
    const needs = region('Needs you now')
    // None of the three is 5.1's: the mission card names no task or node, the proposal changes only 5.2-1, the blocked task is on 5.2-1.
    for (const title of [/Mission: module detail follow-up/, /retitle 5\.2-1/, /Blocked in M-251/]) expect(within(needs).queryByRole('link', { name: title })).toBeNull()
    expect(needs).not.toHaveTextContent('mission M-')
    expect(region('Your actions found')).toHaveTextContent('33 counted under Needs you now · 1 listed, not counted')
    // No number of what is not here, and no claim of where it belongs: only where to find it.
    expect(within(needs).getByRole('link', { name: 'Cards with no evidence here for a module of R5 are on Needs you' })).toHaveAttribute('href', NEEDS_YOU_HREF)
    expect(needs).not.toHaveTextContent(/\d+ open on Needs you/)
    cleanup()
    detail(view, 'R5:5.2')
    const other = region('Needs you now')
    expect(within(other).getByRole('link', { name: /retitle 5\.2-1/ }).closest('li')).toHaveTextContent('On 5.2-1, by the plan change it decides')
    expect(within(other).getByRole('link', { name: /Blocked in M-251/ }).closest('li')).toHaveTextContent('On 5.2-1, by its task')
    expect(within(other).queryByRole('link', { name: /Mission: module detail follow-up/ })).toBeNull()
  })

  it('does not list a card naming a task of another plan root, and says only where such cards are', () => {
    const R4_TASK = 't_01M5FYMA9YBAY9RKQ928TA26KJ'
    seedShell({ questions: [...questions(), question('q_000000000000010b', { number: 20, task_id: R4_TASK, title: 'Blocked in M-5: a task of R4', expires_at: FAR })] })
    detail()
    const needs = region('Needs you now')
    expect(within(needs).queryByRole('link', { name: /a task of R4/ })).toBeNull()
    expect(within(needs).getByRole('link', { name: 'Cards with no evidence here for a module of R5 are on Needs you' })).toHaveAttribute('href', NEEDS_YOU_HREF)
    expect(needs).not.toHaveTextContent('not tied')
  })

  it('B2: a module nothing ties to says "Nothing linked" and what it left out, with no count of the rest', () => {
    seedShell()
    detail(r5(), 'R5:5.4')
    const needs = screen.getByRole('region', { name: 'Needs you now' })
    expect(needs).toHaveTextContent('Nothing linked to 5.4 found.')
    expect(needs).toHaveTextContent('including those that only share a mission with 5.4; so a card of 5.4 can be missing from this list and from Your actions found.')
    expect(needs).toHaveTextContent("seen only while its proposal is among the plan's latest")
    expect(screen.getByRole('link', { name: 'Cards with no evidence here for a module of R5 are on Needs you' })).toBeInTheDocument()
  })

  it('gives no count of cards it cannot place: an open plan-change card whose proposal left the window', () => {
    const raw = r5Raw()
    raw.recent_changes = []
    seedShell({ questions: [...questions(), question(Q_CHANGE, { number: 18, task_id: undefined, title: 'Plan change to R5: add a step to 5.1', expires_at: FAR })],
      progress: progress(r => { r.needs_you = { count: 1, items: [{ id: `question:${Q_CHANGE}`, kind: 'plan_change', title: 'Plan change to R5: add a step to 5.1', action: { where: 'console', question_id: Q_CHANGE }, blocks: { missions: ['M-250'] } }], truncated: false } }) })
    detail(parsePlanSnapshot(raw, 'R5'))
    const needs = region('Needs you now')
    expect(within(needs).queryByRole('link', { name: /add a step to 5\.1/ })).toBeNull()
    expect(needs).not.toHaveTextContent(/\d+ open/)
    expect(needs).not.toHaveTextContent('not tied')
    expect(within(needs).getByRole('link', { name: 'Cards with no evidence here for a module of R5 are on Needs you' })).toHaveAttribute('href', NEEDS_YOU_HREF)
  })

  it('says how many it counts and how many it lists without counting, and an Owner step with only an uncounted request has nothing counted', () => {
    // 5.1-5 (the Owner's step) waits on landing request #13, which this page lists but cannot count; its question is answered.
    const raw = r5Raw()
    Object.assign(raw.nodes.find(n => n.plan_node_id === 'R5:5.1-5')!, { done_rule_json: { approval_executed: 13 } })
    const landing = buildPlatformApproval({ id: 'apr-13', number: 13, action: 'approve_core_landing', summary: 'Land the sign-off', params: { op: 'land:x:1' }, expires_at: FAR })
    seedShell()
    client.setQueryData(['approvals', 'open'], [landing])
    detail(parsePlanSnapshot(raw, 'R5'))
    const tile = region('Your actions found')
    expect(within(region('Needs you now')).getByRole('link', { name: 'Request #13 · Land the sign-off' }).closest('li')).toHaveTextContent('not counted')
    expect(tile).toHaveTextContent('1 counted under Needs you now · 1 listed, not counted')
    expect(tile).toHaveTextContent('1 Owner step not done, 1 with nothing counted')
    expect(tile).not.toHaveTextContent('nothing listed')
    cleanup()
    // Nothing listed without being counted: no "not counted" part.
    seedShell()
    client.setQueryData(['approvals', 'open'], openRequests().filter(a => a.id !== 'apr-8'))
    detail()
    expect(region('Your actions found')).toHaveTextContent('3 counted under Needs you now1 Owner step')
    expect(region('Your actions found')).not.toHaveTextContent('listed, not counted')
  })

  it('B2: gives a floor when Needs you\'s cards are unread or were cut at their bound', () => {
    client.setQueryData(['questions', 'all'], questions())
    client.setQueryData(['approvals', 'open'], openRequests())
    detail()
    expect(region('Your actions found')).toHaveTextContent('3+ or more')
    expect(region('Needs you now')).toHaveTextContent("Needs you's cards (runtime progress) not read yet")
    cleanup()
    seedShell({ progress: progress(raw => { raw.needs_you = { count: 50, items: [], truncated: true } }) })
    detail()
    expect(region('Your actions found')).toHaveTextContent('3+ or more')
    expect(region('Needs you now')).toHaveTextContent("Needs you's cards were cut at their bound: some may be missing")
  })

  it('lists recent activity newest first from the plan, the questions, the requests and the ended tasks', () => {
    seedShell()
    detail()
    const activity = region('Recent activity on 5.1')
    fireEvent.click(within(activity).getByRole('button', { name: 'Show all 20' }))
    expect(within(activity).getAllByRole('listitem').map(li => li.textContent)).toEqual([
      '2026-10-27T02:40:00ZStep5.1-3 · Not started → In progress · its task was leased',
      '2026-10-27T02:35:00ZCheck5.1-3 · Verify fail · Module detail page',
      '2026-10-27T02:31:00ZLandingLanded on main · Module detail model',
      '2026-10-27T02:30:00ZLandingLanding claimed (dev) · Module detail model',
      '2026-10-27T02:10:00ZCreated5.1-6 · 验收',
      '2026-10-27T02:07:00ZPlan changeApplied · add_node',
      '2026-10-27T02:06:00ZPlan changeApplied by owner',
      '2026-10-27T02:05:30ZQuestionAnswered “Not now” · Q14 · Add a step to 5.1?',
      '2026-10-27T02:05:00ZPlan changeProposed · Add a step to 5.1',
      '2026-10-27T02:04:00ZQuestionAsked · Q14 · Add a step to 5.1?',
      '2026-10-27T01:00:00ZQuestionAnswered “Not now” · Q13 · Land the model first?',
      '2026-10-27T00:41:00ZTask5.1-1 · Implement accepted · Module detail model',
      '2026-10-27T00:40:00ZCheck5.1-1 · Review pass · Module detail model',
      '2026-10-26T21:00:00ZRequestApproved · Request #6 · Release 5.1-2',
      '2026-10-26T20:00:00ZRequestMade · Request #6 · Release 5.1-2',
      '2026-10-21T19:22:24.204818ZQuestionAsked · Q11 · Keep the step table in plan order?',
      '2026-10-21T19:22:24.204818ZQuestionAsked · Q12 · Sign the module detail',
      '2026-10-21T19:22:24.204818ZQuestionAsked · Q13 · Land the model first?',
      '2026-10-08T15:00:00ZRequestMade · Request #7 · Sync the STG console',
      '2026-10-08T15:00:00ZRequestMade · Request #8 · Land T3',
    ])
    expect(activity).toHaveTextContent(`Plan events: only the latest 11 of R5 up to ${AT}`)
    expect(activity).toHaveTextContent('A plan change appears here only when one of its events in that window names a node of 5.1 or decides a question of it; the others are under Recent changes.')
    expect(activity).not.toHaveTextContent(/\d+ plan changes? there/)
  })

  it("S2: says ended tasks come only from the runtime's latest 20", () => {
    seedShell()
    detail()
    expect(region('Recent activity on 5.1')).toHaveTextContent("Ended tasks and checks: only those among the runtime's latest 20 ended tasks of every goal.")
  })

  it('S1: says when each shared read was taken and marks one older than twice its poll Stale', () => {
    seedShell()
    client.setQueryData(['questions', 'all'], questions(), { updatedAt: Date.parse(AT) - 61_000 })
    client.setQueryData(['agent-runtime-progress'], progress(), { updatedAt: Date.parse(AT) - 4_000 })
    detail()
    const asOf = screen.getByLabelText('Read as of')
    expect(asOf.querySelector('[data-source="questions"]')).toHaveTextContent(`${new Date(Date.parse(AT) - 61_000).toISOString()} · Stale`)
    expect(asOf.querySelector('[data-source="progress"]')).toHaveTextContent(new Date(Date.parse(AT) - 4_000).toISOString())
    expect(asOf.querySelector('[data-source="progress"]')).not.toHaveTextContent('Stale')
    expect(asOf.querySelector('[data-source="open"]')).toHaveTextContent(new Date(Date.parse(AT)).toISOString())
    expect(region('Your actions found')).toHaveTextContent('Stale: Questions not refreshed for over twice their poll.')
  })

  it('says Unknown with the reason when a list cannot be read, and gives the count as a floor', async () => {
    await client.prefetchQuery({ queryKey: ['questions', 'all'], queryFn: () => Promise.reject(new Error('Questions read failed · HTTP 503')) })
    client.setQueryData(['approvals', 'open'], openRequests())
    detail()
    expect(region('Your actions found')).toHaveTextContent('2+ or more1 counted under Needs you now · 1 listed, not counted1 Owner step not done, 1 with nothing counted')
    expect(region('Your actions found')).toHaveTextContent('Owner steps not done, plus what Needs you now counts; cards without evidence here are not counted (see Needs you).At least: a list is not read or was cut')
    expect(region('Module 5.1')).not.toHaveTextContent(/actions left/i)
    expect(region('Needs you now')).toHaveTextContent('Questions Unknown · Questions read failed · HTTP 503')
    expect(region('Needs you now')).not.toHaveTextContent(/\d+ open on Needs you/)
    expect(screen.getByLabelText('Read as of').querySelector('[data-source="progress"]')).toHaveTextContent('not read yet')
  })

  it('says why each Unknown is Unknown when runtime progress failed, and never that a task ended before the latest 20', async () => {
    client.setQueryData(['questions', 'all'], questions())
    client.setQueryData(['approvals', 'open'], openRequests())
    await client.prefetchQuery({ queryKey: ['agent-runtime-progress'], queryFn: () => Promise.reject(new Error('Agent runtime: HTTP 503')) })
    detail()
    expect(region('Now')).toHaveTextContent('Module detail page · ready · round Unknown (runtime progress not read)')
    expect(region('Now')).toHaveTextContent('Role Unknown (runtime progress not read) · Module detail page')
    // T5 is still a check by its title; its verdict needs the runtime's list, which was not read.
    expect(step('5.1-3')).toHaveTextContent('ReviewVerify of t_…F0WBP3: verdict Unknown (runtime progress not read)')
    expect(step('5.1-6')).toHaveTextContent('ReviewUnknown: runtime progress not read, so task roles are not known')
    expect(region('Module 5.1')).not.toHaveTextContent('latest 20 ended tasks)')
    expect(screen.getByLabelText('Read as of').querySelector('[data-source="progress"]')).toHaveTextContent('Unknown · Agent runtime: HTTP 503')
  })

  it("B1: Review is only the latest work task's check: an older task's failed check does not read as the new task failing", () => {
    // T2 was implemented, its verification T8 failed and T2 was cancelled; T3 is the new implementation and is running.
    const T8 = id('8')
    const raw = r5Raw()
    raw.tasks = raw.tasks.filter(t => t.task_id !== T5)
    Object.assign(raw.tasks.find(t => t.task_id === T3)!, { state: 'running' })
    raw.tasks.push({ task_id: T8, plan_node_id: 'R5:5.1-3', mission_id: 'M-251', title: `verify ${T2}`, state: 'accepted' })
    seedShell({ progress: progress(r => {
      Object.assign(r.work.missions[0].tasks[0], { state: 'running' })
      r.recent.tasks = [{ task_id: T8, mission_id: 'M-251', title: `verify ${T2}`, role: 'verify', state: 'accepted', ended_at: '2026-10-27T02:00:00Z', ended_event_id: 70, conclusion: 'failed' },
        { task_id: T2, mission_id: 'M-251', title: 'First cut of the page', role: 'implement', state: 'cancelled', ended_at: '2026-10-27T02:01:00Z', ended_event_id: 71, conclusion: 'done' },
        ...r.recent.tasks.filter(t => t.task_id !== T5)]
    }) })
    detail(parsePlanSnapshot(raw, 'R5'))
    expect(step('5.1-3')).toHaveTextContent('Latest taskImplement · Module detail page · running · round 2 of 3 · 1 earlier · 1 check')
    expect(step('5.1-3')).toHaveTextContent('ReviewNo check of the latest task yet')
    expect(step('5.1-3')).not.toHaveTextContent('fail')
  })

  it('says reading while the shell is still reading a list', () => {
    void client.prefetchQuery({ queryKey: ['questions', 'all'], queryFn: () => new Promise<Question[]>(() => {}) })
    void client.prefetchQuery({ queryKey: ['approvals', 'open'], queryFn: () => new Promise<ApprovalItem[]>(() => {}) })
    detail()
    const needs = region('Needs you now')
    expect(needs).toHaveTextContent('Questions · reading…')
    expect(needs).toHaveTextContent('Requests · reading…')
    expect(needs).toHaveTextContent('Nothing linked to 5.1 found.')
  })

  it('marks the step an address names, keeps the module around it and puts the keyboard on that step', () => {
    seedShell()
    detail(r5(), 'R5:5.1-3')
    const row = document.getElementById('module-detail-step-R5:5.1-3')!
    expect(row).toHaveAttribute('aria-current', 'step')
    expect(document.activeElement).toBe(row)
    expect(within(row).getByRole('link', { name: 'Open step 5.1-3' })).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1-3')
  })

  it("S4: a step's own detail names where its address goes: its module", () => {
    render(<PlanModuleRows view={r5()} selected="" />)
    fireEvent.click(screen.getByRole('button', { name: 'Step 5.1-3: in progress' }))
    expect(within(screen.getByRole('region', { name: 'Step 5.1-3' })).getByRole('link', { name: 'Open in module 5.1' }))
      .toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1-3')
  })
})

describe('Work → Plan with a module address', () => {
  it('opens the module from its row link, reads nothing but the plan view, and moves the keyboard in and back out (S3)', async () => {
    const scroll = vi.fn(); Element.prototype.scrollIntoView = scroll
    page('#work?section=plan&root=R5')
    await screen.findByRole('region', { name: 'Modules' })
    expect(screen.queryByRole('region', { name: 'Module 5.1' })).toBeNull()
    const link = screen.getByRole('link', { name: /^Open module 5\.1 ·/ })
    expect(link).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1')
    await act(async () => { window.location.hash = link.getAttribute('href')!; window.dispatchEvent(new Event('hashchange')) })
    const module = await screen.findByRole('region', { name: 'Module 5.1' })
    expect(module).toHaveTextContent('4 steps to go')
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    expect(document.activeElement?.tagName).toBe('H3')
    expect(module).toContainElement(document.activeElement as HTMLElement)
    // The shell's lists are not in this test's cache: the detail says so and does not fetch them.
    expect(within(module).getByRole('region', { name: 'Needs you now' })).toHaveTextContent('Questions not read yet')
    expect(urls().length).toBeGreaterThan(0)
    expect(urls().every(u => u === '/api/v1/agentrt/plan-view?root=R5')).toBe(true)
    // With the shell's lists in the cache the detail shows them, still without a request of its own.
    act(() => seedShell())
    await waitFor(() => expect(within(module).getByRole('region', { name: 'Needs you now' })).toHaveTextContent('Request #7'))
    expect(urls().every(u => u === '/api/v1/agentrt/plan-view?root=R5')).toBe(true)
    expect(urls().some(u => /landing|questions|approvals|progress|plan\?|root=R4/.test(u))).toBe(false)
    // ‹ Work: the detail closes and the keyboard goes back to the module's own link in the rows.
    await act(async () => { window.location.hash = '#work?root=R5'; window.dispatchEvent(new Event('hashchange')) })
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Module 5.1' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('link', { name: /^Open module 5\.1 ·/ })))
  })

  it('shows Unknown, not a module, when the plan view cannot be read, and Stale once a read fails after one succeeded', async () => {
    fetcher.mockImplementation(async () => new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 }))
    page('#work?section=plan&root=R5&node=R5%3A5.1')
    expect(screen.getByRole('status')).toHaveTextContent('Loading plan and live work')
    expect(await screen.findByRole('alert')).toHaveTextContent('Unknown')
    expect(screen.queryByRole('region', { name: 'Module 5.1' })).toBeNull()
    fetcher.mockImplementation(async () => new Response(JSON.stringify(r5Raw()), { status: 200 }))
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    const module = await screen.findByRole('region', { name: 'Module 5.1' })
    expect(module).toHaveTextContent(`Plan snapshot${AT} · Fresh`)
    fetcher.mockImplementation(async () => new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 }))
    await act(async () => { await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] }) })
    await waitFor(() => expect(screen.getByRole('region', { name: 'Module 5.1' })).toHaveTextContent(`Plan snapshot${AT} · Stale`))
    expect(screen.getByRole('alert')).toHaveTextContent('Stale · showing the last successful snapshot')
  })
})
