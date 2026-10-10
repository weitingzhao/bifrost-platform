import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  APPROVAL_HISTORY_LIMIT,
  APPROVAL_TOKEN_STORAGE_KEY,
  formatWaited,
  recentClosed,
} from '@/api/approvals'
import {
  buildApprovalListResponse,
  buildPlatformApproval,
} from '@/api/approvalsApiFixture'
import { ApprovalsPage } from '@/pages/ApprovalsPage'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function installStorage(): Storage {
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
  return store
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('ApprovalsPage (request page)', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
  })

  it('opens the request named by #approvals?id= with waited and remaining time', async () => {
    window.location.hash = '#approvals?id=ap-9'
    const opened = buildPlatformApproval({
      id: 'ap-9',
      action: 'drain_node',
      requester: 'sess-phone',
      tier: 'D',
      reason: 'patch the node',
      rollback: 'uncordon',
      params: { node: 'worker-a', env: 'prod' },
      created_at: new Date(Date.now() - 3 * 60 * 60_000).toISOString(),
    })
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-9')) return json(opened)
      return json({ error: url }, 500)
    })

    render(wrapper(<ApprovalsPage />))

    const region = await screen.findByRole('region', { name: 'Approval ap-9' })
    await waitFor(() => {
      expect(screen.getByText('sess-phone')).toBeTruthy()
    })
    expect(region.querySelector('[data-open="true"]')).toBeTruthy()
    expect(screen.getByText('drain_node')).toBeTruthy()
    expect(screen.getByText('prod')).toBeTruthy()
    expect(screen.getByText(/^3h · \d+h( \d+m)? left$/)).toBeTruthy()
    expect(screen.getByText('Request ap-9')).toBeTruthy()
    const urls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(urls.some(url => url.includes('status='))).toBe(false)
  })

  it('opens the same request from an old #maintenance?id= link', async () => {
    window.location.hash = '#maintenance?id=ap-9'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-9')) return json(buildPlatformApproval({ id: 'ap-9' }))
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByRole('region', { name: 'Approval ap-9' })).toBeTruthy()
  })

  it('disables decisions without a token and shows where to save it', async () => {
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ error: url }, 500)
    })

    render(wrapper(<ApprovalsPage />))

    expect(await screen.findByText('sess-owner')).toBeTruthy()
    expect(screen.getByText('gitops_sync_app')).toBeTruthy()
    expect(screen.getByText('prod drift')).toBeTruthy()
    expect(screen.getByText('argocd rollback')).toBeTruthy()
    expect(screen.queryByText('Decided at')).toBeNull()
    expect(screen.getByText(/"app": "rocket"/)).toBeTruthy()
    expect(
      screen.getByText(/Approve and Reject stay disabled until an approval token is saved/),
    ).toBeTruthy()
    expect(screen.getByLabelText('Set approval token')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
  })

  it('reads the request with the saved approval token when no operator token is set', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    await screen.findByText('sess-owner')
    const call = vi.mocked(fetch).mock.calls.find(entry => String(entry[0]).endsWith('/approvals/ap-1'))
    expect(new Headers(call?.[1]?.headers).get('Authorization')).toBe('Bearer admin-token')
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(
      false,
    )
  })

  it('approves with the server line and params hash', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    const opened = buildPlatformApproval({
      id: 'ap-1',
      approval_line: '#1 · tier C · gitops_sync_app · fixture-params',
      params_hash: 'fixture-params-hash',
    })
    const posts: unknown[] = []
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        posts.push(JSON.parse(String(init.body)))
        return json({ id: 'ap-1', status: 'executed' })
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(opened)
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => {
      expect(posts).toEqual([
        {
          channel: 'console',
          approval_line: '#1 · tier C · gitops_sync_app · fixture-params',
          params_hash: 'fixture-params-hash',
        },
      ])
    })
  })

  it('shows a closed request read-only with its result', async () => {
    window.location.hash = '#approvals?id=ap-2'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-2')) {
        return json(buildPlatformApproval({ id: 'ap-2', status: 'failed', error: 'plan expired' }))
      }
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('plan expired')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('keeps the last 50 closed requests', () => {
    const rows = Array.from({ length: 60 }, (_, i) =>
      buildPlatformApproval({
        id: `c-${i}`,
        status: 'rejected',
        expires_at: new Date(Date.UTC(2026, 0, 1) + i * 60_000).toISOString(),
        decided_at: new Date(Date.UTC(2026, 0, 1) + i * 60_000).toISOString(),
      }),
    )
    const closed = recentClosed(rows)
    expect(closed).toHaveLength(APPROVAL_HISTORY_LIMIT)
    expect(closed[0]?.id).toBe('c-59')
    expect(closed.at(-1)?.id).toBe('c-10')
  })

  it('formats how long a request has waited', () => {
    const now = Date.UTC(2026, 9, 10, 12, 0)
    expect(formatWaited(new Date(now - 5 * 60_000).toISOString(), now)).toBe('5m')
    expect(formatWaited(new Date(now - (3 * 60 + 12) * 60_000).toISOString(), now)).toBe('3h 12m')
    expect(formatWaited(new Date(now - 2 * 60 * 60_000).toISOString(), now)).toBe('2h')
    expect(formatWaited(new Date(now - (8 * 24 + 4) * 60 * 60_000).toISOString(), now)).toBe('8d 4h')
    expect(formatWaited('not-a-date', now)).toBe('unknown')
  })
})
