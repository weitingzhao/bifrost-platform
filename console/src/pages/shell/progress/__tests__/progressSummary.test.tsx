import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { StrictMode, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AGENT_PROGRESS_POLL_MS, GENERATION_SKEW_SECONDS } from '@/api/agentProgress'
import { formatHourMinute } from '@/pages/shell/progress/progressModel'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { nightsFigure } from '@/pages/shell/progress/progressSummaryModel'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import sample from './fixtures/sample-progress.json'
import summarySample from './fixtures/sample-progress-summary.json'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const PENDING_ID = 'appr_6f4ac97469fd6f10'
const SOON_ID = 'appr_aaaaaaaaaaaaaaaa'
const PAST_ID = 'appr_bbbbbbbbbbbbbbbb'
const XSS = '<script id="xss">alert(1)</script>'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() { return mem.size },
    clear() { mem.clear() },
    getItem(key) { return mem.has(key) ? (mem.get(key) ?? null) : null },
    key(index) { return [...mem.keys()][index] ?? null },
    removeItem(key) { mem.delete(key) },
    setItem(key, value) { mem.set(key, String(value)) },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function envelope(document: unknown, over: Record<string, unknown> = {}) {
  return {
    document,
    received_at: new Date(NOW).toISOString(),
    age_seconds: 0,
    stale: false,
    host: 'vision-mac',
    generated_at: new Date(NOW).toISOString(),
    generation_lag_seconds: 0,
    ...over,
  }
}

function stub(document: unknown, approvals?: () => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/v1/approvals')) return (approvals ?? (() => json(buildApprovalListResponse([]))))()
      return json(envelope(document))
    }),
  )
}

function box() {
  return screen.getByTestId('progress-summary')
}

describe('progress summary model', () => {
  it('A4 never turns a missing pace into NaN or Infinity', () => {
    expect(nightsFigure(null, undefined)).toBeNull()
    expect(nightsFigure(4, undefined)).toBeNull()
    expect(nightsFigure(4, 0)).toBeNull()
    expect(nightsFigure(4, 'unknown')).toBeNull()
    expect(nightsFigure('unknown', 5)).toBeNull()
    expect(nightsFigure(11, 5.5)).toBe('2')
    expect(nightsFigure(1, 1e-308)).toBe('Unknown')
    expect(nightsFigure(1, 1e-308)).not.toBe('Infinity')
  })
})

