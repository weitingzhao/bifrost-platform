import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import progress from '@/api/__fixtures__/agentrt-progress.stg.json'
import events from '@/api/__fixtures__/agentrt-events-M-2.stg.json'
import roadmap from '@/api/__fixtures__/board-roadmap-round-arch.json'
import { AgentsPage } from '../AgentsPage'
import { GoalForm } from '../GoalForm'
import { TaskTimeline } from '../TaskTimeline'
import { SHELL_NAV, resolveConsoleHash } from '@/lib/shell/consoleRoutes'

const TASK = 't_01M50M1CZSDN8PNHRH8C2241RS'
const goalResult = { mission_id: 'M-6', mission_state: 'active', task_id: 't_new', repo: 'bifrost-platform' }
const clients: QueryClient[] = []
function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  clients.push(client)
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
function submit() { fireEvent.click(screen.getByRole('button', { name: 'Submit' })) }
function enter(value: string) { fireEvent.change(screen.getByLabelText('Goal'), { target: { value } }) }
function mockPageFetch() {
  return vi.fn(async (url: RequestInfo | URL) => {
    const path = String(url)
    if (path.includes('/agentrt/progress')) return json(progress)
    if (path.includes('/agentrt/events?')) return json(events)
    if (path.includes('/agentrt/artifacts/')) return new Response('# Report\nDone <script>unsafe()</script>', { headers: { 'Content-Type': 'text/markdown' } })
    if (path.includes('/agent/progress')) return json({ document: { roadmap }, generated_at: progress.generated_at })
    throw new Error(`Unexpected URL: ${path}`)
  })
}
beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
    clear: () => values.clear(),
  } })
  vi.stubGlobal('fetch', mockPageFetch())
})
afterEach(() => {
  cleanup()
  clients.forEach(c => c.clear())
  clients.length = 0
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('goal submission', () => {
  it('disables empty submissions and sending, then shows the mission and task', async () => {
    let resolve!: (response: Response) => void
    const fetch = vi.fn((_url: RequestInfo | URL, _init?: RequestInit) => new Promise<Response>(r => { resolve = r }))
    vi.stubGlobal('fetch', fetch)
    wrap(<GoalForm />)
    expect(screen.getByRole('button', { name: 'Submit' })).toBeDisabled()
    enter('   ')
    expect(screen.getByRole('button', { name: 'Submit' })).toBeDisabled()
    enter('Add the agent page')
    expect(screen.getByText('18/2000 characters')).toBeInTheDocument()
    submit()
    expect(screen.getByRole('button', { name: 'Submitting…' })).toBeDisabled()
    expect(screen.getByLabelText('Goal')).toBeDisabled()
    await act(async () => { resolve(json(goalResult)) })
    expect(await screen.findByText('M-6 created (state active)')).toBeInTheDocument()
    expect(screen.getByText('Task: t_new')).toBeInTheDocument()
    expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toMatchObject({ goal: 'Add the agent page', repo: 'bifrost-platform' })
  })
  it('shows the platform message for a 422', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'invalid', message: 'The goal contains a control character.' }, 422)))
    wrap(<GoalForm />)
    enter('Try a goal')
    submit()
    expect(await screen.findByRole('alert')).toHaveTextContent('The goal contains a control character.')
  })
  it('reuses the key after a network error and generates a new one after any text edit', async () => {
    const requests: { idempotency_key: string; goal: string }[] = []
    vi.stubGlobal('fetch', vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      requests.push(JSON.parse(init!.body as string))
      throw new Error('Network disconnected')
    }))
    wrap(<GoalForm />)
    enter('First goal')
    submit()
    await screen.findByRole('alert')
    submit()
    await waitFor(() => expect(requests).toHaveLength(2))
    await screen.findByRole('alert')
    expect(requests[0]!.idempotency_key).toBe(requests[1]!.idempotency_key)
    enter('Edited goal')
    submit()
    await waitFor(() => expect(requests).toHaveLength(3))
    await screen.findByRole('alert')
    expect(requests[2]!.idempotency_key).not.toBe(requests[1]!.idempotency_key)
    enter('First goal')
    submit()
    await waitFor(() => expect(requests).toHaveLength(4))
    expect(requests[3]!.idempotency_key).not.toBe(requests[2]!.idempotency_key)
  })
  it('shows waiting details for a mission which is not active', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ ...goalResult, mission_state: 'proposed', waiting: 'Awaiting approval' })))
    wrap(<GoalForm />)
    enter('Waiting goal')
    submit()
    expect(await screen.findByText('Awaiting approval')).toBeInTheDocument()
  })
})

