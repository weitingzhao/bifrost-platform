import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { formatLocalTime } from '@/api/agentProgress'
import { AGENT_TEXT_MAX } from '@/pages/shell/progress/progressSummaryModel'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const LIVE_ID = 'appr_6f4ac97469fd6f10'
const OTHER_ID = 'appr_aaaaaaaaaaaaaaaa'
const MISSING_ID = 'appr_bbbbbbbbbbbbbbbb'

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

function envelope(document: unknown) {
  return {
    document,
    received_at: new Date(NOW).toISOString(),
    age_seconds: 0,
    stale: false,
    host: 'vision-mac',
    generated_at: new Date(NOW).toISOString(),
    generation_lag_seconds: 0,
  }
}

function stub(document: unknown, approvals?: () => Promise<Response> | Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/v1/approvals')) return (approvals ?? (() => json(buildApprovalListResponse([]))))()
      return json(envelope(document))
    }),
  )
}

function summary() {
  return screen.getByTestId('progress-summary')
}

describe('a request row follows the real request', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('A shows Unknown, never green, while the approvals list is loading', async () => {
    stub(
      { at: NOW_SEC, verdict: 'Load', owner_items: [{ name: 'Sign', now: true, approval: { id: LIVE_ID } }] },
      () => new Promise<Response>(() => undefined),
    )
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const item = await within(box).findByRole('listitem')
    expect(item.getAttribute('data-ask')).toBe('unknown')
    expect(item.textContent).toContain('Unknown')
    expect(item?.querySelector('[data-turn-dot]')?.getAttribute('data-turn-dot')).toBe('neutral')
    expect(item?.querySelector('[data-turn-dot]')?.className).not.toContain('bg-emerald-500')
    expect(item?.textContent).not.toContain('You can do this now')
    expect(summary().textContent).not.toMatch(/By /)
    expect(summary().textContent).not.toContain('No deadline')
  })

  it('A shows Unknown when the id is not in the list, after a request he can act on', async () => {
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Order',
        owner_items: [
          { name: 'Missing', now: true, approval: { id: MISSING_ID } },
          { name: 'Live', now: false, approval: { id: LIVE_ID } },
        ],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: LIVE_ID, number: 4, summary: 'real', expires_at: later })])),
    )
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(await within(box).findByText('You can do this now')).toBeTruthy()
    const rows = [...box.querySelectorAll('[data-ask]')].map(row => row.getAttribute('data-ask'))
    expect(rows.indexOf('now')).toBeLessThan(rows.indexOf('unknown'))
    const missing = [...box.querySelectorAll('[data-ask="unknown"]')][0]
    expect(missing?.querySelector('[data-turn-dot]')?.className).not.toContain('bg-emerald-500')
    expect(missing?.textContent).not.toMatch(/By /)
    expect(box.querySelector('[data-now-count]')?.getAttribute('data-now-count')).toBe('1')
  })

  it('A says Expired for a pending request past expires_at, and does not count it', async () => {
    const past = new Date(NOW - 60_000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Past',
        owner_items: [{ name: 'Late', now: true, approval: { id: LIVE_ID } }],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: LIVE_ID, number: 9, summary: 'old', expires_at: past })])),
    )
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const row = await within(box).findByText('Expired', { selector: '[data-ask="expired"] span' })
    const item = row.closest('[data-ask]')
    expect(item?.getAttribute('data-ask')).toBe('expired')
    expect(item?.querySelector('[data-turn-dot]')?.getAttribute('data-turn-dot')).toBe('neutral')
    expect(item?.textContent).not.toContain('You can do this now')
    expect(box.querySelector('[data-now-count]')?.getAttribute('data-now-count')).toBe('0')
    expect(box.textContent).not.toContain('more not yet due')
  })

  it('A says you can do this now when the request is pending, whatever the document says', async () => {
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Ignore now',
        owner_items: [{ name: 'Wait', now: false, not_before: 'the freeze', approval: { id: OTHER_ID } }],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: OTHER_ID, number: 3, summary: 'go', expires_at: later })])),
    )
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const row = (await within(box).findByText('You can do this now')).closest('[data-ask]')
    expect(row?.getAttribute('data-ask')).toBe('now')
    expect(row?.querySelector('[data-turn-dot]')?.className).toContain('bg-emerald-500')
    expect(row?.textContent).not.toContain('Not your turn yet')
    expect(box.querySelector('[data-now-count]')?.getAttribute('data-now-count')).toBe('1')
  })

  it('a pending request ignores document blocks and takes, and an approved request leaves both lists', async () => {
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Facts',
        owner_items: [
          {
            name: 'Hold',
            now: false,
            not_before: 'the freeze',
            blocks: 'the merge',
            takes: 'about 10 min',
            deadline: NOW_SEC + 86_400,
            approval: { id: LIVE_ID },
          },
        ],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: LIVE_ID, number: 4, summary: 'real', status: 'pending', expires_at: later })])),
    )
    const pending = render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const row = (await within(box).findByText('You can do this now')).closest('[data-ask]')
    expect(row?.getAttribute('data-ask')).toBe('now')
    expect(row?.textContent).not.toContain('Not your turn yet')
    expect(row?.textContent).not.toContain('Holding up:')
    expect(row?.textContent).not.toContain('Takes effect in')
    expect(row?.querySelector('[data-holds]')?.textContent).toBe('Unknown')
    expect(row?.querySelector('[data-holds]')?.getAttribute('data-holds')).toBe('no')
    expect(row?.querySelector('[data-holds]')?.className).not.toContain('text-warning')
    const note = row?.querySelector('[data-agent-note]')
    expect(note?.textContent).toContain('the merge')
    expect(note?.textContent).toContain('about 10 min')
    expect(note?.textContent).toContain('the freeze')
    const section = await screen.findByRole('region', { name: 'Needs you' })
    expect(within(section).getByText('You can do this now')).toBeTruthy()
    expect(section.querySelector('[data-ask="now"]')?.textContent).not.toContain('Not your turn yet')
    expect(section.textContent).not.toContain('Holding up:')
    expect(section.textContent).not.toContain('Takes effect in')
    expect(within(section).getByRole('link', { name: 'Open request #4' })).toBeTruthy()
    expect(section.querySelector('[data-agent-note]')?.textContent).toContain('the merge')
    pending.unmount()

    stub(
      {
        at: NOW_SEC,
        verdict: 'Closed',
        owner_items: [{ name: 'Hold', now: true, blocks: 'the merge', approval: { id: LIVE_ID } }],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: LIVE_ID, number: 4, summary: 'real', status: 'approved', expires_at: later })])),
    )
    render(wrap(<AgentWorkBoard />))
    const closed = await screen.findByTestId('progress-summary')
    expect(await within(closed).findByText('Nothing needs you now, and nothing is queued for you.')).toBeTruthy()
    expect(within(closed).queryByText('the merge')).toBeNull()
    expect(within(closed).queryByText('You can do this now')).toBeNull()
    expect(screen.getByRole('region', { name: 'Needs you' })).toBeTruthy()
    expect(closed.querySelector('[data-now-count]')?.getAttribute('data-now-count')).toBe('0')
  })
})