describe('Progress summary', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('A1 renders the published sample with the summary above the first section', async () => {
    stub(sample)
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('现在没有后台步骤在跑')).toBeTruthy()
    const root = screen.getByTestId('agent-work-document')
    const nodes = [...root.children]
    const summary = box()
    expect([...summary.children].map(node => node.getAttribute('data-summary-block'))).toEqual([
      'needs-you',
      'pictures',
      'gauges',
    ])
    const pictures = summary.querySelector('[data-summary-block="pictures"]')
    expect([...pictures!.children].map(node => node.getAttribute('data-summary-block'))).toEqual(['plan', 'running'])
    expect(summary.children.length).toBeLessThanOrEqual(3)
    expect(pictures?.className).toContain('grid-cols-1')
    expect(pictures?.className).toContain('min-[821px]:grid-cols-2')
    expect(nodes[0]).toBe(summary)
    expect(document.getElementById('sec-needs-you')).toBeNull()
    expect(summary.querySelector('#needs-you')).toBeTruthy()
    expect(summary.textContent).not.toMatch(/NaN|Infinity/)
  })

  it('A1 renders a cut of the live sample, and drops a picture when its field is absent', async () => {
    stub(summarySample)
    const first = render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(within(summary).getByText('批 #7')).toBeTruthy()
    expect(within(summary).getByText('1 live')).toBeTruthy()
    expect(within(summary).getByText('1 in progress')).toBeTruthy()
    expect(within(summary).getByText('0 not started')).toBeTruthy()
    expect(within(summary).getByText('W-62 进展看得见')).toBeTruthy()
    expect(summary.querySelector('[data-owner-dot]')).toBeTruthy()
    expect(within(summary).getByText('进展')).toBeTruthy()
    expect(summary.querySelector('[data-pool="claude.weekly"]')).toBeTruthy()
    expect(summary.querySelector('[data-window-tick]')).toBeTruthy()
    first.unmount()

    stub({ at: NOW_SEC, verdict: 'Bare' })
    render(wrap(<AgentWorkBoard />))
    const bare = await screen.findByTestId('progress-summary')
    expect(within(bare).getByText('No plan')).toBeTruthy()
    expect(within(bare).getByText('No pools')).toBeTruthy()
    expect(bare.querySelector('[data-coord="unknown"]')).toBeTruthy()
    expect(bare.querySelector('[data-done-count]')?.getAttribute('data-done-count')).toBe('0')
    expect(screen.queryByTestId('fold-sec-plan')).toBeNull()
    expect(screen.queryByTestId('fold-sec-quota')).toBeNull()
    expect(within(bare).getByText('Nothing needs you now, and nothing is queued for you.')).toBeTruthy()
  })

  it('B2 shows the four markers, orders what blocks or expires first, and counts what is not due', async () => {
    const soon = new Date(NOW + 5 * 60 * 60 * 1000).toISOString()
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    const past = new Date(NOW - 60_000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Markers',
        owner_items: [
          { name: 'Later plain', now: false, not_before: 'the freeze' },
          { name: 'Now blocks', now: true, blocks: 'the merge', takes: 'about 10 min' },
          { name: 'Now plain', now: true },
          { name: 'Later blocks', now: false, at: NOW_SEC + 3600, blocks: 'the release' },
          { name: 'Bad type', now: 'yes', blocks: 'should not show' },
          {
            name: 'Doc deadline',
            now: true,
            deadline: 1,
            approval: { id: PENDING_ID, number: 1, summary: 'harmless' },
          },
          { name: 'Soon request', now: true, approval: { id: SOON_ID } },
          { name: 'Past request', now: true, deadline: 1, approval: { id: PAST_ID } },
        ],
      },
      () =>
        json(
          buildApprovalListResponse([
            buildPlatformApproval({ id: PENDING_ID, number: 12, summary: 'real summary', expires_at: later }),
            buildPlatformApproval({ id: SOON_ID, number: 13, summary: 'soon', expires_at: soon }),
            buildPlatformApproval({ id: PAST_ID, number: 14, summary: 'past', status: 'pending', expires_at: past }),
          ]),
        ),
    )
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    await within(summary).findByText('Request #12 — real summary')
    expect(within(summary).getAllByText('You can do this now').length).toBeGreaterThan(0)
    const rows = () => [...summary.querySelectorAll('[data-ask]')].map(row => row.textContent ?? '')
    const text = rows()
    expect(text.findIndex(row => row.includes('Now blocks'))).toBeLessThan(text.findIndex(row => row.includes('Now plain')))
    expect(text.findIndex(row => row.includes('Now plain'))).toBeLessThan(text.findIndex(row => row.includes('Later blocks')))
    expect(text.findIndex(row => row.includes('Later blocks'))).toBeLessThan(text.findIndex(row => row.includes('Later plain')))
    expect(summary.querySelector('[data-not-yet]')?.getAttribute('data-not-yet')).toBe('2')
    expect(summary.querySelector('[data-needs-total]')?.getAttribute('data-needs-total')).toBe('7')
    const laterPlain = [...summary.querySelectorAll('[data-ask="later"]')].find(row => row.textContent?.includes('Later plain'))
    expect(laterPlain?.textContent).toContain('Not your turn yet')
    fireEvent.click(within(laterPlain as HTMLElement).getByRole('button'))
    expect(laterPlain?.textContent).toContain('the freeze')
    expect(laterPlain?.textContent).not.toContain('Not your turn yet · the freeze')
    expect(laterPlain?.querySelector('[data-agent-note]')?.textContent).toContain('the freeze')
    expect(within(summary).getByText(`Not your turn yet · about ${formatHourMinute(NOW_SEC + 3600)}`)).toBeTruthy()
    expect(summary.textContent).not.toContain('Holding up:')
    expect(summary.textContent).not.toContain('Takes effect in')
    const held = [...summary.querySelectorAll('[data-fact="If not done"] [data-agent-note]')].find(node => node.textContent?.includes('the merge'))
    expect(held?.textContent).toContain('the merge')
    const took = [...summary.querySelectorAll('[data-fact="After"] [data-agent-note]')].find(node => node.textContent?.includes('about 10 min'))
    expect(took?.textContent).toContain('about 10 min')
    expect(within(summary).getAllByText('Not written').length).toBeGreaterThan(0)
    expect(within(summary).getAllByText('No deadline').length).toBeGreaterThan(0)
    const long = within(summary).getByText(/23 h left/)
    expect(long.getAttribute('data-urgent')).toBe('false')
    const short = within(summary).getByText(/5 h left/)
    expect(short.getAttribute('data-urgent')).toBe('true')
    const pastRow = summary.querySelector('[data-ask="expired"]')
    fireEvent.click(within(pastRow as HTMLElement).getByRole('button'))
    const gone = within(pastRow as HTMLElement).getByText(/\(past\)/)
    expect(gone.getAttribute('data-urgent')).toBe('true')
    expect(summary.textContent).not.toContain('1970')
    expect(summary.textContent).not.toContain('harmless')
    expect(within(summary).getAllByText("Agent's note:").length).toBeGreaterThan(0)
    const open = within(summary).getByRole('link', { name: 'Open request #12' })
    expect(open.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
    expect(within(summary).queryByRole('link', { name: 'Open request #14' })).toBeNull()
    const bad = summary.querySelector('[data-ask="unreadable"]')
    expect(bad?.textContent).toContain('Could not read this item')
    expect(bad?.textContent).not.toContain('You can do this now')
    expect(bad?.textContent).not.toContain('should not show')
  })

  it('B2 says the request could not be read and invents no deadline', async () => {
    stub(
      {
        at: NOW_SEC,
        verdict: 'Unread',
        owner_items: [{ name: 'Approve', now: true, deadline: NOW_SEC + 86_400, approval: { id: PENDING_ID } }],
      },
      () => json({ error: 'down' }, 500),
    )
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(await within(summary).findByText("Could not read the request's state")).toBeTruthy()
    expect(summary.textContent).not.toMatch(/By /)
    expect(summary.textContent).not.toContain('No deadline')
    expect(within(summary).getByRole('link', { name: 'Open Needs you' }).getAttribute('href')).toBe('#needs-you')
  })

  it('D1 drops a Needs you row when the request is no longer pending, without a document change', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    let status = 'pending'
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) {
          return json(buildApprovalListResponse([buildPlatformApproval({ id: PENDING_ID, number: 8, summary: 'ship it', status })]))
        }
        return json(
          envelope({
            at: NOW_SEC,
            verdict: 'Same document',
            owner_items: [{ name: '批 #8', now: true, approval: { id: PENDING_ID, number: 1, summary: 'ignore' } }],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(await within(summary).findByText('Request #8 — ship it')).toBeTruthy()
    status = 'approved'
    await act(async () => {
      await vi.advanceTimersByTimeAsync(AGENT_PROGRESS_POLL_MS)
    })
    expect(within(summary).queryByText('Request #8 — ship it')).toBeNull()
    expect(within(summary).queryByText('批 #8')).toBeNull()
    expect(screen.getByText('Same document')).toBeTruthy()
  })

  it('A4 puts counts on the three segments, bars slices, and skips nights when pace is absent', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Plan',
      roadmap: {
        stages: [
          { name: 'Then', state: 'done' },
          { name: 'Now', state: 'current' },
        ],
        groups: [
          { state: 'done', name: 'Shipped', items: ['a', 'b'] },
          {
            state: 'doing',
            name: 'Doing',
            items: ['c'],
            detail: [
              { name: 'W-1', est: { done: 1, total: 4, mine: 2, owner: 1, blocked: '你' } },
              { name: 'W-0', est: { done: 0, total: 0, mine: 3, owner: 0, blocked: '我' } },
            ],
          },
          {
            state: 'todo',
            name: 'Todo',
            items: ['d'],
            detail: [{ name: 'W-3', est: { done: 0, total: 2, mine: 2, owner: 1, blocked: 'me' } }],
          },
        ],
        pace: { per_night: 5, basis: 'last night' },
      },
    })
    const paced = render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(within(summary).getByText('2 live')).toBeTruthy()
    expect(within(summary).getByText('1 in progress')).toBeTruthy()
    expect(within(summary).getByText('1 not started')).toBeTruthy()
    expect(summary.querySelector('[data-stage="current"]')?.textContent).toBe('Now')
    const bars = [...summary.querySelectorAll('[data-slice-pct]')].map(node => node.getAttribute('data-slice-pct'))
    expect(bars).toEqual(['25', '0'])
    expect(summary.querySelectorAll('[data-owner-dot]')).toHaveLength(1)
    expect(within(summary).getByText('7 slices left')).toBeTruthy()
    expect(within(summary).getByText('1.4 nights')).toBeTruthy()
    expect(summary.textContent).not.toContain('times you')
    expect(summary.textContent).toMatch(/Later: about 2 more requests to you over this step/)
    expect([...summary.querySelectorAll('[data-owner-count]')].every(node => node.closest('#needs-you'))).toBe(true)
    paced.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'No pace',
      roadmap: {
        groups: [
          { state: 'doing', items: ['c'], detail: [{ name: 'W-1', est: { done: 1, total: 2, mine: 1, owner: 1 } }] },
        ],
      },
    })
    render(wrap(<AgentWorkBoard />))
    const quiet = await screen.findByTestId('progress-summary')
    expect(quiet.querySelector('[data-nights]')).toBeNull()
    expect(quiet.textContent).not.toMatch(/NaN|Infinity/)
    expect(within(quiet).getByText('1 slices left')).toBeTruthy()
  })

  it('A2 caps a running bar, marks a slow step amber, and puts an Owner step first', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Run',
      works: [
        {
          title: 'Late work',
          short: 'Late',
          steps: [{ name: 'Slow step', state: 'running', who: 'Cursor', started: NOW_SEC - 3600, usual_min: 10 }],
        },
        {
          title: 'Owner work',
          short: 'Owner',
          steps: [{ name: 'Decide', state: 'gap', who: 'You', needs_owner: true, started: NOW_SEC - 600 }],
        },
        {
          title: 'Bad work',
          short: 'Bad',
          steps: [{ name: 'Broke', state: 'failed', who: 'Codex' }],
        },
      ],
    })
    const running = render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    const rows = [...summary.querySelectorAll('[data-running-row]')]
    expect(rows.map(row => row.getAttribute('data-tone'))).toEqual(['owner', 'bad', 'amber'])
    const late = rows[2]?.querySelector('[data-elapsed-pct]')
    expect(late?.getAttribute('data-elapsed-pct')).toBe('100')
    expect(Number(late?.getAttribute('data-elapsed-pct'))).toBeLessThanOrEqual(100)
    running.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'Idle',
      queue: [{ title: 'Review the plan', why_waiting: 'waiting until morning' }],
    })
    render(wrap(<AgentWorkBoard />))
    const idle = await screen.findByTestId('progress-summary')
    const idleRun = idle.querySelector('[data-summary-block="running"]')
    expect(idleRun?.textContent).toContain('Unknown')
    expect(idleRun?.querySelector('[data-run-state]')?.getAttribute('data-run-state')).toBe('unknown')
  })

  it('A2 lights trouble red only for stuck or failed, and the page gauge follows the skew rule', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Slow',
      works: [{ title: 'A', steps: [{ name: 'Pace', state: 'slow' }] }],
    })
    const slow = render(wrap(<AgentWorkBoard />))
    const amber = await screen.findByTestId('progress-summary')
    expect(amber.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('amber')
    expect(amber.querySelector('[data-light="red"]')?.getAttribute('data-on')).toBe('false')
    slow.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'Gap',
      works: [{ title: 'A', steps: [{ name: 'Wait', state: 'gap' }] }],
    })
    const gap = render(wrap(<AgentWorkBoard />))
    expect((await screen.findByTestId('progress-summary')).querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('amber')
    gap.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'Stuck',
      works: [
        { title: 'A', steps: [{ name: 'Quiet', state: 'quiet' }] },
        { title: 'B', steps: [{ name: 'Dead', state: 'dead' }] },
        { title: 'C', steps: [{ name: 'Fail', state: 'failed' }] },
      ],
    })
    const bad = render(wrap(<AgentWorkBoard />))
    const red = await screen.findByTestId('progress-summary')
    expect(red.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('red')
    expect(red.querySelector('[data-light="red"]')?.getAttribute('data-on')).toBe('true')
    expect(red.querySelector('[data-light="amber"]')?.getAttribute('data-on')).toBe('false')
    bad.unmount()

    stub({ at: NOW_SEC, verdict: 'Fresh', works: [{ title: 'A', steps: [{ name: 'Go', state: 'running' }] }] })
    const fresh = render(wrap(<AgentWorkBoard />))
    expect((await screen.findByTestId('progress-summary')).querySelector('[data-fresh]')?.getAttribute('data-fresh')).toBe('ok')
    fresh.unmount()

    stub(
      { at: NOW_SEC - GENERATION_SKEW_SECONDS - 5, verdict: 'Old' },
    )
    render(wrap(<AgentWorkBoard />))
    expect((await screen.findByTestId('progress-summary')).querySelector('[data-fresh]')?.getAttribute('data-fresh')).toBe('stale')
  })

  it('A3 keeps fold defaults, survives a refetch, and opens a section from its summary block', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    const scroll = vi.fn()
    HTMLElement.prototype.scrollIntoView = scroll
    let verdict = 'First'
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        json(
          envelope({
            at: NOW_SEC,
            verdict,
            owner_now: [{ name: 'Approve' }],
            works: [{ title: 'Live', steps: [{ name: 'Go', state: 'running', who: 'Cursor' }] }],
            roadmap: { groups: [{ state: 'doing', items: ['a'] }] },
            feed: [{ at: NOW_SEC, work: 'Live', step: 'Go', text: 'hi' }],
            recent: ['shipped'],
          }),
        ),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('First')).toBeTruthy()
    expect(document.getElementById('sec-needs-you')).toBeNull()
    expect(document.getElementById('needs-you')?.querySelector('details')).toBeNull()
    expect(document.querySelector('#sec-running details')?.hasAttribute('open')).toBe(true)
    expect(document.querySelector('#sec-plan details')?.hasAttribute('open')).toBe(false)
    expect(document.querySelector('#sec-coord details')?.hasAttribute('open')).toBe(false)
    expect(document.querySelector('#sec-activity details')?.hasAttribute('open')).toBe(false)
    expect(document.querySelector('#sec-done details')?.hasAttribute('open')).toBe(false)
    fireEvent.click(document.querySelector('[data-summary-block="plan"]')!)
    expect(document.querySelector('#sec-plan details')?.hasAttribute('open')).toBe(true)
    expect(scroll).toHaveBeenCalled()
    verdict = 'Second'
    await act(async () => {
      await vi.advanceTimersByTimeAsync(AGENT_PROGRESS_POLL_MS)
    })
    expect(await screen.findByText('Second')).toBeTruthy()
    expect(document.getElementById('needs-you')).toBeTruthy()
    expect(document.querySelector('#sec-plan details')?.hasAttribute('open')).toBe(true)
  })

  it('shows agent text, including a script tag, as text and adds no element', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Text',
      owner_items: [{ name: XSS, work: XSS, now: true, blocks: XSS, takes: XSS }],
      standing_items: [{ name: XSS, now: false, deadline_text: XSS, blocks_text: XSS }],
      roadmap: {
        groups: [{ state: 'doing', items: ['a'], detail: [{ name: XSS, est: { done: 1, total: 2, note: XSS, blocked: XSS } }] }],
        pace: { per_night: 2, basis: XSS, owner_wait: XSS },
        delta: { since: XSS, lines: [XSS] },
        uncovered_exit: XSS,
      },
    })
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(summary.querySelector('script')).toBeNull()
    expect(document.getElementById('xss')).toBeNull()
    expect(summary.textContent).toContain(XSS)
    fireEvent.click(screen.getByTestId('fold-sec-plan'))
    fireEvent.click(screen.getByTestId('fold-plan-rest'))
    fireEvent.click(screen.getByTestId('fold-plan-delta'))
    fireEvent.click(screen.getByTestId('fold-plan-basis'))
    const plan = screen.getByRole('region', { name: 'Plan' })
    expect(plan.querySelector('script')).toBeNull()
    expect(plan.textContent).toContain(XSS)
  })

  it('A2 ticks only the running bar, not the summary shell', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    stub({
      at: NOW_SEC,
      verdict: 'Tick',
      roadmap: { groups: [{ state: 'done', items: ['a'] }, { state: 'doing', items: ['b'] }, { state: 'todo', items: [] }] },
      works: [{ title: 'Board', short: 'Board', steps: [{ name: 'Run', state: 'running', who: 'Cursor', started: NOW_SEC - 600, usual_min: 30 }] }],
    })
    render(
      wrap(
        <StrictMode>
          <AgentWorkBoard />
        </StrictMode>,
      ),
    )
    const summary = await screen.findByTestId('progress-summary')
    const paints = summary.getAttribute('data-paints')
    const block = (name: string) => summary.querySelector(`[data-summary-block="${name}"]`)?.getAttribute('data-summary-renders')
    const gauge = (name: string) => summary.querySelector(`[data-gauge="${name}"]`)?.getAttribute('data-summary-renders')
    const before = {
      needs: block('needs-you'),
      plan: block('plan'),
      running: block('running'),
      gauges: block('gauges'),
      quota: gauge('quota'),
      done: gauge('done'),
      elapsed: summary.querySelector('[data-elapsed-pct]')?.getAttribute('data-elapsed-pct'),
      trouble: summary.querySelector('[data-trouble-renders]')?.getAttribute('data-trouble-renders'),
    }
    expect(before.elapsed).toBe('33')
    expect(before.needs).not.toBeNull()
    expect(before.plan).not.toBeNull()
    expect(before.running).not.toBeNull()
    await act(async () => {
      vi.setSystemTime(NOW + 120_000)
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(summary.getAttribute('data-paints')).toBe(paints)
    expect(block('needs-you')).toBe(before.needs)
    expect(block('plan')).toBe(before.plan)
    expect(block('running')).toBe(before.running)
    expect(block('gauges')).toBe(before.gauges)
    expect(gauge('quota')).toBe(before.quota)
    expect(gauge('done')).toBe(before.done)
    expect(summary.querySelector('[data-elapsed-pct]')?.getAttribute('data-elapsed-pct')).toBe('40')
    expect(Number(summary.querySelector('[data-trouble-renders]')?.getAttribute('data-trouble-renders'))).toBeGreaterThan(Number(before.trouble))
  })
})
