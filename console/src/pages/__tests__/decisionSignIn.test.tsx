import { clickDecisionControl } from './clickDecisionControl'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAuthCapabilities } from '@/api/cluster'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { UserMenu } from '@/components/UserMenu'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'
import { NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'

const TOP = 'guard6-top-right-session'
const LEGACY = 'guard6-legacy-field-secret'
const QUESTION_ID = 'q_0123456789abcdef'
const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')

const ABSENT = 'Not signed in. Sign in once at the top right with the admin token; this browser will remember it.'
const UNRECOGNIZED = 'This sign-in was not recognized. Sign in at the top right with the admin token to decide.'
const OPERATOR = 'Signed in as operator, not admin. Sign in at the top right with the admin token to decide.'
const CHECKING = 'Checking your sign-in…'
const UNREADABLE = 'Could not read your sign-in. Retrying automatically; decisions stay unavailable until it can be read.'
const REJECTED = 'The server did not accept this sign-in as admin. Sign in again at the top right with the current admin token.'
const WAIT_RELOAD = 'A newer Console has been deployed. Wait for this decision to finish before reloading.'

const ADMIN: AuthCapabilities = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
const OPERATOR_CAPS: AuthCapabilities = { authenticated: true, role: 'operator', can_operate: true, can_admin: false }
const GUEST: AuthCapabilities = { authenticated: false, role: 'viewer', can_operate: false, can_admin: false }

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn() }
})

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() { return mem.size },
    clear() { mem.clear() },
    getItem(key) { return mem.has(key) ? (mem.get(key) ?? null) : null },
    key(index) { return [...mem.keys()][index] ?? null },
    removeItem(key) { mem.delete(key) },
    setItem(key, value) { mem.set(key, String(value)) },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <PlatformAuthProvider>{children}</PlatformAuthProvider>
    </QueryClientProvider>
  )
}

function questionBody(): Record<string, unknown> {
  const file = JSON.parse(readFileSync(join(CONTRACT, 'question-open.json'), 'utf8')) as { body: Record<string, unknown> }
  return file.body
}

function authHeader(call: unknown[] | undefined): string | null {
  const init = call?.[1] as RequestInit | undefined
  return new Headers(init?.headers).get('Authorization')
}

async function untilEnabled(name: string | RegExp) {
  const button = await screen.findByRole('button', { name })
  await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false))
  return button
}

async function clickEnabled(name: string | RegExp) {
  const button = await untilEnabled(name)
  clickDecisionControl(button)
  return button
}

function needsYouFetch() {
  return async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/v1/self-health')) {
      return json({ generated_at: '2026-10-10T07:25:28Z', overall: 'ok', viewer_env: 'prod', probes: [] })
    }
    if (url.includes('/api/v1/checklist/signals')) {
      return json({ updated_at: '2026-10-10T07:21:54Z', source: 'checklist-prober', signals: [] })
    }
    if (url.includes('/api/v1/telemetry/alerts')) {
      return json({ generated_at: '2026-10-10T07:25:00Z', alerts: [] })
    }
    return json({ approvals: [], questions: [], threads: [] })
  }
}

function decisionPosts(): { url: string; auth: string | null }[] {
  return vi.mocked(fetch).mock.calls
    .filter(call => {
      const url = String(call[0])
      const method = (call[1] as RequestInit | undefined)?.method
      return method === 'POST' && (url.includes('/approve') || url.includes('/reject') || url.includes('/resolve') || url.includes('/answer'))
    })
    .map(call => ({ url: String(call[0]), auth: authHeader(call) }))
}