const XSS = '<script id="xss-field">alert(1)</script>'

type FieldCase = {
  field: string
  document: Record<string, unknown>
  open: string[]
  where: 'summary' | 'plan' | 'needs'
}

const FIELD_CASES: FieldCase[] = [
  { field: 'name', document: { owner_items: [{ name: XSS, now: true }] }, open: [], where: 'summary' },
  { field: 'not_before', document: { owner_items: [{ name: 'Later', now: false, not_before: XSS }] }, open: [], where: 'summary' },
  { field: 'blocks', document: { owner_items: [{ name: 'Hold', now: true, blocks: XSS }] }, open: [], where: 'summary' },
  { field: 'takes', document: { owner_items: [{ name: 'Take', now: true, takes: XSS }] }, open: [], where: 'summary' },
  { field: 'deadline_text', document: { standing_items: [{ name: 'Stand', deadline_text: XSS }] }, open: [], where: 'summary' },
  { field: 'blocks_text', document: { standing_items: [{ name: 'Stand', blocks_text: XSS }] }, open: [], where: 'summary' },
  { field: 'standing takes', document: { standing_items: [{ name: 'Stand', takes: XSS }] }, open: [], where: 'summary' },
  {
    field: 'est.note',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'], detail: [{ name: 'W', est: { done: 1, total: 2, note: XSS, blocked: '你' } }] }] } },
    open: ['fold-sec-plan'],
    where: 'plan',
  },
  {
    field: 'est.blocked',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'], detail: [{ name: 'W', est: { done: 1, total: 2, blocked: XSS } }] }] } },
    open: ['fold-sec-plan', 'fold-plan-rest'],
    where: 'plan',
  },
  {
    field: 'est.sure',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'], detail: [{ name: 'W', est: { done: 1, total: 2, sure: XSS, blocked: '你' } }] }] } },
    open: ['fold-sec-plan'],
    where: 'plan',
  },
  {
    field: 'est.as_of',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'], detail: [{ name: 'W', est: { done: 1, total: 2, as_of: XSS, blocked: '你' } }] }] } },
    open: ['fold-sec-plan'],
    where: 'plan',
  },
  {
    field: 'pace.basis',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'] }], pace: { per_night: 2, basis: XSS } } },
    open: ['fold-sec-plan', 'fold-plan-basis'],
    where: 'plan',
  },
  {
    field: 'pace.owner_wait',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'] }], pace: { per_night: 2, owner_wait: XSS } } },
    open: ['fold-sec-plan', 'fold-plan-basis'],
    where: 'plan',
  },
  {
    field: 'delta.lines',
    document: { roadmap: { groups: [{ state: 'doing', items: ['a'] }], delta: { since: 'yesterday', lines: [XSS] } } },
    open: ['fold-sec-plan', 'fold-plan-delta'],
    where: 'plan',
  },
  {
    field: 'actions[].text',
    document: { owner_items: [{ name: 'Sign', now: true, actions: [{ text: XSS }] }] },
    open: [],
    where: 'needs',
  },
  { field: 'advice', document: { owner_items: [{ name: 'Advise', now: true, advice: XSS }] }, open: [], where: 'summary' },
  { field: 'where', document: { owner_items: [{ name: 'Place', now: true, where: XSS }] }, open: [], where: 'summary' },
  { field: 'reply', document: { owner_items: [{ name: 'Reply', now: true, reply: XSS }] }, open: [], where: 'summary' },
]

