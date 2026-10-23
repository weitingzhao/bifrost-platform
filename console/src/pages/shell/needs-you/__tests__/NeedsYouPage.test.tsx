import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'
import planApprovals from '@/api/__fixtures__/agentrt-plan-approvals.testdb.json'
import { needsYouBadge } from '@/pages/shell/needs-you/needsYouModel'

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

it('reports offline when the list query pauses before fetch starts', async () => {
  installStorage()
  window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'test-only')
  vi.stubGlobal('fetch', vi.fn())
  onlineManager.setOnline(false)
  const view = render(wrapper(<NeedsYouPage />))
  try {
    expect(await screen.findByText('Could not read approvals — offline.')).toBeTruthy()
    expect(screen.queryByText('Nothing needs you right now.')).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  } finally {
    view.unmount()
    onlineManager.setOnline(true)
    vi.unstubAllGlobals()
  }
})

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

function threadsResponse(threads: AgentThread[], hosts: unknown[] = [], extra: Record<string, unknown> = {}): Response {
  return json({
    generated_at: '2026-10-10T07:25:00Z',
    silent_after_seconds: 600,
    tool_grace_seconds: 120,
    threads,
    hosts,
    ...extra,
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

function emptyLineage(): Response {
  return json({
    generated_at: '2026-10-10T07:25:00Z',
    since: '2026-09-26T07:25:00Z',
    days: 14,
    reachability: 'ok',
    threads: [],
    coverage: [],
    releases: [],
    errors: [],
  })
}

/** The agent runtime's hosts GET /agentrt/progress answers (W-31 3.5: For you's host entries are these). */
let runtimeHosts: unknown[] = []

function stubFetch(
  approvals: (headers: Headers) => Response,
  threads: (headers: Headers) => Response = viewerThreads([]),
  lineage: () => Response = emptyLineage,
  handled: (headers: Headers) => Response = () => json(buildApprovalListResponse([])),
  plan: () => Response = () => json({ error: 'unexpected plan read' }, 500),
) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/v1/agentrt/progress')) return json({ needs_you: { items: [], count: 0 }, hosts: runtimeHosts })
      if (url.includes('/api/v1/agentrt/plan?')) return plan()
      if (url.includes('/api/v1/agent/progress')) return json({ document: null })
      const health = healthResponse(url)
      if (health != null) return health
      if (url.includes('/api/v1/approvals?status=open')) return approvals(new Headers(init?.headers))
      if (url.includes('/api/v1/approvals?status=all')) return handled(new Headers(init?.headers))
      if (url.includes('/api/v1/agent/threads')) return threads(new Headers(init?.headers))
      if (url.includes('/api/v1/lineage')) return lineage()
      if (url.includes('/api/v1/questions')) return json({ questions: [] })
      return json({ error: `unexpected ${url}` }, 500)
    }),
  )
}

