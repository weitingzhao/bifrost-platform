import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import cards from '@/api/__fixtures__/question-cards.testdb.json'
import { parseRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { ApprovalItem } from '@/api/approvals'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { parseQuestion, type Question } from '@/api/questions'

/*
 * The module detail's test data (planModuleDetail.test, planModuleDetailLayout.test): an R5 plan view in the PROD
 * shape, the shell's questions and requests, and the runtime's progress, as the controller and the shell leave them.
 */
// Runtime task ids (ULIDs, so their order is the order they were made).
export const id = (c: string) => `t_01M69PWCTHGN75N160E9F0WBP${c}`
export const T1 = id('1'), T1R = id('A'), T2 = id('2'), T3 = id('3'), T4 = id('4'), T5 = id('5'), T6 = id('6'), T7 = id('7'), T_OTHER = id('9')
export const Q_OPEN = 'q_00000000000000a1', Q_RULE = 'q_00000000000000b2', Q_ANSWERED = 'q_00000000000000c3', Q_OTHER = 'q_00000000000000d4'
export const Q_LOOSE = 'q_00000000000000e5', Q_ADD = 'q_00000000000000f6', Q_GONE = 'q_0000000000000107', Q_MISSION = 'q_0000000000000108', Q_CHANGE = 'q_0000000000000109'
export const Q_BLOCKED = 'q_000000000000010a'
export const AT = prod.as_of
export const FAR = '2099-01-01T00:00:00Z'

export type Raw = { root: string; nodes: Record<string, unknown>[]; tasks: Record<string, unknown>[]; recent_changes: Record<string, unknown>[] }

/**
 * An R5 plan-view answer in the real PROD shape: the PROD R4 response (2026-10-27) with every id, label and stream moved
 * to round 5 (R4:4.1-3 → R5:5.1-3), then advanced as the controller moves it. 5.1-1 done: its implementation T1 and the
 * review that passed it (T1R, "review <T1>", on the same step as stepevidence.go has it). 5.1-2 done by request #6:
 * work T4 whose role the runtime no longer lists and its review T7, ended before the runtime's latest 20. 5.1-3 active:
 * an older cancelled task T2, the implementation T3 back to ready in round 2 of 3 after its verification T5 failed (T5 is
 * accepted all the same: "result used: fail"). 5.1-5 is the Owner's step (as on PROD), its rule waits on a question.
 * 5.1-6 holds T6, whose role nothing read says.
 */
export function r5Raw(): Raw {
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
export const r5 = (): PlanSnapshot => parsePlanSnapshot(r5Raw(), 'R5')

export function question(qid: string, patch: Partial<Question> & Record<string, unknown>): Question {
  // A key set to undefined is left out, as the server omits it.
  const body = Object.fromEntries(Object.entries({ ...cards.plan_question.body, id: qid, ...patch }).filter(([, v]) => v !== undefined))
  const parsed = parseQuestion(body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}
export const answer = (at: string) => ({ option: 'a', by: 'owner', at, channel: 'console', approval: false })
export const questions = (): Question[] => [
  question(Q_OPEN, { number: 11, task_id: T3, title: 'Keep the step table in plan order?', expires_at: FAR }),
  question(Q_RULE, { number: 12, task_id: undefined, title: 'Sign the module detail', expires_at: FAR }),
  question(Q_ANSWERED, { number: 13, task_id: T1, status: 'answered', title: 'Land the model first?', answer: answer('2026-10-27T01:00:00Z') }),
  question(Q_OTHER, { number: 15, task_id: T_OTHER, title: 'Another module', expires_at: FAR }),
  question(Q_LOOSE, { number: 16, task_id: undefined, title: 'Rename the Console?', expires_at: FAR }),
  question(Q_ADD, { number: 14, task_id: undefined, status: 'answered', title: 'Add a step to 5.1?', created_at: '2026-10-27T02:04:00Z', answer: answer('2026-10-27T02:05:30Z') }),
]
export const openRequests = (): ApprovalItem[] => [
  buildPlatformApproval({ id: 'apr-7', number: 7, action: 'gitops_sync_app', summary: 'Sync the STG console', params: { task: T3 }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-8', number: 8, action: 'approve_guardrail_landing', summary: 'Land T3', params: { task: T3, op: `land:${T3}:1` }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-9', number: 9, summary: 'Not this module', params: { task: T_OTHER }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-12', number: 12, summary: 'Rotate a node', params: { node: 'n7' }, expires_at: FAR }),
]
export const allRequests = (): ApprovalItem[] => [
  ...openRequests(),
  buildPlatformApproval({ id: 'apr-6', number: 6, status: 'approved', summary: 'Release 5.1-2', created_at: '2026-10-26T20:00:00Z', decided_at: '2026-10-26T21:00:00Z', expires_at: FAR }),
]
export type RawProgress = { work: { missions: { tasks: Record<string, unknown>[] }[] }; recent: { tasks: Record<string, unknown>[] }; needs_you: Record<string, unknown> }
export function progress(edit: (raw: RawProgress) => void = () => {}): RuntimeProgress {
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