describe('agent text is shown as text', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it.each(FIELD_CASES)('B shows $field as text and adds no element', async ({ document: doc, open, where }) => {
    stub({ at: NOW_SEC, verdict: 'Text', ...doc })
    render(wrap(<AgentWorkBoard />))
    await screen.findByTestId('progress-summary')
    for (const id of open) {
      fireEvent.click(await screen.findByTestId(id))
    }
    const root =
      where === 'plan'
        ? screen.getByRole('region', { name: 'Plan' })
        : where === 'needs'
          ? screen.getByRole('region', { name: 'Needs you' })
          : summary()
    for (const button of root.querySelectorAll('button[aria-expanded="false"]')) fireEvent.click(button)
    expect(root.textContent).toContain(XSS)
    expect(root.querySelector('script')).toBeNull()
    expect(window.document.getElementById('xss-field')).toBeNull()
  })

  it('B bounds a long not_before, drops bidi marks, and keeps the action link ahead of it', async () => {
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    const raw = `\u202E${'Z'.repeat(10_000)}`
    stub(
      {
        at: NOW_SEC,
        verdict: 'Bound',
        owner_items: [{ name: 'Sign', now: false, not_before: raw, approval: { id: LIVE_ID } }],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: LIVE_ID, number: 4, summary: 'real', expires_at: later })])),
    )
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const link = await within(box).findByRole('link', { name: 'Open request #4' })
    const note = box.querySelector('[data-agent-note]')
    expect(note).toBeTruthy()
    expect(note?.textContent).not.toContain('\u202E')
    expect(note?.textContent).toContain('…')
    expect(note?.textContent?.match(/Z+/)?.[0]?.length ?? 0).toBeLessThanOrEqual(AGENT_TEXT_MAX - 1)
    expect(note?.className).toContain('truncate')
    expect(link.compareDocumentPosition(note as Node) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    const turn = box.querySelector('[data-ask="now"]')
    expect(turn?.textContent?.startsWith('You can do this now')).toBe(true)
  })

  it('B cleans a name that has no estimate, and does not fold agent words into a console sentence', async () => {
    const raw = `\u202E${'n'.repeat(10_000)}`
    stub({
      at: NOW_SEC,
      verdict: 'Plan text',
      roadmap: {
        groups: [
          {
            state: 'doing',
            items: ['bare', 'full'],
            detail: [
              { name: raw },
              { name: 'Counted', est: { done: 1, total: 2, mine: 1, owner: 0, blocked: 'the lane', sure: 'high', note: 'a note', as_of: 'today' } },
            ],
          },
        ],
      },
    })
    render(wrap(<AgentWorkBoard />))
    await screen.findByTestId('progress-summary')
    fireEvent.click(await screen.findByTestId('fold-sec-plan'))
    fireEvent.click(await screen.findByTestId('fold-plan-rest'))
    const plan = screen.getByRole('region', { name: 'Plan' })
    expect(plan.textContent).not.toContain('\u202E')
    expect(plan.textContent).toContain('…')
    expect(plan.textContent).toContain('No estimate')
    const title = [...plan.querySelectorAll('span')].find(node => (node.textContent ?? '').startsWith('n') && (node.textContent ?? '').includes('…'))
    expect(title?.textContent?.match(/n+/)?.[0]?.length ?? 0).toBeLessThanOrEqual(AGENT_TEXT_MAX - 1)
    expect(plan.textContent).not.toContain('Waiting on:')
    expect(plan.textContent).not.toContain('Confidence:')
    const noted = plan.querySelector('[data-agent-note]')
    expect(noted?.textContent).toContain('the lane')
    expect(noted?.textContent).toContain('high')
    expect(noted?.textContent).toContain('a note')
    expect(noted?.textContent).toContain('today')
    const outside = plan.textContent?.replace(noted?.textContent ?? '', '') ?? ''
    expect(outside).not.toContain('the lane')
    expect(outside).toContain('Waiting on')
    expect(outside).toContain('Confidence')
  })
})

