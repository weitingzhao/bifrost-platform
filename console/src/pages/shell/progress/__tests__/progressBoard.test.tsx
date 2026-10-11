import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { expectationLine, type ProgressDocument } from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import { resetsIn } from '@/pages/shell/progress/progressModel'
import sample from './fixtures/sample-progress.json'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
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

function stubProgress(document: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => json(envelope(document))),
  )
}

function doc(over: Partial<ProgressDocument> = {}): ProgressDocument {
  return {
    at: NOW_SEC,
    verdict: 'All quiet',
    level: 'idle',
    works: [],
    ...over,
  }
}

describe('Progress board blocks', () => {
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
    stubProgress(sample)
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('现在没有后台步骤在跑')).toBeTruthy()
    expect(screen.getByText('All done — the queue is empty')).toBeTruthy()
    fireEvent.click(screen.getByTestId('fold-sec-plan'))
    const plan = screen.getByRole('region', { name: 'Plan' })
    expect(within(plan).getByText('you are here')).toBeTruthy()
    expect(within(plan).getByText('14 shipped · 7 in progress · 11 not started')).toBeTruthy()
    expect(within(plan).getByText('已上线')).toBeTruthy()
    fireEvent.click(screen.getByTestId('fold-plan-exit'))
    expect(within(plan).getByText('You sign')).toBeTruthy()
    fireEvent.click(screen.getByTestId('fold-plan-basis'))
    expect(within(plan).getByText('Generated from 工作台账（WORK.md）')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Usage today' })).toBeNull()
    expect(screen.queryByRole('article')).toBeNull()
    fireEvent.click(screen.getByText(`Finished · ${sample.works.length}`))
    fireEvent.click(screen.getByText(/进展看板搬上 Ops Console/))
    expect(screen.getByText('at the same time')).toBeTruthy()
    fireEvent.click(screen.getByText(/正式环境自动发布：前两个门槛/))
    fireEvent.click(screen.getAllByText('What it did last')[0]!)
    expect(screen.getByText('ok · 0.3 s')).toBeTruthy()
    expect(screen.getByText('+85 −0')).toBeTruthy()
  })

  it('says what is next when nothing is running, and stays quiet while a step runs', async () => {
    stubProgress(
      doc({
        queue: [{ title: 'Review the plan', why_waiting: 'waiting until morning' }],
      }),
    )
    const { unmount } = render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('All done — the queue is empty')).toBeTruthy()
    expect(screen.queryByText(/Next: Review the plan/)).toBeNull()
    unmount()

    stubProgress(doc({ queue: [] }))
    const empty = render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('All done — the queue is empty')).toBeTruthy()
    empty.unmount()

    stubProgress(doc())
    const absent = render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('All done — the queue is empty')).toBeTruthy()
    absent.unmount()

    stubProgress(
      doc({
        queue: [{ title: 'Review the plan', why_waiting: 'waiting until morning' }],
        works: [{ title: 'Live', steps: [{ name: 'Go', state: 'running', who: 'Cursor' }] }],
      }),
    )
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByRole('article')).toBeTruthy()
    expect(screen.queryByText(/Next: Review the plan/)).toBeNull()
    expect(screen.queryByText('Nothing is queued.')).toBeNull()
  })

  it('omits an absent plan and usage, and shows the empty ones', async () => {
    stubProgress(doc())
    const { unmount } = render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('All quiet')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Plan' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Usage today' })).toBeNull()
    unmount()

    stubProgress(doc({ roadmap: {}, usage: {} }))
    render(wrap(<AgentWorkBoard />))
    fireEvent.click(await screen.findByTestId('fold-sec-plan'))
    const plan = screen.getByRole('region', { name: 'Plan' })
    expect(within(plan).getByText('0 shipped · 0 in progress · 0 not started')).toBeTruthy()
    expect(within(plan).queryByText(/Generated from/)).toBeNull()
    expect(within(plan).queryByText('you are here')).toBeNull()
    fireEvent.click(screen.getByTestId('fold-sec-quota'))
    expect(screen.getByText('No usage reported.')).toBeTruthy()
  })

  it('highlights a window at 75% or more and says when it resets', async () => {
    expect(resetsIn(NOW_SEC + 31 * 3600, NOW)).toBe('resets in 1d 7h')
    stubProgress(
      doc({
        usage: {
          cursor: { runs: 3, min: 40, in: 1000, out: 200, cache: 50 },
          codex: { runs: 1, tokens: 800 },
          claude: {
            read_at: NOW_SEC,
            windows: [
              { label: '5-hour', used: 0.8, resets_at: NOW_SEC + 31 * 3600 },
              { label: 'weekly', used: 0.2, resets_at: NOW_SEC + 3600 },
            ],
          },
        },
      }),
    )
    render(wrap(<AgentWorkBoard />))
    fireEvent.click(await screen.findByTestId('fold-sec-quota'))
    const usage = screen.getByRole('region', { name: 'Usage today' })
    expect(within(usage).getByText('Cursor · 3 runs · 40 min · in 1000 · out 200 · cache 50')).toBeTruthy()
    expect(within(usage).getByText('Codex · 1 run · 800 tokens')).toBeTruthy()
    const hot = within(usage).getByText(/5-hour/).closest('[data-usage-hot]')
    const cool = within(usage).getByText(/weekly/).closest('[data-usage-hot]')
    expect(hot?.getAttribute('data-usage-hot')).toBe('true')
    expect(cool?.getAttribute('data-usage-hot')).toBe('false')
    expect(within(usage).getByText('resets in 1d 7h')).toBeTruthy()
    expect(within(usage).getByText('resets in 1h')).toBeTruthy()
  })

  it('lists what is running, scrolls to the work, and shows live events newest first', async () => {
    const scroll = vi.fn()
    HTMLElement.prototype.scrollIntoView = scroll
    stubProgress(
      doc({
        works: [
          {
            title: 'Ship the board',
            short: 'Board',
            steps: [
              {
                name: 'Implement',
                who: 'Cursor',
                state: 'slow',
                overdue: 125,
                doing: 'editing the handler',
                events: [
                  { at: NOW_SEC - 30, kind: 'say', text: 'older' },
                  { at: NOW_SEC, kind: 'run', text: 'echo hi', exit: 0, ms: 1700, out: 'first\nhi' },
                  { at: NOW_SEC - 10, kind: 'edit', text: 'console/src/a.ts', added: 2, removed: 1 },
                  { at: NOW_SEC - 20, kind: 'search', text: 'queue' },
                ],
              },
            ],
          },
        ],
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const row = await screen.findByRole('button', { name: /Board · Implement · Cursor · echo hi · late by 2m/ })
    fireEvent.click(row)
    expect(scroll).toHaveBeenCalled()
    const live = screen.getByRole('region', { name: 'Live Implement' })
    const items = within(live).getAllByRole('listitem')
    expect(items.map(item => item.getAttribute('data-kind'))).toEqual(['run', 'edit', 'search', 'say'])
    expect(within(live).getByText('echo hi').tagName).toBe('CODE')
    expect(within(live).getByText('ok · 1.7 s')).toBeTruthy()
    expect(within(live).getByText('hi')).toBeTruthy()
    expect(within(live).getByText('+2 −1')).toBeTruthy()
    const card = screen.getByRole('listitem', { name: 'Implement' })
    expect(within(card).getByText(expectationLine({ overdue: 125 })!)).toBeTruthy()
    expect(within(card).queryByText(/^expected /)).toBeNull()
  })

  it('stacks parallel steps and puts finished work below the active cards', async () => {
    stubProgress(
      doc({
        works: [
          { title: 'Waiting', steps: [{ name: 'Later', state: 'gap', who: 'Cursor' }] },
          {
            title: 'Running',
            steps: [
              { name: 'A', state: 'running', parallel: true, who: 'Cursor' },
              { name: 'B', state: 'running', parallel: true, who: 'Codex' },
            ],
          },
          { title: 'Owner', steps: [{ name: 'Decide', state: 'waiting', who: 'You', needs_owner: true }] },
          {
            title: 'Already shipped',
            steps: [{ name: 'Old', state: 'done', who: 'Cursor', started: NOW_SEC - 120, ended: NOW_SEC - 60 }],
          },
        ],
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const articles = await screen.findAllByRole('article')
    expect(articles.map(article => article.querySelector('h3')?.textContent)).toEqual(['Owner', 'Running', 'Waiting'])
    const box = screen.getByText('at the same time').closest('li')
    expect(box?.className).toContain('border-dashed')
    expect(box?.className).toContain('flex-col')
    const inner = box?.querySelector('ul')
    expect(inner?.className).toContain('flex-col')
    expect(inner?.className).not.toContain('flex-wrap')
    expect(within(box as HTMLElement).getByRole('listitem', { name: 'A' })).toBeTruthy()
    expect(within(box as HTMLElement).getByRole('listitem', { name: 'B' })).toBeTruthy()
    expect(screen.queryByRole('article', { name: 'Already shipped' })).toBeNull()
    expect(screen.queryByText('Already shipped')).toBeNull()
    fireEvent.click(screen.getByText('Finished · 1'))
    fireEvent.click(screen.getByText(/Already shipped/))
    const old = screen.getByRole('listitem', { name: 'Old' })
    expect(within(old).getByText('took 1m')).toBeTruthy()
    expect(within(old).queryByText(/^expected /)).toBeNull()
  })

  it('ticks a reset time without repainting the board', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    stubProgress(
      doc({
        usage: {
          claude: { windows: [{ label: '5-hour', used: 0.8, resets_at: NOW_SEC + 31 * 3600 }] },
        },
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const board = await screen.findByTestId('agent-work-document')
    fireEvent.click(screen.getByTestId('fold-sec-quota'))
    expect(screen.getByText('resets in 1d 7h')).toBeTruthy()
    const paints = board.getAttribute('data-paints')
    await act(async () => {
      vi.setSystemTime(NOW + 3600_000 - 1000)
      vi.advanceTimersByTime(1000)
    })
    expect(board.getAttribute('data-paints')).toBe(paints)
    expect(screen.getByText('resets in 1d 6h')).toBeTruthy()
    expect(screen.queryByText('resets in 1d 7h')).toBeNull()
  })
})
