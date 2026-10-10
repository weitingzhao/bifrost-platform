import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
import { AgentThreadsInProgress } from '@/pages/shell/status/AgentThreadsInProgress'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function thread(over: Partial<AgentThread>): AgentThread {
  return {
    thread: 'id',
    vendor: 'codex',
    host: 'vision-mac',
    event: 'before_tool',
    tool: 'shell',
    tool_timeout_s: 600,
    at: '2026-10-10T07:00:00Z',
    turn_started_at: '2026-10-10T06:40:00Z',
    status: 'in_turn',
    quiet_seconds: 300,
    in_turn_seconds: 1500,
    threshold_seconds: 720,
    ...over,
  }
}

function stubThreads(response: () => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) =>
      String(input).includes('/api/v1/agent/threads') ? response() : json({ error: 'unused' }, 500),
    ),
  )
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
      return mem.get(key) ?? null
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

describe('AgentThreadsInProgress', () => {
  beforeEach(() => {
    installStorage()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists threads mid-turn with time in turn, silent first, and leaves out idle threads', async () => {
    stubThreads(() =>
      json({
        generated_at: '2026-10-10T07:05:00Z',
        silent_after_seconds: 600,
        tool_grace_seconds: 120,
        threads: [
          thread({ thread: 'run', title: 'Long command' }),
          thread({ thread: 'gone', title: 'Lid closed', status: 'silent', quiet_seconds: 780, work: 'W-54' }),
          thread({ thread: 'done', title: 'Finished', status: 'idle', event: 'turn_end' }),
        ],
      }),
    )
    render(wrapper(<AgentThreadsInProgress />))
    const region = await screen.findByRole('region', { name: 'In progress' })
    expect(await within(region).findByText('Lid closed')).toBeTruthy()
    expect(within(region).getByText('In progress · Agent threads · 2')).toBeTruthy()
    const rows = region.querySelectorAll('[data-agent-thread]')
    expect([...rows].map(row => row.getAttribute('data-agent-thread'))).toEqual(['gone', 'run'])
    expect(within(region).getByText('Silent 13m')).toBeTruthy()
    expect(within(region).getByText('In turn')).toBeTruthy()
    expect(
      within(region).getAllByText('in turn 25m · last before_tool shell (timeout 600s) 5m ago'),
    ).toHaveLength(1)
    expect(within(region).queryByText('Finished')).toBeNull()
  })

  it('says so when no thread is mid-turn', async () => {
    stubThreads(() =>
      json({ generated_at: '2026-10-10T07:05:00Z', silent_after_seconds: 600, tool_grace_seconds: 120, threads: [] }),
    )
    render(wrapper(<AgentThreadsInProgress />))
    expect(await screen.findByText('No agent thread is mid-turn.')).toBeTruthy()
  })

  it('reports a failed read instead of an empty list', async () => {
    stubThreads(() => json({ error: 'boom' }, 502))
    render(wrapper(<AgentThreadsInProgress />))
    expect(await screen.findByText(/Could not read agent threads/)).toBeTruthy()
    expect(screen.queryByText('No agent thread is mid-turn.')).toBeNull()
  })
})
