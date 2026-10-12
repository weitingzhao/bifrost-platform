import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const ID = 'q_0123456789abcdef'
const ID_B = 'q_0000000000000002'
const UNKNOWN = 'Could not confirm whether the answer was saved.'

function contractBody(name: string): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { body: Record<string, unknown> }
  return file.body
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function otherBody(): Record<string, unknown> {
  const body = contractBody('question-open.json')
  body.id = ID_B
  body.number = 2
  body.title = 'The other question'
  return body
}

function pageClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        staleTime: 0,
        refetchOnMount: true,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
      },
    },
  })
}

function wrapper(children: ReactNode, client: QueryClient) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function posts(): number {
  return vi.mocked(fetch).mock.calls.filter(call => {
    const url = String(call[0])
    return url.includes('/answer') && call[1]?.method === 'POST'
  }).length
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

describe('question send lifecycle across mounts', () => {
  const pending: Array<(value: Response) => void> = []

  beforeEach(() => {
    // question-open.json expires_at is 2026-10-12T06:00:00Z. NowClock (progressClock.tsx)
    // publishes Date.now() every 1s, and QuestionAnswerForm.tsx pastExpiry locks the
    // options once that instant is reached, so Confirm answer never mounts. Hold the
    // clock before expiry; do not follow the wall clock.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    resetAnswerSendStore()
    installStorage()
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    cleanup()
    for (const release of pending) release(json(contractBody('question-open.json')))
    pending.length = 0
    resetAnswerSendStore()
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
    vi.useRealTimers()
  })

  it('does not confirm a fresh choice when unknown is left and the next read does not succeed', async () => {
    const client = pageClient()
    const open = contractBody('question-open.json')
    const other = otherBody()
    let hang = false
    let gets = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) throw new TypeError('Failed to fetch')
      if (url.includes(ID_B)) return json(other)
      if (hang) return new Promise<Response>(resolve => pending.push(resolve))
      gets += 1
      if (gets === 1) return json(open)
      throw new TypeError('Failed to fetch')
    })
    window.location.hash = `#questions?id=${ID}`
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(UNKNOWN)).toBeTruthy()
    expect(posts()).toBe(1)
    hang = true
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    window.location.hash = `#questions?id=${ID}`
    expect(await screen.findByText(UNKNOWN)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy()
    const option = screen.getByRole('button', { name: /b Wait/ })
    expect(option.hasAttribute('disabled')).toBe(true)
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    fireEvent.click(option)
    fireEvent.click(confirm)
    expect(posts()).toBe(1)
    expect(screen.getByText(UNKNOWN)).toBeTruthy()
    expect(client.getQueryData<{ status: string }>(['questions', 'item', ID])?.status).toBe('open')
  })

  it('does not let a stale open read replace a saved answer after the question is mounted again', async () => {
    const client = pageClient()
    const open = contractBody('question-open.json')
    const other = otherBody()
    let releaseStale: ((value: Response) => void) | null = null
    let armStale = false
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return Promise.resolve(json(contractBody('answer-response.json')))
      if (url.includes(ID_B)) return Promise.resolve(json(other))
      if (armStale) {
        return new Promise<Response>(resolve => {
          releaseStale = resolve
        })
      }
      return Promise.resolve(json(open))
    })
    window.location.hash = `#questions?id=${ID}`
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText('The thread has not reported reading it')).toBeTruthy()
    armStale = true
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    window.location.hash = `#questions?id=${ID}`
    await waitFor(() => expect(releaseStale).not.toBeNull())
    await act(async () => {
      releaseStale?.(json(open))
    })
    await waitFor(() => expect(client.getQueryData<{ status: string }>(['questions', 'item', ID])?.status).toBe('answered'))
    expect(screen.getByText('The thread has not reported reading it')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(screen.queryByRole('button', { name: /b Wait/ })).toBeNull()
    expect(posts()).toBe(1)
  })

  it('shows saved and no form when the post finishes after the question is left', async () => {
    const client = pageClient()
    const open = contractBody('question-open.json')
    const other = otherBody()
    let releasePost: (value: Response) => void = () => undefined
    let releaseStale: ((value: Response) => void) | null = null
    let armStale = false
    let gets = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return new Promise<Response>(resolve => { releasePost = resolve })
      if (url.includes(ID_B)) return Promise.resolve(json(other))
      if (armStale) return new Promise<Response>(resolve => { releaseStale = resolve })
      gets += 1
      return Promise.resolve(json(open))
    })
    window.location.hash = `#questions?id=${ID}`
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(true))
    const getsAtLeave = gets
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    await act(async () => {
      releasePost(json(contractBody('answer-response.json')))
    })
    await waitFor(() => expect(gets).toBeGreaterThan(getsAtLeave))
    armStale = true
    window.location.hash = `#questions?id=${ID}`
    await waitFor(() => expect(releaseStale).not.toBeNull())
    await act(async () => {
      releaseStale?.(json(open))
    })
    await waitFor(() => expect(client.getQueryData<{ status: string }>(['questions', 'item', ID])?.status).toBe('answered'))
    expect(screen.getByText('The thread has not reported reading it')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(screen.queryByRole('button', { name: /a Ship/ })).toBeNull()
    expect(posts()).toBe(1)
  })

  it('stays unknown when the post fails after the question is left, until a read succeeds', async () => {
    const client = pageClient()
    const open = contractBody('question-open.json')
    const other = otherBody()
    let rejectPost: (reason: unknown) => void = () => undefined
    let releaseRead: ((value: Response) => void) | null = null
    let hang = false
    let gets = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/answer')) return new Promise<Response>((_resolve, reject) => { rejectPost = reject })
      if (url.includes(ID_B)) return json(other)
      if (hang) return new Promise<Response>(resolve => { releaseRead = resolve })
      gets += 1
      if (gets === 1) return json(open)
      throw new TypeError('Failed to fetch')
    })
    window.location.hash = `#questions?id=${ID}`
    render(wrapper(<QuestionPage />, client))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(true))
    window.location.hash = `#questions?id=${ID_B}`
    expect(await screen.findByText('The other question')).toBeTruthy()
    await act(async () => {
      rejectPost(new TypeError('Failed to fetch'))
    })
    await waitFor(() => expect(gets).toBeGreaterThanOrEqual(2))
    hang = true
    window.location.hash = `#questions?id=${ID}`
    expect(await screen.findByText(UNKNOWN)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    fireEvent.click(confirm)
    expect(posts()).toBe(1)
    expect(screen.queryByText('The answer was not confirmed. You can send it again.')).toBeNull()
    await waitFor(() => expect(releaseRead).not.toBeNull())
    await act(async () => {
      releaseRead?.(json(open))
    })
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Confirm answer' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Check again' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(false)
    expect(posts()).toBe(1)
  })
})
