import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const PENDING_ID = 'appr_6f4ac97469fd6f10'

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

function wrap(children: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function progressBody(document: unknown) {
  return {
    document,
    received_at: new Date(NOW).toISOString(),
    age_seconds: 0,
    stale: false,
    host: 'vision-mac',
    generated_at: new Date(NOW).toISOString(),
    generation_lag_seconds: 0,
  }
}

const STATES: Array<[string, string, string, boolean]> = [
  ['appr_aaaaaaaaaaaaaaaa', 'pending', 'Waiting for your decision', true],
  ['appr_bbbbbbbbbbbbbbbb', 'approved', 'You approved this — it is running', false],
  ['appr_cccccccccccccccc', 'running', 'You approved this — it is running', false],
  ['appr_dddddddddddddddd', 'executed', 'Done', false],
  ['appr_eeeeeeeeeeeeeeee', 'failed', 'Failed', false],
  ['appr_ffffffffffffffff', 'rejected', 'Rejected', false],
  ['appr_1111111111111111', 'expired', 'Expired', false],
  ['appr_2222222222222222', 'unknown', 'Unknown', false],
]

describe('a to-do opens its request', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('opens a valid id inside the Console and refuses any other id', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) {
          return json(
            buildApprovalListResponse([
              buildPlatformApproval({ id: PENDING_ID, number: 6, status: 'pending' }),
            ]),
          )
        }
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Needs you',
            owner_now: [
              {
                name: '你批 #6',
                work: 'Console 下一片',
                since: NOW_SEC - 120,
                urgency: '不急',
                approval: { id: PENDING_ID, number: 6, summary: 'prod' },
                actions: [{ text: '点批准', done: false }],
              },
              {
                name: 'Bad id',
                work: 'Other',
                approval: { id: 'appr_NOTHEX', number: 9 },
              },
            ],
            works: [
              {
                title: 'Console 下一片',
                short: 'Console 下一片',
                steps: [
                  {
                    name: '你批 #6',
                    who: '你',
                    state: 'running',
                    needs_owner: true,
                    approval: { id: PENDING_ID, number: 6 },
                  },
                ],
              },
            ],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const now = await screen.findByRole('region', { name: 'Needs you now' })
    expect(within(now).getByText('Console 下一片')).toBeTruthy()
    expect(within(now).getByText('你批 #6')).toBeTruthy()
    expect(within(now).getAllByText("Agent's note:").length).toBeGreaterThan(0)
    expect(await within(now).findByText('Request #6 — Sync rocket')).toBeTruthy()
    expect(within(now).queryByText('prod')).toBeNull()
    expect(await within(now).findByText('Waiting for your decision')).toBeTruthy()
    const open = within(now).getByRole('link', { name: 'Open request #6' })
    expect(open.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
    expect(open.getAttribute('target')).toBeNull()
    expect(within(now).getByText('request not shown')).toBeTruthy()
    expect(within(now).queryByRole('link', { name: 'Open request #9' })).toBeNull()
    const step = screen.getByRole('listitem', { name: '你批 #6' })
    const chip = within(step).getByRole('link', { name: 'request #6' })
    expect(chip.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
  })

  it('shows the real request, and stops asking once that request is no longer pending', async () => {
    const approvals = STATES.map(([id, status], index) =>
      buildPlatformApproval({ id, status, number: 90 + index, summary: `sum-${status}` }),
    )
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) return json(buildApprovalListResponse(approvals))
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'States',
            owner_now: STATES.map(([id, status]) => ({
              name: `agent-${status}`,
              work: `work-${id}`,
              since: NOW_SEC - 120,
              urgency: `urgent-${status}`,
              actions: [{ text: `act-${status}` }],
              approval: { id, number: 6, summary: 'harmless docs' },
            })),
          }),
        )
      }),
    )
    const { unmount } = render(wrap(<AgentWorkBoard />))
    const region = await screen.findByRole('region', { name: 'Needs you now' })
    expect(await within(region).findByText('Waiting for your decision')).toBeTruthy()
    for (const [id, status, label, asking] of STATES) {
      const index = STATES.findIndex(row => row[0] === id)
      const headline = `Request #${90 + index} — sum-${status}`
      const card = within(region).getByText(headline).closest('li')
      expect(card).toBeTruthy()
      expect(within(card as HTMLElement).getByText(label)).toBeTruthy()
      const button = within(card as HTMLElement).queryByRole('link', { name: `Open request #${90 + index}` })
      if (asking) expect(button?.getAttribute('href')).toBe(`#approvals?id=${id}`)
      else expect(button).toBeNull()
      expect((card as HTMLElement).getAttribute('data-request-quiet')).toBe(asking ? 'false' : 'true')
      expect((card as HTMLElement).textContent).not.toContain('harmless docs')
      expect((card as HTMLElement).textContent).not.toContain('#6')
      if (asking) {
        expect(within(card as HTMLElement).getByText(`agent-${status}`)).toBeTruthy()
        expect(within(card as HTMLElement).getByText(`urgent-${status}`)).toBeTruthy()
        expect(within(card as HTMLElement).getByText(`act-${status}`)).toBeTruthy()
      } else {
        expect(within(card as HTMLElement).queryByText(`agent-${status}`)).toBeNull()
        expect(within(card as HTMLElement).queryByText(`urgent-${status}`)).toBeNull()
        expect(within(card as HTMLElement).queryByText(`act-${status}`)).toBeNull()
        expect(within(card as HTMLElement).queryByText(/waiting /)).toBeNull()
      }
    }
    unmount()
  })

  it('shows the real summary and number, never the document copies', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) {
          return json(
            buildApprovalListResponse([
              buildPlatformApproval({
                id: PENDING_ID,
                number: 91,
                summary: 'delete production data',
                status: 'pending',
              }),
            ]),
          )
        }
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Mismatch',
            owner_now: [
              {
                name: '批这一步',
                work: 'Console',
                since: NOW_SEC - 60,
                urgency: '不急',
                actions: [{ text: '点批准' }],
                approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' },
              },
            ],
            works: [
              {
                title: 'Console',
                steps: [{ name: '批这一步', state: 'running', needs_owner: true, approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' } }],
              },
            ],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const now = await screen.findByRole('region', { name: 'Needs you now' })
    expect(await within(now).findByText('Request #91 — delete production data')).toBeTruthy()
    const open = await within(now).findByRole('link', { name: 'Open request #91' })
    expect(open.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
    expect(within(now).getByText("Agent's note:")).toBeTruthy()
    expect(within(now).getByText('批这一步')).toBeTruthy()
    expect(now.textContent).not.toContain('harmless docs')
    expect(now.textContent).not.toContain('#6')
    expect(within(now).queryByRole('link', { name: 'Open request #6' })).toBeNull()
    const step = screen.getByRole('listitem', { name: '批这一步' })
    const chip = await within(step).findByRole('link', { name: 'request #91' })
    expect(chip.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
    expect(within(step).queryByRole('link', { name: 'request #6' })).toBeNull()
  })

  it('does not number a request the list does not contain', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) return json(buildApprovalListResponse([]))
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Missing',
            owner_now: [
              {
                name: 'Missing step',
                approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' },
              },
            ],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const now = await screen.findByRole('region', { name: 'Needs you now' })
    expect(await within(now).findByText('The request this step refers to was not found')).toBeTruthy()
    expect(within(now).queryByRole('link', { name: /Open request/ })).toBeNull()
    expect(within(now).queryByText(/Request #/)).toBeNull()
    expect(now.textContent).not.toContain('harmless docs')
    expect(now.textContent).not.toContain('#6')
    expect(within(now).getByText('Missing step')).toBeTruthy()
  })

  it('does not look like a pending ask when the list cannot be read', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) return json({ error: 'down' }, 500)
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Unread',
            owner_now: [
              {
                name: 'Approve',
                since: NOW_SEC - 125,
                urgency: 'Blocking',
                actions: [{ text: 'Read the note' }],
                approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' },
              },
            ],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const unread = await screen.findByRole('region', { name: 'Needs you now' })
    expect(await within(unread).findByText("Could not read the request's state")).toBeTruthy()
    const link = within(unread).getByRole('link', { name: 'Open Needs you' })
    expect(link.getAttribute('href')).toBe('#needs-you')
    expect(link.className).not.toContain('bg-primary')
    expect(within(unread).queryByRole('link', { name: /Open request/ })).toBeNull()
    expect(within(unread).queryByText(/waiting /)).toBeNull()
    expect(within(unread).queryByText('Waiting for your decision')).toBeNull()
    expect(within(unread).queryByText('Blocking')).toBeNull()
    expect(within(unread).queryByText('Read the note')).toBeNull()
    expect(unread.textContent).not.toContain('#6')
    expect(unread.textContent).not.toContain('harmless docs')
  })

  it('shows a pending request as Expired once expires_at has passed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) {
          return json(
            buildApprovalListResponse([
              buildPlatformApproval({
                id: PENDING_ID,
                number: 91,
                summary: 'delete production data',
                status: 'pending',
                expires_at: new Date(NOW - 1000).toISOString(),
              }),
            ]),
          )
        }
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Stale',
            owner_now: [
              {
                name: '批这一步',
                since: NOW_SEC - 600,
                urgency: '还在催',
                actions: [{ text: '点批准' }],
                approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' },
              },
            ],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const now = await screen.findByRole('region', { name: 'Needs you now' })
    const card = (await within(now).findByText('Request #91 — delete production data')).closest('li')
    expect(card).toBeTruthy()
    expect(within(card as HTMLElement).getByText('Expired')).toBeTruthy()
    expect(within(card as HTMLElement).queryByRole('link', { name: /Open request/ })).toBeNull()
    expect(within(card as HTMLElement).queryByText(/waiting /)).toBeNull()
    expect(within(card as HTMLElement).queryByText('还在催')).toBeNull()
    expect(within(card as HTMLElement).queryByText('点批准')).toBeNull()
    expect(within(card as HTMLElement).queryByText('Waiting for your decision')).toBeNull()
    expect((card as HTMLElement).getAttribute('data-request-quiet')).toBe('true')
  })

  it('refetches the approvals list on the progress interval', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    let approvalCalls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) {
          approvalCalls += 1
          return json(
            buildApprovalListResponse([
              buildPlatformApproval({ id: PENDING_ID, number: 91, summary: 'delete production data', status: 'pending' }),
            ]),
          )
        }
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Poll',
            owner_now: [{ name: '批这一步', approval: { id: PENDING_ID, number: 6, summary: 'harmless docs' } }],
          }),
        )
      }),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(wrap(<AgentWorkBoard />, client))
    expect(await screen.findByText('Request #91 — delete production data')).toBeTruthy()
    const query = client.getQueryCache().find({ queryKey: ['approvals', 'all'] })
    const observer = query?.observers[0]
    expect(observer?.options.refetchInterval).toBe(AGENT_PROGRESS_POLL_MS)
    expect(observer?.options.refetchOnWindowFocus).toBe(true)
    const calls = approvalCalls
    await act(async () => {
      await vi.advanceTimersByTimeAsync(AGENT_PROGRESS_POLL_MS)
    })
    expect(approvalCalls).toBeGreaterThan(calls)
  })
})

