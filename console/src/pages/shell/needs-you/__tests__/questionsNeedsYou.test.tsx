import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'

const CONTRACT = join(
  dirname(fileURLToPath(import.meta.url)),
  '../../../../../../api/internal/questions/testdata/contract',
)

function contractBody(name: string): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { body: Record<string, unknown> }
  return structuredClone(file.body)
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function openQuestion(number: number, over: Record<string, unknown> = {}): Record<string, unknown> {
  const body = contractBody('question-open.json')
  body.number = number
  body.id = `q_${number.toString(16).padStart(16, '0')}`
  return { ...body, ...over }
}

function installStorage(): void {
  const mem = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    value: {
      get length() {
        return mem.size
      },
      clear() {
        mem.clear()
      },
      getItem(key: string) {
        return mem.has(key) ? (mem.get(key) ?? null) : null
      },
      key(index: number) {
        return [...mem.keys()][index] ?? null
      },
      removeItem(key: string) {
        mem.delete(key)
      },
      setItem(key: string, value: string) {
        mem.set(key, String(value))
      },
    },
  })
}

function wrapper(children: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function health(url: string): Response | null {
  if (url.includes('/api/v1/self-health')) {
    return json({ generated_at: '2026-10-11T06:00:00Z', overall: 'ok', viewer_env: 'prod', probes: [] })
  }
  if (url.includes('/api/v1/checklist/signals')) {
    return json({ updated_at: '2026-10-11T06:00:00Z', source: 'checklist-prober', signals: [] })
  }
  if (url.includes('/api/v1/telemetry/alerts')) return json({ generated_at: '2026-10-11T06:00:00Z', alerts: [] })
  return null
}

function stub(questions: () => Response, approvals?: () => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      const probed = health(url)
      if (probed != null) return probed
      if (url.includes('/api/v1/questions')) return questions()
      if (url.includes('/api/v1/approvals')) return (approvals ?? (() => json(buildApprovalListResponse([]))))()
      if (url.includes('/api/v1/agent/threads')) {
        return json({ generated_at: '2026-10-11T06:00:00Z', threads: [], hosts: [] })
      }
      if (url.includes('/api/v1/lineage')) {
        return json({
          generated_at: '2026-10-11T06:00:00Z',
          since: '2026-09-01T00:00:00Z',
          days: 14,
          reachability: 'ok',
          threads: [{ session: 'thread-a', title: 'Quota work', transcripts: [], first_at: '', last_at: '', commit_count: 0, landed: 0, reached: {}, repos: [] }],
          coverage: [],
          releases: [],
          errors: [],
        })
      }
      return json({ error: `unexpected ${url}` }, 500)
    }),
  )
}

const filled = () =>
  json(
    buildApprovalListResponse([
      buildPlatformApproval({ id: 'ap-decide', number: 2, reason: 'Needs a decision.' }),
      buildPlatformApproval({ id: 'ap-run', number: 8, status: 'approved', runner: 'owner', reason: 'Publish.', execution: null }),
      buildPlatformApproval({ id: 'ap-check', number: 9, status: 'unknown', reason: 'The lease lapsed.', error: 'lease expired' }),
    ]),
  )

