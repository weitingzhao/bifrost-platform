import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { RecordsPage } from '@/pages/shell/records/RecordsPage'
import { recordsTabFromHash } from '@/pages/shell/records/recordsTabs'

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

describe('RecordsPage', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals?status=all')) {
          return json(
            buildApprovalListResponse([
              buildPlatformApproval({ id: 'ap-open' }),
              buildPlatformApproval({ id: 'ap-done', status: 'executed', action: 'cordon_node' }),
            ]),
          )
        }
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

  it('reads the tab from the address', () => {
    expect(recordsTabFromHash('#records')).toBe('closed')
    expect(recordsTabFromHash('#records?tab=audit')).toBe('audit')
    expect(recordsTabFromHash('#records?tab=patrol')).toBe('patrol')
    expect(recordsTabFromHash('#records?tab=nope')).toBe('closed')
  })

  it('lists only closed requests, each linking to its page', async () => {
    window.location.hash = '#records'
    render(wrapper(<RecordsPage />))
    const list = await screen.findByRole('list', { name: 'Closed requests' })
    const links = list.querySelectorAll('a')
    expect([...links].map(link => link.getAttribute('href'))).toEqual(['#approvals?id=ap-done'])
    expect(screen.queryByRole('button', { name: /Approve|Reject/ })).toBeNull()
  })

  it('keeps the Autopilot patrol content and the audit history, and writes the tab to the address', async () => {
    window.location.hash = '#records?tab=patrol'
    render(wrapper(<RecordsPage />))
    expect(await screen.findByText('Patrol Skills')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Audit' }))
    expect(window.location.hash).toBe('#records?tab=audit')
    expect(await screen.findByText('ACTUATION HISTORY')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Download JSON' })).toBeTruthy()
    expect(screen.queryByText('Patrol Skills')).toBeNull()
  })
})
