import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, act } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  ADMIN_SIGN_IN_REFUSED,
  COULD_NOT_CONFIRM_SIGN_IN,
  DECISION_ROLE_UNKNOWN,
} from '@/lib/decisionAuth'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { useDecisionAuth } from '@/hooks/useDecisionAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { answerSendSnapshot, resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { QuestionPage } from '@/pages/QuestionPage'
import { applySession, sessionTokenFor, withDecisionAuth } from '@/pages/__tests__/decisionAuthFixture'

const QUESTION_ID = 'q_0123456789abcdef'
const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const PHRASE = 'leaked-server-text'
const REFLECTED = 'reflected-admin-token'

const ADMIN_CAPS = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
const OPERATOR_CAPS = { authenticated: true, role: 'operator', can_operate: true, can_admin: false }
const GUEST_CAPS = { authenticated: false, role: 'viewer', can_operate: false, can_admin: false }

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

function bearer(init?: RequestInit): string {
  const value = new Headers(init?.headers).get('Authorization') ?? ''
  return value.startsWith('Bearer ') ? value.slice('Bearer '.length) : ''
}

function capsFor(token: string): typeof ADMIN_CAPS {
  if (token === 'admin-token' || token === REFLECTED) return ADMIN_CAPS
  if (token === 'operator-token') return OPERATOR_CAPS
  return GUEST_CAPS
}

function decisionCalls(): Array<{ url: string; init?: RequestInit }> {
  return vi
    .mocked(fetch)
    .mock.calls.filter(call => {
      const url = String(call[0])
      const method = call[1]?.method ?? 'GET'
      return method === 'POST' && /\/(approve|reject|resolve|answer)$/.test(url)
    })
    .map(call => ({ url: String(call[0]), init: call[1] }))
}

function client(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function SessionProbe() {
  const auth = usePlatformAuth()
  const gate = useDecisionAuth()
  return (
    <>
      <span data-testid="epoch">{auth.epoch}</span>
      <span data-testid="gate">{gate.kind}</span>
      <span data-testid="role">{gate.kind === 'known' ? gate.role : ''}</span>
      <button type="button" onClick={() => auth.setToken('admin-token')}>
        Become admin
      </button>
      <button type="button" onClick={() => auth.setToken('operator-token')}>
        Become operator
      </button>
      <button type="button" onClick={() => auth.signOut()}>
        Sign out session
      </button>
    </>
  )
}

function renderSession(queryClient: QueryClient) {
  window.location.hash = '#approvals?id=ap-1'
  return render(
    <QueryClientProvider client={queryClient}>
      <PlatformAuthProvider>
        <SessionProbe />
        <ApprovalsPage />
      </PlatformAuthProvider>
    </QueryClientProvider>,
  )
}

function approvalReads(input: RequestInfo | URL, init?: RequestInit): Response | null {
  const url = String(input)
  if (url.endsWith('/approve') && init?.method === 'POST') return json({ id: 'ap-1', status: 'approved' })
  if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
  if (url.includes('/api/v1/approvals')) return json({ approvals: [] })
  return null
}

function expectKeysOmitTokens(queryClient: QueryClient): void {
  for (const query of queryClient.getQueryCache().getAll()) {
    const key = JSON.stringify(query.queryKey)
    expect(key).not.toContain('admin-token')
    expect(key).not.toContain('operator-token')
    expect(key).not.toContain(REFLECTED)
  }
}

function questionOpen(): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, 'question-open.json'), 'utf8')) as {
    body: Record<string, unknown>
  }
  return file.body
}

function wrapper(children: ReactNode, kind: 'admin' | 'operator') {
  return (
    <QueryClientProvider client={client()}>
      {withDecisionAuth(kind, children)}
    </QueryClientProvider>
  )
}

