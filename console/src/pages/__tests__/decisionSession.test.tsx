import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { UserMenu } from '@/components/UserMenu'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import {
  ADMIN_SIGN_IN_REFUSED,
  DECISION_ROLE_UNKNOWN,
  decisionNeedsAdmin,
  deriveDecisionGate,
} from '@/lib/decisionAuth'
import { beginSignInEpoch } from '@/lib/signInEpoch'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { QuestionPage } from '@/pages/QuestionPage'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'
import { applySession, sessionTokenFor, withDecisionAuth, type DecisionKind } from '@/pages/__tests__/decisionAuthFixture'

const LEGACY_KEY = 'bifrost-ops-approval-token'
const QUESTION_ID = 'q_0123456789abcdef'
const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')
const SRC = join(dirname(fileURLToPath(import.meta.url)), '../..')

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

function questionOpen(): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, 'question-open.json'), 'utf8')) as {
    body: Record<string, unknown>
  }
  return file.body
}

function wrapper(children: ReactNode, kind: DecisionKind) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      {withDecisionAuth(kind, children)}
    </QueryClientProvider>
  )
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

function headerNames(init: RequestInit | undefined): string[] {
  return [...new Headers(init?.headers).keys()].sort()
}

function openRequest(kind: DecisionKind) {
  applySession(kind)
  window.localStorage.setItem(LEGACY_KEY, 'legacy-secret')
  window.location.hash = '#approvals?id=ap-1'
  vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.endsWith('/approve') && init?.method === 'POST') return json({ id: 'ap-1', status: 'approved' })
    if (url.endsWith('/reject') && init?.method === 'POST') return json({ id: 'ap-1', status: 'rejected' })
    if (url.endsWith('/resolve') && init?.method === 'POST') return json({ id: 'ap-1', status: 'failed' })
    if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
    if (url.includes('/api/v1/approvals')) return json({ approvals: [] })
    return json({ error: url }, 500)
  })
  return render(wrapper(<ApprovalsPage />, kind))
}

