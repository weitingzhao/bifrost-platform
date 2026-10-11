import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatLocalTime } from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import sample from './fixtures/sample-progress-w62.json'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear() {
      mem.clear()
    },
    getItem(key) {
      return mem.has(key) ? (mem.get(key) ?? null) : null
    },
    key(index) {
      return [...mem.keys()][index] ?? null
    },
    removeItem(key) {
      mem.delete(key)
    },
    setItem(key, value) {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function stub(document: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      json({
        document,
        received_at: new Date(NOW).toISOString(),
        age_seconds: 0,
        stale: false,
        host: 'vision-mac',
        generated_at: new Date(NOW).toISOString(),
        generation_lag_seconds: 0,
      }),
    ),
  )
}

describe('progress board parity', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('renders the published sample without throwing', async () => {
    stub(sample)
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('3 个步骤在做，都正常')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Needs you now' })).toBeTruthy()
    expect(screen.getByText(/Coordinator: 正式环境的发布申请 #6/)).toBeTruthy()
    expect(screen.getByText(/Then: 你批了之后我盯发布并核对/)).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Quota' })).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Activity' })).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Done today' })).toBeTruthy()
    expect(screen.getAllByRole('article').length).toBeGreaterThan(0)
  })

  it('omits coordinator, activity, and done today when they are absent', async () => {
    stub({ at: NOW_SEC, verdict: 'Quiet', works: [], feed: [], recent: [] })
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('Quiet')).toBeTruthy()
    expect(screen.queryByText(/^Coordinator:/)).toBeNull()
    expect(screen.queryByRole('region', { name: 'Activity' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Done today' })).toBeNull()
  })

  it('never calls a step that waits for the Owner late', async () => {
    const at = NOW_SEC + 25 * 60
    stub({
      at: NOW_SEC,
      verdict: 'Waiting',
      works: [
        {
          title: 'Release',
          sure: true,
          next: { name: 'Publish', who: 'Cluster' },
          owner: { name: '你批 #6', at },
          steps: [
            {
              name: 'Approve',
              who: '你',
              state: 'running',
              needs_owner: true,
              started: NOW_SEC - 12 * 60,
              usual_min: 30,
              overdue: 360,
              eta_end: NOW_SEC - 60,
              waits_for: '你批 #6',
            },
          ],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const card = await screen.findByRole('listitem', { name: 'Approve' })
    expect(within(card).getByText('waiting 12m')).toBeTruthy()
    expect(within(card).getByText('waits for: 你批 #6')).toBeTruthy()
    expect(within(card).queryByText(/late/)).toBeNull()
    expect(screen.queryByText(/late/)).toBeNull()
    expect(screen.getByText('Now: Approve · 你')).toBeTruthy()
    expect(screen.getByText('Next: Publish · Cluster')).toBeTruthy()
    expect(
      screen.getByText(`Your turn: 你批 #6 · expected about ${formatLocalTime(at)} (in 25m)`, { exact: false }),
    ).toBeTruthy()
  })

  it('says your turn cannot be estimated when the work is not sure', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Unsure',
      works: [
        {
          title: 'Release',
          sure: false,
          owner: { name: '你批 #6', at: NOW_SEC + 600 },
          steps: [{ name: 'Approve', who: '你', state: 'running', needs_owner: true, started: NOW_SEC - 60 }],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('Your turn: 你批 #6 · cannot be estimated')).toBeTruthy()
    expect(screen.queryByText(/expected about/)).toBeNull()
  })

  it('marks a coordinator update older than 20 minutes as stale', async () => {
    stub({
      at: NOW_SEC,
      verdict: 'Going',
      coordinator: { at: NOW_SEC - 25 * 60, doing: '在等你', next: '然后核对' },
    })
    render(wrap(<AgentWorkBoard />))
    const stale = await screen.findByText(/updated 25m ago — may be stale/)
    expect(stale.getAttribute('data-stale')).toBe('true')
    expect(stale.className).toContain('text-destructive')
  })

  it('keeps at most 25 activity rows and collapses them on a 360px screen', async () => {
    const feed = Array.from({ length: 30 }, (_, index) => ({
      at: NOW_SEC - index * 60,
      work: 'Board',
      step: 'Implement',
      who: 'Cursor',
      state: 'running',
      text: `row ${index}`,
    }))
    stub({ at: NOW_SEC, verdict: 'Feed', feed, recent: ['shipped the board'] })
    const wide = render(wrap(<AgentWorkBoard />))
    const activity = await screen.findByRole('region', { name: 'Activity' })
    expect(activity.querySelector('details')?.open).toBe(true)
    expect(within(activity).getAllByRole('listitem')).toHaveLength(25)
    expect(within(activity).getByText(/row 0/)).toBeTruthy()
    expect(within(activity).queryByText(/row 29/)).toBeNull()
    wide.unmount()

    window.matchMedia = ((query: string) => ({
      matches: query.includes('360px'),
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
      onchange: null,
    })) as typeof window.matchMedia
    render(wrap(<AgentWorkBoard />))
    const narrow = await screen.findByRole('region', { name: 'Activity' })
    expect(narrow.querySelector('details')?.open).toBe(false)
    expect(screen.getByRole('region', { name: 'Done today' }).querySelector('details')?.open).toBe(false)
  })
})
