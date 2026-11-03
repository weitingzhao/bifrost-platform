import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { NowClock } from '../../progress/progressClock'
import { liveNow } from '../liveStepModel'
import { moduleOf } from '../moduleDetailModel'
import { PlanModuleDetail } from '../PlanModuleDetail'
import { cellLabels, cellStyle, moduleRows, noRuleMarked, nowLines, spokenState, stepCounts, type ModuleRow, type StepCell } from '../planModuleModel'
import { PlanModuleRows } from '../PlanModuleRows'
import { stepRows, type StepClock } from '../stepStateModel'
import { WorkPlanView } from '../WorkPlanView'
import { mainlineSentence, moduleClass, othersSentence } from '../workSummaryModel'
import { AT, id, Q_RULE, r5Raw, T3, T5, T_OTHER, type Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

/*
 * R5:5.5-10 (the Owner 2026-11-02, Q75): an active step is shown by what the plan view, read now, has on it; one with
 * nothing running or queued is not "In progress"; a step whose done rule is {} is marked while active or stopped. The R5
 * plan view of the module detail's tests (moduleDetailFixture), moved on as each case needs, in shapes the runtime
 * allows (a running task has its live attempt; plan-view working lists it).
 */
const NOW = Date.parse(AT) + 5_000
const min = (n: number) => new Date(Date.parse(AT) - n * 60_000).toISOString()
type Rec = Record<string, unknown>
type Full = Raw & { working: Rec[]; external_work: Rec[]; owner_waiting: Rec[]; freshness: { database: { status: string } } }
const node = (raw: Raw, nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
const task = (raw: Raw, tid: string) => raw.tasks.find(t => t.task_id === tid)!
const activate = (raw: Raw, nid: string, more: Rec = {}) => Object.assign(node(raw, nid), { state: 'active', ...more })
/** A task on a step, as plan-view lists it. */
const addTask = (raw: Raw, tid: string, nid: string, state: string, more: Rec = {}) => {
  raw.tasks.push({ task_id: tid, plan_node_id: nid, mission_id: 'M-250', title: `Work ${tid.slice(-1)}`, state, ...more })
  return task(raw, tid)
}
/** A task's live attempt (liveAttempts: leased, running or stale), with its heartbeat's age at the snapshot. */
function attempt(raw: Raw, tid: string, more: Rec = {}) {
  const t = task(raw, tid)
  const a = { attempt_id: `a_${tid.slice(2)}`, attempt_no: 1, task_id: tid, mission_id: t.mission_id, host_id: 'mac-mini', executor: 'claude-code', model: 'opus',
    state: 'running', started_at: min(12), elapsed_s: 720, heartbeat_age_s: 4, reason: 'attempt on mac-mini', persisted: false, ...more }
  Object.assign(t, { state: a.state === 'leased' ? 'leased' : 'running', attempt: a })
  ;(raw as Full).working.push(t)
}
const landing = (phase: string, outcome = '', observed = min(3)) => ({ stream: 'landing:bifrost-platform', operation: 'land:x:1', phase, outcome, reason: '', attempt: 'a_1',
  result: 'r', main_commit: '', run: '', observed_at: observed, basis: 'note.landing; current validity requires verification' })
const external = (raw: Raw, more: Rec) => {
  const w = (raw as Full).external_work
  w.push({ ...w[0], external_work_id: `ew_${w.length}`, plan_node_id: 'R5:5.1-4', state: 'Running', status: 'Running', started_at: min(20), heartbeat_age_s: 4, ended_at: undefined, ...more })
}
/** An acceptance sign-off the plan view marks Ready (owner_waiting, PV2). */
const signoff = (nid: string) => ({ plan_node_id: nid, ref: 'acc-1', acceptance: 'Owner accepts 5.4', plan_version: 1, evidence: null, candidate_evidence: [],
  reason: 'every binding true', source: 'plan view', readiness: 'Ready' })
/** The base view: 5.1-3 active with its implementation T3 back to ready, 5.2-1's task T_OTHER ready (no attempt); change moves it on. */
const view = (change: (raw: Raw) => void = () => {}): PlanSnapshot => {
  const raw = r5Raw()
  task(raw, T_OTHER).state = 'ready'
  change(raw)
  return parsePlanSnapshot(raw, 'R5')
}
const clock = (o: Partial<StepClock> = {}): StepClock => ({ failed: null, now: NOW, ...o })
const cell = (v: PlanSnapshot, label: string, c: StepClock = clock()): StepCell => stepRows(v, c).flatMap(r => r.cells).find(x => x.label === label)!
const row = (rows: ModuleRow[], label: string) => rows.find(r => r.label === label)!
/** 5.1-4 active, no task on it: change adds what a case needs. */
const on514 = (change: (raw: Raw) => void = () => {}) => view(raw => { activate(raw, 'R5:5.1-4'); change(raw) })
const at = (now: number) => { vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(now) }

describe('step state now (model)', () => {
  it('an active step with nothing running, queued or held, read now, is Active · nothing running: never In progress', () => {
    const c = cell(on514(), '5.1-4')
    expect(c).toMatchObject({ step: 'active', state: 'idle', now: { kind: 'idle' } })
    expect(cellLabels[c.state]).toBe('Active · nothing running')
    expect(spokenState(c)).toBe('active, nothing running')
    expect(nowLines(c)[0]).toMatch(/^Active · nothing running, read now: no running attempt with its heartbeat confirmed, landing seen moving or running external work; nothing queued/)
    expect(nowLines(c).join(' ')).not.toMatch(/In progress/)
  })

  it('a stale read is Unknown now with the reason the read gives, never idle', () => {
    const v = on514()
    const old = cell(v, '5.1-4', clock({ now: Date.parse(AT) + 61_000 }))
    expect(old).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown', why: [`Round 5's plan view is older than 60 s; as of ${AT}`] } })
    expect(spokenState(old)).toBe('active, what runs on it now is Unknown')
    expect(nowLines(old)[0]).toBe(`Unknown · Round 5's plan view is older than 60 s; as of ${AT}.`)
    expect(cell(v, '5.1-4', clock({ failed: 'Offline' })).now).toEqual({ kind: 'unknown', why: [`Round 5's plan view could not be read (Offline); as of ${AT}`], queued: [], held: [] })
    const notFresh = on514(raw => { (raw as Full).freshness.database.status = 'Stale' })
    expect(cell(notFresh, '5.1-4').now).toMatchObject({ kind: 'unknown', why: [`Round 5's plan view reads the database as Stale; as of ${AT}`] })
    // Even a step with work running reads Unknown from an old read: what runs now is not known from it.
    const running = on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M')) })
    expect(cell(running, '5.1-4', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('active-unknown')
    // Planned, done and stopped keep the controller's state whatever the read's age.
    expect(cell(v, '5.1-1', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('done')
    expect(cell(v, '5.1-6', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('planned')
  })

  it('an attempt whose heartbeat is unconfirmed is Unknown, not running and not idle', () => {
    // 50 s at the snapshot is unconfirmed 15 s later (65 s): the Live tab's rule.
    const v = on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M'), { heartbeat_age_s: 50 }) })
    expect(cell(v, '5.1-4', clock({ now: Date.parse(AT) + 5_000 })).now).toMatchObject({ kind: 'running', what: 'Task running' })
    expect(cell(v, '5.1-4', clock({ now: Date.parse(AT) + 15_000 })).now).toEqual({ kind: 'unknown', queued: [], held: [],
      why: ["task Work M: its attempt's heartbeat is unconfirmed (65 s, over 60 s)"] })
    const server = on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M'), { status: 'Unconfirmed' }) })
    expect(cell(server, '5.1-4').now).toMatchObject({ kind: 'unknown', why: ["task Work M: its attempt's heartbeat is unconfirmed (the server says Unconfirmed)"] })
  })

  it('a stale attempt is Unknown; a task leased or running with no live attempt is Unknown; a state this page does not read is Unknown', () => {
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M'), { state: 'stale', heartbeat_age_s: 130 }) }), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: ['task Work M: its attempt is stale'] })
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'running') }), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: ['task Work M is running, but the plan view holds no live attempt for it'] })
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'dreaming') }), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: ['task Work M: its state "dreaming" is not one this page reads'] })
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('someday') }) }), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: ['landing of task Work M: its phase "someday" is not one this page reads'] })
  })

  it('work running now (the reasons that ring the cell, R5:5.5-8) is In progress, and marks the same step live', () => {
    const v = on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M')) })
    const c = cell(v, '5.1-4')
    expect(c).toMatchObject({ state: 'active', now: { kind: 'running', what: 'Task running', others: 0 } })
    expect(spokenState(c)).toBe('in progress')
    expect(nowLines(c)[0]).toBe('In progress · running now: Task running.')
    const live = liveNow(v, moduleRows(v), { failed: null, now: NOW })
    expect(live.state === 'ok' && [...live.byStep.keys()]).toEqual(['R5:5.1-4'])
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('branch_pushed') }) }), '5.1-4').now)
      .toMatchObject({ kind: 'running', what: 'Landing branch pushed' })
    const ext = on514(raw => external(raw, {}))
    expect(cell(ext, '5.1-4').now).toMatchObject({ kind: 'running', what: expect.stringMatching(/^External work running/) })
  })

  it('a moving landing with no record for over 30 min is Unknown with that condition, not running; its ring still marks it (5.5-8)', () => {
    const quiet = min(180)
    const v = on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('branch_pushed', '', quiet) }) })
    const c = cell(v, '5.1-4')
    expect(c).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown',
      why: [`landing of task Work M at phase "branch_pushed": no landing record for over 30 min (last ${quiet})`] } })
    expect(nowLines(c).join(' ')).not.toMatch(/running now/)
    const live = liveNow(v, moduleRows(v), { failed: null, now: NOW })
    expect(live.state === 'ok' && live.byStep.get('R5:5.1-4')?.quiet).toBe(true)
    // A record 29 min old is a landing seen moving.
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('branch_pushed', '', min(29)) }) }), '5.1-4').now)
      .toMatchObject({ kind: 'running', what: 'Landing branch pushed' })
  })

  it('a task ready, proposed, back for rework, submitted or verifying is queued: In progress, not idle', () => {
    // 5.1-3 as the fixture has it: its implementation T3 back to ready after its verification T5 failed (T5 accepted: a check, it does not land).
    const base = cell(view(), '5.1-3')
    expect(base).toMatchObject({ state: 'active', now: { kind: 'queued', why: ['task Module detail page ready'], held: [] } })
    expect(spokenState(base)).toBe('in progress')
    for (const [state, words] of [['proposed', 'proposed'], ['rework', 'back for rework'], ['submitted', 'submitted, waiting for its check'], ['verifying', 'being verified']]) {
      expect(cell(view(raw => { task(raw, T3).state = state }), '5.1-3').now).toEqual({ kind: 'queued', why: [`task Module detail page ${words}`], held: [] })
    }
  })

  it('an accepted work task with no landing record holds the step (no landing promised), never in progress; a check, or one whose landing ended, does not', () => {
    // The probe of the review: an accepted design task, which the server would never land, must not read In progress for ever.
    const c = cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { title: 'Plan R5:5.1-4 design' }) }), '5.1-4')
    expect(c).toMatchObject({ state: 'held', now: { kind: 'held', held: [{ kind: 'no-landing', text: 'task Plan R5:5.1-4 design accepted · no landing recorded' }], recorded: null } })
    expect(spokenState(c)).toBe('active, held: 1 accepted task with no landing recorded')
    expect(nowLines(c)[0]).toBe('Active · held: task Plan R5:5.1-4 design accepted · no landing recorded. Whether an accepted task lands the plan view does not say: it may never land; '
      + 'nothing on the step runs or is queued.')
    expect(nowLines(c).join(' ')).not.toMatch(/In progress|landing not begun/)
    // 5.1-3 with its implementation cancelled: what is left is the accepted check T5 ("verify <T3>"), which does not land.
    expect(cell(view(raw => { task(raw, T3).state = 'cancelled' }), '5.1-3').now).toEqual({ kind: 'idle' })
    expect(task(r5Raw(), T5).title).toBe(`verify ${T3}`)
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('landed', 'ok') }) }), '5.1-4').now).toEqual({ kind: 'idle' })
  })

  it("a landing awaiting the grant holds the step on your grant; a parked one is queued (the controller unparks it); an ended one queues nothing", () => {
    const grant = cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('awaiting_grant') }) }), '5.1-4')
    expect(grant).toMatchObject({ state: 'held', now: { kind: 'held', held: [{ kind: 'grant', text: 'landing of task Work M waits for your grant' }], recorded: null } })
    expect(spokenState(grant)).toBe('active, held: 1 landing waits for your grant')
    expect(nowLines(grant)[0]).toBe('Active · held: landing of task Work M waits for your grant. A landing awaiting the grant waits on your grant (Needs you lists grants); nothing on the step runs or is queued.')
    const parked = cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: { ...landing('parked'), reason: 'window' } }) }), '5.1-4')
    expect(parked).toMatchObject({ state: 'active', now: { kind: 'queued', why: ['landing of task Work M parked (window)'], held: [] } })
    expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('parked') }) }), '5.1-4').now)
      .toEqual({ kind: 'queued', why: ['landing of task Work M parked'], held: [] })
    for (const [phase, outcome] of [['landed', 'ok'], ['failed', 'ci_red'], ['stg_verified', ''], ['main_result', 'non_ff']]) {
      expect(cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing(phase, outcome) }) }), '5.1-4').now).toEqual({ kind: 'idle' })
    }
    // Two kinds held at once are each counted aloud.
    const both = cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { landing: landing('awaiting_grant') }); addTask(raw, id('N'), 'R5:5.1-4', 'blocked') }), '5.1-4')
    expect(spokenState(both)).toBe('active, held: 1 task waits on a decision, 1 landing waits for your grant')
  })

  it('external work registered and not started is queued; without a confirmed heartbeat it is Unknown', () => {
    expect(cell(on514(raw => external(raw, { state: 'Registered', status: 'Registered', started_at: undefined })), '5.1-4').now)
      .toEqual({ kind: 'queued', why: [expect.stringMatching(/^external work “.+” registered, not started$/)], held: [] })
    expect(cell(on514(raw => external(raw, { state: 'Registered', status: 'Registered', started_at: undefined, heartbeat_age_s: 70 })), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: [expect.stringMatching(/” Registered: no heartbeat confirmed for over 60 s$/)] })
    expect(cell(on514(raw => external(raw, { status: 'Unconfirmed', heartbeat_age_s: 90 })), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: [expect.stringMatching(/” Running: no heartbeat confirmed for over 60 s$/)] })
    expect(cell(on514(raw => external(raw, { state: 'Cancelled', status: 'Cancelled' })), '5.1-4').now).toEqual({ kind: 'idle' })
  })

  it('external work submitted and not read Verified holds the step: Active · held, counted held, never in progress; Verified has ended', () => {
    const v = on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }))
    const c = cell(v, '5.1-4')
    expect(c).toMatchObject({ state: 'held', now: { kind: 'held', recorded: null,
      held: [{ kind: 'unverified', text: expect.stringMatching(/^external work “.+” submitted · waits for its verification \(verification Unknown\)$/) }] } })
    expect(cellLabels[c.state]).toBe('Active · held')
    expect(spokenState(c)).toBe('active, held: 1 external work waits for verification')
    expect(nowLines(c)[0]).toMatch(/^Active · held: external work “.+” submitted · waits for its verification \(verification Unknown\)\. Submitted external work leaves this only when its verification reads Verified; nothing on the step runs or is queued\.$/)
    // A failed verification is said as such, not as waiting for one.
    const failed = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Verification failed' })), '5.1-4')
    expect(failed.now).toEqual({ kind: 'held', recorded: null, held: [{ kind: 'verify-failed', text: expect.stringMatching(/^external work “.+” submitted · verification failed$/) }] })
    expect(spokenState(failed)).toBe('active, held: 1 external work failed verification')
    const r51 = stepRows(v, clock()).find(r => r.label === '5.1')!
    expect(r51).toMatchObject({ held: 1, queued: 1, running: 0, idle: 0 })
    expect(stepCounts(r51)).toBe('2 / 6 done · 1 queued · 1 held')
    // 5.1 with its queued step done: the module is held, not in progress.
    const only = stepRows(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Stale' }); node(raw, 'R5:5.1-3').state = 'done' }), clock())
    expect(moduleClass(row(only, '5.1'))).toBe('held')
    expect(stepCounts(row(only, '5.1'))).toBe('3 / 6 done · 1 held')
    expect(cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Verified' })), '5.1-4').now).toEqual({ kind: 'idle' })
    // Beside a queued task the step is in progress, and the submitted work is still listed as held.
    const beside = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }); addTask(raw, id('M'), 'R5:5.1-4', 'ready') }), '5.1-4')
    expect(beside.now).toEqual({ kind: 'queued', why: ['task Work M ready'], held: [expect.stringMatching(/submitted · waits for its verification/)] })
  })

  it('tasks blocked, paused or awaiting the Owner hold the step: its own state, not idle, not Unknown, each listed', () => {
    const c = cell(on514(raw => {
      addTask(raw, id('M'), 'R5:5.1-4', 'blocked'); addTask(raw, id('N'), 'R5:5.1-4', 'paused'); addTask(raw, id('P'), 'R5:5.1-4', 'awaiting_owner')
      addTask(raw, id('R'), 'R5:5.1-4', 'cancelled')
    }), '5.1-4')
    expect(c).toMatchObject({ state: 'held', now: { kind: 'held', recorded: null,
      held: ['task Work M blocked', 'task Work N paused', 'task Work P awaiting the Owner'].map(text => ({ kind: 'decision', text })) } })
    expect(cellLabels[c.state]).toBe('Active · held')
    expect(spokenState(c)).toBe('active, held: 3 tasks wait on a decision')
    expect(nowLines(c)[0]).toBe('Active · held: task Work M blocked; task Work N paused; task Work P awaiting the Owner. A held task moves only when it is made ready again or cancelled; nothing on the step runs or is queued.')
    // Beside a queued task the step is in progress, and the held one is still listed.
    const queued = cell(on514(raw => { addTask(raw, id('M'), 'R5:5.1-4', 'blocked'); addTask(raw, id('N'), 'R5:5.1-4', 'ready') }), '5.1-4')
    expect(queued.now).toEqual({ kind: 'queued', why: ['task Work N ready'], held: ['task Work M blocked'] })
    expect(nowLines(queued)).toContain('Held: task Work M blocked.')
  })

  it('Unknown is said before queued (with the queue and the held kept), and running before Unknown', () => {
    const both = cell(on514(raw => {
      addTask(raw, id('M'), 'R5:5.1-4', 'ready'); addTask(raw, id('N'), 'R5:5.1-4', 'ready'); attempt(raw, id('N'), { state: 'stale' }); addTask(raw, id('P'), 'R5:5.1-4', 'blocked')
    }), '5.1-4')
    expect(both.now).toEqual({ kind: 'unknown', why: ['task Work N: its attempt is stale'], queued: ['task Work M ready'], held: ['task Work P blocked'] })
    expect(nowLines(both)).toEqual(['Unknown · task Work N: its attempt is stale.', 'Queued as read: task Work M ready.', 'Held: task Work P blocked.'])
    const run = cell(on514(raw => {
      addTask(raw, id('M'), 'R5:5.1-4', 'ready'); attempt(raw, id('M'))
      addTask(raw, id('N'), 'R5:5.1-4', 'ready'); attempt(raw, id('N'), { state: 'stale' })
    }), '5.1-4')
    expect(run.now).toMatchObject({ kind: 'running', what: 'Task running' })
  })

  it('an Owner step names the sources it read, never that nothing asks the Owner; with records, names them', () => {
    // 5.4-3 is the Owner's step on PROD, its done rule {}.
    const bare = cell(view(raw => activate(raw, 'R5:5.4-3')), '5.4-3')
    expect(bare).toMatchObject({ owner: true, state: 'yours', now: { kind: 'yours', recorded: [] } })
    expect(cellLabels[bare.state]).toBe('Yours')
    expect(spokenState(bare)).toBe('yours, none in plan-view owner_waiting or its done rule')
    expect(nowLines(bare)[0]).toBe('Yours · none in plan-view owner_waiting or its done rule: no wait for you listed on it, and no question, request or sign-off named. '
      + 'Questions or requests tied to it only on Needs you are not read here.')
    expect([spokenState(bare), ...nowLines(bare)].join(' ')).not.toMatch(/nothing (recorded )?asks you/)
    // 5.1-5 is the Owner's step whose done rule waits on a question: recorded, not said to ask or not.
    const ruled = cell(view(raw => activate(raw, 'R5:5.1-5')), '5.1-5')
    expect(ruled.now).toEqual({ kind: 'yours', recorded: [`question ${Q_RULE} (its done rule)`] })
    expect(spokenState(ruled)).toBe(`yours, recorded on it: question ${Q_RULE} (its done rule)`)
    // An acceptance sign-off the plan view marks Ready on it is recorded too.
    const accepted = cell(view(raw => { activate(raw, 'R5:5.4-3'); (raw as Full).owner_waiting.push(signoff('R5:5.4-3')) }), '5.4-3')
    expect(accepted.now).toEqual({ kind: 'yours', recorded: ['acceptance sign-off Owner accepts 5.4 Ready (plan-view owner_waiting)'] })
    // With work queued it is in progress, not yours.
    expect(cell(view(raw => { activate(raw, 'R5:5.4-3'); addTask(raw, id('M'), 'R5:5.4-3', 'ready') }), '5.4-3').now).toEqual({ kind: 'queued', why: ['task Work M ready'], held: [] })
    // An old read: Unknown now, not yours.
    expect(cell(view(raw => activate(raw, 'R5:5.4-3')), '5.4-3', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('active-unknown')
  })

  it('an Owner step with a task held lists it (held, with what is recorded on it); with a stale attempt it is Unknown, not yours', () => {
    const blocked = cell(view(raw => {
      activate(raw, 'R5:5.4-3')
      addTask(raw, id('P'), 'R5:5.4-3', 'awaiting_owner')
      ;(raw as Full).owner_waiting.push({ task_id: id('P'), plan_node_id: 'R5:5.4-3', reason: 'Runtime task awaits Owner', source: 'agentrt.task' })
      addTask(raw, id('M'), 'R5:5.4-3', 'blocked')
    }), '5.4-3')
    expect(blocked).toMatchObject({ state: 'held', now: { kind: 'held', held: ['task Work P awaiting the Owner', 'task Work M blocked'].map(text => ({ kind: 'decision', text })),
      recorded: ['task Work P awaits you (plan-view owner_waiting)'] } })
    // Its state reads Active · held, so its records are the Owner step's, not "Yours".
    expect(nowLines(blocked)).toContain('Owner step · recorded on it: task Work P awaits you (plan-view owner_waiting). Whether each still asks you is on Needs you.')
    expect(nowLines(blocked).some(l => l.startsWith('Yours'))).toBe(false)
    const stale = cell(view(raw => { activate(raw, 'R5:5.4-3'); addTask(raw, id('M'), 'R5:5.4-3', 'ready'); attempt(raw, id('M'), { state: 'stale' }) }), '5.4-3')
    expect(stale).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown', why: ['task Work M: its attempt is stale'] } })
  })

  it("says since when only from the step's state event in the recent window, when that event moved it to its state", () => {
    // 5.1-3 went active by event 9003 (plan_node_state_changed to active, at 02:40) in the window.
    const c = cell(view(), '5.1-3')
    expect(c.since).toBe('2026-10-27T02:40:00Z')
    expect(nowLines(c)).toContain('Active since 2026-10-27T02:40:00Z (its state event).')
    expect(cell(on514(), '5.1-4').since).toBeNull()
    expect(cell(view(raw => { raw.recent_changes = raw.recent_changes.filter(e => e.event_id !== 9003) }), '5.1-3').since).toBeNull()
    // An event that moved the step to another state than the one it holds says nothing of when it took this one.
    const other = view(raw => { (raw.recent_changes.find(e => e.event_id === 9003)!.body_json as Rec).to = 'done' })
    expect(cell(other, '5.1-3').since).toBeNull()
  })

  it('with no clock, an active step is Unknown now (not judged), never in progress', () => {
    const c = moduleRows(on514()).flatMap(r => r.cells).find(x => x.label === '5.1-4')!
    expect(c).toMatchObject({ step: 'active', state: 'active-unknown', now: { kind: 'unknown' } })
  })
})