describe('one sign-in for decisions', () => {
  beforeEach(() => {
    resetAnswerSendStore()
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('sends approve, reject, and resolve with the admin session token and nothing else', async () => {
    const view = openRequest('admin')
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(decisionCalls().some(call => call.url.endsWith('/approve'))).toBe(true))
    expect(view.container.innerHTML).not.toContain(sessionTokenFor('admin'))
    view.unmount()

    openRequest('admin')
    fireEvent.change(await screen.findByLabelText('Reject reason'), { target: { value: 'not now' } })
    fireEvent.click(screen.getByRole('button', { name: 'Reject gitops_sync_app' }))
    await waitFor(() => expect(decisionCalls().some(call => call.url.endsWith('/reject'))).toBe(true))

    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/resolve') && init?.method === 'POST') return json({ id: 'ap-1', status: 'failed' })
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        return json(buildPlatformApproval({ id: 'ap-1', status: 'unknown', error: 'lease lost' }))
      }
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.click(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    await waitFor(() => expect(decisionCalls().some(call => call.url.endsWith('/resolve'))).toBe(true))

    for (const call of decisionCalls()) {
      expect(new Headers(call.init?.headers).get('Authorization')).toBe(`Bearer ${sessionTokenFor('admin')}`)
      expect(headerNames(call.init)).toEqual(['authorization', 'content-type'])
      expect(String(call.init?.body)).not.toContain(sessionTokenFor('admin'))
      expect(String(call.init?.body)).not.toContain('legacy-secret')
      expect(new Headers(call.init?.headers).get('Authorization')).not.toContain('legacy-secret')
    }
  })

  it('sends a question answer with the admin session token and nothing else', async () => {
    applySession('admin')
    window.localStorage.setItem(LEGACY_KEY, 'legacy-secret')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    const open = questionOpen()
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/answer') && init?.method === 'POST') {
        const file = JSON.parse(readFileSync(join(CONTRACT, 'answer-response.json'), 'utf8')) as {
          body: unknown
          status?: number
        }
        return json(file.body, file.status ?? 200)
      }
      return json(open)
    })
    const { container } = render(wrapper(<QuestionPage />, 'admin'))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    await waitFor(() => expect(decisionCalls()).toHaveLength(1))
    const call = decisionCalls()[0]
    expect(call?.url.endsWith('/answer')).toBe(true)
    expect(new Headers(call?.init?.headers).get('Authorization')).toBe(`Bearer ${sessionTokenFor('admin')}`)
    expect(headerNames(call?.init)).toEqual(['authorization', 'content-type', 'x-bifrost-session'])
    expect(String(call?.init?.body)).toBe(JSON.stringify({ channel: 'console', option: 'b' }))
    expect(String(call?.init?.body)).not.toContain('legacy-secret')
    expect(container.innerHTML).not.toContain(sessionTokenFor('admin'))
    expect(container.innerHTML).not.toContain('legacy-secret')
  })

  it.each(['operator', 'viewer', 'guest'] as const)(
    'does not send a decision when signed in as %s, and names that role',
    async kind => {
      openRequest(kind)
      const role = kind === 'guest' ? 'GUEST' : kind.toUpperCase()
      expect(await screen.findByText(decisionNeedsAdmin(role))).toBeTruthy()
      expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy()
      fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'not now' } })
      fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
      fireEvent.click(screen.getByRole('button', { name: 'Reject gitops_sync_app' }))
      expect(decisionCalls()).toEqual([])
      const read = vi.mocked(fetch).mock.calls.find(call => String(call[0]).endsWith('/approvals/ap-1'))
      if (kind === 'guest') expect(new Headers(read?.[1]?.headers).get('Authorization')).toBeNull()
      else expect(new Headers(read?.[1]?.headers).get('Authorization')).toBe(`Bearer ${sessionTokenFor(kind)}`)
    },
  )

  it('does not send a question answer for an operator, and names that role', async () => {
    applySession('operator')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockResolvedValue(json(questionOpen()))
    render(wrapper(<QuestionPage />, 'operator'))
    expect(await screen.findByText(decisionNeedsAdmin('OPERATOR'))).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(decisionCalls()).toEqual([])
  })

  it('does not close an unknown request for an operator', async () => {
    applySession('operator')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        return json(buildPlatformApproval({ id: 'ap-1', status: 'unknown', error: 'lease lost' }))
      }
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />, 'operator'))
    expect(await screen.findByText(decisionNeedsAdmin('OPERATOR'))).toBeTruthy()
    fireEvent.click(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(decisionCalls()).toEqual([])
  })

  it('does not send when the capability read fails', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/auth/capabilities')) return json({ error: 'down' }, 500)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <PlatformAuthProvider>
          <ApprovalsPage />
        </PlatformAuthProvider>
      </QueryClientProvider>,
    )
    expect(await screen.findByText(DECISION_ROLE_UNKNOWN)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])
  })

  it('does not send when the signed-in role is not known', async () => {
    openRequest('unknown')
    expect(await screen.findByText(DECISION_ROLE_UNKNOWN)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Sign in' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])

    applySession('unknown')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockResolvedValue(json(questionOpen()))
    render(wrapper(<QuestionPage />, 'unknown'))
    expect(await screen.findByText(DECISION_ROLE_UNKNOWN)).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(decisionCalls()).toEqual([])
  })

  it('has no Set approval token field on Needs you, the request page, or the question page', async () => {
    openRequest('guest')
    expect(await screen.findByText('sess-owner')).toBeTruthy()
    expect(screen.queryByLabelText('Set approval token')).toBeNull()

    applySession('admin')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockResolvedValue(json(questionOpen()))
    render(wrapper(<QuestionPage />, 'admin'))
    expect(await screen.findByText('Where is the quota?')).toBeTruthy()
    expect(screen.queryByLabelText('Set approval token')).toBeNull()

    vi.mocked(fetch).mockResolvedValue(json({ error: 'viewer token required' }, 401))
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <NeedsYouPage />
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Needs you: Unknown')).toBeTruthy()
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
  })

  it('deletes the legacy key when the console starts and does not send it when it is still present', async () => {
    window.localStorage.setItem(LEGACY_KEY, 'legacy-secret')
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    vi.mocked(fetch).mockResolvedValue(
      json({ authenticated: false, role: 'viewer', can_operate: false, can_admin: false }),
    )
    render(
      <QueryClientProvider client={client}>
        <PlatformAuthProvider>
          <div>started</div>
        </PlatformAuthProvider>
      </QueryClientProvider>,
    )
    expect(window.localStorage.getItem(LEGACY_KEY)).toBeNull()

    const view = openRequest('admin')
    expect(window.localStorage.getItem(LEGACY_KEY)).toBe('legacy-secret')
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(decisionCalls()).toHaveLength(1))
    const sent = decisionCalls()[0]
    expect(String(sent?.init?.body)).not.toContain('legacy-secret')
    expect(new Headers(sent?.init?.headers).get('Authorization')).toBe(`Bearer ${sessionTokenFor('admin')}`)
    expect([...new Headers(sent?.init?.headers).values()].join(' ')).not.toContain('legacy-secret')
    expect(view.container.innerHTML).not.toContain('legacy-secret')
    expect(view.container.innerHTML).not.toContain(sessionTokenFor('admin'))
  })

  it('names the top-right sign-in when the server refuses the admin role, and opens that sign-in', async () => {
    applySession('admin')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        return json({ error: `admin token required ${sessionTokenFor('admin')}` }, 401)
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    const { container } = render(
      wrapper(
        <>
          <UserMenu opsApiHealthy onRefresh={() => undefined} />
          <ApprovalsPage />
        </>,
        'admin',
      ),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 401')).toBeTruthy()
    expect(document.querySelector('[data-refusal-detail]')).toBeNull()
    expect(container.innerHTML).not.toContain(sessionTokenFor('admin'))
    expect(container.innerHTML).not.toContain('admin token required')
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    const input = await screen.findByPlaceholderText('Platform operator token…')
    expect((input as HTMLInputElement).type).toBe('password')
    expect((input as HTMLInputElement).value).toBe('')
    expect(container.innerHTML).not.toContain(sessionTokenFor('admin'))
  })

  it('names the top-right sign-in when closing a request is refused for lack of admin', async () => {
    applySession('admin')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/resolve') && init?.method === 'POST') return json({ error: 'admin token required' }, 401)
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        return json(buildPlatformApproval({ id: 'ap-1', status: 'unknown', error: 'lease lost' }))
      }
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.click(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy()
    expect(decisionCalls()).toHaveLength(1)
  })

  it('names the top-right sign-in when an answer is refused for lack of admin', async () => {
    applySession('admin')
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/answer') && init?.method === 'POST') return json({ error: 'admin token required' }, 401)
      return json(questionOpen())
    })
    render(wrapper(<QuestionPage />, 'admin'))
    fireEvent.click(await screen.findByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(ADMIN_SIGN_IN_REFUSED)).toBeTruthy()
    expect(screen.getByText('HTTP 401')).toBeTruthy()
    expect(document.body.innerHTML).not.toContain('admin token required')
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeTruthy()
    expect(decisionCalls()).toHaveLength(1)
  })

  it('does not send a second decision after the stored session changes, including one already in flight', async () => {
    applySession('admin')
    window.location.hash = '#approvals?id=ap-1'
    let release: (value: Response) => void = () => undefined
    let captured = ''
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        captured = new Headers(init.headers).get('Authorization') ?? ''
        window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'operator-token')
        return new Promise(resolve => {
          release = resolve
        })
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return Promise.resolve(json(buildPlatformApproval({ id: 'ap-1' })))
      return Promise.resolve(json({ approvals: [] }))
    })
    render(wrapper(<ApprovalsPage />, 'admin'))
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(captured).toBe(`Bearer ${sessionTokenFor('admin')}`))
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toHaveLength(1)
    expect(captured).not.toContain('operator-token')
    release(json({ id: 'ap-1', status: 'approved' }))
    expect(await screen.findByText(DECISION_ROLE_UNKNOWN)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toHaveLength(1)
  })

  it('stops sending when another tab changes the sign-in', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const auth = new Headers(init?.headers).get('Authorization')
      if (url.endsWith('/api/v1/auth/capabilities')) {
        if (auth === 'Bearer admin-token') {
          return json({ authenticated: true, role: 'admin', can_operate: true, can_admin: true })
        }
        if (auth === 'Bearer operator-token') {
          return json({ authenticated: true, role: 'operator', can_operate: true, can_admin: false })
        }
        return json({ authenticated: false, role: 'viewer', can_operate: false, can_admin: false })
      }
      if (url.endsWith('/approve')) return json({ id: 'ap-1', status: 'approved' })
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <PlatformAuthProvider>
          <ApprovalsPage />
        </PlatformAuthProvider>
      </QueryClientProvider>,
    )
    expect(await screen.findByRole('button', { name: 'Approve gitops_sync_app' })).toBeTruthy()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(false),
    )
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'operator-token')
    window.dispatchEvent(new StorageEvent('storage', { key: PLATFORM_TOKEN_KEY, newValue: 'operator-token' }))
    expect(await screen.findByText(decisionNeedsAdmin('OPERATOR'))).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])

    window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
    window.dispatchEvent(new StorageEvent('storage', { key: PLATFORM_TOKEN_KEY, newValue: null }))
    expect(await screen.findByText(decisionNeedsAdmin('GUEST'))).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(decisionCalls()).toEqual([])
  })

  it('mentions the removed storage key only on the line that deletes it', () => {
    const hits: string[] = []
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        if (name === '__tests__' || name === 'node_modules') continue
        const path = join(dir, name)
        if (statSync(path).isDirectory()) {
          walk(path)
          continue
        }
        if (!name.endsWith('.ts') && !name.endsWith('.tsx')) continue
        const lines = readFileSync(path, 'utf8').split('\n')
        lines.forEach((line, index) => {
          if (line.includes(LEGACY_KEY)) hits.push(`${path}:${index + 1}:${line.trim()}`)
        })
      }
    }
    walk(SRC)
    expect(hits).toHaveLength(1)
    expect(hits[0]).toContain('removeItem')
    expect(hits[0]).toContain(LEGACY_KEY)
  })

  it('does not treat a contradictory capability read as admin', () => {
    const epoch = beginSignInEpoch('admin-token')
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    expect(
      deriveDecisionGate({
        token: epoch.token,
        epoch: epoch.id,
        capsEpoch: epoch.id,
        capsLoading: false,
        failed: false,
        caps: { authenticated: true, role: 'admin', can_operate: true, can_admin: false },
      }).kind,
    ).toBe('unknown')
    expect(
      deriveDecisionGate({
        token: epoch.token,
        epoch: epoch.id,
        capsEpoch: epoch.id,
        capsLoading: true,
        failed: false,
        caps: { authenticated: true, role: 'admin', can_operate: true, can_admin: true },
      }).kind,
    ).toBe('unknown')
  })
})
