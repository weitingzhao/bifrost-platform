import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
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
    expect(await screen.findByText('2 个步骤在做，都正常')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Needs you now' })).toBeTruthy()
    fireEvent.click(screen.getByTestId('fold-sec-coord'))
    expect(screen.getByText(/Coordinator: 三件事在跑：Cursor 做调度脚本第 3 轮和 Progress 第三片/)).toBeTruthy()
    expect(screen.getByText(/Then: 哪件先完先处理哪件/)).toBeTruthy()
    fireEvent.click(screen.getByTestId('fold-sec-quota'))
    const quota = screen.getByRole('region', { name: 'Quota' })
    expect(within(quota).getByText('Cursor · 1 run · 24 min · 365K in · 145K out · 10.1M cached')).toBeTruthy()
    expect(within(quota).getByText('Codex · 5 runs · 383K tokens')).toBeTruthy()
    expect(quota.textContent).not.toContain('23.783')
    expect(quota.textContent).not.toContain('364769')
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
    fireEvent.click(await screen.findByTestId('fold-sec-coord'))
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
    expect(activity.querySelector('details')?.open).toBe(false)
    fireEvent.click(activity.querySelector('summary')!)
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

  it('words activity states and leaves a note as text only', async () => {
    const rows: Array<{ state: string; words: string; text: string; step: string }> = [
      { state: 'gap', words: 'waiting for the coordinator', text: '等统筹线程接手', step: 'Takeover' },
      { state: 'note', words: '', text: 'vitest exit=0', step: 'Log' },
      { state: 'waiting', words: 'not started', text: 'later', step: 'Hold' },
      { state: 'running', words: 'running', text: 'going', step: 'Build' },
      { state: 'slow', words: 'slower than usual', text: 'slowing', step: 'Pace' },
      { state: 'quiet', words: 'may be stuck', text: 'quiet line', step: 'Watch' },
      { state: 'dead', words: 'process gone', text: 'gone', step: 'Exit' },
      { state: 'failed', words: 'failed', text: 'broke', step: 'Check' },
      { state: 'done', words: 'done', text: 'finished', step: 'Ship' },
    ]
    stub({
      at: NOW_SEC,
      verdict: 'Feed',
      feed: rows.map((row, index) => ({
        at: NOW_SEC - index,
        work: 'Board',
        step: row.step,
        who: 'Cursor',
        state: row.state,
        text: row.text,
      })),
    })
    render(wrap(<AgentWorkBoard />))
    const region = await screen.findByRole('region', { name: 'Activity' })
    fireEvent.click(region.querySelector('summary')!)
    const activity = within(region).getByRole('list', { name: 'Activity' })
    const items = within(activity).getAllByRole('listitem')
    expect(items).toHaveLength(rows.length)
    items.forEach((item, index) => {
      const row = rows[index]
      expect(row).toBeTruthy()
      const parts = (item.querySelector('span')?.textContent ?? '').split(' · ').map(part => part.trim())
      expect(parts).not.toContain('gap')
      expect(parts).not.toContain('note')
      expect(item.textContent).toContain(row?.text)
      if (row?.words === '') expect(parts).not.toContain(row.state)
      else expect(parts).toContain(row?.words)
    })
  })
})
