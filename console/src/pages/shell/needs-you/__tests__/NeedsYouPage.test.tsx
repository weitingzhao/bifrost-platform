import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'

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
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

/** Health sources as PROD returned them on 2026-10-10: one warning alert firing. */
function healthResponse(url: string): Response | null {
  if (url.includes('/api/v1/self-health')) {
    return json({
      generated_at: '2026-10-10T07:25:28Z',
      overall: 'ok',
      viewer_env: 'prod',
      probes: [{ id: 'platform-api-prod', category: 'api', env: 'prod', status: 'ok', detail: 'HTTP 200', latency_ms: 0 }],
    })
  }
  if (url.includes('/api/v1/checklist/signals')) {
    return json({
      updated_at: '2026-10-10T07:21:54Z',
      source: 'checklist-prober',
      signals: [{ item_id: 'redis', signal: 'ok', detail: 'redis ok', env: 'prod' }],
    })
  }
  if (url.includes('/api/v1/telemetry/alerts')) {
    return json({
      generated_at: '2026-10-10T07:25:00Z',
      alerts: [{ labels: { alertname: 'KubePodNotReady', severity: 'warning' }, annotations: {}, state: 'firing' }],
    })
  }
  return null
}

function stubFetch(approvals: (headers: Headers) => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const health = healthResponse(url)
      if (health != null) return health
      if (url.includes('/api/v1/approvals?status=pending')) return approvals(new Headers(init?.headers))
      return json({ error: `unexpected ${url}` }, 500)
    }),
  )
}

describe('NeedsYouPage', () => {
  beforeEach(() => {
    installStorage()
    window.location.hash = '#needs-you'
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('lists pending requests as links with waited and remaining time, and no approve button', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    const created = new Date(Date.now() - 3 * 60 * 60_000).toISOString()
    stubFetch(() =>
      json(
        buildApprovalListResponse([
          buildPlatformApproval({ id: 'ap-1', created_at: created, params: { app: 'rocket', env: 'prod' } }),
          buildPlatformApproval({ id: 'ap-2', action: 'drain_node', tier: 'D', created_at: created }),
        ]),
      ),
    )

    render(wrapper(<NeedsYouPage />))

    expect(await screen.findByText('2 waiting for you')).toBeTruthy()
    const approve = screen.getByRole('region', { name: 'Approve' })
    const links = within(approve).getAllByRole('link')
    expect(links.map(link => link.getAttribute('href'))).toEqual([
      '#approvals?id=ap-1',
      '#approvals?id=ap-2',
    ])
    expect(within(approve).getAllByText(/^waited 3h · \d+h( \d+m)? left$/)).toHaveLength(2)
    expect(within(approve).getByText('prod')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve|Reject/ })).toBeNull()
    expect(within(screen.getByRole('region', { name: 'Decide' })).getByText('Not connected yet.')).toBeTruthy()
    expect(within(screen.getByRole('region', { name: 'Sign off' })).getByText('Not connected yet.')).toBeTruthy()
    expect(await screen.findByText('Degraded — 1 warning alert')).toBeTruthy()
  })

  it('says nothing needs you when the list is empty', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(() => json(buildApprovalListResponse([])))
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Nothing needs you right now')).toBeTruthy()
    expect(screen.getByText('Nothing to approve.')).toBeTruthy()
  })

  it('shows Unknown, not zero, on a device without a token', async () => {
    stubFetch(headers =>
      headers.get('Authorization') == null
        ? json({ error: 'viewer token required' }, 401)
        : json(buildApprovalListResponse([])),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Needs you: Unknown')).toBeTruthy()
    expect(screen.queryByText('Nothing needs you right now')).toBeNull()
    expect(screen.getByText(/This device has no approval token/)).toBeTruthy()
    expect(screen.getByLabelText('Set approval token')).toBeTruthy()
    expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('reads the list with a saved approval token when there is no operator token', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    stubFetch(headers =>
      headers.get('Authorization') === 'Bearer admin-token'
        ? json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-7' })]))
        : json({ error: 'viewer token required' }, 401),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
  })
})
