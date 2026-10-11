import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => null }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => null }))
import {
  AGENT_PROGRESS_POLL_MS,
  expectationLine,
  formatLocalTime,
  staleSentence,
  waitingLine,
  type AgentProgressResponse,
  type ProgressDocument,
  type ProgressStep,
} from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import { ProgressPage } from '@/pages/shell/progress/ProgressPage'

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

function wrap(children: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function step(over: Partial<ProgressStep> = {}): ProgressStep {
  return {
    name: 'Implement',
    who: 'Cursor',
    state: 'running',
    started: NOW_SEC - 90,
    ended: 0,
    last: NOW_SEC - 30,
    usual_min: 15,
    eta_start: 0,
    eta_end: NOW_SEC + 600,
    overdue: 0,
    note: '',
    doing: 'editing the handler',
    needs_owner: false,
    ...over,
  }
}

function envelope(document: ProgressDocument | null, over: Partial<AgentProgressResponse> = {}): AgentProgressResponse {
  return {
    document,
    received_at: new Date(NOW).toISOString(),
    age_seconds: 0,
    stale: false,
    host: 'vision-mac',
    ...over,
  }
}

function baseDoc(over: Partial<ProgressDocument> = {}): ProgressDocument {
  return {
    at: NOW_SEC,
    verdict: 'All quiet',
    level: 'run',
    coordinator: { at: NOW_SEC - 120, state: 'watching', note: 'all threads reporting' },
    owner_now: [],
    owner_next: null,
    works: [],
    feed: [],
    recent: [],
    ...over,
  }
}

function stubProgress(body: unknown, status = 200) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/v1/agent/progress')) return json(body, status)
      if (url.includes('/api/v1/lineage')) {
        return json({
          generated_at: '2026-10-10T15:00:00Z',
          since: '2026-09-01T00:00:00Z',
          days: 14,
          reachability: 'ok',
          threads: [],
          coverage: [],
          releases: [],
          errors: [],
          branches: [],
        })
      }
      if (url.includes('/api/v1/code-health')) return json({ error: 'none' }, 500)
      return json({})
    }),
  )
}

describe('agent progress format', () => {
  it('ticks a waiting duration from the timestamps and polls every 15s', () => {
    expect(AGENT_PROGRESS_POLL_MS).toBe(15_000)
    expect(waitingLine(100, 100_000 + 125_000)).toBe('waiting 2m')
    expect(waitingLine(100, 100_000 + 185_000)).toBe('waiting 3m')
    expect(staleSentence(360)).toBe(
      'Progress was last published 6m ago. The coordinating thread may be offline.',
    )
  })
})