describe('sign-in epoch and role refusals', () => {
  beforeEach(() => {
    resetAnswerSendStore()
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    window.location.hash = ''
  })

  it('never treats the operator token as admin across operator, admin, operator, then admin', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'operator-token')
    const held: Array<{ token: string; resolve: (response: Response) => void }> = []
    let hold = false
    const queryClient = client()
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) {
        const token = bearer(init)
        if (hold) return new Promise(resolve => held.push({ token, resolve }))
        return Promise.resolve(json(capsFor(token)))
      }
      const page = approvalReads(input, init)
      return Promise.resolve(page ?? json({ error: 'unused' }, 500))
    })
    renderSession(queryClient)
    expect(await screen.findByTestId('role')).toBeTruthy()
    await waitFor(() => expect(screen.getByTestId('role').textContent).toBe('OPERATOR'))
    expect(screen.getByTestId('gate').textContent).not.toBe('admin')

    fireEvent.click(screen.getByRole('button', { name: 'Become admin' }))
    await waitFor(() => expect(screen.getByTestId('gate').textContent).toBe('admin'))
    expectKeysOmitTokens(queryClient)

    hold = true
    fireEvent.click(screen.getByRole('button', { name: 'Become operator' }))
    await waitFor(() => expect(screen.getByTestId('gate').textContent).not.toBe('admin'))
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])

    for (const item of held.splice(0)) item.resolve(json(capsFor(item.token)))
    await waitFor(() => expect(screen.getByTestId('role').textContent).toBe('OPERATOR'))
    expect(screen.getByTestId('gate').textContent).not.toBe('admin')
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])

    hold = false
    fireEvent.click(screen.getByRole('button', { name: 'Become admin' }))
    await waitFor(() => expect(screen.getByTestId('gate').textContent).toBe('admin'))
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(decisionCalls()).toHaveLength(1))
    expect(new Headers(decisionCalls()[0]?.init?.headers).get('Authorization')).toBe('Bearer admin-token')
    expectKeysOmitTokens(queryClient)
  })

  it('discards a capability answer that resolves after a newer sign-in began', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'operator-token')
    const held: Array<{ token: string; resolve: (response: Response) => void }> = []
    const queryClient = client()
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) {
        const token = bearer(init)
        return new Promise(resolve => held.push({ token, resolve }))
      }
      const page = approvalReads(input, init)
      return Promise.resolve(page ?? json({ error: 'unused' }, 500))
    })
    renderSession(queryClient)
    await waitFor(() => expect(held.some(item => item.token === 'operator-token')).toBe(true))
    const first = held.find(item => item.token === 'operator-token')
    expect(first).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Become admin' }))
    await waitFor(() => expect(held.some(item => item.token === 'admin-token')).toBe(true))
    const beforeReturn = [...held]
    fireEvent.click(screen.getByRole('button', { name: 'Become operator' }))
    await waitFor(() => expect(held.length).toBeGreaterThan(beforeReturn.length))

    await act(async () => {
      for (const item of beforeReturn) item.resolve(json(ADMIN_CAPS))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(screen.getByTestId('gate').textContent).not.toBe('admin')
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])
    expect(screen.getByTestId('gate').textContent).not.toBe('admin')
    expectKeysOmitTokens(queryClient)
  })

  it('starts a new epoch when another tab changes the stored token', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    const queryClient = client()
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) return Promise.resolve(json(capsFor(bearer(init))))
      const page = approvalReads(input, init)
      return Promise.resolve(page ?? json({ error: 'unused' }, 500))
    })
    renderSession(queryClient)
    await waitFor(() => expect(screen.getByTestId('gate').textContent).toBe('admin'))
    const first = Number(screen.getByTestId('epoch').textContent)
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'operator-token')
    window.dispatchEvent(new StorageEvent('storage', { key: PLATFORM_TOKEN_KEY, newValue: 'operator-token' }))
    await waitFor(() => expect(Number(screen.getByTestId('epoch').textContent)).toBeGreaterThan(first))
    await waitFor(() => expect(screen.getByTestId('gate').textContent).not.toBe('admin'))
    await waitFor(() => expect(screen.getByTestId('role').textContent).toBe('OPERATOR'))
  })

  it('keeps a reflected token out of the page, state, and console after sign-out', async () => {
    const spies = (['log', 'info', 'warn', 'error', 'debug'] as const).map(method =>
      vi.spyOn(console, method).mockImplementation(() => {}),
    )
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, REFLECTED)
    let release: (response: Response) => void = () => undefined
    const queryClient = client()
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) return Promise.resolve(json(capsFor(bearer(init))))
      if (url.endsWith('/approve') && init?.method === 'POST') {
        return new Promise(resolve => {
          release = resolve
        })
      }
      const page = approvalReads(input, init)
      return Promise.resolve(page ?? json({ error: 'unused' }, 500))
    })
    renderSession(queryClient)
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(false),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(decisionCalls()).toHaveLength(1))
    fireEvent.click(screen.getByRole('button', { name: 'Sign out session' }))
    release(json({ error: `admin token required ${REFLECTED} ${PHRASE}` }, 401))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 401')).toBeTruthy()
    expect(document.documentElement.innerHTML).not.toContain(REFLECTED)
    expect(document.documentElement.innerHTML).not.toContain(PHRASE)
    expect(document.querySelector('[data-decision-failure]')?.getAttribute('data-decision-failure')).toBe(
      'console-role-refusal:401',
    )
    const stored = JSON.stringify(answerSendSnapshot('ap-1'))
    expect(stored).not.toContain(REFLECTED)
    for (const query of queryClient.getQueryCache().getAll()) {
      expect(JSON.stringify({ key: query.queryKey, data: query.state.data })).not.toContain(REFLECTED)
    }
    for (const spy of spies) {
      for (const args of spy.mock.calls) expect(String(args)).not.toContain(REFLECTED)
    }
  })

  it('shows the console sentence and status for a 403 on approve, reject, resolve, and answer', async () => {
    const spies = (['log', 'info', 'warn', 'error', 'debug'] as const).map(method =>
      vi.spyOn(console, method).mockImplementation(() => {}),
    )
    const refused = json({ error: `${PHRASE} ${sessionTokenFor('admin')}` }, 403)

    applySession('admin')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') return refused
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    const approved = render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 403')).toBeTruthy()
    expect(approved.container.innerHTML).not.toContain(PHRASE)
    expect(approved.container.innerHTML).not.toContain(sessionTokenFor('admin'))
    approved.unmount()

    applySession('admin')
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/reject') && init?.method === 'POST') return json({ error: `${PHRASE} ${sessionTokenFor('admin')}` }, 403)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    const rejected = render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.change(await screen.findByLabelText('Reject reason'), { target: { value: 'not now' } })
    fireEvent.click(screen.getByRole('button', { name: 'Reject gitops_sync_app' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 403')).toBeTruthy()
    expect(rejected.container.innerHTML).not.toContain(PHRASE)
    rejected.unmount()

    applySession('admin')
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/resolve') && init?.method === 'POST') return json({ error: `${PHRASE} ${sessionTokenFor('admin')}` }, 403)
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        return json(buildPlatformApproval({ id: 'ap-1', status: 'unknown', error: 'lease lost' }))
      }
      return json({ approvals: [] })
    })
    const closed = render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.click(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 403')).toBeTruthy()
    expect(closed.container.innerHTML).not.toContain(PHRASE)
    closed.unmount()

    applySession('admin')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    const open = questionOpen()
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/answer') && init?.method === 'POST') {
        return json({ error: `${PHRASE} ${sessionTokenFor('admin')}` }, 403)
      }
      return json(open)
    })
    const answered = render(wrapper(<QuestionPage />, 'admin'))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 403')).toBeTruthy()
    expect(answered.container.innerHTML).not.toContain(PHRASE)
    expect(answered.container.innerHTML).not.toContain(sessionTokenFor('admin'))
    const phase = answerSendSnapshot(QUESTION_ID).phase
    expect(phase.kind === 'confirming' ? phase.rejection : '').not.toContain(PHRASE)
    expect(phase.kind === 'confirming' ? phase.rejection : '').not.toContain(sessionTokenFor('admin'))
    for (const spy of spies) {
      for (const args of spy.mock.calls) expect(String(args)).not.toContain(PHRASE)
    }
  })

  it('offers try again and sign in when the capability read fails, without sending a decision', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    let capsCalls = 0
    const queryClient = client()
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) {
        capsCalls += 1
        return json({ error: 'down' }, 500)
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    renderSession(queryClient)
    expect(await screen.findByText(DECISION_ROLE_UNKNOWN)).toBeTruthy()
    expect(screen.getByText(COULD_NOT_CONFIRM_SIGN_IN)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy()
    const epoch = screen.getByTestId('epoch').textContent
    const before = capsCalls
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(capsCalls).toBe(before + 1))
    expect(screen.getByTestId('epoch').textContent).toBe(epoch)
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])
  })
})
