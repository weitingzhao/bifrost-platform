import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
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
    generated_at: new Date(NOW).toISOString(),
    generation_lag_seconds: 0,
    ...over,
  }
}

function clockTimers(): void {
  vi.useRealTimers()
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date(NOW))
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
                { text: 'Open the brief', done: false, link: 'https://ops.bifrost.lan/brief', link_text: 'Open the brief' },
              ],
            },
          ],
          owner_next: { name: 'Later', at: NOW_SEC + 3600, work: 'W-62' },
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Needs you now' })
    expect(within(needed).getByText('Approve the release')).toBeTruthy()
    expect(within(needed).getByText('Blocking the rollout')).toBeTruthy()
    expect(within(needed).getByText('waiting 2m')).toBeTruthy()
    expect(within(needed).getByText('Read the note').closest('li')?.className).toContain('line-through')
    const link = within(needed).getByRole('link', { name: 'Open the brief (ops.bifrost.lan)' })
    expect(link.getAttribute('href')).toBe('https://ops.bifrost.lan/brief')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
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
      'not started',
      'waiting for the coordinator',
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
                step({
                  name,
                  state,
                  needs_owner: needs,
                  who: 'Cursor',
                  doing: name === 'Run' ? 'editing the handler' : '',
                  ...(name === 'Done' ? { started: NOW_SEC - 90, ended: NOW_SEC - 30 } : {}),
                }),
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
    expect(within(work).getByText('Next: Review · Owner — this one needs you')).toBeTruthy()
    expect(within(work).getByRole('list', { name: 'Steps' }).className).toContain('flex-wrap')
    states.forEach(([name], index) => {
      if (name === 'Done') return
      const card = within(work).getByRole('listitem', { name })
      expect(within(card).getByText(labels[index])).toBeTruthy()
      expect(within(card).getByText('Cursor')).toBeTruthy()
      expect(within(card).queryByRole('img')).toBeNull()
    })
    fireEvent.click(within(work).getByText('1 earlier step done'))
    const done = within(work).getByRole('listitem', { name: 'Done' })
    expect(within(done).getByText('done')).toBeTruthy()
    expect(within(done).getByText('took 1m')).toBeTruthy()
    expect(within(done).queryByText(/^expected /)).toBeNull()
    expect(within(done).queryByText(/elapsed/)).toBeNull()
    expect(within(done).queryByRole('img')).toBeNull()
    expect(within(done).getByText('What it did last')).toBeTruthy()
    const running = within(work).getByRole('listitem', { name: 'Run' })
    expect(within(running).getByText('1m of the usual 15')).toBeTruthy()
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
    expect(screen.getByRole('region', { name: 'Needs you now' })).toBeTruthy()
    const feed = screen.getByRole('list', { name: 'Activity' })
    const items = within(feed).getAllByRole('listitem')
    expect(items[0]?.textContent).toContain('正在写页面')
    expect(items[1]?.textContent).toContain('旧的一句')
    expect(screen.getByText('刚做完验收')).toBeTruthy()
  })

  it('shows a trusted destination and does not open an outside link', async () => {
    stubProgress(
      envelope(
        baseDoc({
          owner_now: [
            {
              name: 'Needs you',
              since: NOW_SEC - 10,
              actions: [
                { text: 'Open Needs you', link: 'https://ops.bifrost.lan/needs', link_text: 'Open Needs you' },
                { text: 'Open Needs you', link: 'http://ops.bifrost.lan/needs' },
                { text: 'Jump', link: '#needs-you', link_text: 'Jump' },
                { text: 'Read the note', link: 'https://evil.example/phish', link_text: 'Approve the release' },
                { text: 'Stay', link: 'https://ops.bifrost.lan.evil.com/x' },
                { text: 'Script', link: 'javascript:alert(1)' },
                { text: 'Default port', link: 'https://ops.bifrost.lan:443/ok', link_text: 'Default port' },
                { text: 'Plain http', link: 'http://ops.bifrost.lan:80/ok', link_text: 'Plain http' },
                { text: 'Other port', link: 'https://ops.bifrost.lan:8443/' },
              ],
            },
          ],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Needs you now' })
    const open = within(needed).getAllByRole('link', { name: 'Open Needs you (ops.bifrost.lan)' })
    expect(open.map(link => link.getAttribute('href')).sort()).toEqual([
      'http://ops.bifrost.lan/needs',
      'https://ops.bifrost.lan/needs',
    ])
    open.forEach(link => {
      expect(link.getAttribute('target')).toBe('_blank')
      expect(link.getAttribute('rel')).toBe('noopener noreferrer')
    })
    const hash = within(needed).getByRole('link', { name: 'Jump (this console)' })
    expect(hash.getAttribute('href')).toBe('#needs-you')
    expect(hash.getAttribute('target')).toBeNull()
    expect(hash.getAttribute('rel')).toBe('noopener noreferrer')
    expect(within(needed).queryByRole('link', { name: /Approve the release/ })).toBeNull()
    expect(within(needed).queryByText('Approve the release')).toBeNull()
    expect(within(needed).getByText('Read the note')).toBeTruthy()
    expect(within(needed).getByText('link not shown: evil.example')).toBeTruthy()
    expect(within(needed).getByText('link not shown: ops.bifrost.lan.evil.com')).toBeTruthy()
    expect(within(needed).getByText('Script')).toBeTruthy()
    expect(within(needed).getByText('link not shown')).toBeTruthy()
    expect(within(needed).queryByRole('link', { name: /Script/ })).toBeNull()
    const details = within(needed).getByText('3 more').closest('details')
    expect(details?.open).toBe(false)
    if (details != null) details.open = true
    const def = within(needed).getByRole('link', { name: 'Default port (ops.bifrost.lan)' })
    expect(def.getAttribute('href')).toBe('https://ops.bifrost.lan/ok')
    expect(def.getAttribute('href')).not.toContain(':443')
    const plain = within(needed).getByRole('link', { name: 'Plain http (ops.bifrost.lan)' })
    expect(plain.getAttribute('href')).toBe('http://ops.bifrost.lan/ok')
    expect(within(needed).queryByRole('link', { name: /Other port/ })).toBeNull()
    expect(within(needed).getByText('Other port')).toBeTruthy()
    expect(within(needed).getByText('link not shown: ops.bifrost.lan:8443')).toBeTruthy()
  })

  it('bounds an action link label when the text is only control characters', async () => {
    const controls = '\u0000\u0007\u061C\u200E\u200F\u202E\uFEFF'
    const hash = `#${'a'.repeat(4998)}\u200F`
    const hidden = `#${'\u200F'.repeat(4999)}`
    const trusted = `https://ops.bifrost.lan/${'b'.repeat(5000)}`
    stubProgress(
      envelope(
        baseDoc({
          owner_now: [
            {
              name: 'Needs you',
              since: NOW_SEC - 10,
              actions: [
                { text: controls, link_text: controls, link: hash },
                { text: controls, link_text: controls, link: hidden },
                { link: trusted },
                { text: 'Open the brief', link: 'https://ops.bifrost.lan/brief', link_text: 'Open the brief' },
              ],
            },
          ],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Needs you now' })
    const bounded = within(needed).getByRole('link', { name: /^#a+…$/ })
    expect(bounded.getAttribute('href')).toBe(hash)
    expect(bounded.textContent?.includes('\u200F')).toBe(false)
    expect(bounded.textContent?.length).toBeLessThanOrEqual(200)
    const open = within(needed).getByRole('link', { name: 'Open link' })
    expect(open.getAttribute('href')).toBe(hidden)
    const longUrl = within(needed).getByRole('link', { name: /^https:\/\/ops\.bifrost\.lan\/b+…$/ })
    expect(longUrl.getAttribute('href')).toBe(trusted)
    expect(longUrl.textContent?.length).toBeLessThanOrEqual(200)
    expect(longUrl.getAttribute('href')).not.toBe(longUrl.textContent)
    expect(within(needed).getByRole('link', { name: 'Open the brief (ops.bifrost.lan)' }).getAttribute('href')).toBe(
      'https://ops.bifrost.lan/brief',
    )
  })

  it('shows unknown time when a timestamp cannot be a date', async () => {
    stubProgress(envelope(baseDoc({ owner_next: { name: 'Ship', at: 1e16, work: 'W-62' } })))
    render(wrap(<AgentWorkBoard />))
    const needed = await screen.findByRole('region', { name: 'Next time you are needed' })
    expect(within(needed).getByText('Ship · expected around unknown time')).toBeTruthy()
    expect(within(needed).queryByText(/Invalid Date/)).toBeNull()
  })

  it('renders a document the server would have rejected, dropping null entries', async () => {
    stubProgress({
      document: {
        at: 1,
        verdict: 'Still here',
        owner_now: [null, { name: 'Keep', actions: [null, { text: 'Go', done: false }] }],
        works: [null, { title: 'One', risk: null, round: null, steps: [null, { name: 'Step', state: 'running', who: 'Cursor', started: null }] }],
        needs_you: [null, 'you'],
        recent: [1, 'line'],
        feed: [null, { text: 'hi', at: NOW_SEC }],
        owner_next: null,
        coordinator: null,
      },
      received_at: new Date(NOW).toISOString(),
      age_seconds: 0,
      stale: false,
      host: 'vision-mac',
    })
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('Still here')).toBeTruthy()
    expect(screen.getByText('Keep')).toBeTruthy()
    expect(screen.getByText('Go')).toBeTruthy()
    expect(screen.getByText('One')).toBeTruthy()
    expect(screen.getByText('Step')).toBeTruthy()
    expect(screen.getByText('line')).toBeTruthy()
    expect(screen.getByText(/hi/)).toBeTruthy()
  })

  it('shows computed age beside the verdict', async () => {
    stubProgress(envelope(baseDoc({ at: NOW_SEC - 90, verdict: 'All quiet' })))
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('computed 1m ago')).toBeTruthy()
    expect(screen.getByText('All quiet')).toBeTruthy()
  })

  it('dims a document whose at is much older than when it was published', async () => {
    stubProgress(envelope(baseDoc({ at: NOW_SEC - 180 })))
    render(wrap(<AgentWorkBoard />))
    const warning = await screen.findByText(
      'The published progress was computed 3m ago; it may not reflect the present.',
    )
    const known = screen.getByRole('region', { name: 'Last known' })
    expect(known.className).toContain('opacity-60')
    expect(within(known).getByText('All quiet')).toBeTruthy()
    expect(known.contains(warning)).toBe(false)
  })

  it('dims a document dated more than two minutes in the future', async () => {
    stubProgress(envelope(baseDoc({ at: NOW_SEC + 180 })))
    render(wrap(<AgentWorkBoard />))
    expect(
      await screen.findByText('The published progress was computed 3m ago; it may not reflect the present.'),
    ).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Last known' })).toBeTruthy()
  })

  it('marks the page stale from received_at as the clock moves', async () => {
    clockTimers()
    const received = NOW - 4 * 60_000
    stubProgress(
      envelope(baseDoc({ at: received / 1000, coordinator: { at: received / 1000, state: 'watching', note: 'quiet' } }), {
        stale: false,
        age_seconds: 240,
        received_at: new Date(received).toISOString(),
        generated_at: new Date(received).toISOString(),
        generation_lag_seconds: 0,
      }),
    )
    render(wrap(<AgentWorkBoard />))
    expect(await screen.findByText('All quiet')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Last known' })).toBeNull()
    await act(async () => {
      vi.setSystemTime(NOW + 2 * 60_000)
      vi.advanceTimersByTime(1000)
    })
    const warning = screen.getByText('Progress was last published 6m ago. The coordinating thread may be offline.')
    const known = screen.getByRole('region', { name: 'Last known' })
    expect(known.className).toContain('opacity-60')
    expect(known.contains(warning)).toBe(false)
  })

  it('ticks a waiting duration without repainting the board', async () => {
    clockTimers()
    stubProgress(
      envelope(
        baseDoc({
          owner_now: [{ name: 'Approve', since: NOW_SEC - 125, actions: [] }],
        }),
      ),
    )
    render(wrap(<AgentWorkBoard />))
    const doc = await screen.findByTestId('agent-work-document')
    expect(screen.getByText('waiting 2m')).toBeTruthy()
    const paints = doc.getAttribute('data-paints')
    await act(async () => {
      vi.setSystemTime(NOW + 60_000)
      vi.advanceTimersByTime(1000)
    })
    expect(doc.getAttribute('data-paints')).toBe(paints)
    expect(screen.getByText('waiting 3m')).toBeTruthy()
    expect(screen.queryByText('waiting 2m')).toBeNull()
  })

  it(
    'keeps the last document when the next read times out',
    async () => {
      let calls = 0
      vi.stubGlobal(
        'fetch',
        vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
          calls += 1
          if (calls === 1) return Promise.resolve(json(envelope(baseDoc())))
          return new Promise((_resolve, reject) => {
            const signal = init?.signal
            if (signal == null) {
              reject(new Error('missing signal'))
              return
            }
            const abort = () => reject(new DOMException('The operation was aborted', 'AbortError'))
            if (signal.aborted) {
              abort()
              return
            }
            signal.addEventListener('abort', abort)
          })
        }),
      )
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      render(wrap(<AgentWorkBoard />, client))
      expect(await screen.findByText('All quiet')).toBeTruthy()
      expect(screen.queryByRole('region', { name: 'Last known' })).toBeNull()
      await client.refetchQueries({ queryKey: ['agent-progress'] })
      expect(await screen.findByText('Progress: Unknown — timed out')).toBeTruthy()
      const known = screen.getByRole('region', { name: 'Last known' })
      expect(within(known).getByText('All quiet')).toBeTruthy()
      expect(known.className).toContain('opacity-60')
    },
    20_000,
  )

  it('places the seven project blocks in the Owner question order, then lineage and code health', async () => {
    stubProgress(envelope(null))
    render(wrap(<ProgressPage />))
    expect(await screen.findAllByText('No progress has been published.')).not.toHaveLength(0)
    const page = screen.getByTestId('merged-progress')
    expect(Array.from(page.children).map(child => child.getAttribute('aria-label'))).toEqual([
      'Needs you', 'Overview', 'Give the agents a goal', 'Running', 'Plan and architecture', 'Hosts', 'History',
      'Commit lineage', 'Code health',
    ])
  })
})
