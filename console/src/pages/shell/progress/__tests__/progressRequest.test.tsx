import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
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

  it('shows every request state, and the button with no state when the list cannot be read', async () => {
    const approvals = STATES.map(([id, status]) => buildPlatformApproval({ id, status, number: 6 }))
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
              name: status,
              work: id,
              approval: { id, number: 6 },
            })),
          }),
        )
      }),
    )
    const { unmount } = render(wrap(<AgentWorkBoard />))
    const region = await screen.findByRole('region', { name: 'Needs you now' })
    expect(await within(region).findByText('Waiting for your decision')).toBeTruthy()
    for (const [id, , label, asking] of STATES) {
      const card = within(region).getByText(id).closest('li')
      expect(card).toBeTruthy()
      expect(within(card as HTMLElement).getByText(label)).toBeTruthy()
      const button = within(card as HTMLElement).queryByRole('link', { name: 'Open request #6' })
      if (asking) expect(button?.getAttribute('href')).toBe(`#approvals?id=${id}`)
      else expect(button).toBeNull()
      expect((card as HTMLElement).getAttribute('data-request-quiet')).toBe(asking ? 'false' : 'true')
    }
    unmount()

    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/v1/approvals')) return json({ error: 'down' }, 500)
        return json(
          progressBody({
            at: NOW_SEC,
            verdict: 'Unread',
            owner_now: [{ name: 'Approve', approval: { id: PENDING_ID, number: 6 } }],
          }),
        )
      }),
    )
    render(wrap(<AgentWorkBoard />))
    const unread = await screen.findByRole('region', { name: 'Needs you now' })
    const button = await within(unread).findByRole('link', { name: 'Open request #6' })
    expect(button.getAttribute('href')).toBe(`#approvals?id=${PENDING_ID}`)
    expect(within(unread).queryByText('Waiting for your decision')).toBeNull()
    expect(within(unread).queryByText('Unknown')).toBeNull()
    expect(within(unread).queryByText('Done')).toBeNull()
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
    const line = await screen.findByRole('link', { name: 'Part of: Console 下一片 — 你批 #6' })
    expect(line.getAttribute('href')).toBe('#progress')
  })

  it('shows no part line when the document is absent or does not name the request', async () => {
    stub(null, true)
    const missing = render(wrap(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    expect(screen.queryByText(/Part of:/)).toBeNull()
    missing.unmount()

    stub({ owner_now: [{ name: 'Other', work: 'Else', approval: { id: 'appr_aaaaaaaaaaaaaaaa', number: 1 } }] })
    render(wrap(<NeedsYouPage />))
    expect(await screen.findByText('1 waiting for you')).toBeTruthy()
    expect(screen.queryByText(/Part of:/)).toBeNull()
  })
})
