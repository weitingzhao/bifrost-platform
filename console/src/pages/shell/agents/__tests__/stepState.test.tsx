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
import { cellLabels, cellStyle, howRead, moduleRows, noRuleMarked, nowLines, spokenState, stepCounts, unverifiedNote, type ModuleRow, type StepCell } from '../planModuleModel'
import { PlanModuleRows } from '../PlanModuleRows'
import { stepRows, type StepClock } from '../stepStateModel'
import { WorkPlanView } from '../WorkPlanView'
import { mainlineSentence, moduleClass, othersSentence } from '../workSummaryModel'
import { AT, externalWork, Q_RULE, r5Raw, type Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

/*
 * R5:5.5-10 (ISSUE-33): an active step is shown by what the plan view, read now, has on it; one with
 * nothing running or queued is not "In progress"; a step whose done rule is {} is marked while active or stopped. The R5
 * plan view of the module detail's tests (moduleDetailFixture), moved on as each case needs, in the shapes plan-view
 * writes (registered external work is in external_work, and in working while it still expects a heartbeat).
 */
const NOW = Date.parse(AT) + 5_000
const min = (n: number) => new Date(Date.parse(AT) - n * 60_000).toISOString()
type Rec = Record<string, unknown>
type Full = Raw & { freshness: { database: { status: string } } }
const node = (raw: Raw, nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
const activate = (raw: Raw, nid: string, more: Rec = {}) => Object.assign(node(raw, nid), { state: 'active', ...more })
/** External work on 5.1-4 (or more.plan_node_id): Running, started 20 min ago, its heartbeat 4 s old, unless more says otherwise. */
const external = (raw: Raw, more: Rec) => {
  const { plan_node_id = 'R5:5.1-4', ...rest } = more
  return externalWork(raw, plan_node_id as string, rest)
}
const titled = (raw: Raw, task: string) => ({ description: { ...(raw.external_work[0].description as Rec), task } })
const SUBMITTED = { state: 'Submitted', status: 'Submitted', type: 'submitted' }
const REGISTERED = { state: 'Registered', status: 'Registered', type: 'registered', started_at: undefined }
const FAILED = { ...SUBMITTED, verification: 'Verification failed' }
/** An acceptance sign-off the plan view marks Ready (owner_waiting, PV2). */
const signoff = (nid: string) => ({ plan_node_id: nid, ref: 'acc-1', acceptance: 'Owner accepts 5.4', plan_version: 1, evidence: null, candidate_evidence: [],
  reason: 'every binding true', source: 'plan view', readiness: 'Ready' })
/** The base view: 5.1-3 active with external work registered on it and not started; change moves it on. */
const view = (change: (raw: Raw) => void = () => {}): PlanSnapshot => {
  const raw = r5Raw()
  change(raw)
  return parsePlanSnapshot(raw, 'R5')
}
const clock = (o: Partial<StepClock> = {}): StepClock => ({ failed: null, now: NOW, ...o })
const cell = (v: PlanSnapshot, label: string, c: StepClock = clock()): StepCell => stepRows(v, c).flatMap(r => r.cells).find(x => x.label === label)!
const row = (rows: ModuleRow[], label: string) => rows.find(r => r.label === label)!
/** 5.1-4 active, no work on it: change adds what a case needs. */
const on514 = (change: (raw: Raw) => void = () => {}) => view(raw => { activate(raw, 'R5:5.1-4'); change(raw) })
const at = (now: number) => { vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(now) }

describe('step state now (model)', () => {
  it('an active step with nothing running now, queued or held, read now, is Active · nothing running now: never In progress', () => {
    const c = cell(on514(), '5.1-4')
    expect(c).toMatchObject({ step: 'active', state: 'idle', now: { kind: 'idle' } })
    expect(cellLabels[c.state]).toBe('Active · nothing running now')
    expect(spokenState(c)).toBe('active, nothing running now')
    // How it was read: four rows, folded on the card (R5:5.5-20 R6), not one long line.
    expect(nowLines(c)).toEqual([])
    expect(howRead(c)!.map(r => r.label)).toEqual(['Running', 'Queued', 'Waits on you', 'Held'])
    expect(howRead(c)![0].text).toBe('Nothing: no running external work with its heartbeat confirmed.')
    expect(howRead(c)![1].text).toBe('Nothing: no external work registered and not started.')
    expect(howRead(c)!.map(r => r.text).join(' ')).not.toMatch(/In progress/)
  })

  it('a stale read is Unknown now with the reason the read gives, never idle', () => {
    const v = on514()
    const old = cell(v, '5.1-4', clock({ now: Date.parse(AT) + 61_000 }))
    expect(old).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown', why: [`Round 5's plan view is older than 60 s; as of ${AT}`] } })
    expect(spokenState(old)).toBe('active, what runs on it now is Unknown')
    expect(nowLines(old)[0]).toBe(`Unknown · Round 5's plan view is older than 60 s; as of ${AT}.`)
    expect(cell(v, '5.1-4', clock({ failed: 'Offline' })).now).toEqual({ kind: 'unknown', why: [`Round 5's plan view could not be read (Offline); as of ${AT}`], queued: [], held: [], waiting: [] })
    const notFresh = on514(raw => { (raw as Full).freshness.database.status = 'Stale' })
    expect(cell(notFresh, '5.1-4').now).toMatchObject({ kind: 'unknown', why: [`Round 5's plan view reads the database as Stale; as of ${AT}`] })
    // Even a step with work running reads Unknown from an old read: what runs now is not known from it.
    const running = on514(raw => external(raw, {}))
    expect(cell(running, '5.1-4').now?.kind).toBe('running')
    expect(cell(running, '5.1-4', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('active-unknown')
    // Planned, done and stopped keep the controller's state whatever the read's age.
    expect(cell(v, '5.1-1', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('done')
    expect(cell(v, '5.1-6', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('planned')
  })

  it('work whose heartbeat is unconfirmed is Unknown, not running and not idle', () => {
    // 50 s at the snapshot is unconfirmed 15 s later (65 s): the Live tab's rule.
    const v = on514(raw => external(raw, { heartbeat_age_s: 50, ...titled(raw, 'Run M') }))
    expect(cell(v, '5.1-4', clock({ now: Date.parse(AT) + 5_000 })).now).toMatchObject({ kind: 'running', what: 'External work running (codex)' })
    expect(cell(v, '5.1-4', clock({ now: Date.parse(AT) + 15_000 })).now).toEqual({ kind: 'unknown', queued: [], held: [], waiting: [],
      why: ['external work “Run M” Running: no heartbeat confirmed for over 60 s'] })
    const server = on514(raw => external(raw, { status: 'Unconfirmed', ...titled(raw, 'Run M') }))
    expect(cell(server, '5.1-4').now).toMatchObject({ kind: 'unknown', why: ['external work “Run M” Running: no heartbeat confirmed for over 60 s'] })
  })

  it('work running now (the reasons that ring the cell, R5:5.5-8) is In progress, and marks the same step live', () => {
    const v = on514(raw => external(raw, {}))
    const c = cell(v, '5.1-4')
    expect(c).toMatchObject({ state: 'active', now: { kind: 'running', what: 'External work running (codex)', others: 0 } })
    expect(spokenState(c)).toBe('in progress')
    expect(nowLines(c)[0]).toBe('In progress · running now: External work running (codex).')
    const live = liveNow(v, moduleRows(v), { failed: null, now: NOW })
    expect(live.state === 'ok' && [...live.byStep.keys()]).toEqual(['R5:5.1-4'])
  })

  it('external work registered and not started is queued; without a confirmed heartbeat it is Unknown', () => {
    const queued = cell(on514(raw => external(raw, REGISTERED)), '5.1-4')
    expect(queued.now).toEqual({ kind: 'queued', why: [expect.stringMatching(/^external work “.+” registered, not started$/)], held: [], waiting: [] })
    // Queued is in progress, not idle: 5.1-3 as the fixture has it.
    const base = cell(view(), '5.1-3')
    expect(base).toMatchObject({ state: 'active', now: { kind: 'queued', why: ['external work “Module detail page” registered, not started'], held: [] } })
    expect(spokenState(base)).toBe('in progress')
    expect(cell(on514(raw => external(raw, { ...REGISTERED, heartbeat_age_s: 70 })), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: [expect.stringMatching(/” Registered: no heartbeat confirmed for over 60 s$/)] })
    expect(cell(on514(raw => external(raw, { status: 'Unconfirmed', heartbeat_age_s: 90 })), '5.1-4').now)
      .toMatchObject({ kind: 'unknown', why: [expect.stringMatching(/” Running: no heartbeat confirmed for over 60 s$/)] })
    expect(cell(on514(raw => external(raw, { state: 'Cancelled', status: 'Cancelled', type: 'cancelled' })), '5.1-4').now).toEqual({ kind: 'idle' })
  })

  it('external work submitted and not read Verified holds nothing: Active · nothing running now with a note, counted idle; Verified has ended', () => {
    const v = on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }))
    const c = cell(v, '5.1-4')
    expect(c).toMatchObject({ state: 'idle', now: { kind: 'idle', unverified: [{ task: expect.any(String), verification: 'Unknown' }] } })
    expect(cellLabels[c.state]).toBe('Active · nothing running now')
    expect(spokenState(c)).toBe('active, nothing running now')
    expect(nowLines(c).join(' ')).not.toMatch(/held:|waits for its verification|In progress/)
    expect(unverifiedNote(c)).toMatch(/^1 external work submitted, not read Verified \(verification Unknown\): “.+”$/)
    const r51 = stepRows(v, clock()).find(r => r.label === '5.1')!
    expect(r51).toMatchObject({ held: 0, idle: 1, queued: 1, running: 0 })
    expect(stepCounts(r51)).toBe('2 / 6 done · 1 queued · 1 idle')
    // No verification recorded is not a fact that anything waits either: said as read in the note.
    const none = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: undefined })), '5.1-4')
    expect(none.now?.kind).toBe('idle')
    expect(unverifiedNote(none)).toMatch(/\(verification not recorded\)/)
    // Verified has ended: no note.
    const verified = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Verified' })), '5.1-4')
    expect(verified.now).toEqual({ kind: 'idle' })
    expect(unverifiedNote(verified)).toBeNull()
    // Beside registered work the step is in progress; the submitted work is its note, not held.
    const beside = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }); external(raw, { ...REGISTERED, ...titled(raw, 'Run M') }) }), '5.1-4')
    expect(beside.now).toMatchObject({ kind: 'queued', why: ['external work “Run M” registered, not started'], held: [], unverified: [{ verification: 'Unknown' }] })
    // An old read says nothing of it: Unknown now, no note.
    expect(unverifiedNote(cell(v, '5.1-4', clock({ now: Date.parse(AT) + 61_000 })))).toBeNull()
  })

  it('external work whose verification read failed holds the step: a binding on it read false, so something must be decided', () => {
    const failed = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Verification failed' })), '5.1-4')
    expect(failed).toMatchObject({ state: 'held', now: { kind: 'held', recorded: null, held: [{ kind: 'verify-failed', text: expect.stringMatching(/^external work “.+” submitted · verification failed$/) }] } })
    expect(failed.now).not.toHaveProperty('unverified')
    expect(cellLabels[failed.state]).toBe('Active · held')
    expect(spokenState(failed)).toBe('active, held: 1 external work failed verification')
    expect(nowLines(failed)[0]).toMatch(/^Active · held: external work “.+” submitted · verification failed\. A submitted external work has a verification result of Verification failed; nothing on the step runs or is queued\.$/)
    // 5.1 with its queued step done: the module is held, not in progress.
    const only = stepRows(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Verification failed' }); node(raw, 'R5:5.1-3').state = 'done' }), clock())
    expect(moduleClass(row(only, '5.1'))).toBe('held')
    expect(stepCounts(row(only, '5.1'))).toBe('3 / 6 done · 1 held')
  })

  it('external work submitted whose verification reads Stale is Unknown now, named, with the note; running still comes first', () => {
    // A Stale reading is Unknown and does not show that an earlier failure cleared (store/planfactssnapshot.go reading).
    const STALE = /^external work “.+” submitted: its verification reading is stale \(the last recorded successful read is over 25 min old; an earlier failure is not known to have cleared\)$/
    const stale = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Stale' })), '5.1-4')
    expect(stale).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown', why: [expect.stringMatching(STALE)], queued: [], held: [], unverified: [{ verification: 'Stale' }] } })
    expect(spokenState(stale)).toBe('active, what runs on it now is Unknown')
    expect(nowLines(stale)[0]).toMatch(/^Unknown · external work “.+” submitted: its verification reading is stale/)
    expect(nowLines(stale).join(' ')).not.toMatch(/nothing running|nothing held/)
    expect(unverifiedNote(stale)).toMatch(/\(verification Stale\)/)
    // Unknown over queued and held, each kept.
    const queued = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Stale' }); external(raw, { ...REGISTERED, ...titled(raw, 'Run M') }) }), '5.1-4')
    expect(queued.now).toMatchObject({ kind: 'unknown', why: [expect.stringMatching(STALE)], queued: ['external work “Run M” registered, not started'], held: [] })
    const held = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Stale' }); external(raw, { ...FAILED, ...titled(raw, 'Run M') }) }), '5.1-4')
    expect(held.now).toMatchObject({ kind: 'unknown', why: [expect.stringMatching(STALE)], queued: [], held: ['external work “Run M” submitted · verification failed'] })
    // Running before Unknown.
    const run = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Stale' }); external(raw, {}) }), '5.1-4')
    expect(run.now).toMatchObject({ kind: 'running', unverified: [{ verification: 'Stale' }] })
    // The round's freshness.verification is no condition of a step: Unknown or Stale there leaves a step with Unknown work idle.
    for (const round of ['Unknown', 'Stale']) {
      const c = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }); (raw as Full).freshness = { ...(raw as Full).freshness, verification: round } as Full['freshness'] }), '5.1-4')
      expect(c.now).toMatchObject({ kind: 'idle', unverified: [{ verification: 'Unknown' }] })
    }
  })

  it('quotes at most 80 characters of the latest title in the note; the step lists it whole', () => {
    const long = `${'Review the plan view '.repeat(95)}end`
    const c = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown',
      description: { ...(raw.external_work[0].description as Rec), task: long } })), '5.1-4')
    expect(unverifiedNote(c)).toBe(`1 external work submitted, not read Verified (verification Unknown): “${long.slice(0, 80)}…”`)
    expect(c.external).toEqual([expect.stringContaining(long)])
    const exact = 'x'.repeat(80)
    const fits = cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown',
      description: { ...(raw.external_work[0].description as Rec), task: exact } })), '5.1-4')
    expect(unverifiedNote(fits)).toBe(`1 external work submitted, not read Verified (verification Unknown): “${exact}”`)
  })

  it('counts the 80-character cap in code points, not UTF-16 units: emoji and CJK are not cut in half or cut early', () => {
    const titled = (task: string) => cell(on514(raw => external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown',
      description: { ...(raw.external_work[0].description as Rec), task } })), '5.1-4')
    const prefix = '1 external work submitted, not read Verified (verification Unknown): '
    const eighty = '😀'.repeat(80)
    expect(unverifiedNote(titled(eighty))).toBe(`${prefix}“${eighty}”`)
    const c81 = titled(`${eighty}😀`)
    expect(unverifiedNote(c81)).toBe(`${prefix}“${eighty}…”`)
    expect(c81.external).toEqual([expect.stringContaining(`${eighty}😀`)])
    const mixed = `${'复核A'.repeat(26)}核验😀`
    expect([...mixed].length).toBe(81)
    expect(unverifiedNote(titled(mixed))).toBe(`${prefix}“${[...mixed].slice(0, 80).join('')}…”`)
  })

  it('running external work with its heartbeat confirmed is still in progress beside submitted work not read Verified, with the note', () => {
    const c = cell(on514(raw => { external(raw, { state: 'Submitted', status: 'Submitted', verification: 'Unknown' }); external(raw, {}) }), '5.1-4')
    expect(c).toMatchObject({ state: 'active', now: { kind: 'running', what: expect.stringMatching(/^External work running/), unverified: [{ verification: 'Unknown' }] } })
    expect(spokenState(c)).toBe('in progress')
  })

  it('external work that failed verification holds the step: its own state, not idle, not Unknown, each listed; a sign-off Ready waits on you', () => {
    const c = cell(on514(raw => {
      external(raw, { ...FAILED, ...titled(raw, 'Run M') }); external(raw, { ...FAILED, ...titled(raw, 'Run N') })
      external(raw, { state: 'Cancelled', status: 'Cancelled', type: 'cancelled', ...titled(raw, 'Run R') })
    }), '5.1-4')
    expect(c).toMatchObject({ state: 'held', now: { kind: 'held', recorded: null,
      held: ['external work “Run M” submitted · verification failed', 'external work “Run N” submitted · verification failed'].map(text => ({ kind: 'verify-failed', text })) } })
    expect(cellLabels[c.state]).toBe('Active · held')
    expect(spokenState(c)).toBe('active, held: 2 external works failed verification')
    expect(nowLines(c)[0]).toBe('Active · held: external work “Run M” submitted · verification failed; external work “Run N” submitted · verification failed. '
      + 'A submitted external work has a verification result of Verification failed; nothing on the step runs or is queued.')
    const owner = cell(on514(raw => { external(raw, { ...FAILED, ...titled(raw, 'Run M') }); raw.owner_waiting.push(signoff('R5:5.1-4')) }), '5.1-4')
    expect(owner).toMatchObject({ state: 'waiting', now: { kind: 'waiting', recorded: null, waiting: [{ kind: 'signoff', text: 'acceptance sign-off Owner accepts 5.4 is Ready for you' }],
      held: [{ kind: 'verify-failed', text: 'external work “Run M” submitted · verification failed' }] } })
    expect(cellLabels[owner.state]).toBe('Waiting on you')
    expect(spokenState(owner)).toBe('waiting on you: 1 acceptance sign-off Ready for you; held: 1 external work failed verification')
    expect(nowLines(owner)[0]).toBe('Waiting on you: acceptance sign-off Owner accepts 5.4 is Ready for you. An acceptance sign-off Ready is yours to give; nothing on the step runs or is queued.')
    expect(nowLines(owner)).toContain('Also held: external work “Run M” submitted · verification failed.')
    // Beside registered work the step is in progress, and the held one is still listed.
    const queued = cell(on514(raw => { external(raw, { ...FAILED, ...titled(raw, 'Run M') }); external(raw, { ...REGISTERED, ...titled(raw, 'Run N') }) }), '5.1-4')
    expect(queued.now).toEqual({ kind: 'queued', why: ['external work “Run N” registered, not started'], held: ['external work “Run M” submitted · verification failed'], waiting: [] })
    expect(nowLines(queued)).toContain('Held: external work “Run M” submitted · verification failed.')
  })

  it('Unknown is said before queued (with the queue and the held kept), and running before Unknown', () => {
    const both = cell(on514(raw => {
      external(raw, { ...REGISTERED, ...titled(raw, 'Run M') }); external(raw, { heartbeat_age_s: 130, ...titled(raw, 'Run N') }); external(raw, { ...FAILED, ...titled(raw, 'Run P') })
    }), '5.1-4')
    const SILENT = 'external work “Run N” Running: no heartbeat confirmed for over 60 s'
    expect(both.now).toEqual({ kind: 'unknown', why: [SILENT], queued: ['external work “Run M” registered, not started'], held: ['external work “Run P” submitted · verification failed'], waiting: [] })
    expect(nowLines(both)).toEqual([`Unknown · ${SILENT}.`, 'Queued as read: external work “Run M” registered, not started.', 'Held: external work “Run P” submitted · verification failed.'])
    const run = cell(on514(raw => { external(raw, {}); external(raw, { heartbeat_age_s: 130, ...titled(raw, 'Run N') }) }), '5.1-4')
    expect(run.now).toMatchObject({ kind: 'running', what: 'External work running (codex)' })
  })

  it('an Owner step names the sources it read, never that nothing asks the Owner; with records, names them', () => {
    // 5.4-3 is the Owner's step on PROD, its done rule {}.
    const bare = cell(view(raw => activate(raw, 'R5:5.4-3')), '5.4-3')
    expect(bare).toMatchObject({ owner: true, state: 'yours', now: { kind: 'yours', recorded: [] } })
    expect(cellLabels[bare.state]).toBe('Yours')
    expect(spokenState(bare)).toBe('yours, none in plan-view owner_waiting or its done rule')
    expect(nowLines(bare)[0]).toBe('Yours · none in plan-view owner_waiting or its done rule: no wait for you listed on it, and no question, request or sign-off named.')
    expect([spokenState(bare), ...nowLines(bare)].join(' ')).not.toMatch(/nothing (recorded )?asks you/)
    // 5.1-5 is the Owner's step whose done rule waits on a question: recorded, not said to ask or not.
    const ruled = cell(view(raw => activate(raw, 'R5:5.1-5')), '5.1-5')
    expect(ruled.now).toEqual({ kind: 'yours', recorded: [`question ${Q_RULE} (its done rule)`] })
    expect(spokenState(ruled)).toBe(`yours, recorded on it: question ${Q_RULE} (its done rule)`)
    // An acceptance sign-off the plan view marks Ready on it waits on you now, and is recorded too.
    const accepted = cell(view(raw => { activate(raw, 'R5:5.4-3'); raw.owner_waiting.push(signoff('R5:5.4-3')) }), '5.4-3')
    expect(accepted.now).toEqual({ kind: 'waiting', waiting: [{ kind: 'signoff', text: 'acceptance sign-off Owner accepts 5.4 is Ready for you' }], held: [],
      recorded: ['acceptance sign-off Owner accepts 5.4 Ready (plan-view owner_waiting)'] })
    // With work queued it is in progress, not yours.
    expect(cell(view(raw => { activate(raw, 'R5:5.4-3'); external(raw, { plan_node_id: 'R5:5.4-3', ...REGISTERED, ...titled(raw, 'Run M') }) }), '5.4-3').now)
      .toEqual({ kind: 'queued', why: ['external work “Run M” registered, not started'], held: [], waiting: [] })
    // An old read: Unknown now, not yours.
    expect(cell(view(raw => activate(raw, 'R5:5.4-3')), '5.4-3', clock({ now: Date.parse(AT) + 61_000 })).state).toBe('active-unknown')
  })

  it('an Owner step with a sign-off Ready waits on you (with what is recorded on it, and what is held); with work unconfirmed it is Unknown, not yours', () => {
    const waiting = cell(view(raw => {
      activate(raw, 'R5:5.4-3')
      raw.owner_waiting.push(signoff('R5:5.4-3'))
      external(raw, { plan_node_id: 'R5:5.4-3', ...FAILED, ...titled(raw, 'Run M') })
    }), '5.4-3')
    expect(waiting).toMatchObject({ state: 'waiting', now: { kind: 'waiting', waiting: [{ kind: 'signoff', text: 'acceptance sign-off Owner accepts 5.4 is Ready for you' }],
      held: [{ kind: 'verify-failed', text: 'external work “Run M” submitted · verification failed' }], recorded: ['acceptance sign-off Owner accepts 5.4 Ready (plan-view owner_waiting)'] } })
    // Its state reads Waiting on you, so its records are the Owner step's, not "Yours".
    expect(nowLines(waiting)).toContain('Owner step · recorded on it: acceptance sign-off Owner accepts 5.4 Ready (plan-view owner_waiting). Whether each still asks you is on Needs you.')
    expect(nowLines(waiting).some(l => l.startsWith('Yours'))).toBe(false)
    const silent = cell(view(raw => { activate(raw, 'R5:5.4-3'); external(raw, { plan_node_id: 'R5:5.4-3', heartbeat_age_s: 130, ...titled(raw, 'Run M') }) }), '5.4-3')
    expect(silent).toMatchObject({ state: 'active-unknown', now: { kind: 'unknown', why: ['external work “Run M” Running: no heartbeat confirmed for over 60 s'] } })
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
      'Step 5.1-4: active, nothing running now, no done rule', 'Step 5.1-5: not started', 'Step 5.1-6: stopped, no done rule'])
    expect(bar.querySelectorAll('[data-no-rule-mark]')).toHaveLength(2)
    // Every planned step of 5.2 has rule {}: none is marked in its cell.
    expect(screen.getByRole('toolbar', { name: /^Steps of 5\.2,/ }).querySelectorAll('[data-no-rule-mark]')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Step 5.1-4: active, nothing running now, no done rule' }))
    const active = screen.getByRole('region', { name: 'Step 5.1-4' })
    expect(active.querySelector('[data-no-rule-text]')).toHaveTextContent(/^No done rule: nothing closes it by evidence\./)
    expect(active.querySelector('[data-no-rule]')).toHaveTextContent('No done rule')
    expect(active).toHaveTextContent('Active · nothing running now')
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
    external(raw, { plan_node_id: 'R5:5.2-1' })
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
    expect(othersSentence(after, 'R5:5.2')).toMatch(/^By runtime step state, of the 9 other items, 1 has an active step with nothing running now and /)
    // A held step is counted and classed apart from idle.
    const held = stepRows(view(raw => { activate(raw, 'R5:5.1-4'); node(raw, 'R5:5.1-3').state = 'done'; external(raw, FAILED) }), clock())
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
    expect(screen.getByRole('button', { name: 'Step 5.2-1: in progress, live now: External work running (codex), no done rule' })).toHaveAttribute('data-live', 'true')
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
  it('5.4-1 and 5.4-2, active with rule {} and no work, read Active · nothing running now with the no-rule mark, not In progress', () => {
    at(NOW)
    render(<NowClock><PlanModuleRows view={view(raw => { activate(raw, 'R5:5.4-1'); activate(raw, 'R5:5.4-2') })} selected="" /></NowClock>)
    for (const label of ['5.4-1', '5.4-2']) {
      const button = screen.getByRole('button', { name: `Step ${label}: active, nothing running now, no done rule` })
      expect(button).toHaveAttribute('data-state', 'idle')
      expect(button.getAttribute('title')).toMatch(new RegExp(`^${label.replace('.', '\\.')} · Active · nothing running now · No done rule · `))
    }
    const item = within(screen.getByRole('list', { name: 'Modules in mainline order' })).getAllByRole('listitem').find(li => li.dataset.module === '5.4')!
    expect(item).toHaveTextContent('0 / 6 done · 2 idle')
    expect(item).not.toHaveTextContent('in progress')
  })
})

