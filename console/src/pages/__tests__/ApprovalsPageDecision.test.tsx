import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'

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
import {
  buildApprovalListResponse,
  buildPlatformApproval,
} from '@/api/approvalsApiFixture'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'
import { approvalsPageHooks, htmlWithModule, installModuleScript, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'

const POST_DEADLINE = APPROVAL_WAITING_NOTICE
const POST_NETWORK =
  'The request is still pending. That attempt did not take effect. Try again.'
const NEWER_CONSOLE = 'A newer Console has been deployed. Reload before approving.'

// The decision tests of the request page (TD-325): what the page shows while a decision is in flight, after it
// fails, and when the page itself is an old build. Split from ApprovalsPage.test.tsx for the file-size ratchet.
describe('ApprovalsPage (deciding a request)', () => {
  approvalsPageHooks()

  function pendingPage(id = 'ap-1') {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    window.location.hash = `#approvals?id=${id}`
    return buildPlatformApproval({ id })
  }

  function itemReads(): number {
    return vi.mocked(fetch).mock.calls.filter(call => {
      const url = String(call[0])
      return url.endsWith('/api/v1/approvals/ap-1') && call[1]?.method !== 'POST'
    }).length
  }

  it('keeps a hanging approve disabled without starting reconciliation', async () => {
    const opened = pendingPage()
    let posts = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        posts += 1
        return new Promise((_resolve, reject) => {
          const abort = () => reject(new DOMException('The operation was aborted', 'AbortError'))
          if (init.signal?.aborted) abort()
          else init.signal?.addEventListener('abort', abort, { once: true })
        })
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return Promise.resolve(json(opened))
      return Promise.resolve(json(buildApprovalListResponse([])))
    })
    render(wrapper(<ApprovalsPage />))
    const button = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    const readsAtClick = itemReads()
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    fireEvent.click(button)
    expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    const card = document.getElementById('approval-ap-1')
    expect(card?.textContent).toContain(POST_DEADLINE)
    expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
    expect(itemReads()).toBe(readsAtClick)
    expect(posts).toBe(1)
  })

  it('continues waiting for POST even if the server may already have approved', async () => {
    pendingPage()
    let itemGets = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        return new Promise((_resolve, reject) => {
          const abort = () => reject(new DOMException('The operation was aborted', 'AbortError'))
          if (init.signal?.aborted) abort()
          else init.signal?.addEventListener('abort', abort, { once: true })
        })
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        itemGets += 1
        return Promise.resolve(json(buildPlatformApproval({ id: 'ap-1', status: itemGets === 1 ? 'pending' : 'approved' })))
      }
      return Promise.resolve(json(buildApprovalListResponse([])))
    })
    render(wrapper(<ApprovalsPage />))
    const button = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    fireEvent.click(button)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(screen.getByText(POST_DEADLINE)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
    expect(itemGets).toBe(1)
  })

  it('retains the network reason and re-reads once before restoring pending buttons', async () => {
    const opened = pendingPage()
    let posts = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        posts += 1
        return Promise.reject(new TypeError('Failed to fetch'))
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return Promise.resolve(json(opened))
      return Promise.resolve(json(buildApprovalListResponse([])))
    })
    render(wrapper(<ApprovalsPage />))
    const button = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    const readsAtClick = itemReads()
    fireEvent.click(button)
    const card = document.getElementById('approval-ap-1')
    expect(await screen.findByText(POST_NETWORK)).toBeTruthy()
    expect(card?.textContent).toContain(POST_NETWORK)
    expect(screen.getByText(/Failed to fetch/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(false)
    expect(itemReads()).toBe(readsAtClick + 1)
    expect(posts).toBe(1)
  })

  it('shows the request decided when a network failure was already approved', async () => {
    pendingPage()
    let itemGets = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') return Promise.reject(new TypeError('Failed to fetch'))
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        itemGets += 1
        return Promise.resolve(json(buildPlatformApproval({ id: 'ap-1', status: itemGets === 1 ? 'pending' : 'approved' })))
      }
      return Promise.resolve(json(buildApprovalListResponse([])))
    })
    render(wrapper(<ApprovalsPage />))
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    expect(await screen.findByText('Approved. Running now.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
    expect(screen.queryByText(POST_NETWORK)).toBeNull()
    expect(screen.getByText(/Failed to fetch/)).toBeTruthy()
    expect(itemGets).toBe(2)
  })

  it('does not send a second approve while the first is in flight', async () => {
    const opened = pendingPage()
    let posts = 0
    vi.mocked(fetch).mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        posts += 1
        return new Promise(() => undefined)
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return Promise.resolve(json(opened))
      return Promise.resolve(json(buildApprovalListResponse([])))
    })
    render(wrapper(<ApprovalsPage />))
    const button = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    fireEvent.click(button)
    fireEvent.click(button)
    expect(posts).toBe(1)
  })

  it('retries a failed request read on the next interval', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(2026, 9, 12, 21, 36, 8))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    let reads = 0
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) {
        reads += 1
        if (reads === 1) return json({ error: 'boom' }, 503)
        return json(buildPlatformApproval({ id: 'ap-1' }))
      }
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('Could not read approvals — last tried 21:36:08')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(NEEDS_YOU_REFRESH_MS)
    })
    expect(await screen.findByRole('button', { name: 'Approve gitops_sync_app' })).toBeTruthy()
    expect(reads).toBe(2)
  })

  it('does not banner when the served console build matches this page', async () => {
    const src = '/assets/index-same.js'
    installModuleScript(src)
    const opened = pendingPage()
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('index.html')) {
        expect(init?.cache).toBe('no-store')
        return htmlWithModule(src)
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(opened)
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    await waitFor(() => {
      const checks = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('index.html'))
      expect(checks.length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(NEWER_CONSOLE)).toBeNull()
    expect(approve.hasAttribute('disabled')).toBe(false)
    const before = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('index.html')).length
    document.dispatchEvent(new Event('visibilitychange'))
    await waitFor(() => {
      const checks = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('index.html'))
      expect(checks.length).toBeGreaterThan(before)
    })
    expect(screen.queryByText(NEWER_CONSOLE)).toBeNull()
  })

  it('banners and disables approve when a newer console is deployed', async () => {
    installModuleScript('/assets/index-old.js')
    const opened = pendingPage()
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload, hash: window.location.hash })
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('index.html')) return htmlWithModule('/assets/index-new.js')
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(opened)
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText(NEWER_CONSOLE)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy()
    const approve = screen.getByRole('button', { name: 'Approve gitops_sync_app' })
    const reject = screen.getByRole('button', { name: 'Reject gitops_sync_app' })
    expect(approve.hasAttribute('disabled')).toBe(true)
    expect(reject.hasAttribute('disabled')).toBe(true)
    expect(approve.getAttribute('title')).toBe(NEWER_CONSOLE)
    expect(reject.getAttribute('title')).toBe(NEWER_CONSOLE)
    expect(reload).not.toHaveBeenCalled()
  })

  it('does not banner when the build check cannot reach the server', async () => {
    installModuleScript('/assets/index-old.js')
    const opened = pendingPage()
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('index.html')) throw new TypeError('Failed to fetch')
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(opened)
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve gitops_sync_app' })
    await waitFor(() => {
      const checks = vi.mocked(fetch).mock.calls.filter(call => String(call[0]).includes('index.html'))
      expect(checks.length).toBeGreaterThan(0)
    })
    expect(screen.queryByText(NEWER_CONSOLE)).toBeNull()
    expect(approve.hasAttribute('disabled')).toBe(false)
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).getAttribute('title')).toBeNull()
  })
})
