import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { fetchApproval, postApprovalDecision } from '@/api/approvals'
import { fetchQuestion, fetchQuestionList, postQuestionAnswer } from '@/api/questions'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
import { decisionBlockReason } from '@/pages/decisionSignIn'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'

const A = 'operator-abcdefgh/01234567'
const B = 'admin-ijklmnop/87654321'
const ID = 'q_0123456789abcdef'
const question = { id: ID, number: 12, status: 'open', requester: 'agent', title: 'Recovery question',
  options: [{ key: 'a', label: 'Continue' }], allow_text: true, urgency: 'normal', meanwhile: 'Waiting',
  created_at: '2026-10-11T12:00:00Z', expires_at: '2099-10-11T12:00:00Z' }
const caps = (admin: boolean) => ({ authenticated: true, role: admin ? 'admin' : 'operator', can_operate: true, can_admin: admin })
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function Controls() {
  const auth = usePlatformAuth()
  return <><button onClick={() => auth.setToken(A)}>Login A</button><button onClick={() => auth.setToken(B)}>Login B</button>
    <button onClick={auth.signOut}>Logout</button><button onClick={auth.refreshCapabilities}>Check role</button>
    <output data-testid="role">{auth.caps?.role ?? 'none'}</output>
    <button disabled={decisionBlockReason(auth) != null}>Probe decision</button>
    <p>{decisionBlockReason(auth)}</p></>
}
function mount(page?: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><PlatformAuthProvider><Controls />{page}</PlatformAuthProvider></QueryClientProvider>)
  return client
}
function bearer(init?: RequestInit) { return new Headers(init?.headers).get('Authorization') }
function installFetch(handler?: (url: string, init?: RequestInit) => Response | Promise<Response> | undefined) {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const handled = handler?.(url, init)
    if (handled != null) return handled
    if (url.includes('/auth/capabilities')) return json(caps(bearer(init) === `Bearer ${B}`))
    if (url.includes('/questions?')) return json({ questions: [question] })
    if (url.includes('/questions/')) return json(question)
    if (url.includes('/approvals?')) return json({ approvals: [] })
    if (url.includes('/approvals/')) return json(buildPlatformApproval({ id: 'ap-recovery' }))
    return json({ threads: [], hosts: [], probes: [], signals: [], alerts: [], overall: 'ok' })
  }))
}
async function enabled(name: string) {
  const button = await screen.findByRole('button', { name })
  await waitFor(() => expect(button).not.toBeDisabled())
  return button
}
async function flush() { await act(async () => { await vi.advanceTimersByTimeAsync(10) }) }

beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key), clear: () => values.clear(),
  } })
  window.location.hash = ''
  resetAnswerSendStore()
  installFetch()
})
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('roles belong to one sign-in', () => {
  it('uses the bearer in the role query key even if storage changes before a reread', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, A)
    const client = mount()
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent('operator'))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    await act(async () => { await client.invalidateQueries({ queryKey: ['platform', 'auth', 'capabilities'] }) })
    expect(screen.getByTestId('role')).toHaveTextContent('operator')
    expect(screen.getByRole('button', { name: 'Probe decision' })).toBeDisabled()
    const reads = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('/auth/capabilities'))
    expect(bearer(reads.at(-1)?.[1])).toBe(`Bearer ${A}`)
  })
  it.each([[A, B, false], [B, A, true]] as const)('does not reuse roles for %s → %s → first login', async (first, second, admin) => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, first)
    const client = mount()
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent(admin ? 'admin' : 'operator'))
    fireEvent.click(screen.getByText(second === A ? 'Login A' : 'Login B'))
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent(admin ? 'operator' : 'admin'))
    expect(client.getQueryCache().findAll({ queryKey: ['platform', 'auth', 'capabilities', first] })).toHaveLength(0)
    const before = vi.mocked(fetch).mock.calls.length
    fireEvent.click(screen.getByText(first === A ? 'Login A' : 'Login B'))
    expect(screen.getByRole('button', { name: 'Probe decision' })).toBeDisabled()
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent(admin ? 'admin' : 'operator'))
    expect(vi.mocked(fetch).mock.calls.slice(before).some(call => bearer(call[1]) === `Bearer ${first}`)).toBe(true)
    expect(screen.getByRole('button', { name: 'Probe decision' }).hasAttribute('disabled')).toBe(!admin)
  })

  it('discards a late old role when login changes during the read', async () => {
    const old = deferred<Response>()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    installFetch((url, init) => url.includes('/auth/capabilities') && bearer(init) === `Bearer ${B}` ? old.promise : undefined)
    const client = mount()
    fireEvent.click(screen.getByText('Login A'))
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent('operator'))
    await act(async () => { old.resolve(json(caps(true))) })
    expect(screen.getByRole('button', { name: 'Probe decision' })).toBeDisabled()
    expect(client.getQueryCache().findAll({ queryKey: ['platform', 'auth', 'capabilities', B] })).toHaveLength(0)
  })

  it.each([401, 403])('holds decisions until the role reread after HTTP %s completes', async status => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = '#approvals?id=ap-recovery'
    let reads = 0
    const next = deferred<Response>()
    installFetch((url, init) => {
      if (url.includes('/auth/capabilities')) return ++reads === 1 ? json(caps(true)) : next.promise
      if (init?.method === 'POST') return json({ error: 'denied' }, status)
    })
    mount(<ApprovalsPage />)
    fireEvent.click(await enabled('Approve gitops_sync_app'))
    await screen.findByText('The request is still pending. That attempt did not take effect. Try again.')
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' })).toBeDisabled()
    expect(screen.getAllByText('Checking your sign-in…').length).toBeGreaterThan(0)
    await act(async () => { next.resolve(json(caps(true))) })
    await enabled('Approve gitops_sync_app')
  })

  it('times out a hung role and starts fresh periodic and manual reads, then recovers', async () => {
    vi.useFakeTimers()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    let reads = 0
    const signals: AbortSignal[] = []
    installFetch((url, init) => {
      if (!url.includes('/auth/capabilities')) return
      reads++
      if (init?.signal) signals.push(init.signal)
      return reads < 3 ? new Promise<Response>(() => undefined) : json(caps(true))
    })
    mount()
    await flush()
    await act(async () => { await vi.advanceTimersByTimeAsync(15_010) })
    expect(screen.getByText(/Could not read your sign-in/)).toBeTruthy()
    expect(signals[0]?.aborted).toBe(true)
    await act(async () => { await vi.advanceTimersByTimeAsync(60_010) })
    expect(reads).toBeGreaterThanOrEqual(2)
    fireEvent.click(screen.getByText('Check role'))
    await flush()
    expect(reads).toBe(3)
    expect(signals[1]?.aborted).toBe(true)
    expect(screen.getByRole('button', { name: 'Probe decision' })).not.toBeDisabled()
  })

  it('periodically rereads a completed role without a user action', async () => {
    vi.useFakeTimers()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    let reads = 0
    installFetch(url => url.includes('/auth/capabilities') ? json(caps(++reads === 1)) : undefined)
    mount()
    await flush()
    expect(screen.getByRole('button', { name: 'Probe decision' })).not.toBeDisabled()
    await act(async () => { await vi.advanceTimersByTimeAsync(60_010) })
    expect(reads).toBe(2)
    expect(screen.getByRole('button', { name: 'Probe decision' })).toBeDisabled()
  })
})

describe('login rereads page data', () => {
  it('rereads approvals, questions and progress on login, replacement and logout', async () => {
    mount(<NeedsYouPage />)
    const roots = ['/approvals?', '/questions?', '/agent/progress']
    const progressRead = () => vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes(roots[2]))
    await waitFor(() => expect(progressRead().length).toBeGreaterThan(0))
    for (const [action, token] of [['Login B', B], ['Login A', A], ['Logout', '']] as const) {
      const before = vi.mocked(fetch).mock.calls.length
      fireEvent.click(screen.getByText(action))
      await waitFor(() => {
        const fresh = vi.mocked(fetch).mock.calls.slice(before)
        for (const path of roots) {
          expect(fresh.some(call => String(call[0]).includes(path) && bearer(call[1]) === (token ? `Bearer ${token}` : null))).toBe(true)
        }
      })
    }
  })
  it.each(['question', 'approval', 'needs-you'] as const)('recovers %s after old storage is removed and a new login is saved', async page => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'retired-only')
    window.location.hash = page === 'question' ? `#questions?id=${ID}` : '#approvals?id=ap-recovery'
    installFetch((url, init) => {
      if ((url.includes('/questions') || url.includes('/approvals')) && !bearer(init)) return json({ error: 'viewer required' }, 401)
    })
    mount(page === 'question' ? <QuestionPage /> : page === 'approval' ? <ApprovalsPage /> : <NeedsYouPage />)
    if (page === 'question') await waitFor(() => expect(screen.getByTestId('question-page')).toHaveAttribute('data-question-state', 'unreadable'))
    else if (page === 'approval') await screen.findByText(/Could not read approvals/)
    else await screen.findByText(/Could not read questions/)
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
    fireEvent.click(screen.getByText('Login B'))
    if (page === 'approval') await screen.findByText('Waiting for your decision.')
    else await screen.findByText(/Recovery question/)
    await waitFor(() => expect(screen.getByTestId('role')).toHaveTextContent('admin'))
  })

  it('offers Retry when the first question read fails with no record', async () => {
    window.location.hash = `#questions?id=${ID}`
    let failed = true
    installFetch(url => url.includes(`/questions/${ID}`) && failed ? json({ error: 'temporarily unavailable' }, 503) : undefined)
    mount(<QuestionPage />)
    const retry = await screen.findByRole('button', { name: 'Retry' })
    failed = false
    fireEvent.click(retry)
    await screen.findByText('Recovery question')
  })
})