describe('NeedsYouPage', () => {
  beforeEach(() => {
    installStorage()
    window.location.hash = '#needs-you'
    runtimeHosts = []
  })

  afterEach(() => {
    vi.useRealTimers()
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

    await waitFor(() => expect(document.querySelector('[data-needs-you-count="2"]')).toBeTruthy())
    const decide = screen.getByRole('region', { name: 'Decide' })
    const links = within(decide).getAllByRole('link')
    expect(links.map(link => link.getAttribute('href'))).toEqual([
      '#approvals?id=ap-1',
      '#approvals?id=ap-2',
    ])
    expect(within(decide).getAllByText(/^waited 3h · \d+h( \d+m)? left$/)).toHaveLength(2)
    expect(within(decide).getByText('prod')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve|Reject/ })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Run in a terminal' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Check' })).toBeNull()
    // Sign-off is on the plan, not gathered here (review 488 C1): the group says how many wait and where.
    const signOff = within(screen.getByRole('region', { name: 'Sign off' }))
    expect(signOff.getByText(/^Steps are signed on/)).toBeTruthy()
    expect(signOff.getByRole('link', { name: 'Work › Plan' }).getAttribute('href')).toBe('#work?section=signoff')
    expect(screen.queryByText('Not connected yet.')).toBeNull()
    // The system verdict is the shell header's and For you's, not repeated in this section (W-31 2.2b).
    expect(screen.queryByText('Degraded — 1 warning alert')).toBeNull()
  })

  it('says under a request which plan step waits on it, from the runtime\'s plan (v4 §20.5 scenario 10)', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-16', number: 16 }), buildPlatformApproval({ id: 'ap-99', number: 99 })])),
      undefined, undefined, undefined, () => json(planApprovals.plan),
    )
    render(wrapper(<NeedsYouPage />))
    const step = await screen.findByRole('link', { name: 'Plan step 2.1-3 (yours): （你）批 PROD 迁移作业（O10、O11） — item 2.1 数据层：状态库' })
    expect(step.getAttribute('href')).toBe('#work?section=plan')
    const decide = screen.getByRole('region', { name: 'Decide' })
    const rows = within(decide).getAllByRole('listitem')
    expect(within(rows[0]).getAllByRole('link').map(l => l.getAttribute('href'))).toEqual(['#approvals?id=ap-16', '#work?section=plan', '#work?section=plan'])
    expect(within(rows[1]).getAllByRole('link').map(l => l.getAttribute('href'))).toEqual(['#approvals?id=ap-99'])
  })

  it('lists a drill apart, labelled, with nothing to approve, and does not count it (v4 §20.5 scenario 9)', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(() => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-real' }), buildPlatformApproval({ id: 'ap-drill', number: 9, drill: true })])))
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const decide = screen.getByRole('region', { name: 'Decide' })
    expect(within(decide).getAllByRole('link').map(l => l.getAttribute('href'))).toEqual(['#approvals?id=ap-real'])
    const drills = screen.getByRole('region', { name: 'Drills' })
    expect(within(drills).getByText('Not counted: a drill shows what a request looks like. Nothing runs, and it cannot be approved.')).toBeTruthy()
    expect(within(drills).getByText('Drill')).toBeTruthy()
    expect(within(drills).getByText('Drill · nothing runs, nothing to approve')).toBeTruthy()
    expect(within(drills).getAllByRole('link').map(l => l.getAttribute('href'))).toEqual(['#approvals?id=ap-drill'])
  })

  it('says nothing needs you when the list is empty', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(() => json(buildApprovalListResponse([])))
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Nothing is waiting in these lists.')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Decide' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Run in a terminal' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Check' })).toBeNull()
    expect(screen.queryByText(/Recently handled · 0/)).toBeNull()
    expect(screen.queryByText('Notifications you received earlier are under Recently handled.')).toBeNull()
  })

  it('shows Unknown, not zero, on a device without a token', async () => {
    stubFetch(headers =>
      headers.get('Authorization') == null
        ? json({ error: 'viewer token required' }, 401)
        : json(buildApprovalListResponse([])),
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(screen.queryByText('Nothing is waiting in these lists.')).toBeNull()
    expect(screen.getByText(/Could not read approvals/)).toBeTruthy()
    expect(screen.queryByText(/This device has no approval token/)).toBeNull()
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
    expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('does not read the list with the retired approval token', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    stubFetch(headers =>
      headers.get('Authorization') == null
        ? json({ error: 'viewer token required' }, 401)
        : json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-7' })]))
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
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
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="2"]')).toBeTruthy())
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(
      within(group).getByText(
        'A thread stopped in the middle of a turn and has sent nothing since. Nobody is working on it until someone opens it.',
      ),
    ).toBeTruthy()
    expect(within(group).getByText('claude 0b8e2f4a-1111-2222-3333-444455556666')).toBeTruthy()
    expect(within(group).queryByText('W-54 thread heartbeat')).toBeNull()
    expect(
      within(group).getByText('Stopped 15m ago · last activity: started a tool call · vision-mac'),
    ).toBeTruthy()
    expect(within(group).getByText('Open it in claude on vision-mac')).toBeTruthy()
    expect(within(group).queryByRole('button', { name: /dismiss/i })).toBeNull()
    const row = group.querySelector('[data-agent-thread="0b8e2f4a-1111-2222-3333-444455556666"]')
    expect(row?.querySelector('.truncate')).toBeNull()
    expect(within(group).queryByText('Busy thread')).toBeNull()
    expect(within(group).queryByText('Done thread')).toBeNull()
  })

  it('leaves a silent headless run off Needs you and does not tell the Owner to open a thread with no sidebar link', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    const runId = 'run-bc611f64-0000-4000-8000-000000000001'
    stubFetch(
      () => json(buildApprovalListResponse([])),
      viewerThreads([
        silentThread(),
        silentThread({
          thread: runId,
          vendor: 'codex',
          host: 'vision-mac',
          title: 'Headless review',
          event: 'turn_start',
          tool: undefined,
          tool_timeout_s: undefined,
        }),
      ]),
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(group).getByText('Silent threads · 1')).toBeTruthy()
    expect(within(group).getByText('claude 0b8e2f4a-1111-2222-3333-444455556666')).toBeTruthy()
    expect(within(group).getByText('Open it in claude on vision-mac')).toBeTruthy()
    expect(within(group).queryByText(`codex ${runId}`)).toBeNull()
    expect(within(group).queryByText('Headless review')).toBeNull()
    expect(within(group).queryByText('Open it in codex on vision-mac')).toBeNull()
    expect(group.querySelector(`[data-agent-thread="${runId}"]`)).toBeNull()
  })

  it('counts an agent host the runtime reports offline, not the retired heartbeat route\'s hosts, and not a waiting thread', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    runtimeHosts = [
      { host_id: 'agent-06', state: 'active', online: false, reported_offline: true, poll_age_s: 45, last_poll_at: '2026-10-10T07:24:15Z', live_attempts: 2 },
      { host_id: 'gate-b', state: 'active', online: false, reported_offline: false, poll_age_s: 900, last_poll_at: '2026-10-10T07:10:00Z' },
    ]
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
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const runtime = screen.getByRole('region', { name: 'Agent runtime' })
    expect(within(runtime).getByText('agent-06')).toBeTruthy()
    expect(within(runtime).getByText('Offline')).toBeTruthy()
    expect(within(runtime).getByText('last poll 45s ago')).toBeTruthy()
    expect(within(runtime).getByText('2 attempts on it')).toBeTruthy()
    expect(within(runtime).queryByText('gate-b')).toBeNull()
    expect(within(runtime).queryByText('No requests.')).toBeNull()
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(group).queryByText('mbp')).toBeNull()
    expect(within(group).queryByText('Waiting on you')).toBeNull()
    expect(within(group).queryByText('Busy thread')).toBeNull()
  })

  it('counts a capacity refusal younger than one hour, and not one that is exactly an hour old', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([])),
      () =>
        threadsResponse([], [], {
          threads_refused: 2,
          last_thread_refusal: '2026-10-10T07:00:00Z',
        }),
    )
    const { unmount } = render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(group).getByText('monitoring is full: 2 threads refused, last at 2026-10-10T07:00:00Z')).toBeTruthy()
    unmount()

    stubFetch(
      () => json(buildApprovalListResponse([])),
      () =>
        threadsResponse([], [], {
          generated_at: '2026-10-10T08:00:00Z',
          threads_refused: 2,
          last_thread_refusal: '2026-10-10T07:00:00Z',
        }),
    )
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Nothing is waiting in these lists.')).toBeTruthy()
    expect(screen.queryByText(/monitoring is full/)).toBeNull()
    expect(screen.queryByText(/monitoring refused/)).toBeNull()
  })

  it('counts an agent host that never polled', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    runtimeHosts = [{ host_id: 'mini', state: 'active', online: false, reported_offline: true, last_poll_at: null }]
    stubFetch(() => json(buildApprovalListResponse([])))
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const runtime = screen.getByRole('region', { name: 'Agent runtime' })
    expect(within(runtime).getByText('Never polled')).toBeTruthy()
    expect(within(runtime).getByText('no poll yet')).toBeTruthy()
  })

  it('shows Unknown when the agent threads cannot be read, and still lists approvals', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })])),
      () => json({ error: 'boom' }, 500),
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(within(screen.getByRole('region', { name: 'Decide' })).getAllByRole('link')).toHaveLength(1)
    const silent = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(silent).getByText(/Could not read agent threads/)).toBeTruthy()
    expect(within(silent).getByText('Silent threads · Unknown')).toBeTruthy()
    expect(within(silent).queryByText('Silent threads · 0')).toBeNull()
  })

  it('does not read threads with the retired approval token', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    const seen: (string | null)[] = []
    stubFetch(
      () => json({ error: 'viewer token required' }, 401),
      headers => {
        seen.push(headers.get('Authorization'))
        return viewerThreads([silentThread()])(headers)
      },
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(seen.some(value => value === 'Bearer admin-token')).toBe(false)
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
  })

  it('uses the lineage title and link when a transcript matches, and the id when lineage fails', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([])),
      viewerThreads([
        silentThread(),
        silentThread({
          thread: 'thread-no-link',
          vendor: 'codex',
          host: 'mini',
          event: 'turn_start',
          title: 'payload title that lineage replaces',
        }),
      ]),
      () =>
        json({
          threads: [
            {
              session: 'local_s1',
              title: 'Heartbeat work',
              transcripts: ['0b8e2f4a-1111-2222-3333-444455556666'],
              link: 'https://claude.example/t/1',
            },
            {
              session: 'local_s2',
              title: 'Codex job',
              transcripts: ['sessions/thread-no-link.jsonl'],
              link: '',
            },
          ],
        }),
    )
    const { unmount } = render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Heartbeat work')).toBeTruthy()
    const group = screen.getByRole('region', { name: 'Silent threads' })
    expect(document.querySelector('[data-needs-you-count="2"]')).toBeTruthy()
    expect(within(group).getByRole('link', { name: 'Open thread' }).getAttribute('href')).toBe(
      'https://claude.example/t/1',
    )
    expect(within(group).getByText('Codex job')).toBeTruthy()
    expect(within(group).queryByText('claude 0b8e2f4a-1111-2222-3333-444455556666')).toBeNull()
    expect(within(group).getByText('Stopped 15m ago · last activity: started a turn · mini')).toBeTruthy()
    expect(within(group).getByText('Open it in codex on mini')).toBeTruthy()
    expect(within(group).queryByText('payload title that lineage replaces')).toBeNull()
    expect(within(group).queryByRole('button', { name: /dismiss/i })).toBeNull()
    unmount()

    stubFetch(
      () => json(buildApprovalListResponse([])),
      viewerThreads([silentThread({ event: 'after_tool' })]),
      () => json({ error: 'lineage down' }, 500),
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const failed = screen.getByRole('region', { name: 'Silent threads' })
    expect(within(failed).getByText('Silent threads · 1')).toBeTruthy()
    expect(within(failed).queryByText('Silent threads · 0')).toBeNull()
    expect(within(failed).getByText('claude 0b8e2f4a-1111-2222-3333-444455556666')).toBeTruthy()
    expect(
      within(failed).getByText('Stopped 15m ago · last activity: finished a tool call · vision-mac'),
    ).toBeTruthy()
  })

  it('does not put a saved approval token in the page HTML', async () => {
    const secret = 'w61-saved-token-not-in-dom'
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, secret)
    stubFetch(() => json({ error: 'down' }, 500))
    const { container } = render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(screen.queryByLabelText('Set approval token')).toBeNull()
    expect(container.innerHTML).not.toContain(secret)
    expect(container.textContent ?? '').not.toContain(secret)
    expect(document.body.innerHTML).not.toContain(secret)
    await waitFor(() => expect(window.localStorage.getItem(APPROVAL_TOKEN_STORAGE_KEY)).toBeNull())
  })

  it('lists decide, run, and check, and the badge is their sum plus silent threads', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    const created = new Date(Date.now() - 3 * 60 * 60_000).toISOString()
    stubFetch(
      () =>
        json(
          buildApprovalListResponse([
            buildPlatformApproval({ id: 'ap-decide', number: 2, reason: 'Needs a decision.', created_at: created }),
            buildPlatformApproval({
              id: 'ap-run',
              number: 8,
              status: 'approved',
              runner: 'owner',
              reason: 'Publish the ConfigMap.',
              execution: null,
            }),
            buildPlatformApproval({
              id: 'ap-check',
              number: 9,
              status: 'unknown',
              reason: 'The lease lapsed.',
              error: 'lease expired before a result',
            }),
            buildPlatformApproval({
              id: 'ap-platform',
              number: 4,
              status: 'approved',
              runner: 'platform',
              reason: 'Platform runs this itself.',
            }),
            buildPlatformApproval({
              id: 'ap-running',
              number: 5,
              status: 'running',
              runner: 'owner',
              reason: 'Already running in the terminal.',
            }),
          ]),
        ),
      viewerThreads([silentThread()]),
    )
    render(wrapper(<NeedsYouPage />))

    await waitFor(() => expect(document.querySelector('[data-needs-you-count="4"]')).toBeTruthy())
    expect(document.querySelector('[data-needs-you-count]')?.getAttribute('data-needs-you-count')).toBe('4')
    expect(needsYouBadge({ state: 'known', count: 4 })).toEqual({
      text: '4',
      title: 'Needs you: 4 waiting',
      tone: 'attention',
    })
    const decide = screen.getByRole('region', { name: 'Decide' })
    expect(within(decide).getByText('#2 · Needs a decision.')).toBeTruthy()
    expect(within(decide).getByText(/^waited 3h · /)).toBeTruthy()
    const run = screen.getByRole('region', { name: 'Run in a terminal' })
    expect(within(run).getByText('#8 · Publish the ConfigMap.')).toBeTruthy()
    expect(within(run).getByText('Approved. It has not run yet.')).toBeTruthy()
    expect(within(run).getByRole('link').getAttribute('href')).toBe('#approvals?id=ap-run')
    const check = screen.getByRole('region', { name: 'Check' })
    expect(within(check).getByText('#9 · The lease lapsed.')).toBeTruthy()
    expect(within(check).getByText('The result could not be confirmed: lease expired before a result')).toBeTruthy()
    expect(within(check).getByText('Open it and say what happened.')).toBeTruthy()
    expect(screen.queryByText('Platform runs this itself.')).toBeNull()
    expect(screen.queryByText('Already running in the terminal.')).toBeNull()
    expect(document.querySelector('[data-needs-you-count="0"]')).toBeNull()
  })

  it('keeps a notification under Recently handled after the groups are empty', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    const finished = new Date(Date.now() - 60 * 60_000).toISOString()
    const failedAt = new Date(Date.now() - 2 * 60 * 60_000).toISOString()
    const rejectedAt = new Date(Date.now() - 3 * 60 * 60_000).toISOString()
    const expiredAt = new Date(Date.now() - 4 * 60 * 60_000).toISOString()
    const notified = new Date(Date.now() - 30 * 60_000).toISOString()
    stubFetch(
      () => json(buildApprovalListResponse([])),
      viewerThreads([
        silentThread({
          thread: 'busy',
          title: 'Heartbeat work',
          status: 'in_turn',
          event: 'turn_start',
          notified_at: notified,
        }),
        silentThread({
          thread: 'done',
          title: 'Codex job',
          status: 'idle',
          event: 'turn_end',
          notified_at: new Date(Date.now() - 90 * 60_000).toISOString(),
        }),
      ]),
      emptyLineage,
      () =>
        json(
          buildApprovalListResponse([
            buildPlatformApproval({
              id: 'ap-done',
              number: 3,
              status: 'executed',
              reason: 'Sync rocket.',
              execution: { exit_code: 0, finished_at: finished },
            }),
            buildPlatformApproval({
              id: 'ap-broke',
              number: 4,
              status: 'failed',
              reason: 'Drain the node.',
              execution: { exit_code: 7, finished_at: failedAt },
            }),
            buildPlatformApproval({
              id: 'ap-nope',
              number: 6,
              status: 'rejected',
              reason: 'Do not sync.',
              decided_at: rejectedAt,
            }),
            buildPlatformApproval({
              id: 'ap-old',
              number: 7,
              status: 'expired',
              reason: 'Sat too long.',
              decided_at: expiredAt,
            }),
            buildPlatformApproval({
              id: 'ap-stale',
              number: 1,
              status: 'executed',
              reason: 'Yesterday.',
              execution: {
                exit_code: 0,
                finished_at: new Date(Date.now() - 25 * 60 * 60_000).toISOString(),
              },
            }),
          ]),
        ),
    )
    render(wrapper(<NeedsYouPage />))

    expect(await screen.findByText('Nothing is waiting in these lists.')).toBeTruthy()
    expect(screen.getByText('Notifications you received earlier are under Recently handled.')).toBeTruthy()
    const block = document.getElementById('recently-handled')
    expect(block?.tagName).toBe('DETAILS')
    expect(block?.hasAttribute('open')).toBe(false)
    expect(block?.textContent).toContain('Recently handled · 6')
    expect(block?.textContent).toContain(`You approved it; it ran and finished at ${new Date(finished).toLocaleString()}`)
    expect(block?.textContent).toContain('You approved it; it ran and failed (exit code 7)')
    expect(block?.textContent).toContain(`Rejected at ${new Date(rejectedAt).toLocaleString()}`)
    expect(block?.textContent).toContain(`Expired at ${new Date(expiredAt).toLocaleString()}`)
    expect(block?.textContent).toContain('Heartbeat work was silent; it is active again')
    expect(block?.textContent).toContain('Codex job was silent; its turn ended')
    expect(block?.textContent).not.toContain('Yesterday.')
    expect(screen.queryByRole('region', { name: 'Decide' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Run in a terminal' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Check' })).toBeNull()
  })

  it('shows Unknown, never zero, when the open read fails', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(() => json({ error: 'boom' }, 500))
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(screen.getByText(/Could not read approvals/)).toBeTruthy()
    expect(screen.queryByText('Nothing is waiting in these lists.')).toBeNull()
    expect(document.querySelector('[data-needs-you-count="0"]')).toBeNull()
    expect(screen.queryByText(/Decide · 0/)).toBeNull()
    expect(screen.queryByText(/Run in a terminal · 0/)).toBeNull()
    expect(screen.queryByText(/Check · 0/)).toBeNull()
  })

  it('shows Unknown, never zero, when the handled read fails', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    stubFetch(
      () => json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })])),
      viewerThreads([]),
      emptyLineage,
      () => json({ error: 'boom' }, 500),
    )
    render(wrapper(<NeedsYouPage />))
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy())
    const block = document.getElementById('recently-handled')
    expect(block?.textContent).toContain('Recently handled · Unknown')
    expect(block?.textContent).toContain('Could not read handled requests')
    expect(block?.textContent).not.toContain('Recently handled · 0')
    expect(screen.queryByText('Nothing is waiting in these lists.')).toBeNull()
  })

  it('shows a failed approvals read and recovers it on the next interval', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(2026, 9, 12, 21, 36, 8))
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    let openCalls = 0
    stubFetch(() => {
      openCalls += 1
      if (openCalls === 1) return json({ error: 'boom' }, 500)
      return json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-1' })]))
    })
    render(wrapper(<NeedsYouPage />))
    expect(await screen.findByText('Could not read approvals — last tried 21:36:08')).toBeTruthy()
    expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy()
    expect(screen.queryByText('Nothing is waiting in these lists.')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Decide' })).toBeNull()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000)
    })
    expect(await screen.findByRole('region', { name: 'Decide' })).toBeTruthy()
    expect(document.querySelector('[data-needs-you-count="1"]')).toBeTruthy()
    expect(openCalls).toBeGreaterThanOrEqual(2)
  })
})