describe('one list for the summary, the section, and the empty state', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('C shows an owner_items action in the section and does not say nothing is waiting', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Items',
      owner_items: [{ name: 'Sign', now: true, actions: [{ text: '签署规则' }] }],
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(within(box).getByText('Sign')).toBeTruthy()
    const section = await screen.findByRole('region', { name: 'Needs you' })
    expect(within(section).getByText('签署规则')).toBeTruthy()
    expect(screen.queryByText('Nothing is waiting for you.')).toBeNull()
  })
})

describe('progress numbers that can be wrong are Unknown', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('D shows Unknown for a contradictory estimate and keeps a legitimate 0/0', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Est',
      roadmap: {
        groups: [
          {
            state: 'doing',
            items: ['a'],
            detail: [
              { name: 'Over', est: { done: 9, total: 2, mine: 1, owner: 1 } },
              { name: 'Empty', est: { done: 0, total: 0, mine: 1, owner: 0 } },
              { name: 'Zero total', est: { done: 1, total: 0, mine: 1, owner: 0 } },
              { name: 'Fraction', est: { done: 1.5, total: 4, mine: 1, owner: 0 } },
            ],
          },
        ],
      },
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const rows = [...box.querySelectorAll('[data-plan-item]')]
    expect(rows[0]?.textContent).toContain('Unknown')
    expect(rows[0]?.textContent).not.toContain('9/2')
    expect(rows[0]?.querySelector('[data-slice-pct]')).toBeNull()
    expect(rows[0]?.querySelector('.bg-emerald-500')).toBeNull()
    expect(rows[1]?.querySelector('[data-slice-pct]')?.getAttribute('data-slice-pct')).toBe('0')
    expect(rows[1]?.textContent).toContain('0/0')
    expect(rows[2]?.querySelector('[data-slice="unknown"]')?.textContent).toBe('Unknown')
    expect(rows[3]?.querySelector('[data-slice="unknown"]')?.textContent).toBe('Unknown')
  })

  it('E labels a partial total and does not derive nights from it', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Partial',
      roadmap: {
        groups: [
          {
            state: 'doing',
            items: ['a', 'b'],
            detail: [
              { name: 'Counted', est: { done: 1, total: 2, mine: 2, owner: 1 } },
              { name: 'Bare', est: { done: 0, total: 10 } },
            ],
          },
        ],
        pace: { per_night: 2 },
      },
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(within(box).getByText('at least 2 slices (1 item not estimated)')).toBeTruthy()
    expect(box.textContent).not.toContain('times you')
    expect(box.querySelector('[data-nights]')).toBeNull()
    expect(box.textContent).not.toMatch(/nights/)
  })

  it('E counts an item that has no detail as not estimated, and will not derive nights from that', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Gap',
      roadmap: {
        groups: [
          {
            state: 'doing',
            items: ['a'],
            detail: [{ name: 'Counted', est: { done: 1, total: 2, mine: 1, owner: 0 } }],
          },
          { state: 'todo', items: ['b'] },
        ],
        pace: { per_night: 1 },
      },
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(within(box).getByText('at least 1 slices (1 item not estimated)')).toBeTruthy()
    expect(box.querySelector('[data-nights]')).toBeNull()
    expect(box.textContent).not.toMatch(/Infinity|NaN/)
    expect(box.textContent).not.toMatch(/\d+(\.\d+)? nights/)
  })

  it('E shows Unknown when the nights figure overflows, as with per_night 1e-308', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Tiny',
      roadmap: {
        groups: [
          {
            state: 'doing',
            items: ['a'],
            detail: [{ name: 'One', est: { done: 0, total: 1, mine: 1, owner: 0 } }],
          },
        ],
        pace: { per_night: 1e-308 },
      },
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(box.querySelector('[data-nights="unknown"]')?.textContent).toBe('Unknown')
    expect(box.textContent).not.toMatch(/Infinity/)
  })

  it('F shows when a running step is expected, and Unknown when it has no time', async () => {
    const eta = NOW_SEC + 600
    stub({
      at: NOW_SEC,
      verdict: 'Eta',
      works: [
        {
          title: 'Board',
          short: 'Board',
          steps: [
            { name: 'Run', state: 'running', who: 'Cursor', started: NOW_SEC - 60, usual_min: 30, eta_end: eta },
            { name: 'Bare', state: 'running', who: 'Codex', started: NOW_SEC - 60, usual_min: 30 },
            { name: 'Yours', state: 'gap', who: 'You', needs_owner: true, eta_end: eta },
          ],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const rows = [...box.querySelectorAll('[data-running-row]')]
    const when = (name: string) => rows.find(row => row.textContent?.includes(name))?.querySelector('[data-running-when]')?.textContent
    expect(when('Run')).toBe(formatLocalTime(eta))
    expect(when('Bare')).toBe('Unknown')
    expect(when('Yours')).toBe('Waiting for you')
  })

  it('G turns the trouble light amber when a running step passes its usual time', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    stub({
      at: NOW_SEC,
      verdict: 'Cross',
      works: [
        {
          title: 'Board',
          short: 'Board',
          steps: [{ name: 'Run', state: 'running', who: 'Cursor', started: NOW_SEC - 9 * 60, usual_min: 10 }],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    expect(box.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('green')
    const stable = {
      paints: box.getAttribute('data-paints'),
      plan: box.querySelector('[data-summary-block="plan"]')?.getAttribute('data-summary-renders'),
      running: box.querySelector('[data-summary-block="running"]')?.getAttribute('data-summary-renders'),
      gauges: box.querySelector('[data-summary-block="gauges"]')?.getAttribute('data-summary-renders'),
      needs: box.querySelector('[data-summary-block="needs-you"]')?.getAttribute('data-summary-renders'),
    }
    await act(async () => {
      vi.setSystemTime(NOW + 2 * 60 * 1000)
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(box.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('amber')
    expect(box.querySelector('[data-running-row]')?.getAttribute('data-tone')).toBe('amber')
    expect(box.getAttribute('data-paints')).toBe(stable.paints)
    expect(box.querySelector('[data-summary-block="plan"]')?.getAttribute('data-summary-renders')).toBe(stable.plan)
    expect(box.querySelector('[data-summary-block="running"]')?.getAttribute('data-summary-renders')).toBe(stable.running)
    expect(box.querySelector('[data-summary-block="gauges"]')?.getAttribute('data-summary-renders')).toBe(stable.gauges)
    expect(box.querySelector('[data-summary-block="needs-you"]')?.getAttribute('data-summary-renders')).toBe(stable.needs)
    expect(Number(box.querySelector('[data-trouble-renders]')?.getAttribute('data-trouble-renders'))).toBeGreaterThan(1)
  })

  it('H shows Unknown for an absurd usual time or a start in the future', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Absurd',
      works: [
        {
          title: 'Board',
          short: 'Board',
          steps: [
            { name: 'Huge', state: 'running', who: 'Cursor', started: NOW_SEC - 3600, usual_min: 1e100 },
            { name: 'Future', state: 'running', who: 'Codex', started: NOW_SEC + 120, usual_min: 10 },
          ],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const box = await screen.findByTestId('progress-summary')
    const rows = [...box.querySelectorAll('[data-running-row]')]
    for (const row of rows) {
      expect(row.querySelector('[data-elapsed="unknown"]')?.textContent).toBe('Unknown')
      expect(row.querySelector('[data-elapsed-pct]')).toBeNull()
      expect(row.querySelector('.bg-emerald-500')).toBeNull()
    }
  })
})