describe('Needs you questions', () => {
  beforeEach(() => {
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    window.location.hash = '#needs-you'
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('places Answer after the three approval groups', async () => {
    stub(() => json({ questions: [openQuestion(12)] }), filled)
    render(wrapper(<NeedsYouPage />))
    const answer = await screen.findByRole('region', { name: 'Answer' })
    const names = screen.getAllByRole('region').map(region => region.getAttribute('aria-label'))
    expect(names.indexOf('Answer')).toBeGreaterThan(names.indexOf('Check'))
    expect(names.indexOf('Check')).toBeGreaterThan(names.indexOf('Run in a terminal'))
    expect(names.indexOf('Run in a terminal')).toBeGreaterThan(names.indexOf('Decide'))
    expect(within(answer).getByText('Q12 Where is the quota?')).toBeTruthy()
    expect(within(answer).getByText(/Quota work/)).toBeTruthy()
    expect(within(answer).getByText(/W-65/)).toBeTruthy()
    expect(within(answer).getByText('Agent-reported: quota thread')).toBeTruthy()
    expect(within(answer).getByRole('link').getAttribute('href')).toBe('#questions?id=q_000000000000000c')
    expect(within(answer).queryByRole('button', { name: 'Confirm answer' })).toBeNull()
  })

  it('shows five open questions and Show 2 more when seven are open', async () => {
    const questions = [1, 2, 3, 4, 5, 6, 7].map(number => openQuestion(number, { title: `Question ${number}` }))
    stub(() => json({ questions }))
    render(wrapper(<NeedsYouPage />))
    const answer = await screen.findByRole('region', { name: 'Answer' })
    expect(within(answer).getAllByRole('link')).toHaveLength(5)
    const more = within(answer).getByRole('button', { name: 'Show 2 more' })
    fireEvent.click(more)
    expect(within(answer).getAllByRole('link')).toHaveLength(7)
    expect(screen.getByText('7 waiting for you')).toBeTruthy()
  })

  it('says Needs you: Unknown, not 0, when the question list cannot be read', async () => {
    stub(() => json({ error: 'boom' }, 500), () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })])))
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Needs you: Unknown')).toBeTruthy()
    expect(screen.getByText('Could not read questions: boom')).toBeTruthy()
    expect(screen.queryByText('0 waiting for you')).toBeNull()
    expect(screen.queryByText('Nothing needs you right now')).toBeNull()
    expect(screen.queryByText('1 waiting for you')).toBeNull()
  })

  it('shows Loading… while questions are loading, and a stale line when a refresh fails', async () => {
    let release: (value: Response) => void = () => undefined
    stub(() => new Promise(resolve => { release = resolve }))
    const { unmount } = render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Checking what needs you…')).toBeTruthy()
    expect(document.querySelector('[data-questions-state="loading"]')?.textContent).toBe('Loading…')
    expect(screen.queryByText('0 waiting for you')).toBeNull()
    expect(screen.queryByText('Nothing needs you right now')).toBeNull()
    release(json({ questions: [] }))
    unmount()

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    let fail = false
    stub(() => (fail ? json({ error: 'boom' }, 500) : json({ questions: [openQuestion(12)] })))
    render(wrapper(<NeedsYouPage />, client))
    expect(await screen.findByText('Q12 Where is the quota?')).toBeTruthy()
    fail = true
    await client.refetchQueries({ queryKey: ['questions', 'all'] })
    const stale = await screen.findByText('Could not refresh questions: boom')
    expect(stale.getAttribute('data-questions-state')).toBe('stale')
    expect(screen.getByText('Q12 Where is the quota?')).toBeTruthy()
    expect(screen.queryByText('Needs you: Unknown')).toBeNull()
  })

  it('lists unread answers, and one Recently handled line per finished question', async () => {
    const now = Date.now()
    const fresh = contractBody('question-answered.json')
    fresh.number = 3
    fresh.id = 'q_0000000000000003'
    const answer = fresh.answer as Record<string, unknown>
    answer.at = new Date(now - 60 * 60_000).toISOString()
    const aged = contractBody('question-answered.json')
    aged.number = 2
    aged.id = 'q_0000000000000002'
    const agedAnswer = aged.answer as Record<string, unknown>
    agedAnswer.at = new Date(now - 8 * 24 * 60 * 60_000).toISOString()
    const expired = contractBody('question-expired.json')
    expired.number = 13
    expired.id = 'q_000000000000000d'
    expired.expires_at = new Date(now - 2 * 60 * 60_000).toISOString()
    const withdrawn = contractBody('question-withdrawn.json')
    withdrawn.number = 14
    withdrawn.id = 'q_000000000000000e'
    const withdrawal = withdrawn.withdrawal as Record<string, unknown>
    withdrawal.at = new Date(now - 3 * 60 * 60_000).toISOString()
    stub(() => json({ questions: [fresh, aged, expired, withdrawn] }))
    render(wrapper(<NeedsYouPage />))
    const answerGroup = await screen.findByRole('region', { name: 'Answer' })
    expect(within(answerGroup).getByText('Answered, not reported as read: Q3')).toBeTruthy()
    expect(within(answerGroup).getByText('Answered, never read by the thread: Q2')).toBeTruthy()
    expect(screen.getByText('Nothing needs you right now')).toBeTruthy()
    const handled = document.getElementById('recently-handled')
    expect(handled?.textContent).toContain('Q3 answered')
    expect(handled?.textContent).toContain('Q13 expired with no answer')
    expect(handled?.textContent).toContain('Q14 withdrawn by the thread')
    expect(handled?.textContent).not.toContain('Ship')
    expect(handled?.textContent).not.toContain('one day')
  })

  it('ticks the waited line without repainting the Answer group', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    const now = Date.parse('2026-10-11T10:00:00Z')
    vi.setSystemTime(now)
    const question = openQuestion(12, {
      created_at: new Date(now - 2 * 60 * 60_000).toISOString(),
      expires_at: new Date(now + 22 * 60 * 60_000).toISOString(),
    })
    stub(() => json({ questions: [question] }))
    render(wrapper(<NeedsYouPage />))
    const group = await screen.findByRole('region', { name: 'Answer' })
    expect(within(group).getByText(/waited 2h · 22h left/)).toBeTruthy()
    const list = group.querySelector('[data-answer-group]')
    const paints = list?.getAttribute('data-paints')
    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })
    expect(list?.getAttribute('data-paints')).toBe(paints)
    expect(within(group).getByText(/waited 2h 1m · 21h 59m left/)).toBeTruthy()
    vi.useRealTimers()
  })
})