/*
 * The shape PROD's R5 plan view had on 2026-11-02 for a step whose external reviews and repairs all finished (synthetic
 * titles and ids, no record copied): one active step, nine
 * external-work records submitted through the dispatch wrapper with verification Unknown (the view's freshness.verification
 * Unknown too). Nothing on the step runs, is queued or waits on a decision: it reads Active · nothing running now with a note,
 * never Active · held on a verification nobody reads.
 */
describe('a step whose external work all finished, its verification Unknown (PROD shape, 2026-11-02)', () => {
  const finished = (n: number) => min(200 - n * 10)
  const reviewed = () => on514(raw => {
    const full = raw as Full
    const description = raw.external_work[0].description as Rec
    for (let n = 1; n <= 9; n++) {
      external(raw, { state: 'Submitted', status: 'Submitted', type: 'submitted', verification: 'Unknown', started_at: finished(n + 1), ended_at: finished(n),
        recorded_at: finished(n), heartbeat_age_s: 600 * n, description: { ...description, task: `${n % 2 ? 'Review' : 'Repair'} pass ${n}`, executor: 'codex' } })
    }
    full.freshness = { ...full.freshness, verification: 'Unknown' } as Full['freshness']
  })
  const NOTE = '9 external works submitted, not read Verified (verification Unknown): “Review pass 9” +8'

  it('reads Active · nothing running now with a short note, not held', () => {
    const c = cell(reviewed(), '5.1-4')
    expect(c).toMatchObject({ step: 'active', state: 'idle', now: { kind: 'idle' } })
    expect(c.now?.unverified).toHaveLength(9)
    expect(cellLabels[c.state]).toBe('Active · nothing running now')
    expect(spokenState(c)).toBe('active, nothing running now')
    expect(nowLines(c)).toEqual([])
    expect(howRead(c)).toHaveLength(4)
    expect(howRead(c)!.map(r => r.text).join(' ')).not.toMatch(/Review pass|held:|waits for its verification/)
    expect(unverifiedNote(c)).toBe(NOTE)
    const r51 = row(stepRows(reviewed(), clock()), '5.1')
    expect(r51).toMatchObject({ held: 0, idle: 1 })
    expect(stepCounts(r51)).toBe('2 / 6 done · 1 queued · 1 idle')
  })

  it('shows the note under the state on the module page and in the opened cell, the state word idle', () => {
    at(NOW)
    const v = reviewed()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
    const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>
      <PlanModuleDetail view={v} item={moduleOf(v, 'R5:5.1')!.item} step="" stale={false} failed={null} /></NowClock></PlatformAuthContext.Provider></QueryClientProvider>)
    const stage = document.querySelector('[data-step-row="5.1-4"]') as HTMLElement
    expect(stage.querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running now')
    expect(stage.querySelector('[data-step-note="unverified"]')).toHaveTextContent(NOTE)
    expect(stage).not.toHaveTextContent('Active · held')
    // A step with no such work carries no note.
    expect(document.querySelector('[data-step-row="5.1-3"] [data-step-note]')).toBeNull()
    client.clear()
    cleanup()
    render(<NowClock><PlanModuleRows view={v} selected="" /></NowClock>)
    fireEvent.click(screen.getByRole('button', { name: 'Step 5.1-4: active, nothing running now, no done rule' }))
    const detail = screen.getByRole('region', { name: 'Step 5.1-4' })
    expect(detail.querySelector('[data-step-note="unverified"]')).toHaveTextContent(NOTE)
    expect(detail).not.toHaveTextContent('Active · held')
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
    expect(within(screen.getByRole('region', { name: 'Now' })).getByRole('link', { name: 'Step 5.1-4 active, nothing running now' })).toBeInTheDocument()
    expect(stepRow('5.1-4').querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running now')
    expect(stepRow('5.1-4').querySelector('[data-no-rule]')).toHaveTextContent('No done rule')
    expect(stepRow('5.1-4').querySelector('[data-step-read="idle"]')).toHaveTextContent(/^How “Active · nothing running now” was read · \d\d:\d\d/)
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
    expect(stepRow('5.1-4').querySelector('[data-step-state]')).toHaveTextContent('Active · nothing running now')
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
})
