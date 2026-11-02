import '@testing-library/jest-dom/vitest'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { NowClock } from '../../progress/progressClock'
import { liveLine, liveNow, liveSpoken, NOW_SHOWN, type LiveNow } from '../liveStepModel'
import { runtimeTasks } from '../moduleDetailModel'
import { moduleRows } from '../planModuleModel'
import { PlanModuleRows } from '../PlanModuleRows'
import { WorkPlanView } from '../WorkPlanView'
import { AT, id, r5Raw, T1, T3, T_OTHER, type Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

/*
 * The R5 plan view of the module detail's tests (moduleDetailFixture: 5.1-3 active with its implementation back to
 * ready, 5.2-1's task running), in shapes the runtime's database allows: a running task has its live attempt
 * (attempt_one_live), and plan-view working lists it (planviewsnapshot.go).
 */
const NOW = Date.parse(AT) + 5_000
const min = (n: number) => new Date(Date.parse(AT) - n * 60_000).toISOString()
type Rec = Record<string, unknown>
type Full = Raw & { working: Rec[]; external_work: Rec[]; freshness: { database: { status: string } } }
const task = (raw: Raw, tid: string) => raw.tasks.find(t => t.task_id === tid)!
/** A task's live attempt, as liveAttempts reads it: leased, running or stale, with the heartbeat's age at the snapshot. */
function run(raw: Raw, tid: string, more: Rec = {}) {
  const t = task(raw, tid)
  const attempt = { attempt_id: `a_${tid.slice(2)}`, attempt_no: 1, task_id: tid, mission_id: t.mission_id, host_id: 'mac-mini', executor: 'claude-code', model: 'opus',
    state: 'running', started_at: min(12), elapsed_s: 720, heartbeat_age_s: 4, reason: 'attempt on mac-mini', persisted: false, ...more }
  Object.assign(t, { state: attempt.state === 'leased' ? 'leased' : 'running', attempt })
  ;(raw as Full).working.push(t)
}
/** The base view: 5.2-1's running task on its attempt; change(raw) moves it on. */
const view = (change: (raw: Raw) => void = () => {}): PlanSnapshot => {
  const raw = r5Raw()
  run(raw, T_OTHER)
  change(raw)
  return parsePlanSnapshot(raw, 'R5')
}
const idle = (raw: Raw) => { task(raw, T_OTHER).state = 'ready'; delete task(raw, T_OTHER).attempt; (raw as Full).working = [] }
const landing = (phase: string, outcome = '', reason = '', observed = min(3)) => ({ stream: 'landing:bifrost-platform', operation: `land:${T1}:1`, phase, outcome, reason,
  attempt: 'a_01M6BE9AWEAYR33CGF7WD9NZQG', result: '6c4f6c04c28bd14c686c6383a36369df5b8c35c8', main_commit: '', run: '', observed_at: observed, basis: 'note.landing; current validity requires verification' })
const external = (raw: Raw, more: Rec) => {
  const w = (raw as Full).external_work
  w.push({ ...w[0], external_work_id: `ew_${w.length}`, plan_node_id: 'R5:5.1-4', state: 'Running', status: 'Running', started_at: min(20), heartbeat_age_s: 4, ended_at: undefined, ...more })
  return w[w.length - 1]
}
const live = (v: PlanSnapshot, now = NOW, failed: string | null = null, runtime?: Parameters<typeof liveNow>[2]['runtime']) => liveNow(v, moduleRows(v), { failed, now, runtime })
const ok = (l: LiveNow) => { if (l.state !== 'ok') throw new Error(`not ok: ${l.reason}`); return l }
const labels = (v: PlanSnapshot, now = NOW) => ok(live(v, now)).steps.map(s => s.label)

describe('live now (model)', () => {
  it('a running attempt makes its step live; an active step with no work running is not', () => {
    const l = ok(live(view()))
    // 5.1-3 is active, its implementation back to ready: in progress, but nothing runs on it now.
    expect(l.byStep.has('R5:5.1-3')).toBe(false)
    expect(l.steps.map(s => [s.label, s.what, s.quiet])).toEqual([['5.2-1', 'Task running', false]])
    expect(liveLine(l.steps[0], NOW)).toBe('5.2-1 · Task running · for 12 min')
    expect(l.steps[0].href).toBe('#work?section=plan&root=R5&node=R5%3A5.2')
    expect(l.unmarked).toBe(0)
  })

  it('a step whose only claim is its state (active, no work running) is not live', () => {
    const l = ok(live(view(idle)))
    expect(l.steps).toEqual([])
    expect(l.unmarked).toBe(0)
  })

  it("names the running task's role from the runtime", () => {
    const runtime = runtimeTasks({ state: 'ok', at: NOW, data: parseRuntimeProgress({ work: { missions: [{ mission_id: 'M-260', tasks: [{ task_id: T_OTHER, role: 'implement', state: 'running' }] }] } }) })
    expect(liveLine(ok(live(view(), Date.parse(AT), null, runtime)).steps[0], Date.parse(AT))).toBe('5.2-1 · Implement task running · for 12 min')
  })

  it('a leased attempt is live; a task waiting (submitted, verifying, ready, rework, blocked, paused) is not', () => {
    expect(ok(live(view(raw => { idle(raw); run(raw, T_OTHER, { state: 'leased', started_at: undefined, elapsed_s: undefined }) }))).steps.map(s => liveLine(s, NOW)))
      .toEqual(['5.2-1 · Task leased to a host'])
    for (const state of ['submitted', 'verifying', 'ready', 'rework', 'blocked', 'paused', 'awaiting_owner', 'accepted']) {
      expect(labels(view(raw => { idle(raw); task(raw, T_OTHER).state = state }))).toEqual([])
    }
  })

  it('a stale attempt, or one whose heartbeat is unconfirmed by now, is not running: Live lists it, no step is marked', () => {
    const stale = ok(live(view(raw => { idle(raw); run(raw, T_OTHER, { state: 'stale', heartbeat_age_s: 130 }) })))
    expect(stale.steps).toEqual([])
    expect(stale.unmarked).toBe(1)
    // 50 s at the snapshot is confirmed 5 s later (55 s) and unconfirmed 15 s later (65 s): the Live tab's rule.
    const beat = view(raw => { idle(raw); run(raw, T_OTHER, { heartbeat_age_s: 50 }) })
    expect(labels(beat, Date.parse(AT) + 5_000)).toEqual(['5.2-1'])
    expect(labels(beat, Date.parse(AT) + 15_000)).toEqual([])
    expect(labels(view(raw => { idle(raw); run(raw, T_OTHER, { status: 'Unconfirmed' }) }))).toEqual([])
  })

  it("a moving landing makes an accepted task's step live, with the age of its last record", () => {
    const step = ok(live(view(raw => { task(raw, T1).landing = landing('branch_pushed') }))).byStep.get('R5:5.1-1')!
    expect(step.what).toBe('Landing branch pushed')
    expect(step.since).toEqual({ kind: 'last_record', at: Date.parse(min(3)) })
    expect(liveLine(step, NOW)).toBe('5.1-1 · Landing branch pushed · last landing record 3 min ago')
    expect(ok(live(view(raw => { task(raw, T1).landing = landing('main_result', 'ok') }))).byStep.get('R5:5.1-1')!.what).toBe('Landing main answered the push (ok)')
    expect(ok(live(view(raw => { task(raw, T1).landing = landing('claimed') }))).byStep.get('R5:5.1-1')!.what).toBe('Landing claimed')
  })

  it("a landing waiting (for the Owner's grant, or parked) is not live: Operation.Active leaves both out", () => {
    expect(ok(live(view(raw => { task(raw, T1).landing = landing('awaiting_grant') }))).byStep.has('R5:5.1-1')).toBe(false)
    expect(ok(live(view(raw => { task(raw, T1).landing = landing('parked', '', 'window') }))).byStep.has('R5:5.1-1')).toBe(false)
  })

  it('an ended landing is not live (landed, failed, discarded, rehearsed, STG verified or failed, main_result non_ff)', () => {
    for (const [phase, outcome] of [['landed', 'ok'], ['failed', 'ci_red'], ['failed', 'declined'], ['discarded', 'main_changed'], ['rehearsed', ''], ['stg_verified', ''], ['stg_failed', 'bound'], ['main_result', 'non_ff'], ['someday', '']]) {
      expect(ok(live(view(raw => { task(raw, T1).landing = landing(phase, outcome) }))).byStep.has('R5:5.1-1')).toBe(false)
    }
  })

  it('a moving landing with no record for over 30 min is quiet, and comes after the steps seen moving', () => {
    const l = ok(live(view(raw => { task(raw, T1).landing = landing('branch_pushed', '', '', min(45)) })))
    expect(l.steps.map(s => [s.label, s.quiet])).toEqual([['5.2-1', false], ['5.1-1', true]])
    expect(liveLine(l.steps[1], NOW)).toBe('5.1-1 · Landing branch pushed · last landing record 45 min ago')
    expect(liveSpoken(l.steps[1])).toBe('live now: Landing branch pushed, no landing record for over 30 min')
    expect(ok(live(view(raw => { task(raw, T1).landing = landing('branch_pushed', '', '', min(29)) }))).byStep.get('R5:5.1-1')!.quiet).toBe(false)
  })

  it('registered external work is live while Running with its heartbeat confirmed, and only once under a running attempt', () => {
    const step = ok(live(view(raw => { external(raw, {}) }))).byStep.get('R5:5.1-4')!
    expect(liveLine(step, Date.parse(AT))).toMatch(/^5\.1-4 · External work running \(.+\) · for 20 min$/)
    expect(ok(live(view(raw => { external(raw, { heartbeat_age_s: 70 }) }))).byStep.has('R5:5.1-4')).toBe(false)
    expect(ok(live(view(raw => { external(raw, { status: 'Unconfirmed', heartbeat_age_s: 90 }) }))).byStep.has('R5:5.1-4')).toBe(false)
    expect(ok(live(view(raw => { external(raw, { state: 'Registered', status: 'Registered', started_at: undefined }) }))).byStep.has('R5:5.1-4')).toBe(false)
    // External work under a task with a live attempt is that attempt's history (planviewsnapshot.go): not a second reason.
    const under = ok(live(view(raw => { external(raw, { plan_node_id: 'R5:5.2-1', task_id: T_OTHER }) })))
    expect(under.byStep.get('R5:5.2-1')!.others).toBe(0)
  })

  it('tells one reason per step, the running work before its landing, and counts the others', () => {
    const l = ok(live(view(raw => {
      task(raw, T1).landing = landing('branch_pushed')
      raw.tasks.push({ task_id: id('B'), plan_node_id: 'R5:5.1-1', mission_id: 'M-250', title: `review ${T1}`, state: 'ready' })
      run(raw, id('B'))
    })))
    expect(liveLine(l.byStep.get('R5:5.1-1')!, NOW)).toBe('5.1-1 · Review task running (+1 more) · for 12 min')
  })

  it("lists the live steps in the rows' order: the mainline's module first, then plan order", () => {
    const l = ok(live(view(raw => {
      task(raw, T1).landing = landing('claimed')
      run(raw, T3)
      raw.tasks.push({ task_id: id('C'), plan_node_id: 'R5:5.1-6', mission_id: 'M-252', title: 'Acceptance notes, round 2', state: 'ready' })
      run(raw, id('C'), { state: 'leased' })
    })))
    expect(l.steps.map(s => s.label)).toEqual(['5.1-1', '5.1-3', '5.1-6', '5.2-1'])
    expect(NOW_SHOWN).toBe(3)
  })

  it('counts what plan-view working lists that marks no step, so "nothing running" is never said beside it', () => {
    // A running attempt on the module itself (no step cell), and registered work not started yet.
    const l = ok(live(view(raw => {
      idle(raw)
      raw.tasks.push({ task_id: id('D'), plan_node_id: 'R5:5.1', mission_id: 'M-250', title: 'Module-wide check', state: 'ready' })
      run(raw, id('D'))
      ;(raw as Full).working.push(external(raw, { state: 'Registered', status: 'Registered', started_at: undefined }))
    })))
    expect(l.steps).toEqual([])
    expect(l.unmarked).toBe(2)
  })

  it('an old, failed or not Fresh read says nothing of now: Unknown, never "nothing running"', () => {
    const v = view()
    expect(live(v, Date.parse(AT) + 61_000)).toEqual({ state: 'unknown', reason: `Round 5's plan view is older than 60 s; as of ${AT}`, asOf: Date.parse(AT) })
    expect(live(v, NOW, 'Offline')).toMatchObject({ state: 'unknown', reason: `Round 5's plan view could not be read (Offline); as of ${AT}` })
    const stale = view(raw => { (raw as Full).freshness.database.status = 'Stale' })
    expect(live(stale, NOW)).toMatchObject({ state: 'unknown', reason: `Round 5's plan view reads the database as Stale; as of ${AT}` })
  })
})

function rows(v: PlanSnapshot, failed: string | null = null) {
  return render(<NowClock><PlanModuleRows view={v} selected="" failed={failed} /></NowClock>)
}
const at = (now: number) => { vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(now) }

describe('live now (module rows)', () => {
  it('marks the live cell with the breathing ring and says it aloud with no clock; other cells carry neither', () => {
    at(NOW)
    rows(view(raw => { task(raw, T1).landing = landing('branch_pushed') }))
    const cell = screen.getByRole('button', { name: 'Step 5.2-1: not started, live now: Task running' })
    expect(cell).toHaveAttribute('data-live', 'true')
    expect(cell.querySelector('[data-live-ring]')).toHaveClass('work-live-ring')
    expect(cell.querySelector('[data-live-ring]')).not.toHaveClass('work-live-ring--quiet')
    const landingCell = screen.getByRole('button', { name: 'Step 5.1-1: done, live now: Landing branch pushed' })
    expect(landingCell.querySelector('[data-live-ring]')).toHaveClass('work-live-ring')
    // 5.1-3 is in progress by its state, with nothing running: no marker.
    const active = screen.getByRole('button', { name: 'Step 5.1-3: in progress' })
    expect(active).not.toHaveAttribute('data-live')
    expect(active.querySelector('.work-live-ring')).toBeNull()
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(2)

    const nowLine = document.querySelector('[data-live-now="live"]') as HTMLElement
    expect(within(nowLine).getAllByRole('link').map(a => [a.textContent, a.getAttribute('href')])).toEqual([
      ['5.1-1 · Landing branch pushed · last landing record 3 min ago', '#work?section=plan&root=R5&node=R5%3A5.1'],
      ['5.2-1 · Task running · for 12 min', '#work?section=plan&root=R5&node=R5%3A5.2'],
    ])
    expect(nowLine.querySelectorAll('.work-live-dot')).toHaveLength(2)
  })

  it('draws a landing gone quiet still and faint, and says so aloud', () => {
    at(NOW)
    rows(view(raw => { task(raw, T1).landing = landing('branch_pushed', '', '', min(45)) }))
    const cell = screen.getByRole('button', { name: 'Step 5.1-1: done, live now: Landing branch pushed, no landing record for over 30 min' })
    expect(cell).toHaveAttribute('data-live', 'quiet')
    expect(cell.querySelector('[data-live-ring]')).toHaveClass('work-live-ring', 'work-live-ring--quiet')
    const nowLine = document.querySelector('[data-live-now="live"]') as HTMLElement
    expect(within(nowLine).getAllByRole('link').map(a => a.dataset.liveStep)).toEqual(['5.2-1', '5.1-1'])
    expect(nowLine.querySelectorAll('.work-live-dot--quiet')).toHaveLength(1)
  })

  it('does not mark a step whose landing waits for the grant', () => {
    at(NOW)
    rows(view(raw => { idle(raw); task(raw, T1).landing = landing('awaiting_grant') }))
    expect(screen.getByRole('button', { name: 'Step 5.1-1: done' })).not.toHaveAttribute('data-live')
    expect(document.querySelector('[data-live-now="none"]')).toHaveTextContent(/^Now · Nothing running now/)
  })

  it('shows at most three live steps on the Now line and counts the rest', () => {
    at(NOW)
    rows(view(raw => {
      task(raw, T1).landing = landing('claimed')
      run(raw, T3)
      raw.tasks.push({ task_id: id('C'), plan_node_id: 'R5:5.1-6', mission_id: 'M-252', title: 'Acceptance notes, round 2', state: 'ready' })
      run(raw, id('C'))
    }))
    const nowLine = document.querySelector('[data-live-now="live"]') as HTMLElement
    expect(within(nowLine).getAllByRole('link').map(a => a.dataset.liveStep)).toEqual(['5.1-1', '5.1-3', '5.1-6'])
    expect(nowLine).toHaveTextContent('+1 more live step in the rows below')
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(4)
  })

  it('says nothing runs only of a read that is now, and links Live when it lists work that marks no step', () => {
    at(NOW)
    rows(view(raw => { idle(raw); run(raw, T_OTHER, { state: 'stale', heartbeat_age_s: 130 }) }))
    const line = document.querySelector('[data-live-now="none"]') as HTMLElement
    expect(line).toHaveTextContent(/^Now · Nothing running now/)
    expect(within(line).getByRole('link')).toHaveAttribute('href', '#work?tab=live&root=R5')
    expect(line.querySelector('[data-live-unmarked]')).toHaveTextContent('1 registered on Live not marked here')
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(0)
  })

  it('an old read marks no cell and says what runs now is Unknown, with its age', () => {
    at(Date.parse(AT) + 3 * 60_000)
    rows(view())
    expect(document.querySelectorAll('[data-live-ring], [data-live]')).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Step 5.2-1: not started' })).toBeInTheDocument()
    expect(document.querySelector('[data-live-now="unknown"]')).toHaveTextContent(
      `Now · Unknown: Round 5's plan view is older than 60 s; as of ${AT}, 3 min old. What runs now is not known from it.`)
    expect(document.querySelector('[data-live-now="none"]')).toBeNull()
  })

  it('a failed refresh is Unknown too, though its last snapshot is young', () => {
    at(NOW)
    rows(view(), 'Plan view: HTTP 502')
    expect(document.querySelector('[data-live-now="unknown"]')).toHaveTextContent('could not be read (Plan view: HTTP 502)')
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(0)
  })
})

describe('live now (Work → Plan)', () => {
  const work = (now: number) => {
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
    vi.spyOn(Date, 'now').mockReturnValue(now)
    const raw = r5Raw()
    run(raw, T_OTHER)
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('/agentrt/plan-view')
      ? new Response(JSON.stringify(raw), { status: 200 }) : new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    window.location.hash = '#work'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    // The shell's GET /progress (never fetched by Work): R5 the newest round, and the running task's role.
    client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress({ plan: [{ plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }],
      work: { missions: [{ mission_id: 'M-260', tasks: [{ task_id: T_OTHER, role: 'implement', state: 'running' }] }] } }))
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={{ token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
    return client
  }

  it("names the running task's role from the shell's cached progress, and fetches no progress of its own", async () => {
    const client = work(Date.parse(AT))
    const modules = await screen.findByRole('region', { name: 'Modules' })
    expect(modules.querySelector('[data-live-now="live"]')).toHaveTextContent('5.2-1 · Implement task running · for 12 min')
    expect(vi.mocked(fetch).mock.calls.some(([input]) => String(input).includes('/progress'))).toBe(false)
    client.clear()
  })

  it('reads the page clock: a plan view over 60 s old marks nothing', async () => {
    const client = work(Date.parse(AT) + 2 * 60_000)
    const modules = await screen.findByRole('region', { name: 'Modules' })
    expect(modules.querySelector('[data-live-now="unknown"]')).toHaveTextContent('is older than 60 s')
    expect(modules.querySelectorAll('[data-live-ring]')).toHaveLength(0)
    client.clear()
  })
})

describe('live now (motion)', () => {
  const css = readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../styles/opsWorkspace.css'), 'utf8')
  it('breathes slowly (a 2 s ease-in-out cycle, under 1 Hz), stands still under reduced motion, and a quiet landing never breathes', () => {
    const rule = (selector: string) => new RegExp(`\\${selector} \\{([^}]*)\\}`).exec(css)?.[1] ?? ''
    for (const selector of ['.work-live-ring', '.work-live-dot']) {
      const animation = /animation: work-live-breathe (\d+(?:\.\d+)?)s ease-in-out infinite;/.exec(rule(selector))
      expect(animation).not.toBeNull()
      expect(Number(animation![1])).toBeGreaterThanOrEqual(1)
    }
    expect(css).toMatch(/@keyframes work-live-breathe \{\s*0%, 100% \{ opacity: \.35; \}\s*50% \{ opacity: 1; \}\s*\}/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.work-live-ring, \.work-live-dot \{ animation: none; opacity: 1; \}\s*\}/)
    expect(css).toMatch(/\.work-live-ring\.work-live-ring--quiet, \.work-live-dot\.work-live-dot--quiet \{ animation: none; opacity: \.4; \}/)
  })
})
