import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import { parseRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { ApprovalItem } from '@/api/approvals'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { parseQuestion, type Question } from '@/api/questions'

/*
 * The module detail's test data (planModuleDetail.test, planModuleDetailLayout.test): an R5 plan view in the PROD
 * shape, the shell's questions and requests, and the runtime's progress, as the controller and the shell leave them.
 */
export const Q_OPEN = 'q_00000000000000a1', Q_RULE = 'q_00000000000000b2', Q_ANSWERED = 'q_00000000000000c3', Q_OTHER = 'q_00000000000000d4'
export const Q_LOOSE = 'q_00000000000000e5', Q_ADD = 'q_00000000000000f6', Q_GONE = 'q_0000000000000107', Q_MISSION = 'q_0000000000000108', Q_CHANGE = 'q_0000000000000109'
export const Q_BLOCKED = 'q_000000000000010a'
/** The external work registered on 5.1-3. */
export const W3 = 'ew_r5_5_1_3'
export const AT = prod.as_of
export const FAR = '2099-01-01T00:00:00Z'

type Rec = Record<string, unknown>
export type Raw = { root: string; nodes: Rec[]; external_work: Rec[]; working: Rec[]; owner_waiting: Rec[]; recent_changes: Rec[] }
const before = (minutes: number) => new Date(Date.parse(AT) - minutes * 60_000).toISOString()

/**
 * Registered external work on a step, as plan-view lists it: in external_work, and in working too while it still
 * expects a heartbeat (Running or Registered). Running, started 20 min before the snapshot, its heartbeat 4 s old,
 * unless more says otherwise.
 */
export function externalWork(raw: Raw, plan_node_id: string, more: Rec = {}): Rec {
  const { ended_at: _ended, exit_code: _exit, report_digest: _report, report_path_digest: _path, verification: _verification, ...base } = raw.external_work[0]
  const row: Rec = { ...base, external_work_id: `ew_${raw.external_work.length}`, plan_node_id, type: 'started', state: 'Running', status: 'Running', started_at: before(20),
    elapsed_s: 1200, heartbeat_at: AT, heartbeat_age_s: 4, recorded_at: before(20), ...more }
  raw.external_work.push(row)
  if (row.state === 'Running' || row.state === 'Registered') raw.working.push(row)
  return row
}

/**
 * An R5 plan-view answer in the real PROD shape: the PROD R4 response (2026-10-27) with every id, label and stream moved
 * to round 5 (R4:4.1-3 → R5:5.1-3), then advanced as the controller moves it. 5.1-1 done; its rule names an answered
 * question. 5.1-2 done by request #6. 5.1-3 active: external work W3 is registered on it and not started, and its rule
 * waits on an open question and on requests #7 and #8. 5.1-5 is the Owner's step (as on PROD), its rule waits on a
 * question. 5.3-1's rule names another module's question and request.
 */
export function r5Raw(): Raw {
  const text = JSON.stringify(prod).replace(/"R4"/g, '"R5"').replace(/R4:4\./g, 'R5:5.').replace(/"4\.(\d+(?:-\d+)?)"/g, '"5.$1"').replace(/plan:R4/g, 'plan:R5')
  const raw = JSON.parse(text) as Raw
  const node = (nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
  const set = (nid: string, state: string, rule: Record<string, unknown>, event: number | null) => Object.assign(node(nid), { state, done_rule_json: rule, state_event_id: event })
  const asks = (q: string) => ({ question_answered: { question: q, option: 'a' } })
  set('R5:5.1-1', 'done', { all: [{ step_tasks: { state: 'accepted', min: 1 } }, asks(Q_ANSWERED)] }, 9001)
  set('R5:5.1-2', 'done', { approval_executed: 6 }, 9002)
  set('R5:5.1-3', 'active', { all: [asks(Q_OPEN), { approval_executed: 7 }, { approval_executed: 8 }] }, 9003)
  set('R5:5.1-5', 'planned', asks(Q_RULE), null)
  set('R5:5.3-1', 'planned', { all: [asks(Q_OTHER), { approval_executed: 9 }] }, null)
  Object.assign(node('R5:5.1'), { execution_progress: { state: 'active', done: 2, total: 6, stopped: 0, basis: 'Existing controller step states; not delivery verification' } })
  externalWork(raw, 'R5:5.1-3', { external_work_id: W3, type: 'registered', state: 'Registered', status: 'Registered', started_at: undefined, elapsed_s: undefined,
    description: { ...(raw.external_work[0].description as Rec), task: 'Module detail page' } })
  const ev = (event_id: number, event_type: string, body_json: Record<string, unknown>, at: string, stream = 'plan:R5') =>
    ({ event_id, event_stream_id: stream, seq: event_id, event_type, actor_id: 'controller', at, body_json })
  raw.recent_changes = [
    ev(9003, 'plan_node_state_changed', { from: 'planned', to: 'active', cause: 'external work was registered' }, '2026-10-27T02:40:00Z'),
    ev(9100, 'plan_node_state_changed', { from: 'planned', to: 'active', cause: 'not this module' }, '2026-10-27T02:39:00Z'),
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

/** An open question of the runtime as GET /api/v1/questions/{id} answered it (the question store, 2026-10-21). */
const QUESTION_BODY = {
  id: 'q_ac31c8792a726308',
  number: 4,
  status: 'open',
  requester: 'agentrt',
  requester_principal: 'agentrt',
  owner_duty: 'goal',
  task_id: 't_01M5FYMAJ0ANC4P90NDZQM03YD',
  title: 'Document the Console too?',
  body: 'It has no goal docs.',
  options: [
    { key: 'a', label: 'Not now' },
    { key: 'b', label: 'Yes, a third task' },
  ],
  recommended: 'a',
  allow_text: false,
  urgency: 'when you can: the work goes on with the recommended option',
  meanwhile: 'the task goes on without the Console',
  created_at: '2026-10-21T19:22:24.204818Z',
  expires_at: '2026-10-22T19:22:24.204818Z',
  deliveries: [
    {
      kind: 'created',
      channel: 'ntfy',
      target: 'relay',
      at: '2026-10-21T19:22:24.205518Z',
      result: 'skipped',
      error: 'APPROVAL_NOTIFY_URL or APPROVAL_NOTIFY_TOKEN unset',
    },
  ],
}

export function question(qid: string, patch: Partial<Question> & Record<string, unknown>): Question {
  // A key set to undefined is left out, as the server omits it.
  const body = Object.fromEntries(Object.entries({ ...QUESTION_BODY, id: qid, ...patch }).filter(([, v]) => v !== undefined))
  const parsed = parseQuestion(body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}
export const answer = (at: string) => ({ option: 'a', by: 'owner', at, channel: 'console', approval: false })
export const questions = (): Question[] => [
  question(Q_OPEN, { number: 11, task_id: undefined, title: 'Keep the step table in plan order?', expires_at: FAR }),
  question(Q_RULE, { number: 12, task_id: undefined, title: 'Sign the module detail', expires_at: FAR }),
  question(Q_ANSWERED, { number: 13, task_id: undefined, status: 'answered', title: 'Land the model first?', answer: answer('2026-10-27T01:00:00Z') }),
  question(Q_OTHER, { number: 15, task_id: undefined, title: 'Another module', expires_at: FAR }),
  question(Q_LOOSE, { number: 16, task_id: undefined, title: 'Rename the Console?', expires_at: FAR }),
  question(Q_ADD, { number: 14, task_id: undefined, status: 'answered', title: 'Add a step to 5.1?', created_at: '2026-10-27T02:04:00Z', answer: answer('2026-10-27T02:05:30Z') }),
]
export const openRequests = (): ApprovalItem[] => [
  buildPlatformApproval({ id: 'apr-7', number: 7, action: 'gitops_sync_app', summary: 'Sync the STG console', expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-8', number: 8, action: 'approve_guardrail_landing', summary: 'Land the page', params: { op: 'land:x:1' }, expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-9', number: 9, summary: 'Not this module', expires_at: FAR }),
  buildPlatformApproval({ id: 'apr-12', number: 12, summary: 'Rotate a node', params: { node: 'n7' }, expires_at: FAR }),
]
export const allRequests = (): ApprovalItem[] => [
  ...openRequests(),
  buildPlatformApproval({ id: 'apr-6', number: 6, status: 'approved', summary: 'Release 5.1-2', created_at: '2026-10-26T20:00:00Z', decided_at: '2026-10-26T21:00:00Z', expires_at: FAR }),
]
export type RawProgress = { needs_you: Record<string, unknown> }
/** The runtime's progress as the shell reads it, with no card on Needs you unless edit adds one. */
export function progress(edit: (raw: RawProgress) => void = () => {}): RuntimeProgress {
  const raw = structuredClone(progressFixture) as unknown as RawProgress
  raw.needs_you = { count: 0, items: [], truncated: false }
  edit(raw)
  return parseRuntimeProgress(raw)
}
