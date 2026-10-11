import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, within } from '@testing-library/react'
import { StrictMode, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const PENDING_ID = 'appr_6f4ac97469fd6f10'

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

function consoleWords(root: HTMLElement): string {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const parts: string[] = []
  let node = walker.nextNode()
  while (node != null) {
    if (node.parentElement?.closest('[data-agent-note]') == null) parts.push(node.textContent ?? '')
    node = walker.nextNode()
  }
  return parts.join('\n')
}

describe('one Needs you block', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('J counts two sources in one block, N = a + b + c', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Two',
      owner_items: [{ name: 'Sign now', now: true }],
      standing_items: [{ name: 'Stand later' }],
    })
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(screen.getAllByRole('region', { name: 'Needs you' })).toHaveLength(1)
    expect(summary.querySelector('[data-can-now]')?.textContent).toBe('1')
    expect(summary.querySelector('[data-not-yet]')?.textContent).toBe('1')
    expect(summary.querySelector('[data-needs-unknown]')).toBeNull()
    const n = Number(summary.querySelector('[data-needs-total]')?.getAttribute('data-needs-total'))
    const a = Number(summary.querySelector('[data-can-now]')?.getAttribute('data-can-now'))
    const b = Number(summary.querySelector('[data-not-yet]')?.getAttribute('data-not-yet'))
    expect(n).toBe(a + b)
    expect(summary.querySelector('[data-needs-total]')?.textContent).toBe(String(n))
    expect(document.getElementById('sec-needs-you')).toBeNull()
  })

  it('J shows owner_all once and ignores the older lists', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Merged',
      owner_all: [{ name: 'Only once', now: true }],
      owner_items: [{ name: 'Older item', now: true }],
      standing_items: [{ name: 'Standing old', now: true }],
      owner_now: [{ name: 'Now old' }],
    })
    render(wrap(<AgentWorkBoard />))
    const block = await screen.findByRole('region', { name: 'Needs you' })
    expect(within(block).getAllByText('Only once')).toHaveLength(1)
    expect(within(block).queryByText('Older item')).toBeNull()
    expect(within(block).queryByText('Standing old')).toBeNull()
    expect(within(block).queryByText('Now old')).toBeNull()
    expect(block.querySelector('[data-needs-total]')?.getAttribute('data-needs-total')).toBe('1')
  })

  it('J keeps every count of him inside the block', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Counts',
      owner_items: [{ name: 'Sign', now: true }],
      roadmap: {
        groups: [{ state: 'doing', items: ['a'], detail: [{ name: 'W', est: { done: 1, total: 2, mine: 1, owner: 2 } }] }],
      },
    })
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(summary.textContent).toMatch(/Later: about 2 more requests to you/)
    const counts = [...summary.querySelectorAll('[data-owner-count]')]
    expect(counts.length).toBeGreaterThan(0)
    expect(counts.every(node => node.closest('#needs-you'))).toBe(true)
    expect(summary.querySelector('[data-summary-block="plan"]')?.textContent).not.toMatch(/times you|\b\d+ times/)
  })

  it('J does not say me or I in console words when the gauge is amber', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Amber',
      owner_items: [{ name: 'Sign', now: true, advice: 'I will email me' }],
      works: [{ title: 'Slow work', steps: [{ name: 'Pace', state: 'slow', who: 'Cursor' }] }],
    })
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(summary.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('amber')
    const written = consoleWords(summary)
    expect(written).not.toMatch(/\bme\b/)
    expect(written).not.toMatch(/(^|\s)I /)
    expect(summary.querySelector('[data-agent-note]')?.textContent).toContain('I will email me')
  })

  it('K labels a request row and a plain row, and says Not written when a field is missing', async () => {
    const later = new Date(NOW + 23 * 60 * 60 * 1000).toISOString()
    stub(
      {
        at: NOW_SEC,
        verdict: 'Act',
        owner_items: [
          {
            name: 'Approve',
            now: true,
            advice: 'ship the branch',
            approval: { id: PENDING_ID },
          },
          {
            name: 'Plain',
            now: true,
            advice: 'read this',
            where: 'the thread',
            reply: 'reply in the thread',
            takes: 'about 10 min',
            deadline_text: 'by Friday',
            blocks_text: 'the merge',
            actions: [{ text: 'Read the note' }],
          },
          { name: 'Bare', now: true },
        ],
      },
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: PENDING_ID, number: 4, summary: 'real', expires_at: later })])),
    )
    render(wrap(<AgentWorkBoard />))
    const block = await screen.findByRole('region', { name: 'Needs you' })
    await within(block).findByRole('link', { name: 'Open request #4' })
    const labels = [...block.querySelectorAll('[data-fact]')].map(node => node.getAttribute('data-fact'))
    expect(labels.slice(0, 7)).toEqual(['Recommended', 'Where', 'How to do it', 'How to reply', 'After', 'Latest', 'If not done'])
    const request = within(block).getByRole('link', { name: 'Open request #4' }).closest('[data-ask]') as HTMLElement
    expect(request.querySelector('[data-fact="Recommended"] [data-agent-note]')?.textContent).toContain('ship the branch')
    expect(within(request).getByText('The request card, here')).toBeTruthy()
    expect(request.querySelector('[data-fact="Where"] [data-agent-note]')).toBeNull()
    expect(request.querySelector('[data-fact="How to do it"]')?.textContent).toContain('Open request #4')
    expect(within(request).getByText('No reply needed — this row closes when the request is decided')).toBeTruthy()
    expect(request.querySelector('[data-fact="After"]')?.textContent).toContain('Unknown')
    expect(request.querySelector('[data-fact="Latest"]')?.textContent).toMatch(/left|past/)
    expect(request.querySelector('[data-fact="If not done"]')?.textContent).toContain('Unknown')
    const plain = [...block.querySelectorAll('[data-ask]')].find(row => row.textContent?.includes('Plain')) as HTMLElement
    expect(plain.querySelector('[data-fact="Recommended"] [data-agent-note]')?.textContent).toContain('read this')
    expect(plain.querySelector('[data-fact="Where"] [data-agent-note]')?.textContent).toContain('the thread')
    expect(within(plain).getByText('Read the note')).toBeTruthy()
    expect(plain.querySelector('[data-fact="How to reply"] [data-agent-note]')?.textContent).toContain('reply in the thread')
    expect(plain.querySelector('[data-fact="After"] [data-agent-note]')?.textContent).toContain('about 10 min')
    expect(plain.querySelector('[data-fact="Latest"] [data-agent-note]')?.textContent).toContain('by Friday')
    expect(plain.querySelector('[data-fact="If not done"] [data-agent-note]')?.textContent).toContain('the merge')
    const bare = [...block.querySelectorAll('[data-ask]')].find(row => row.textContent?.includes('Bare')) as HTMLElement
    expect(bare.querySelector('[data-missing="advice"]')?.textContent).toBe('Not written')
    expect(bare.querySelector('[data-missing="where"]')?.className).toContain('text-warning')
    expect(bare.querySelector('[data-missing="reply"]')?.className).toContain('text-warning')
    expect(bare.querySelector('[data-missing="takes"]')?.textContent).toBe('Not written')
    expect(bare.querySelector('[data-missing="blocks"]')?.textContent).toBe('Not written')
    expect(within(bare).getByText('No deadline')).toBeTruthy()
    expect(block.textContent).toContain('Three places to act: reply in the thread · decide in Needs you · paste a command in your terminal. Each row says which.')
  })

  it('L colours an empty queue green, a turn red, a coordinator wait amber, and a missing list Unknown', async () => {
    stub({ at: NOW_SEC, verdict: 'Green', works: [] })
    const green = render(wrap(<AgentWorkBoard />))
    const greenBox = await screen.findByTestId('progress-summary')
    const greenPill = greenBox.querySelector('[data-run-state]')
    expect(greenPill?.getAttribute('data-run-state')).toBe('green')
    expect(greenPill?.className).toContain('bg-emerald-500')
    expect(greenBox.querySelector('[data-summary-block="running"]')?.textContent).toContain('All done — the queue is empty')
    green.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'Red',
      owner_items: [{ name: 'Sign', now: true }],
      works: [{ title: 'Open', steps: [{ name: 'Wait', state: 'waiting', who: 'you', needs_owner: true }] }],
    })
    const red = render(wrap(<AgentWorkBoard />))
    const redBox = await screen.findByTestId('progress-summary')
    const redPill = redBox.querySelector('[data-run-state]')
    expect(redPill?.getAttribute('data-run-state')).toBe('red')
    expect(redPill?.className).toContain('bg-destructive')
    const redLink = within(redBox).getByRole('link', { name: 'Stopped — waiting on you' })
    expect(redLink.getAttribute('href')).toBe('#needs-you')
    expect(redLink.textContent).not.toMatch(/\d/)
    red.unmount()

    stub({
      at: NOW_SEC,
      verdict: 'Amber',
      works: [
        {
          title: 'Open',
          steps: [
            { name: 'Did', state: 'done', ended: NOW_SEC - 180 },
            { name: 'Wait', state: 'waiting', who: 'Cursor' },
          ],
        },
      ],
    })
    const amber = render(wrap(<AgentWorkBoard />))
    const amberBox = await screen.findByTestId('progress-summary')
    const amberPill = amberBox.querySelector('[data-run-state]')
    expect(amberPill?.getAttribute('data-run-state')).toBe('amber')
    expect(amberPill?.className).toContain('bg-amber-500')
    expect(amberBox.textContent).toContain('Stopped — waiting on the coordinator — nothing for you')
    expect(amberBox.querySelector('[data-idle-for]')?.textContent).toContain('for 3 min')
    expect(amberBox.querySelector('[data-trouble]')?.getAttribute('data-trouble')).toBe('green')
    expect(amberBox.querySelector('[data-trouble-whose]')?.textContent).toBe(
      "1 steps finished, waiting for the coordinator's check — nothing for you",
    )
    amber.unmount()

    stub({ at: NOW_SEC, verdict: 'Missing' })
    render(wrap(<AgentWorkBoard />))
    const unknown = await screen.findByTestId('progress-summary')
    const unknownPill = unknown.querySelector('[data-run-state]')
    expect(unknownPill?.getAttribute('data-run-state')).toBe('unknown')
    expect(unknownPill?.className).toContain('bg-foreground/20')
    expect(unknown.querySelector('[data-summary-block="running"]')?.textContent).toContain('Unknown')
  })

  it('L ticks for N min without repainting the other sub-blocks', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    stub({
      at: NOW_SEC,
      verdict: 'Idle tick',
      works: [
        {
          title: 'Open',
          steps: [
            { name: 'Did', state: 'done', ended: NOW_SEC - 120 },
            { name: 'Wait', state: 'waiting', who: 'Cursor' },
          ],
        },
      ],
    })
    render(
      wrap(
        <StrictMode>
          <AgentWorkBoard />
        </StrictMode>,
      ),
    )
    const summary = await screen.findByTestId('progress-summary')
    expect(summary.querySelector('[data-idle-for]')?.textContent).toContain('for 2 min')
    const block = (name: string) => summary.querySelector(`[data-summary-block="${name}"]`)?.getAttribute('data-summary-renders')
    const before = {
      needs: block('needs-you'),
      plan: block('plan'),
      running: block('running'),
      gauges: block('gauges'),
      idle: summary.querySelector('[data-idle-renders]')?.getAttribute('data-idle-renders'),
    }
    await act(async () => {
      vi.setSystemTime(NOW + 60_000)
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(summary.querySelector('[data-idle-for]')?.textContent).toContain('for 3 min')
    expect(block('needs-you')).toBe(before.needs)
    expect(block('plan')).toBe(before.plan)
    expect(block('running')).toBe(before.running)
    expect(block('gauges')).toBe(before.gauges)
    expect(Number(summary.querySelector('[data-idle-renders]')?.getAttribute('data-idle-renders'))).toBeGreaterThan(Number(before.idle))
  })
})