describe('done rule {}', () => {
  it('is marked on an active or stopped step, not on a planned one', () => {
    const v = view(raw => { activate(raw, 'R5:5.1-4'); node(raw, 'R5:5.1-6').state = 'stopped' })
    expect(noRuleMarked(cell(v, '5.1-4'))).toBe(true)
    expect(noRuleMarked(cell(v, '5.1-6'))).toBe(true)
    const planned = cell(v, '5.2-1')
    expect(planned).toMatchObject({ step: 'planned', noRule: true })
    expect(noRuleMarked(planned)).toBe(false)
    // A rule with a condition is not marked, active or not.
    expect(noRuleMarked(cell(v, '5.1-3'))).toBe(false)
  })

  it('marks the active and stopped cells and their detail; a planned one only in its detail', () => {
    at(NOW)
    render(<NowClock><PlanModuleRows view={view(raw => { activate(raw, 'R5:5.1-4'); node(raw, 'R5:5.1-6').state = 'stopped' })} selected="" /></NowClock>)
    const bar = screen.getByRole('toolbar', { name: /^Steps of 5\.1,/ })
    expect(within(bar).getAllByRole('button').map(b => b.getAttribute('aria-label'))).toEqual(['Step 5.1-1: done', 'Step 5.1-2: done', 'Step 5.1-3: in progress',
      'Step 5.1-4: active, nothing running, no done rule', 'Step 5.1-5: not started', 'Step 5.1-6: stopped, no done rule'])
    expect(bar.querySelectorAll('[data-no-rule-mark]')).toHaveLength(2)
    // Every planned step of 5.2 has rule {}: none is marked in its cell.
    expect(screen.getByRole('toolbar', { name: /^Steps of 5\.2,/ }).querySelectorAll('[data-no-rule-mark]')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Step 5.1-4: active, nothing running, no done rule' }))
    const active = screen.getByRole('region', { name: 'Step 5.1-4' })
    expect(active.querySelector('[data-no-rule-text]')).toHaveTextContent(/^No done rule: nothing closes it by evidence\./)
    expect(active.querySelector('[data-no-rule]')).toHaveTextContent('No done rule')
    expect(active).toHaveTextContent('Active · nothing running')
    fireEvent.click(screen.getByRole('button', { name: 'Step 5.2-1: not started' }))
    const planned = screen.getByRole('region', { name: 'Step 5.2-1' })
    expect(planned.querySelector('[data-no-rule-text]')).toHaveTextContent(/^No done rule yet\./)
    expect(planned.querySelector('[data-no-rule]')).toBeNull()
  })
})

describe('counts of the derived states', () => {
  /** 5.1: 5.1-3 queued, 5.1-4 idle, 5.1-5 (Owner) yours; 5.2-1 running. */
  const mixed = () => view(raw => {
    activate(raw, 'R5:5.1-4'); activate(raw, 'R5:5.1-5'); activate(raw, 'R5:5.2-1')
    attempt(raw, T_OTHER)
  })

  it('count running, queued, held, idle, yours and Unknown apart in the rows and the summary, never idle as in progress', () => {
    const v = mixed()
    const rows = stepRows(v, clock())
    const r51 = row(rows, '5.1')
    expect(r51).toMatchObject({ active: 3, running: 0, queued: 1, held: 0, idle: 1, yours: 1, nowUnknown: 0, done: 2 })
    expect(stepCounts(r51)).toBe('2 / 6 done · 1 queued · 1 idle · 1 yours')
    expect(stepCounts(row(rows, '5.2'))).toMatch(/^0 \/ \d+ done · 1 running$/)
    expect(mainlineSentence(v, rows).text).toBe('The mainline stands at 5.1 · runtime steps 2 of 6 done, 1 queued, 1 idle, 1 yours.')
    expect(moduleClass(r51)).toBe('in-progress')
    // 5.1 with its queued step done: an idle and an Owner step are not in progress.
    const after = stepRows(view(raw => { activate(raw, 'R5:5.1-4'); node(raw, 'R5:5.1-3').state = 'done' }), clock())
    expect(moduleClass(row(after, '5.1'))).toBe('idle')
    expect(othersSentence(after, 'R5:5.2')).toMatch(/^By runtime step state, of the 9 other items, 1 has an active step with nothing running and /)
    // A held step is counted and classed apart from idle.
    const held = stepRows(view(raw => { activate(raw, 'R5:5.1-4'); node(raw, 'R5:5.1-3').state = 'done'; addTask(raw, id('M'), 'R5:5.1-4', 'blocked') }), clock())
    expect(stepCounts(row(held, '5.1'))).toBe('3 / 6 done · 1 held')
    expect(moduleClass(row(held, '5.1'))).toBe('held')
    expect(mainlineSentence(v, held).text).toBe('The mainline stands at 5.1 · runtime steps 3 of 6 done, 1 held.')
    // An old read: every active step is Unknown now; none idle, none in progress.
    const old = row(stepRows(v, clock({ now: Date.parse(AT) + 61_000 })), '5.1')
    expect(old).toMatchObject({ active: 3, running: 0, queued: 0, idle: 0, yours: 0, nowUnknown: 3 })
    expect(stepCounts(old)).toBe('2 / 6 done · 3 active, Unknown now')
    expect(moduleClass(old)).toBe('active-unknown')
  })

  it('the module row says the same counts on the page', () => {
    at(NOW)
    render(<NowClock><PlanModuleRows view={mixed()} selected="" /></NowClock>)
    const items = within(screen.getByRole('list', { name: 'Modules in mainline order' })).getAllByRole('listitem').filter(li => li.dataset.module)
    expect(items.find(li => li.dataset.module === '5.1')).toHaveTextContent('2 / 6 done · 1 queued · 1 idle · 1 yours')
    expect(screen.getByRole('button', { name: 'Step 5.2-1: in progress, live now: Task running, no done rule' })).toHaveAttribute('data-live', 'true')
    expect(screen.getByRole('button', { name: `Step 5.1-5: yours, recorded on it: question ${Q_RULE} (its done rule)` })).toHaveAttribute('data-state', 'yours')
  })

  it('draws an active step Unknown now apart from a step with no state, in the cell and the legend', () => {
    at(NOW)
    render(<NowClock><PlanModuleRows view={view()} selected="" failed="Plan view: HTTP 502" /></NowClock>)
    const legend = (s: string) => document.querySelector(`[data-legend="${s}"]`) as HTMLElement
    expect(legend('active-unknown')).toHaveTextContent('Active · Unknown now')
    expect(legend('unknown')).toHaveTextContent('Unknown (no state)')
    expect(cellStyle['active-unknown']).not.toEqual(cellStyle.unknown)
    // A failed refresh, its snapshot 5 s old: the active step is Unknown now, its state still active.
    const c = screen.getByRole('button', { name: 'Step 5.1-3: active, what runs on it now is Unknown' })
    expect(c).toHaveAttribute('data-state', 'active-unknown')
    fireEvent.click(c)
    expect(screen.getByRole('region', { name: 'Step 5.1-3' })).toHaveTextContent(`Unknown · Round 5's plan view could not be read (Plan view: HTTP 502); as of ${AT}.`)
  })
})

describe('Round 5 as on PROD 2026-11-02', () => {
  it('5.4-1 and 5.4-2, active with rule {} and no task, read Active · nothing running with the no-rule mark, not In progress', () => {
    at(NOW)
    render(<NowClock><PlanModuleRows view={view(raw => { activate(raw, 'R5:5.4-1'); activate(raw, 'R5:5.4-2') })} selected="" /></NowClock>)
    for (const label of ['5.4-1', '5.4-2']) {
      const button = screen.getByRole('button', { name: `Step ${label}: active, nothing running, no done rule` })
      expect(button).toHaveAttribute('data-state', 'idle')
      expect(button.getAttribute('title')).toMatch(new RegExp(`^${label.replace('.', '\\.')} · Active · nothing running · No done rule · `))
    }
    const item = within(screen.getByRole('list', { name: 'Modules in mainline order' })).getAllByRole('listitem').find(li => li.dataset.module === '5.4')!
    expect(item).toHaveTextContent('0 / 6 done · 2 idle')
    expect(item).not.toHaveTextContent('in progress')
  })
})

describe('the module page', () => {
  const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
  const page = (v: PlanSnapshot, failed: string | null = null) => {
    const opened = moduleOf(v, 'R5:5.1')!
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>
      <PlanModuleDetail view={v} item={opened.item} step="" stale={failed !== null} failed={failed} /></NowClock></PlatformAuthContext.Provider></QueryClientProvider>)
    return client
  }
  const stepRow = (label: string) => document.querySelector(`[data-step-row="${label}"]`) as HTMLElement

  it('counts and names each active step by what it has now, and marks the one with no done rule', () => {
    at(NOW)
    const client = page(view(raw => activate(raw, 'R5:5.1-4')))
    expect(screen.getByRole('region', { name: 'Steps to go' })).toHaveTextContent('of 6 · 2 done · 1 queued · 1 idle')
    expect(screen.getByRole('region', { name: 'Steps to go' })).not.toHaveTextContent('in progress')
    expect(within(screen.getByRole('region', { name: 'Now' })).getByRole('link', { name: 'Step 5.1-4 active, nothing running' })).toBeInTheDocument()
    expect(stepRow('5.1-4').querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running')
    expect(stepRow('5.1-4').querySelector('[data-no-rule]')).toHaveTextContent('No done rule')
    expect(stepRow('5.1-4').querySelector('[data-step-now="idle"]')).toHaveTextContent(/^Active · nothing running, read now:/)
    expect(stepRow('5.1-4').querySelector('[data-no-rule-text]')).toHaveTextContent(/^No done rule: nothing closes it by evidence\./)
    expect(stepRow('5.1-3').querySelector('[data-step-state]')).toHaveTextContent('In progress')
    client.clear()
  })

  it('a failed refresh makes every active step Unknown now on the page, with why, and says so aloud on Now', () => {
    at(NOW)
    const client = page(view(raw => activate(raw, 'R5:5.1-4')), 'Offline')
    expect(screen.getByRole('region', { name: 'Steps to go' })).toHaveTextContent('of 6 · 2 done · 2 active, Unknown now')
    expect(stepRow('5.1-4').querySelector('[data-step-now="unknown"]')).toHaveTextContent(`Unknown · Round 5's plan view could not be read (Offline); as of ${AT}.`)
    expect(within(screen.getByRole('region', { name: 'Now' })).getByRole('link', { name: 'Step 5.1-4 active, what runs on it now is Unknown' })).toBeInTheDocument()
    client.clear()
  })

  it('judges the module again as the read ages, on the page clock (its scopes built once per plan view)', () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(NOW)
    const client = page(view(raw => activate(raw, 'R5:5.1-4')))
    expect(stepRow('5.1-4').querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running')
    act(() => { vi.setSystemTime(Date.parse(AT) + 60_000); vi.advanceTimersByTime(2_000) })
    expect(stepRow('5.1-4').querySelector('[data-step-state]')).toHaveTextContent('Active · Unknown now')
    expect(stepRow('5.1-4').querySelector('[data-step-now="unknown"]')).toHaveTextContent(`Unknown · Round 5's plan view is older than 60 s; as of ${AT}.`)
    client.clear()
  })
})

describe('Work with a failed refresh', () => {
  const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }

  it('the summary and the rows say the active step is Unknown now once the refresh fails, though the last snapshot is young', async () => {
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
    vi.spyOn(Date, 'now').mockReturnValue(NOW)
    const raw = r5Raw()
    task(raw, T_OTHER).state = 'ready'
    let status = 200
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => !String(input).includes('/agentrt/plan-view')
      ? new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })
      : new Response(JSON.stringify(status === 200 ? raw : { error: 'unavailable' }), { status })))
    window.location.hash = '#work'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress({ plan: [{ plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }] }))
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
    const sentence = async () => (await screen.findByRole('region', { name: 'Where the round stands' })).querySelector('[data-summary-sentence]')
    await waitFor(async () => expect(await sentence()).toHaveTextContent('The mainline stands at 5.1 · runtime steps 2 of 6 done, 1 queued.'))
    expect(screen.getByRole('button', { name: 'Step 5.1-3: in progress' })).toBeInTheDocument()
    status = 503
    await act(async () => { await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] }) })
    await waitFor(async () => expect(await sentence()).toHaveTextContent('The mainline stands at 5.1 · runtime steps 2 of 6 done, 1 active, Unknown now.'))
    expect(screen.getByRole('button', { name: 'Step 5.1-3: active, what runs on it now is Unknown' })).toHaveAttribute('data-state', 'active-unknown')
    client.clear()
  })

  it("tells a check task apart by the runtime's role alike in the summary and the rows (the shell's cached progress, no fetch)", async () => {
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
    vi.spyOn(Date, 'now').mockReturnValue(NOW)
    const raw = r5Raw()
    task(raw, T_OTHER).state = 'ready'
    activate(raw, 'R5:5.1-4')
    // An accepted task whose title names no check: only the runtime says it is a review (it does not land).
    addTask(raw, id('M'), 'R5:5.1-4', 'accepted', { title: 'Look over the page' })
    const fetcher = vi.fn(async (input: RequestInfo | URL) => String(input).includes('/agentrt/plan-view')
      ? new Response(JSON.stringify(raw), { status: 200 }) : new Response(JSON.stringify({ error: 'not_found' }), { status: 404 }))
    vi.stubGlobal('fetch', fetcher)
    window.location.hash = '#work'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress({ plan: [{ plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }],
      work: { missions: [{ mission_id: 'M-250', tasks: [{ task_id: id('M'), role: 'review', state: 'accepted' }] }] } }))
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
    const summary = await screen.findByRole('region', { name: 'Where the round stands' })
    await waitFor(() => expect(summary.querySelector('[data-summary-sentence]')).toHaveTextContent('The mainline stands at 5.1 · runtime steps 2 of 6 done, 1 queued, 1 idle.'))
    expect(screen.getByRole('button', { name: 'Step 5.1-4: active, nothing running, no done rule' })).toHaveAttribute('data-state', 'idle')
    expect(fetcher.mock.calls.some(([input]) => String(input).includes('/progress'))).toBe(false)
    client.clear()
  })
})
