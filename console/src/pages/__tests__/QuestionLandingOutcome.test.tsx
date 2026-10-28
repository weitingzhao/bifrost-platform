import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { QuestionPage } from '@/pages/QuestionPage'
import landing from '@/api/__fixtures__/landing-card.json'
import task from '@/api/__fixtures__/agentrt-task.stg.json'

const ADMIN_CAPS: AuthCapabilities = {
  authenticated: true,
  role: 'admin',
  can_operate: true,
  can_admin: true,
}

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return {
    ...actual,
    fetchAuthCapabilities: vi.fn(async () => ({
      authenticated: true,
      role: 'admin',
      can_operate: true,
      can_admin: true,
    })),
  }
})
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const ID = 'q_0123456789abcdef'
const ID_B = 'q_0000000000000002'

function contractBody(name: string): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { body: Record<string, unknown> }
  return file.body
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
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

function wrapper(children: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  const token = window.localStorage.getItem(PLATFORM_TOKEN_KEY) ?? ''
  if (token !== '') client.setQueryData(['platform', 'auth', 'capabilities', token], ADMIN_CAPS)
  return (
    <QueryClientProvider client={client}>
      <PlatformAuthProvider>{children}</PlatformAuthProvider>
    </QueryClientProvider>
  )
}

function posts(): number {
  return vi.mocked(fetch).mock.calls.filter(call => {
    const url = String(call[0])
    return url.includes('/answer') && call[1]?.method === 'POST'
  }).length
}