describe('one sign-in for decisions', () => {
  beforeEach(() => {
    installStorage()
    resetAnswerSendStore()
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(ADMIN)
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    resetAnswerSendStore()
    vi.unstubAllGlobals()
    vi.useRealTimers()
    window.location.hash = ''
    document.querySelectorAll('script[data-console-build]').forEach(node => node.remove())
  })

  function openApproval(id = 'ap-1', item = buildPlatformApproval({ id })) {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') || url.endsWith('/reject') || url.endsWith('/resolve')) {
        if (init?.method === 'POST') return json({ id, status: 'executed' })
      }
      if (url.endsWith(`/api/v1/approvals/${id}`)) return json(item)
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />))
  }

  it('sends approve with the top-right token and ignores the old field', async () => {
    openApproval()
    await clickEnabled('Approve gitops_sync_app')
    await waitFor(() => expect(decisionPosts().some(post => post.url.endsWith('/approve'))).toBe(true))
    const approve = decisionPosts().find(post => post.url.endsWith('/approve'))
    expect(approve?.auth).toBe(`Bearer ${TOP}`)
    expect(approve?.auth).not.toBe(`Bearer ${LEGACY}`)
  })

  it('sends reject with the top-right token and ignores the old field', async () => {
    openApproval()
    await untilEnabled('Approve gitops_sync_app')
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'not now' } })
    clickDecisionControl(screen.getByRole('button', { name: 'Reject gitops_sync_app' }))
    await waitFor(() => expect(decisionPosts().some(post => post.url.endsWith('/reject'))).toBe(true))
    expect(decisionPosts().find(post => post.url.endsWith('/reject'))?.auth).toBe(`Bearer ${TOP}`)
  })

  it('sends close-unknown with the top-right token and ignores the old field', async () => {
    openApproval('ap-unknown', buildPlatformApproval({ id: 'ap-unknown', status: 'unknown', error: 'lease lost' }))
    await screen.findByLabelText('It did not run')
    await waitFor(() => expect(screen.queryByText(CHECKING)).toBeNull())
    clickDecisionControl(screen.getByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    await clickEnabled('Close this request')
    await waitFor(() => expect(decisionPosts().some(post => post.url.endsWith('/resolve'))).toBe(true))
    expect(decisionPosts().find(post => post.url.endsWith('/resolve'))?.auth).toBe(`Bearer ${TOP}`)
  })

  it('drops the old approval-token key when the request page loads', async () => {
    openApproval()
    await screen.findByText('Waiting for your decision.')
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
  })

  it('drops the old approval-token key when the question page loads', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockResolvedValue(json(questionBody()))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Where is the quota?')).toBeTruthy()
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
  })

  it('drops the old approval-token key when Needs you loads', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    vi.mocked(fetch).mockImplementation(needsYouFetch())
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByTestId('needs-you-page')).toBeTruthy()
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
  })

  it('does not send the old field on an approvals read', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />))
    await screen.findByText('Waiting for your decision.')
    const read = vi.mocked(fetch).mock.calls.find(call => String(call[0]).endsWith('/api/v1/approvals/ap-1'))
    expect(authHeader(read)).not.toBe(`Bearer ${LEGACY}`)
  })

  it('disables decisions when nobody is signed in', async () => {
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(GUEST)
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'ap-1' })))
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    expect(await screen.findByText(ABSENT)).toBeTruthy()
    expect(approve.hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).hasAttribute('disabled')).toBe(true)
    clickDecisionControl(approve)
    expect(decisionPosts()).toEqual([])
  })

  it('disables decisions when the server does not recognize the sign-in', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(GUEST)
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'ap-1' })))
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    expect(await screen.findByText(UNRECOGNIZED)).toBeTruthy()
    expect(approve.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(approve)
    expect(decisionPosts()).toEqual([])
  })

  it('disables decisions when the sign-in is an operator', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(OPERATOR_CAPS)
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'ap-1' })))
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    expect(await screen.findByText(OPERATOR)).toBeTruthy()
    expect(approve.hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).hasAttribute('disabled')).toBe(true)
    clickDecisionControl(approve)
    expect(decisionPosts()).toEqual([])
  })

  it('disables close-unknown for an operator even if the form is submitted', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-unknown'
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(OPERATOR_CAPS)
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/resolve') && init?.method === 'POST') return json({ id: 'ap-unknown', status: 'failed' })
      if (url.endsWith('/api/v1/approvals/ap-unknown')) {
        return json(buildPlatformApproval({ id: 'ap-unknown', status: 'unknown', error: 'lease lost' }))
      }
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText(OPERATOR)).toBeTruthy()
    const close = await screen.findByRole('button', { name: 'Close this request' })
    expect(close.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked' } })
    const form = close.closest('form')
    expect(form).toBeTruthy()
    fireEvent.submit(form!)
    expect(decisionPosts()).toEqual([])
  })

  it('disables decisions while the sign-in is still being read', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetchAuthCapabilities).mockImplementation(() => new Promise(() => undefined))
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'ap-1' })))
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    expect(await screen.findByText(CHECKING)).toBeTruthy()
    expect(approve.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(approve)
    expect(decisionPosts()).toEqual([])
  })

  it('disables decisions when the sign-in cannot be read', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetchAuthCapabilities).mockRejectedValue(new Error('auth capabilities: HTTP 503'))
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'ap-1' })))
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    expect(await screen.findByText(UNREADABLE)).toBeTruthy()
    expect(screen.queryByText(ABSENT)).toBeNull()
    expect(screen.queryByText(UNRECOGNIZED)).toBeNull()
    expect(approve.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(approve)
    expect(decisionPosts()).toEqual([])
  })

  it.each([401, 403])('explains HTTP %s and reads the sign-in again', async status => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') return json({ error: 'admin token required' }, status)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />))
    await untilEnabled('Approve gitops_sync_app')
    const before = vi.mocked(fetchAuthCapabilities).mock.calls.length
    clickDecisionControl(screen.getByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(await screen.findByText(REJECTED)).toBeTruthy()
    expect(document.body.innerHTML).not.toContain(TOP)
    expect(document.body.innerHTML).not.toContain(LEGACY)
    await waitFor(() => expect(vi.mocked(fetchAuthCapabilities).mock.calls.length).toBeGreaterThan(before))
  })

  it('does not put a known token in the request page HTML', async () => {
    openApproval()
    await screen.findByText('Waiting for your decision.')
    expect(document.body.innerHTML).not.toContain(TOP)
    expect(document.body.innerHTML).not.toContain(LEGACY)
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
  })

  it('does not put a known token in the Needs you HTML', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    vi.mocked(fetch).mockImplementation(needsYouFetch())
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByTestId('needs-you-page')).toBeTruthy()
    expect(document.body.innerHTML).not.toContain(TOP)
    expect(document.body.innerHTML).not.toContain(LEGACY)
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
  })

  it('does not put a known token in the question page HTML', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockResolvedValue(json(questionBody()))
    render(wrapper(<QuestionPage />))
    expect(await screen.findByText('Where is the quota?')).toBeTruthy()
    expect(document.body.innerHTML).not.toContain(TOP)
    expect(document.body.innerHTML).not.toContain(LEGACY)
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
  })

  it('does not put a known token in the top-right sign-in after save', async () => {
    vi.mocked(fetch).mockResolvedValue(json({}))
    render(wrapper(<UserMenu opsApiHealthy onRefresh={() => undefined} />))
    const menu = screen.getByRole('button', { name: 'User menu' })
    fireEvent.pointerDown(menu, { button: 0, ctrlKey: false, pointerType: 'mouse', pointerId: 1 })
    clickDecisionControl(menu)
    clickDecisionControl(await screen.findByRole('button', { name: 'Authenticate' }))
    fireEvent.change(screen.getByPlaceholderText('Platform operator token…'), { target: { value: TOP } })
    clickDecisionControl(screen.getByRole('button', { name: 'Connect' }))
    await waitFor(() => expect(window.localStorage.getItem(PLATFORM_TOKEN_KEY)).toBe(TOP))
    expect(document.body.innerHTML).not.toContain(TOP)
  })

  it('confirms an answer with the top-right token, not the old field', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/answer') && init?.method === 'POST') {
        return json({ id: QUESTION_ID, status: 'answered', answer: { option: 'b', text: '' } })
      }
      return json(questionBody())
    })
    render(wrapper(<QuestionPage />))
    clickDecisionControl(await screen.findByRole('button', { name: /b Wait/ }))
    await clickEnabled('Confirm answer')
    await waitFor(() => expect(decisionPosts().some(post => post.url.endsWith('/answer'))).toBe(true))
    expect(decisionPosts().find(post => post.url.endsWith('/answer'))?.auth).toBe(`Bearer ${TOP}`)
  })

  it('disables Confirm answer for an operator and says why', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(OPERATOR_CAPS)
    vi.mocked(fetch).mockResolvedValue(json(questionBody()))
    render(wrapper(<QuestionPage />))
    clickDecisionControl(await screen.findByRole('button', { name: /b Wait/ }))
    expect(await screen.findByText(OPERATOR)).toBeTruthy()
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(confirm)
    expect(decisionPosts()).toEqual([])
  })

  it('disables Confirm answer when nobody is signed in', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    window.location.hash = `#questions?id=${QUESTION_ID}`
    vi.mocked(fetchAuthCapabilities).mockResolvedValue(GUEST)
    vi.mocked(fetch).mockResolvedValue(json(questionBody()))
    render(wrapper(<QuestionPage />))
    clickDecisionControl(await screen.findByRole('button', { name: /b Wait/ }))
    expect(await screen.findByText(ABSENT)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(true)
    clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(decisionPosts()).toEqual([])
  })

  it('holds Reload while a decision is still in flight', async () => {
    const src = '/assets/index-old.js'
    const script = document.createElement('script')
    script.type = 'module'
    script.src = src
    script.setAttribute('data-console-build', '')
    document.head.append(script)
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, TOP)
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, LEGACY)
    window.location.hash = '#approvals?id=ap-1'
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload, hash: '#approvals?id=ap-1' })
    let newer = false
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('index.html')) {
        const moduleSrc = newer ? '/assets/index-new.js' : src
        return new Response(`<!doctype html><script type="module" src="${moduleSrc}"></script>`, { status: 200 })
      }
      if (url.endsWith('/approve') && init?.method === 'POST') return new Promise(() => undefined)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ approvals: [] })
    })
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    await waitFor(() => {
      const checks = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('index.html'))
      expect(checks.length).toBeGreaterThan(0)
      expect(approve.hasAttribute('disabled')).toBe(false)
    })
    expect(screen.queryByText(NEWER_CONSOLE_BANNER)).toBeNull()
    clickDecisionControl(approve)
    expect(await screen.findByRole('button', { name: 'Sending…' })).toBeTruthy()
    newer = true
    await waitFor(() => {
      document.dispatchEvent(new Event('visibilitychange'))
      expect(screen.getByText(WAIT_RELOAD)).toBeTruthy()
    })
    const reloadButton = screen.getByRole('button', { name: 'Reload' })
    expect(reloadButton.hasAttribute('disabled')).toBe(true)
    clickDecisionControl(reloadButton)
    expect(reload).not.toHaveBeenCalled()
  })
})
