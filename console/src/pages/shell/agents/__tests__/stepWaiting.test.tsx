import { describe, expect, it } from 'vitest'
import type { ApprovalItem } from '@/api/approvals'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import type { Question } from '@/api/questions'
import { shellRead } from '../moduleDetailModel'
import type { OwnerAsks } from '../ownerAsksModel'
import { cellLabels, howRead, nowLines, spokenState, stepCounts, type StepCell } from '../planModuleModel'
import { stepRows, type StepClock } from '../stepStateModel'
import { CLASS_MEANINGS, moduleClass } from '../workSummaryModel'
import { AT, id, Q_RULE, r5Raw, T3, T_OTHER, type Raw } from './moduleDetailFixture'

/*
 * R5:5.5-10 slice 2 (the Owner 2026-11-02, Q75): a step on which something only the Owner moves waits on him reads
 * "Waiting on you". Questions and requests come from the lists Needs you polls (the shell's cache), given to the judge
 * as asks; a list not read now is said, never taken for "none".
 */
const NOW = Date.parse(AT) + 5_000
type Rec = Record<string, unknown>
const step = (raw: Raw, nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
/** 5.1-4 active, with what a case adds. */
const view514 = (change: (raw: Raw) => void = () => {}): PlanSnapshot => {
  const raw = r5Raw()
  raw.tasks.find(t => t.task_id === T_OTHER)!.state = 'ready'
  Object.assign(step(raw, 'R5:5.1-4'), { state: 'active' })
  change(raw)
  return parsePlanSnapshot(raw, 'R5')
}
const onStep = (raw: Raw, tid: string, state: string, more: Rec = {}) =>
  raw.tasks.push({ task_id: tid, plan_node_id: 'R5:5.1-4', mission_id: 'M-250', title: `Work ${tid.slice(-1)}`, state, ...more })
const ended = { stream: 'landing:bifrost-trade-infra', operation: 'land:x:1', phase: 'failed', outcome: 'ci_red', reason: '', attempt: 'a_1',
  result: 'r', main_commit: '', run: '', observed_at: new Date(Date.parse(AT) - 60_000).toISOString(), basis: 'note.landing; current validity requires verification' }
const question = (n: number, more: Partial<Question>): Question => ({ id: `q_${String(n).padStart(16, '0')}`, number: n, status: 'open', requester: 'agentrt',
  title: `Landing failed: card ${n}`, options: [], allow_text: true, urgency: 'when you can', meanwhile: 'nothing merges', created_at: AT, expires_at: AT, ...more })
const LATER = new Date(NOW + 3_600_000).toISOString()
const request = (n: number, status: string, more: Partial<ApprovalItem> = {}): ApprovalItem => ({ id: `appr_${n}`, number: n, action: 'deliver', tier: 'C', params: {},
  params_hash: 'h', status, reason: 'r', requester: 'agent', created_at: AT, expires_at: LATER, ...more })
const lists = (questions: Question[], requests: ApprovalItem[] = [], at = NOW - 10_000): OwnerAsks =>
  ({ questions: { state: 'ok', data: questions, at }, requests: { state: 'ok', data: requests, at } })
const clock = (asks?: OwnerAsks): StepClock => ({ failed: null, now: NOW, asks })
const cellOf = (v: PlanSnapshot, label: string, c: StepClock): StepCell => stepRows(v, c).flatMap(r => r.cells).find(x => x.label === label)!

describe('waiting on you (R5:5.5-10 slice 2)', () => {
  it('an open question on a task of the step waits on you: a landing that failed and asks "Try again" (5.11-1 on 11-03)', () => {
    const tid = id('M')
    const v = view514(raw => onStep(raw, tid, 'accepted', { landing: ended }))
    // The plan view alone: an accepted task whose landing ended reads nothing running.
    expect(cellOf(v, '5.1-4', clock()).now).toEqual({ kind: 'idle' })
    const c = cellOf(v, '5.1-4', clock(lists([question(91, { task_id: tid })])))
    expect(c).toMatchObject({ state: 'waiting', now: { kind: 'waiting', held: [], recorded: null,
      waiting: [{ kind: 'question', text: 'question #91 “Landing failed: card 91” waits for your answer (on task Work M)' }] } })
    expect(cellLabels[c.state]).toBe('Waiting on you')
    expect(spokenState(c)).toBe('waiting on you: 1 open question for you')
    expect(nowLines(c)[0]).toBe('Waiting on you: question #91 “Landing failed: card 91” waits for your answer (on task Work M). '
      + 'An open question moves when you answer it (Needs you lists questions); nothing on the step runs or is queued.')
    // Answered, withdrawn or on another task's step: nothing waits on you.
    for (const q of [question(91, { task_id: tid, status: 'answered' }), question(91, { task_id: tid, status: 'withdrawn' }), question(91, { task_id: id('Z') }), question(91, {})]) {
      expect(cellOf(v, '5.1-4', clock(lists([q]))).now).toEqual({ kind: 'idle' })
    }
  })

  it('a question title is cut at 80 characters in the line; its page has it whole', () => {
    const tid = id('M')
    const v = view514(raw => onStep(raw, tid, 'accepted', { landing: ended }))
    const c = cellOf(v, '5.1-4', clock(lists([question(92, { task_id: tid, title: 'x'.repeat(100) })])))
    expect(c.now?.kind === 'waiting' && c.now.waiting[0].text).toBe(`question #92 “${'x'.repeat(80)}…” waits for your answer (on task Work M)`)
  })

  it('a question or request the done rule names waits on you while it is open or pending', () => {
    // 5.1-5 is the Owner's step whose done rule waits on question Q_RULE.
    const v = view514(raw => Object.assign(step(raw, 'R5:5.1-5'), { state: 'active' }))
    const open = cellOf(v, '5.1-5', clock(lists([question(7, { id: Q_RULE })])))
    expect(open).toMatchObject({ state: 'waiting', now: { kind: 'waiting', recorded: [`question ${Q_RULE} (its done rule)`],
      waiting: [{ kind: 'question', text: 'question #7 “Landing failed: card 7” waits for your answer (its done rule names it)' }] } })
    expect(cellOf(v, '5.1-5', clock(lists([question(7, { id: Q_RULE, status: 'answered' })]))).now).toMatchObject({ kind: 'yours' })
    const ruled = view514(raw => Object.assign(step(raw, 'R5:5.1-4'), { done_rule_json: { approval_executed: 280 } }))
    expect(cellOf(ruled, '5.1-4', clock(lists([], [request(280, 'pending')]))).now).toMatchObject({ kind: 'waiting',
      waiting: [{ kind: 'request', text: 'request #280 waits for your decision (its done rule names it)' }] })
    // Only a request still awaiting a decision as Needs you counts one: pending and not expired, not a drill, not a
    // landing request (a landing's grant is its own phase); not another number.
    for (const r of [request(280, 'approved'), request(281, 'pending'), request(280, 'pending', { drill: true }),
      request(280, 'pending', { expires_at: new Date(NOW - 1).toISOString() }), request(280, 'pending', { action: 'approve_core_landing' })]) {
      expect(cellOf(ruled, '5.1-4', clock(lists([], [r]))).now).toEqual({ kind: 'idle' })
    }
  })

  it('an acceptance sign-off Ready on an agent step waits on you', () => {
    const v = view514(raw => (raw as Raw & { owner_waiting: Rec[] }).owner_waiting.push({ plan_node_id: 'R5:5.1-4', ref: 'acc-2', acceptance: 'Owner accepts 5.1',
      plan_version: 1, evidence: null, candidate_evidence: [], reason: 'every binding true', source: 'plan view', readiness: 'Ready' }))
    expect(cellOf(v, '5.1-4', clock(lists([]))).now).toMatchObject({ kind: 'waiting',
      waiting: [{ kind: 'signoff', text: 'acceptance sign-off Owner accepts 5.1 is Ready for you' }] })
  })

  it('beside queued or Unknown work, what waits on you is kept and said, not the state', () => {
    const tid = id('M')
    const asked = 'question #91 “Landing failed: card 91” waits for your answer (on task Work M)'
    const v = view514(raw => { onStep(raw, tid, 'accepted', { landing: ended }); onStep(raw, id('N'), 'ready') })
    const c = cellOf(v, '5.1-4', clock(lists([question(91, { task_id: tid })])))
    expect(c.now).toEqual({ kind: 'queued', why: ['task Work N ready'], held: [], waiting: [asked] })
    expect(nowLines(c)).toContain(`Waiting on you: ${asked}.`)
    // A task leased with no live attempt is Unknown; what waits on you is kept beside it.
    const u = cellOf(view514(raw => { onStep(raw, tid, 'accepted', { landing: ended }); onStep(raw, id('N'), 'leased') }), '5.1-4', clock(lists([question(91, { task_id: tid })])))
    expect(u.now).toEqual({ kind: 'unknown', why: ['task Work N is leased, but the plan view holds no live attempt for it'], queued: [], held: [], waiting: [asked] })
    expect(nowLines(u)).toContain(`Waiting on you: ${asked}.`)
  })

  it('lists not read now are named, and "nothing waits on you" is then said of the plan view only', () => {
    const tid = id('M')
    const v = view514(raw => onStep(raw, tid, 'accepted', { landing: ended }))
    const unread: OwnerAsks = { questions: { state: 'unread' }, requests: { state: 'unknown', reason: 'Offline' } }
    const c = cellOf(v, '5.1-4', clock(unread))
    expect(c.now).toEqual({ kind: 'idle', unread: ['Questions: not read yet'] })
    const yours = (cell: StepCell) => howRead(cell)!.find(r => r.label === 'Waits on you')!.text
    expect(yours(c)).toMatch(/^Nothing in the plan view: .* Questions and requests were not read now\.$/)
    expect(yours(c)).not.toMatch(/no open question on its tasks/)
    expect(nowLines(c)).toContain('Not read now: Questions: not read yet. Whether a question or request of yours waits on this step is Unknown.')
    // Stale past two of Needs you's 30 s polls, as the module page's sources read it: 60 s is now, 60.001 s is not.
    const at = (ms: number) => cellOf(v, '5.1-4', clock(lists([question(91, { task_id: tid })], [], NOW - ms))).now
    expect(at(60_000)).toMatchObject({ kind: 'waiting' })
    expect(at(60_001)).toEqual({ kind: 'idle', unread: ['Questions: last read 60 s ago (stale)'] })
    expect(at(90_000)).toEqual({ kind: 'idle', unread: ['Questions: last read 90 s ago (stale)'] })
    // Read now and none open: the whole clause.
    expect(yours(cellOf(v, '5.1-4', clock(lists([]))))).toMatch(/no open question on its tasks or named by its done rule, no request its done rule names awaiting your decision/)
    // A step nothing could tie a question or request to (no task, no rule naming one) reads neither list.
    expect(cellOf(view514(), '5.1-4', clock(unread)).now).toEqual({ kind: 'idle' })
  })

  it('a list is read only while its latest read neither failed nor is paused offline (the module page reads its sources the same way)', () => {
    const base = { dataUpdatedAt: NOW, isError: false, isPaused: false, error: null, fetchStatus: 'idle' as const }
    expect(shellRead({ ...base, data: [1] })).toEqual({ state: 'ok', data: [1], at: NOW })
    expect(shellRead({ ...base, data: [1], isError: true, error: new Error('HTTP 502') })).toEqual({ state: 'unknown', reason: 'HTTP 502' })
    expect(shellRead({ ...base, data: [1], isPaused: true })).toEqual({ state: 'unknown', reason: 'Offline' })
    expect(shellRead({ ...base, data: undefined })).toEqual({ state: 'unread' })
    expect(shellRead({ ...base, data: undefined, fetchStatus: 'fetching' })).toEqual({ state: 'reading' })
    // The step names each such read: an unread or failed list is never taken for none.
    const tid = id('M')
    const v = view514(raw => onStep(raw, tid, 'accepted', { landing: ended }))
    expect(cellOf(v, '5.1-4', clock({ questions: { state: 'unknown', reason: 'HTTP 502' }, requests: { state: 'unread' } })).now)
      .toEqual({ kind: 'idle', unread: ['Questions: HTTP 502'] })
  })

  it("the summary's meanings say nothing waits on you only of what was read, never of lists not read", () => {
    for (const [name, meaning] of CLASS_MEANINGS) {
      if (/nothing (on it )?(was read as )?wait/.test(meaning)) expect(meaning, name).toMatch(/while they were read now; a step whose lists were not read says so/)
      expect(meaning, name).not.toMatch(/nothing on it runs, is queued, waits on you/)
    }
  })

  it('the row counts it and the summary classes its module as waiting on you, ahead of held (in progress still comes first)', () => {
    const tid = id('M')
    // 5.1-3's implementation cancelled, so nothing in the module is queued: 5.1-3 idle, 5.1-4 waiting on you.
    const v = view514(raw => { onStep(raw, tid, 'accepted', { landing: ended }); raw.tasks.find(t => t.task_id === T3)!.state = 'cancelled' })
    const r = stepRows(v, clock(lists([question(91, { task_id: tid })]))).find(x => x.cells.some(c => c.label === '5.1-4'))!
    expect(r.waiting).toBe(1)
    expect(stepCounts(r)).toMatch(/1 waiting on you/)
    expect(moduleClass(r)).toBe('waiting')
  })
})