describe('QuestionPage landing outcome', () => {
  beforeEach(() => {
    // question-open.json expires_at is 2026-10-12T06:00:00Z. NowClock (progressClock.tsx)
    // publishes Date.now() every 1s, and QuestionAnswerForm.tsx pastExpiry locks the
    // options once that instant is reached, so Confirm answer never mounts. Hold the
    // clock before expiry; do not follow the wall clock.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    resetAnswerSendStore()
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    cleanup()
    resetAnswerSendStore()
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
    window.location.hash = ''
    vi.useRealTimers()
  })

  function replies(card: () => Response | Promise<Response>, question = 'question-answered.json', taskResponse = task) {
    vi.mocked(fetch).mockImplementation(async input => {
      const url = String(input)
      if (url.includes('/landing/cards/')) return card()
      if (url.includes('/agentrt/tasks/')) return json(taskResponse)
      if (url.endsWith('/answer')) return json(contractBody('answer-response.json'))
      return json(contractBody(question))
    })
    window.location.hash = `#questions?id=${ID}`
  }

  it.each([
    ['applied', 'Carried out'], ['moot', 'Not carried out (moot)'],
    ['refused', 'Not carried out (refused)'], ['acknowledged', 'Noted'],
    ['held', `Held until ${new Date('2026-10-12T08:00:00Z').toLocaleString()}`],
  ])('shows %s beside the stored answer and links to the actual task', async (outcome, label) => {
    replies(() => json({ ...landing, outcome: { ...landing.outcome, outcome, remind_at: '2026-10-12T08:00:00Z' }, next_step: 'The runtime explains what happens next.' }))
    render(wrapper(<QuestionPage />))
    const block = await screen.findByRole('region', { name: 'Landing outcome' })
    expect(block.previousElementSibling?.hasAttribute('data-stored-answer')).toBe(true)
    expect(screen.getByText('Landing failed')).toBeTruthy()
    expect(screen.getByText(label)).toBeTruthy()
    expect(screen.getByText('retry')).toBeTruthy()
    expect(screen.getByText('The runtime explains what happens next.')).toBeTruthy()
    expect((await screen.findByRole('link', { name: 'View task' })).getAttribute('href'))
      .toBe(`#work?goal=${task.mission_id}&task=${landing.root}`)
    expect(posts()).toBe(0)
  })

  it('shows the backend next step while processing, then refreshes to the outcome', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    let handled = false
    replies(() => json(handled ? landing : { ...landing, outcome: null, next_step: 'Waiting for the runtime.' }))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(wrapper(<QuestionPage />, client))
    expect(await screen.findByText('Waiting for the runtime.')).toBeTruthy()
    expect(screen.queryByText('Status')).toBeNull()
    expect(screen.queryByText('Carried out')).toBeNull()
    handled = true
    await act(async () => { await vi.advanceTimersByTimeAsync(15_000) })
    expect(await screen.findByText('Carried out')).toBeTruthy()
    expect(screen.queryByText('Waiting for the runtime.')).toBeNull()
  })

  it.each([['in_main', 'Change already in main'], ['late_revert', 'Landing reverted after close']])('shows a %s card', async (kind, label) => {
    replies(() => json({ ...landing, kind, outcome: { ...landing.outcome, outcome: kind === 'in_main' ? 'applied' : 'acknowledged', why: kind === 'in_main' ? 'counted_delivered' : 'ack' } }))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText(label)).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Landing outcome' })).toBeTruthy()
  })

  it('shows the lasting record when the question itself is gone (404), apart from a missing landing record', async () => {
    replies(() => json(landing))
    const routed = vi.mocked(fetch).getMockImplementation()
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/landing/cards/') || url.includes('/agentrt/tasks/')) return routed!(input, init)
      return json({ error: 'not_found' }, 404)
    })
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText(/The question itself is no longer kept/)).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Landing outcome' })).toBeTruthy()
    expect(screen.getByText('Carried out')).toBeTruthy()
  })

  it('shows neither when both the question and its landing record are gone', async () => {
    replies(() => json({ error: 'not_found' }, 404))
    vi.mocked(fetch).mockImplementation(async () => json({ error: 'not_found' }, 404))
    render(wrapper(<QuestionPage />))
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/landing/cards/'))).toBe(true))
    expect(screen.queryByText(/The question itself is no longer kept/)).toBeNull()
    expect(screen.queryByRole('region', { name: 'Landing outcome' })).toBeNull()
  })

  it('does not request a task or show a task link for repository cards', async () => {
    replies(() => json({ ...landing, kind: 'repo_stopped', root: '', op: '' }))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Repository landings stopped')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'View task' })).toBeNull()
    expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/agentrt/tasks/'))).toBe(false)
  })

  it('does not invent a task route when the task lookup cannot be read', async () => {
    replies(() => json(landing), 'question-answered.json', { ...task, mission_id: '' })
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Carried out')).toBeTruthy()
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/agentrt/tasks/'))).toBe(true))
    expect(screen.queryByRole('link', { name: 'View task' })).toBeNull()
  })

  it.each(['404', '500', 'network', 'malformed', 'json', 'pending'])('hides an unavailable landing block (%s) and still answers', async failure => {
    replies(() => {
      if (failure === 'pending') return new Promise<Response>(() => undefined)
      if (failure === 'network') throw new TypeError('offline')
      if (failure === 'json') return new Response('{')
      if (failure === 'malformed') return json({ ...landing, outcome: { outcome: 'applied' } })
      return json({ error: failure === '404' ? 'not_found' : 'unavailable' }, Number(failure))
    }, 'question-open.json')
    render(wrapper(<QuestionPage />))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The thread has not reported reading it')).toBeTruthy()
    expect(posts()).toBe(1)
    expect(screen.queryByRole('region', { name: 'Landing outcome' })).toBeNull()
  })

  it('never shows A’s landing result on B, including a late response', async () => {
    let finish: (r: Response) => void = () => undefined
    replies(() => new Promise<Response>(resolve => { finish = resolve }))
    render(wrapper(<QuestionPage />))
    await screen.findByText('The thread has not reported reading it')
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/landing/cards/'))).toBe(true))
    vi.mocked(fetch).mockImplementation(async input => {
      if (String(input).includes('/landing/cards/')) return json({ error: 'not_found' }, 404)
      return json({ ...contractBody('question-open.json'), id: ID_B, title: 'The other question' })
    })
    await act(async () => {
      window.location.hash = `#questions?id=${ID_B}`
      window.dispatchEvent(new Event('hashchange'))
    })
    await screen.findByText('The other question')
    await act(async () => { finish(json(landing)) })
    expect(screen.queryByRole('region', { name: 'Landing outcome' })).toBeNull()
  })
})