describe('errors never reflect the sending sign-in', () => {
  const leaks = [B, encodeURIComponent(B), B.slice(3, 11)]
  it.each(leaks)('checks %s with the sending bearer in each read and write API', async leak => {
    for (const run of [
      () => fetchQuestion(ID),
      () => fetchQuestionList('all'),
      () => postQuestionAnswer(ID, 'a', ''),
      () => fetchApproval('ap-recovery'),
      () => postApprovalDecision('ap-recovery', B, 'reject', { reason: 'wait' }),
    ]) {
      window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
      const response = deferred<Response>()
      vi.mocked(fetch).mockImplementation(() => response.promise)
      const result = run().catch(error => error as Error)
      window.localStorage.setItem(PLATFORM_TOKEN_KEY, A)
      response.resolve(json({ error: `reflected ${leak}` }, 400))
      const error = await result
      expect(error).toBeInstanceOf(Error)
      expect((error as Error).message).toContain('details are hidden')
      expect((error as Error).message).not.toContain(leak)
    }
  })
  it.each(leaks)('hides %s in a first question read', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = `#questions?id=${ID}`
    const response = deferred<Response>()
    installFetch(url => url.includes(`/questions/${ID}`) ? response.promise : undefined)
    mount(<QuestionPage />)
    await act(async () => { response.resolve(json({ error: `reflected ${leak}` }, 400)) })
    await screen.findByText(/details are hidden/)
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each(leaks)('hides %s in a question refresh', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = `#questions?id=${ID}`
    let failReads = false
    installFetch(url => url.includes('/questions') && failReads ? json({ error: `reflected ${leak}` }, 400) : undefined)
    const client = mount(<QuestionPage />)
    await screen.findByText('Recovery question')
    failReads = true
    await act(async () => { await client.invalidateQueries({ queryKey: ['questions'] }) })
    await screen.findByText(/Could not refresh this question:.*details are hidden/)
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each(leaks)('hides %s in Needs you question reads', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    installFetch(url => url.includes('/questions') ? json({ error: `reflected ${leak}` }, 400) : undefined)
    mount(<NeedsYouPage />)
    await screen.findByText(/Could not read questions:.*details are hidden/)
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each(leaks)('hides %s in an answer rejection that arrives after logout', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = `#questions?id=${ID}`
    const answer = deferred<Response>()
    installFetch(url => url.endsWith('/answer') ? answer.promise : undefined)
    mount(<QuestionPage />)
    fireEvent.click(await screen.findByRole('button', { name: /a Continue/ }))
    fireEvent.click(await enabled('Confirm answer'))
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(call => String(call[0]).endsWith('/answer'))).toBe(true))
    fireEvent.click(screen.getByText('Logout'))
    await act(async () => { answer.resolve(json({ error: `reflected ${leak}` }, 400)) })
    await waitFor(() => expect(document.body.textContent).toContain('details are hidden'))
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each(leaks)('hides %s in approval read errors', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = '#approvals?id=ap-recovery'
    installFetch(url => url.includes('/approvals') ? json({ error: `reflected ${leak}` }, 400) : undefined)
    mount(<ApprovalsPage />)
    await waitFor(() => expect(screen.getByTestId('approvals-page').textContent).toMatch(/Could not|could not|Cannot/))
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each(leaks)('hides %s in late approval decision errors', async leak => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = '#approvals?id=ap-recovery'
    const response = deferred<Response>()
    installFetch((_url, init) => init?.method === 'POST' ? response.promise : undefined)
    mount(<ApprovalsPage />)
    fireEvent.click(await enabled('Approve gitops_sync_app'))
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(call => call[1]?.method === 'POST')).toBe(true))
    fireEvent.click(screen.getByText('Logout'))
    await act(async () => { response.resolve(json({ error: `reflected ${leak}` }, 400)) })
    await screen.findByText(/details are hidden/)
    expect(document.body.innerHTML).not.toContain(leak)
  })

  it.each([401, 403])('rereads roles when Confirm answer is refused with HTTP %s', async status => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, B)
    window.location.hash = `#questions?id=${ID}`
    installFetch((url, init) => url.endsWith('/answer') && init?.method === 'POST' ? json({ error: 'denied' }, status) : undefined)
    mount(<QuestionPage />)
    fireEvent.click(await screen.findByRole('button', { name: /a Continue/ }))
    const before = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('/auth/capabilities')).length
    fireEvent.click(await enabled('Confirm answer'))
    await screen.findByText(/The server did not accept this sign-in as admin/)
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('/auth/capabilities')).length).toBeGreaterThan(before))
  })
})