describe('runtime timeline', () => {
  it('shows the implementation events in fixture order and opens raw artifact text', async () => {
    wrap(<TaskTimeline mission="M-2" task={TASK} />)
    const timeline = screen.getByLabelText(`Timeline ${TASK}`)
    await waitFor(() => expect(within(timeline).getAllByRole('listitem').length).toBeGreaterThan(0))
    const expected = events.filter(e => e.task_id === TASK || ('inputs' in e.body_json && (e.body_json.inputs as { subject?: string }).subject === TASK))
    const rendered = within(timeline).getAllByRole('listitem')
    expect(rendered.map(e => e.getAttribute('data-event-type'))).toEqual(expected.map(e => e.event_type))
    expected.forEach((event, i) => expect(rendered[i]).toHaveTextContent(event.at))
    const persisted = expected.find(e => e.event_type === 'attempt_persisted')!
    const id = (persisted.body_json as { artifacts: string[] }).artifacts[0]!
    const link = within(timeline).getByRole('link', { name: id })
    expect(link).toHaveAttribute('href', `/api/v1/agentrt/artifacts/${encodeURIComponent(id)}`)
    fireEvent.click(link)
    const panel = await screen.findByRole('region', { name: `Artifact ${id}` })
    expect(await within(panel).findByText(/Done <script>unsafe/)).toBeInTheDocument()
    expect(panel.querySelector('script')).toBeNull()
    fireEvent.click(within(panel).getByRole('button', { name: 'Close artifact' }))
    expect(screen.queryByRole('region', { name: `Artifact ${id}` })).not.toBeInTheDocument()
  })
})

describe('Agents page', () => {
  it('registers the nav and renders missions, recent implementation tasks, hosts and the plan', async () => {
    expect(SHELL_NAV.map(n => n.id).slice(-3)).toEqual(['progress', 'agents', 'records'])
    expect(resolveConsoleHash('agents').location).toEqual({ kind: 'shell', id: 'agents' })
    wrap(<AgentsPage />)
    expect(await screen.findByText('M-2 · 2.2 walkthrough 2: Cursor writes, Codex reviews (UniFi client tests)')).toBeInTheDocument()
    const titles = screen.getAllByRole('heading', { level: 4 }).map(h => h.textContent).filter(t => t?.startsWith('M-'))
    expect(titles.map(t => t!.slice(0, 3))).toEqual(['M-5', 'M-4', 'M-3', 'M-2', 'M-1'])
    expect(screen.getByText('mbp-dev')).toBeInTheDocument()
    expect(screen.getByText('No attempts running.')).toBeInTheDocument()
    expect(screen.getByText('Count: 0')).toBeInTheDocument()
    expect(await screen.findByText(/Round 2 · Checked:/)).toBeInTheDocument()
    expect(screen.getByText(/2.0 设计对齐：v4 到 v5 · 3\/3/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /UniFi client V2Post\/V2Delete tests/ }))
    expect(await screen.findByLabelText(`Timeline ${TASK}`)).toBeInTheDocument()
  })
  it('shows failures independently for all read sections', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'unavailable' }, 503)))
    wrap(<AgentsPage />)
    await waitFor(() => expect(screen.getAllByRole('alert')).toHaveLength(5))
    expect(screen.getByText(/Could not load missions:/)).toBeInTheDocument()
    expect(screen.getByText(/Could not load plan and architecture:/)).toBeInTheDocument()
  })
  it('shows empty data and an unpublished plan', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL) => json(String(url).includes('/agentrt/') ? { generated_at: 'now' } : { document: null })))
    wrap(<AgentsPage />)
    expect(await screen.findByText('No missions reported.')).toBeInTheDocument()
    expect(screen.getByText('No hosts reported.')).toBeInTheDocument()
    expect(await screen.findByText('The plan is not published in this environment.')).toBeInTheDocument()
  })
})
