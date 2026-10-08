import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApprovalItem } from '@/api/approvals'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { MaintenancePage } from '@/pages/shell/maintenance/MaintenancePage'

function item(partial: Partial<ApprovalItem> & Pick<ApprovalItem, 'id'>): ApprovalItem {
  return {
    action: 'gitops_sync_app',
    tier: 'C',
    params: { app: 'rocket' },
    reason: 'prod drift',
    rollback: 'argocd rollback',
    requester: 'sess-owner',
    status: 'pending',
    expires_at: new Date(Date.now() + 2 * 60 * 60_000).toISOString(),
    ...partial,
  }
}

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

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <PlatformAuthContext.Provider
        value={{
          token: '',
          caps: undefined,
          capsLoading: false,
          canOperate: false,
          canAdmin: false,
          setToken: () => undefined,
          signOut: () => undefined,
          refreshCapabilities: () => undefined,
        }}
      >
        {children}
      </PlatformAuthContext.Provider>
    </QueryClientProvider>
  )
}

describe('MaintenancePage', () => {
  beforeEach(() => {
    installStorage()
    window.location.hash = '#maintenance'
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/approvals/ap-9')) {
          return json(
            item({
              id: 'ap-9',
              action: 'drain_node',
              requester: 'sess-phone',
              tier: 'D',
              reason: 'patch the node',
              rollback: 'uncordon',
              params: { node: 'worker-a' },
            }),
          )
        }
        if (url.includes('status=pending')) return json({ items: [] })
        if (url.includes('status=all')) return json({ items: [] })
        if (url.includes('/api/v1/patrol/skills')) return json({ skills: [] })
        if (url.includes('/api/v1/patrol/runs')) return json({ runs: [], total: 0 })
        if (url.includes('/api/v1/audit')) return json({ records: [] })
        return json({ error: url }, 500)
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('opens the request named by #maintenance?id=', async () => {
    window.location.hash = '#maintenance?id=ap-9'
    render(wrapper(<MaintenancePage />))
    const region = await screen.findByRole('region', { name: 'Approval ap-9' })
    await waitFor(() => {
      expect(screen.getByText('sess-phone')).toBeTruthy()
    })
    expect(region.querySelector('[data-open="true"]')).toBeTruthy()
    expect(screen.getByText('drain_node')).toBeTruthy()
  })

  it('puts patrol skills and one run history on Autopilot, and audit on History', async () => {
    render(wrapper(<MaintenancePage />))

    fireEvent.click(screen.getByRole('button', { name: 'Autopilot' }))
    expect(await screen.findByText('Patrol Skills')).toBeTruthy()
    expect(screen.queryByText('Patrol Log')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'History' }))
    expect(await screen.findByText('ACTUATION HISTORY')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Download JSON' })).toBeTruthy()
    expect(screen.queryByText('Patrol Skills')).toBeNull()
  })
})
