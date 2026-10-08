import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  APPROVAL_HISTORY_LIMIT,
  APPROVAL_TOKEN_STORAGE_KEY,
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

describe('ApprovalsPage', () => {
  beforeEach(() => {
    installStorage()
    window.location.hash = '#approvals'
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
  })

  it('renders a pending request and disables decisions without a token', async () => {
    const pending = buildPlatformApproval({ id: 'ap-1' })
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('status=pending')) return json(buildApprovalListResponse([pending]))
      if (url.includes('status=all')) return json(buildApprovalListResponse([]))
      return json({ error: url }, 500)
    })

    render(wrapper(<ApprovalsPage />))

    expect(await screen.findByText('sess-owner')).toBeTruthy()
    expect(screen.getByText('gitops_sync_app')).toBeTruthy()
    expect(screen.getByText('C')).toBeTruthy()
    expect(screen.getByText('prod drift')).toBeTruthy()
    expect(screen.getByText('argocd rollback')).toBeTruthy()
    expect(screen.getByText(/\d+h( \d+m)? left/)).toBeTruthy()
    expect(screen.queryByText('Decided at')).toBeNull()
    expect(screen.getByText(/"app": "rocket"/)).toBeTruthy()
    expect(
      screen.getByText(/Approve and Reject stay disabled until an approval token is saved/),
    ).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
    expect(screen.getByTestId('approvals-page').className).toContain('flex-col')
  })

  it('opens the request named by #approvals?id=', async () => {
    window.location.hash = '#approvals?id=ap-9'
    const opened = buildPlatformApproval({
      id: 'ap-9',
      action: 'drain_node',
      requester: 'sess-phone',
      tier: 'D',
      reason: 'patch the node',
      rollback: 'uncordon',
      params: { node: 'worker-a' },
    })
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/approvals/ap-9')) return json(opened)
      if (url.includes('status=pending')) return json(buildApprovalListResponse([]))
      if (url.includes('status=all')) return json(buildApprovalListResponse([]))
      return json({ error: url }, 500)
    })

    render(wrapper(<ApprovalsPage />))

    const region = await screen.findByRole('region', { name: 'Approval ap-9' })
    expect(region).toBeTruthy()
    await waitFor(() => {
      expect(screen.getByText('sess-phone')).toBeTruthy()
    })
    expect(screen.getByText('drain_node')).toBeTruthy()
    expect(region.querySelector('[data-open="true"]')).toBeTruthy()
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
})