function healthResponse(url: string): Response | null {
  if (url.includes('/api/v1/self-health')) {
    return json({
      generated_at: '2026-10-10T07:25:28Z',
      overall: 'ok',
      viewer_env: 'prod',
      probes: [],
    })
  }
  if (url.includes('/api/v1/checklist/signals')) {
    return json({ updated_at: '2026-10-10T07:21:54Z', source: 'checklist-prober', signals: [] })
  }
  if (url.includes('/api/v1/telemetry/alerts')) {
    return json({ generated_at: '2026-10-10T07:25:00Z', alerts: [] })
  }
  return null
}

describe('Needs you names the progress work', () => {
  beforeEach(() => {
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    window.location.hash = '#needs-you'
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  function stub(document: unknown, failProgress = false) {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        const health = healthResponse(url)
        if (health != null) return health
        if (url.includes('/api/v1/approvals')) {
          return json(buildApprovalListResponse([buildPlatformApproval({ id: PENDING_ID, number: 6 })]))
        }
        if (url.includes('/api/v1/agent/threads')) {
          return json({ generated_at: '2026-10-10T07:25:00Z', threads: [], hosts: [] })
        }
        if (url.includes('/api/v1/lineage')) {
          return json({
            generated_at: '2026-10-10T07:25:00Z',
            since: '2026-09-01T00:00:00Z',
            days: 14,
            reachability: 'ok',
            threads: [],
            coverage: [],
            releases: [],
            errors: [],
            branches: [],
          })
        }
        if (url.includes('/api/v1/agent/progress')) {
          if (failProgress) return json({ error: 'down' }, 500)
          return json(progressBody(document))
        }
        return json({ error: `unexpected ${url}` }, 500)
      }),
    )
  }

  it('links a pending request back to Progress when the document names it', async () => {
    stub({
      owner_now: [{ name: '你批 #6', work: 'Console 下一片', approval: { id: PENDING_ID, number: 6 } }],
    })
    render(wrap(<NeedsYouPage />))
    const line = await screen.findByRole('link', { name: 'Agent-reported: Console 下一片 — 你批 #6' })
    expect(line.getAttribute('href')).toBe('#progress')
  })

  it('shows no part line when the document is absent or does not name the request', async () => {
    stub(null, true)
    const missing = render(wrap(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    expect(screen.queryByText(/Agent-reported:/)).toBeNull()
    missing.unmount()

    stub({ owner_now: [{ name: 'Other', work: 'Else', approval: { id: 'appr_aaaaaaaaaaaaaaaa', number: 1 } }] })
    render(wrap(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    expect(screen.queryByText(/Agent-reported:/)).toBeNull()
  })
})
