import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { ApprovalsPage } from '@/pages/ApprovalsPage'

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return {
    ...actual,
    fetchAuthCapabilities: vi.fn(async () => ({
      authenticated: false,
      role: 'viewer',
      can_operate: false,
      can_admin: false,
    })),
  }
})

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
      <PlatformAuthProvider>{children}</PlatformAuthProvider>
    </QueryClientProvider>
  )
}

function route(itemId: string, item: unknown, approvals: unknown[]) {
  vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/v1/approvals?status=all')) return json({ approvals })
    if (url.endsWith(`/api/v1/approvals/${itemId}`)) return json(item)
    return json({ error: url }, 500)
  })
}

describe('unknown request facts', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('hides the terminal block and lists sibling requests found by hash', async () => {
    const id = 'appr-unknown'
    const claimed = '2026-10-10T19:00:00Z'
    window.location.hash = `#approvals?id=${id}`
    const item = buildPlatformApproval({
      id,
      status: 'unknown',
      number: 3,
      runner: 'owner',
      params_hash: 'same-hash',
      error: 'executor lost: lease lapsed with no result',
      execution: { claimed_at: claimed, executor_id: 'owner-run:box', attempts: 1 },
      result: { command_sha256: 'abc', message: `Run: bash scripts/owner/owner-run.sh ${id}` },
    })
    const byHash = buildPlatformApproval({
      id: 'appr-later',
      status: 'executed',
      number: 7,
      params_hash: 'same-hash',
      result: { command_sha256: 'different' },
    })
    const bySha = buildPlatformApproval({
      id: 'appr-sha',
      status: 'running',
      number: 8,
      params_hash: 'other-hash',
      result: { command_sha256: 'abc' },
    })
    const stranger = buildPlatformApproval({
      id: 'appr-no',
      status: 'failed',
      number: 9,
      params_hash: 'nope',
      result: { command_sha256: 'zzz' },
    })
    route(id, item, [item, byHash, bySha, stranger])
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByRole('region', { name: 'What is known' })).toBeTruthy()
    expect(screen.getByText(`Claimed ${new Date(claimed).toLocaleString()} by owner-run:box.`)).toBeTruthy()
    expect(screen.getByText('No result was posted.')).toBeTruthy()
    expect(screen.getByText('If the same command later ran as another request, this one did not run.')).toBeTruthy()
    expect(screen.getByText('It did not run')).toBeTruthy()
    expect(screen.queryByText('After approving, run in a terminal:')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Copy' })).toBeNull()
    const hashLink = await screen.findByRole('link', { name: '#7' })
    expect(hashLink.getAttribute('href')).toBe('#approvals?id=appr-later')
    expect(hashLink.closest('li')?.textContent).toContain('ran and finished')
    const shaLink = screen.getByRole('link', { name: '#8' })
    expect(shaLink.getAttribute('href')).toBe('#approvals?id=appr-sha')
    expect(shaLink.closest('li')?.textContent).toContain(': running')
    expect(screen.queryByRole('link', { name: '#9' })).toBeNull()
  })

  it('shows the same sibling line on a request that is not unknown', async () => {
    const id = 'appr-owner'
    window.location.hash = `#approvals?id=${id}`
    const item = buildPlatformApproval({
      id,
      status: 'approved',
      runner: 'owner',
      params_hash: 'same-hash',
      result: { message: `Run: bash scripts/owner/owner-run.sh ${id}`, recorded: true },
    })
    const sibling = buildPlatformApproval({
      id: 'appr-again',
      status: 'executed',
      number: 4,
      params_hash: 'same-hash',
    })
    route(id, item, [item, sibling])
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('After approving, run in a terminal:')).toBeTruthy()
    const link = await screen.findByRole('link', { name: '#4' })
    expect(link.closest('li')?.textContent).toContain('The same command was requested again as')
    expect(link.closest('li')?.textContent).toContain('ran and finished')
    expect(screen.queryByRole('region', { name: 'What is known' })).toBeNull()
  })

  it('draws no sibling line when no other record shares the hash', async () => {
    const id = 'appr-alone'
    window.location.hash = `#approvals?id=${id}`
    const item = buildPlatformApproval({
      id,
      status: 'unknown',
      params_hash: 'only-this',
      error: 'lease lost',
      result: { command_sha256: 'solo' },
    })
    const other = buildPlatformApproval({
      id: 'appr-other',
      status: 'executed',
      number: 2,
      params_hash: 'different',
      result: { command_sha256: 'else' },
    })
    route(id, item, [item, other])
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('No result was posted.')).toBeTruthy()
    expect(screen.queryByText(/The same command was requested again/)).toBeNull()
    expect(screen.queryByRole('list', { name: 'Related requests' })).toBeNull()
  })
})
