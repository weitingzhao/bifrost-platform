import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
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

function threadsResponse(threads: AgentThread[], hosts: unknown[] = []): Response {
  return json({
    generated_at: '2026-10-10T07:25:00Z',
    silent_after_seconds: 600,
    tool_grace_seconds: 120,
    threads,
    hosts,
  })
}

function silentThread(over: Partial<AgentThread> = {}): AgentThread {
  return {
    thread: '0b8e2f4a-1111-2222-3333-444455556666',
    vendor: 'claude',
    host: 'vision-mac',
    work: 'W-54',
    title: 'W-54 thread heartbeat',
    event: 'before_tool',
    tool: 'Bash',
    tool_timeout_s: 120,
    at: '2026-10-10T07:10:00Z',
    turn_started_at: '2026-10-10T06:50:00Z',
    status: 'silent',
    quiet_seconds: 900,
    in_turn_seconds: 2100,
    threshold_seconds: 600,
    ...over,
  }
}

/** The threads list is viewer-level: no bearer, 401. */
function viewerThreads(threads: AgentThread[]): (headers: Headers) => Response {
  return headers =>
    headers.get('Authorization') == null
      ? json({ error: 'viewer token required' }, 401)
      : threadsResponse(threads)
}

function stubFetch(
  approvals: (headers: Headers) => Response,
  threads: (headers: Headers) => Response = viewerThreads([]),
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const health = healthResponse(url)
      if (health != null) return health
      if (url.includes('/api/v1/approvals?status=pending')) return approvals(new Headers(init?.headers))
      if (url.includes('/api/v1/agent/threads')) return threads(new Headers(init?.headers))
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

  it('counts silent agent threads toward the total and lists them, but not threads in turn', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })])),
      viewerThreads([
        silentThread(),
        silentThread({ thread: 'busy', title: 'Busy thread', status: 'in_turn', quiet_seconds: 30 }),
        silentThread({ thread: 'done', title: 'Done thread', status: 'idle', event: 'turn_end' }),
      ]),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('2 waiting for you')).toBeTruthy()
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(group).getByText('W-54 thread heartbeat')).toBeTruthy()
    expect(within(group).getByText('Silent 15m')).toBeTruthy()
    expect(within(group).getByText('in turn 35m · last before_tool Bash (timeout 120s) 15m ago')).toBeTruthy()
    expect(within(group).queryByText('Busy thread')).toBeNull()
    expect(within(group).queryByText('Done thread')).toBeNull()
  })

  it('counts a lost host toward the total and does not count a thread that is waiting', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([])),
      () =>
        threadsResponse(
          [
            silentThread({ thread: 'wait', title: 'Waiting on you', status: 'waiting_owner', reason: 'idle_prompt' }),
            silentThread({ thread: 'busy', title: 'Busy thread', status: 'in_turn' }),
          ],
          [
            {
              host: 'mbp',
              at: '2026-10-10T07:00:00Z',
              age_seconds: 240,
              status: 'lost',
              vendors: [{ vendor: 'codex', wired: false, token: false, monitored: false }],
            },
          ],
        ),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(group).getByText('mbp')).toBeTruthy()
    expect(within(group).getByText('not monitored: codex')).toBeTruthy()
    expect(within(group).queryByText('Waiting on you')).toBeNull()
    expect(within(group).queryByText('Busy thread')).toBeNull()
  })

  it('shows Unknown when the agent threads cannot be read, and still lists approvals', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })])),
      () => json({ error: 'boom' }, 500),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Needs you: Unknown')).toBeTruthy()
    expect(within(screen.getByRole('region', { name: 'Approve' })).getAllByRole('link')).toHaveLength(1)
    expect(
      within(screen.getByRole('region', { name: 'Silent threads' })).getByText(/Could not read agent threads/),
    ).toBeTruthy()
  })

  it('reads the threads with a saved approval token when there is no operator token', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    const seen: (string | null)[] = []
    stubFetch(
      headers =>
        headers.get('Authorization') === 'Bearer admin-token'
          ? json(buildApprovalListResponse([]))
          : json({ error: 'viewer token required' }, 401),
      headers => {
        seen.push(headers.get('Authorization'))
        return viewerThreads([silentThread()])(headers)
      },
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    expect(seen).toContain('Bearer admin-token')
    expect(within(screen.getByRole('region', { name: 'Silent threads' })).getByText('W-54 thread heartbeat')).toBeTruthy()
  })
})