describe('AgentWorkBoard', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('says when nothing has been published', async () => {
    stubProgress(envelope(null))
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('No progress has been published.')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Last known' })).toBeNull()
    expect(screen.queryByText('All quiet')).toBeNull()
  })

  it('does not present a stale verdict as current', async () => {
    stubProgress(
      envelope(baseDoc(), {
        stale: true,
        age_seconds: 360,
        received_at: new Date(NOW - 6 * 60_000).toISOString(),
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const warning = await screen.findByText(
      'Progress was last published 6m ago. The coordinating thread may be offline.',
    )
    const known = screen.getByRole('region', { name: 'Last known' })
    expect(known.className).toContain('opacity-60')
    expect(within(known).getByText('All quiet')).toBeTruthy()
    expect(known.contains(warning)).toBe(false)
  })

  it('says unknown when the read fails', async () => {
    stubProgress({ error: 'down' }, 500)
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('Progress: Unknown — Agent progress: down')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Last known' })).toBeNull()
    expect(screen.queryByText('No progress has been published.')).toBeNull()
  })

  it('dims the last document when a later read fails', async () => {
    let fail = false
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => (fail ? json({ error: 'boom' }, 500) : json(envelope(baseDoc())))),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(wrap(<AgentWorkBoard />, client))
    expect(await screen.findByText('All quiet')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Last known' })).toBeNull()
    fail = true
    await client.refetchQueries({ queryKey: ['agent-progress'] })
    expect(await screen.findByText('Progress: Unknown — Agent progress: boom')).toBeTruthy()
    const known = screen.getByRole('region', { name: 'Last known' })
    expect(within(known).getByText('All quiet')).toBeTruthy()
    expect(known.className).toContain('opacity-60')
  })

  it('shows owner_now cards with waiting time, struck done items, and a new-tab link', async () => {
    const since = NOW_SEC - 125
    stubProgress(
      envelope(
        baseDoc({
          owner_now: [
            {
              name: 'Approve the release',
              urgency: 'Blocking the rollout',
              since,
              actions: [
                { text: 'Read the note', done: true },
                { text: 'Open the brief', done: false, link: 'https://example.test/brief', link_text: 'Open the brief' },
              ],
            },
          ],
          owner_next: { name: 'Later', at: NOW_SEC + 3600, work: 'W-62' },
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Next time you are needed' })
    expect(within(needed).getByText('Approve the release')).toBeTruthy()
    expect(within(needed).getByText('Blocking the rollout')).toBeTruthy()
    expect(within(needed).getByText('waiting 2m')).toBeTruthy()
    expect(within(needed).getByText('Read the note').closest('li')?.className).toContain('line-through')
    const link = within(needed).getByRole('link', { name: 'Open the brief' })
    expect(link.getAttribute('href')).toBe('https://example.test/brief')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(within(needed).queryByText(/Later · expected around/)).toBeNull()
    expect(within(needed).queryByText('Nothing is waiting for you.')).toBeNull()
  })

  it('shows owner_next when nothing is waiting now', async () => {
    const at = NOW_SEC + 7200
    stubProgress(envelope(baseDoc({ owner_next: { name: 'Ship', at, work: 'W-62' } })))
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Next time you are needed' })
    expect(within(needed).getByText(`Ship · expected around ${formatLocalTime(at)}`)).toBeTruthy()
    expect(within(needed).queryByText('Nothing is waiting for you.')).toBeNull()
  })

  it('renders every step state in plain English', async () => {
    const states: Array<[string, string, boolean]> = [
      ['Wait', 'waiting', false],
      ['Gap', 'gap', false],
      ['Run', 'running', false],
      ['Slow', 'slow', false],
      ['Quiet', 'quiet', false],
      ['Dead', 'dead', false],
      ['Done', 'done', false],
      ['Fail', 'failed', false],
      ['Yours', 'running', true],
    ]
    const labels = [
      'waiting for the coordinator',
      'not started',
      'running',
      'slower than usual',
      'may be stuck',
      'process gone',
      'done',
      'failed',
      'waiting for you',
    ]
    stubProgress(
      envelope(
        baseDoc({
          works: [
            {
              title: 'W-62',
              goal: 'Show the board',
              round: 2,
              risk: 'one more round',
              next: { name: 'Review', who: 'Owner', at: NOW_SEC + 3600, needs_owner: true },
              steps: states.map(([name, state, needs]) =>
                step({ name, state, needs_owner: needs, who: 'Cursor', doing: name === 'Run' ? 'editing the handler' : '' }),
              ),
            },
          ],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const work = await screen.findByRole('article')
    expect(within(work).getByText('W-62')).toBeTruthy()
    expect(within(work).getByText('Show the board')).toBeTruthy()
    expect(within(work).getByText('Round 2')).toBeTruthy()
    expect(within(work).getByText('Risk: one more round')).toBeTruthy()
    expect(within(work).getByText(`Next: Review · expected ${formatLocalTime(NOW_SEC + 3600)}`)).toBeTruthy()
    expect(within(work).getByRole('list', { name: 'Steps' }).className).toContain('flex-wrap')
    states.forEach(([name], index) => {
      const card = within(work).getByRole('listitem', { name })
      expect(within(card).getByText(labels[index])).toBeTruthy()
      expect(within(card).getByText('Cursor')).toBeTruthy()
      expect(within(card).queryByRole('img')).toBeNull()
    })
    const running = within(work).getByRole('listitem', { name: 'Run' })
    expect(within(running).getByText('elapsed 1m of usual 15 min')).toBeTruthy()
    expect(within(running).getByText('last output 30s ago')).toBeTruthy()
    expect(within(running).getByText(expectationLine(step())!)).toBeTruthy()
    expect(within(work).getByText('Run: editing the handler')).toBeTruthy()
    expect(screen.getByText(/watching — all threads reporting · 2m ago/)).toBeTruthy()
  })

  it('shows a late step as late rather than expected', async () => {
    stubProgress(
      envelope(
        baseDoc({
          works: [
            {
              title: 'Late work',
              steps: [step({ name: 'Late step', state: 'slow', overdue: 125, doing: '', note: '' })],
            },
          ],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const card = await screen.findByRole('listitem', { name: 'Late step' })
    expect(within(card).getByText('late by 2m')).toBeTruthy()
    expect(within(card).getByText('slower than usual')).toBeTruthy()
    expect(within(card).queryByText(/^expected /)).toBeNull()
  })

  it('shows a Chinese document as it arrived, with English chrome', async () => {
    stubProgress(
      envelope(
        baseDoc({
          verdict: '三件事在推进，都正常',
          level: 'run',
          owner_now: [
            {
              name: '批准发布',
              urgency: '挡住了发布',
              since: NOW_SEC - 125,
              actions: [{ text: '看一眼说明', done: false }],
            },
          ],
          works: [
            {
              title: '协作进展',
              goal: '手机上也能看见进展',
              round: 1,
              steps: [step({ name: '实现接口', who: 'Cursor', state: 'running', doing: '在写页面' })],
            },
          ],
          recent: ['刚做完验收'],
          feed: [
            { at: NOW_SEC - 10, work: '协作进展', step: '实现接口', who: 'Cursor', state: 'running', text: '旧的一句' },
            { at: NOW_SEC, work: '协作进展', step: '实现接口', who: 'Cursor', state: 'running', text: '正在写页面' },
          ],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('三件事在推进，都正常')).toBeTruthy()
    expect(screen.getByText('批准发布')).toBeTruthy()
    expect(screen.getByText('挡住了发布')).toBeTruthy()
    expect(screen.getByText('看一眼说明')).toBeTruthy()
    expect(screen.getByText('协作进展')).toBeTruthy()
    expect(screen.getByText('手机上也能看见进展')).toBeTruthy()
    expect(screen.getByText('实现接口')).toBeTruthy()
    expect(screen.getByText('running')).toBeTruthy()
    expect(screen.getByText('实现接口: 在写页面')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Next time you are needed' })).toBeTruthy()
    const feed = screen.getByRole('list', { name: 'Feed' })
    const items = within(feed).getAllByRole('listitem')
    expect(items[0]?.textContent).toContain('正在写页面')
    expect(items[1]?.textContent).toContain('旧的一句')
    expect(screen.getByText('刚做完验收')).toBeTruthy()
  })

  it('places Agent work above commit lineage and code health', async () => {
    stubProgress(envelope(null))
    render(wrap(<ProgressPage />))
    expect(await screen.findByText('No progress has been published.')).toBeTruthy()
    const agent = screen.getByRole('region', { name: 'Agent work' })
    const lineage = screen.getByRole('region', { name: 'Commit lineage' })
    const health = screen.getByRole('region', { name: 'Code health' })
    expect(agent.compareDocumentPosition(lineage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(lineage.compareDocumentPosition(health) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(agent.className).toContain('overflow-x-hidden')
  })
})
