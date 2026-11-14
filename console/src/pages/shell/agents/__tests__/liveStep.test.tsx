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
import { liveLine, liveNow, NOW_SHOWN, type LiveNow } from '../liveStepModel'
import { moduleRows } from '../planModuleModel'
import { PlanModuleRows } from '../PlanModuleRows'
import { WorkPlanView } from '../WorkPlanView'
import { AT, externalWork, r5Raw, W3, type Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

/*
 * The R5 plan view of the module detail's tests (moduleDetailFixture: 5.1-3 active with external work registered on it
 * and not started), with external work running on 5.2-1, as plan-view lists it: in external_work and in working.
 */
const NOW = Date.parse(AT) + 5_000
type Rec = Record<string, unknown>
type Full = Raw & { freshness: { database: { status: string } } }
/** External work on a step: Running, started 20 min before the snapshot, its heartbeat 4 s old, unless more says otherwise. */
const external = (raw: Raw, nid: string, more: Rec = {}) => externalWork(raw, nid, more)
/** The base view: external work running on 5.2-1; change(raw) moves it on. */
const view = (change: (raw: Raw) => void = () => {}): PlanSnapshot => {
  const raw = r5Raw()
  external(raw, 'R5:5.2-1', { external_work_id: 'ew_other' })
  change(raw)
  return parsePlanSnapshot(raw, 'R5')
}
/** Nothing registered anywhere: 5.2-1's run and 5.1-3's registered work are gone. */
const idle = (raw: Raw) => { raw.external_work = raw.external_work.filter(w => w.external_work_id !== 'ew_other' && w.external_work_id !== W3); raw.working = [] }
const live = (v: PlanSnapshot, now = NOW, failed: string | null = null) => liveNow(v, moduleRows(v), { failed, now })
const ok = (l: LiveNow) => { if (l.state !== 'ok') throw new Error(`not ok: ${l.reason}`); return l }
const labels = (v: PlanSnapshot, now = NOW) => ok(live(v, now)).steps.map(s => s.label)

describe('live now (model)', () => {
  it('running external work makes its step live; an active step with no work running is not', () => {
    const l = ok(live(view()))
    // 5.1-3 is active, the work on it registered and not started: in progress, but nothing runs on it now.
    expect(l.byStep.has('R5:5.1-3')).toBe(false)
    expect(l.steps.map(s => [s.label, s.what])).toEqual([['5.2-1', 'External work running (codex)']])
    expect(liveLine(l.steps[0], NOW)).toBe('5.2-1 · External work running (codex) · for 20 min')
    expect(l.steps[0].href).toBe('#work?section=plan&root=R5&node=R5%3A5.2')
    // The registered work on 5.1-3 is on Live and marks no step.
    expect(l.unmarked).toBe(1)
  })

  it('a step whose only claim is its state (active, no work running) is not live', () => {
    const l = ok(live(view(idle)))
    expect(l.steps).toEqual([])
    expect(l.unmarked).toBe(0)
  })

  it('work whose heartbeat is unconfirmed by now is not running: Live lists it, no step is marked', () => {
    const silent = ok(live(view(raw => { idle(raw); external(raw, 'R5:5.2-1', { heartbeat_age_s: 130 }) })))
    expect(silent.steps).toEqual([])
    expect(silent.unmarked).toBe(1)
    // 50 s at the snapshot is confirmed 5 s later (55 s) and unconfirmed 15 s later (65 s): the Live tab's rule.
    const beat = view(raw => { idle(raw); external(raw, 'R5:5.2-1', { heartbeat_age_s: 50 }) })
    expect(labels(beat, Date.parse(AT) + 5_000)).toEqual(['5.2-1'])
    expect(labels(beat, Date.parse(AT) + 15_000)).toEqual([])
    expect(labels(view(raw => { idle(raw); external(raw, 'R5:5.2-1', { status: 'Unconfirmed' }) }))).toEqual([])
  })

  it('registered external work is live while Running with its heartbeat confirmed', () => {
    const step = ok(live(view(raw => { external(raw, 'R5:5.1-4') }))).byStep.get('R5:5.1-4')!
    expect(liveLine(step, Date.parse(AT))).toMatch(/^5\.1-4 · External work running \(.+\) · for 20 min$/)
    expect(ok(live(view(raw => { external(raw, 'R5:5.1-4', { heartbeat_age_s: 70 }) }))).byStep.has('R5:5.1-4')).toBe(false)
    expect(ok(live(view(raw => { external(raw, 'R5:5.1-4', { status: 'Unconfirmed', heartbeat_age_s: 90 }) }))).byStep.has('R5:5.1-4')).toBe(false)
    expect(ok(live(view(raw => { external(raw, 'R5:5.1-4', { state: 'Registered', status: 'Registered', started_at: undefined }) }))).byStep.has('R5:5.1-4')).toBe(false)
  })

  it('tells one reason per step and counts the others', () => {
    const l = ok(live(view(raw => { external(raw, 'R5:5.1-1'); external(raw, 'R5:5.1-1') })))
    expect(liveLine(l.byStep.get('R5:5.1-1')!, NOW)).toBe('5.1-1 · External work running (codex) (+1 more) · for 20 min')
  })

  it("lists the live steps in the rows' order: the mainline's module first, then plan order", () => {
    const l = ok(live(view(raw => { external(raw, 'R5:5.1-6'); external(raw, 'R5:5.1-3'); external(raw, 'R5:5.1-1') })))
    expect(l.steps.map(s => s.label)).toEqual(['5.1-1', '5.1-3', '5.1-6', '5.2-1'])
    expect(NOW_SHOWN).toBe(3)
  })

  it('counts what plan-view working lists that marks no step, so "nothing running" is never said beside it', () => {
    // Work running on the module itself (no step cell), and registered work not started yet.
    const l = ok(live(view(raw => {
      idle(raw)
      external(raw, 'R5:5.1')
      external(raw, 'R5:5.1-4', { state: 'Registered', status: 'Registered', started_at: undefined })
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
    rows(view(raw => { external(raw, 'R5:5.1-1', { started_at: new Date(Date.parse(AT) - 3 * 60_000).toISOString() }) }))
    const cell = screen.getByRole('button', { name: 'Step 5.2-1: not started, live now: External work running (codex)' })
    expect(cell).toHaveAttribute('data-live', 'true')
    expect(cell.querySelector('[data-live-ring]')).toHaveClass('work-live-ring')
    const doneCell = screen.getByRole('button', { name: 'Step 5.1-1: done, live now: External work running (codex)' })
    expect(doneCell.querySelector('[data-live-ring]')).toHaveClass('work-live-ring')
    // 5.1-3 is in progress by its state, with nothing running: no marker.
    const active = screen.getByRole('button', { name: 'Step 5.1-3: in progress' })
    expect(active).not.toHaveAttribute('data-live')
    expect(active.querySelector('.work-live-ring')).toBeNull()
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(2)

    const nowLine = document.querySelector('[data-live-now="live"]') as HTMLElement
    // The registered work on 5.1-3 has its own link to Live beside these (data-live-unmarked).
    expect([...nowLine.querySelectorAll<HTMLElement>('a[data-live-step]')].map(a => [a.textContent, a.getAttribute('href')])).toEqual([
      ['5.1-1 · External work running (codex) · for 3 min', '#work?section=plan&root=R5&node=R5%3A5.1'],
      ['5.2-1 · External work running (codex) · for 20 min', '#work?section=plan&root=R5&node=R5%3A5.2'],
    ])
    expect(nowLine.querySelectorAll('.work-live-dot')).toHaveLength(2)
  })

  it('shows at most three live steps on the Now line and counts the rest', () => {
    at(NOW)
    rows(view(raw => { external(raw, 'R5:5.1-1'); external(raw, 'R5:5.1-3'); external(raw, 'R5:5.1-6') }))
    const nowLine = document.querySelector('[data-live-now="live"]') as HTMLElement
    expect([...nowLine.querySelectorAll<HTMLElement>('a[data-live-step]')].map(a => a.dataset.liveStep)).toEqual(['5.1-1', '5.1-3', '5.1-6'])
    expect(nowLine).toHaveTextContent('+1 more live step in the rows below')
    expect(document.querySelectorAll('[data-live-ring]')).toHaveLength(4)
  })

  it('says nothing runs only of a read that is now, and links Live when it lists work that marks no step', () => {
    at(NOW)
    rows(view(raw => { idle(raw); external(raw, 'R5:5.2-1', { heartbeat_age_s: 130 }) }))
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
    external(raw, 'R5:5.2-1')
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('/agentrt/plan-view')
      ? new Response(JSON.stringify(raw), { status: 200 }) : new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    window.location.hash = '#work'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    // The shell's GET /progress (never fetched by Work): R5 the newest round.
    client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress({ plan: [{ plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }] }))
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={{ token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
    return client
  }

  it("names what runs from the plan view, and fetches no progress of its own", async () => {
    const client = work(Date.parse(AT))
    const modules = await screen.findByRole('region', { name: 'Modules' })
    expect(modules.querySelector('[data-live-now="live"]')).toHaveTextContent('5.2-1 · External work running (codex) · for 20 min')
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
  it('breathes slowly (a 2 s ease-in-out cycle, under 1 Hz), stands still under reduced motion', () => {
    const rule = (selector: string) => new RegExp(`\\${selector} \\{([^}]*)\\}`).exec(css)?.[1] ?? ''
    for (const selector of ['.work-live-ring', '.work-live-dot']) {
      const animation = /animation: work-live-breathe (\d+(?:\.\d+)?)s ease-in-out infinite;/.exec(rule(selector))
      expect(animation).not.toBeNull()
      expect(Number(animation![1])).toBeGreaterThanOrEqual(1)
    }
    expect(css).toMatch(/@keyframes work-live-breathe \{\s*0%, 100% \{ opacity: \.35; \}\s*50% \{ opacity: 1; \}\s*\}/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.work-live-ring, \.work-live-dot \{ animation: none; opacity: 1; \}\s*\}/)
  })
})
