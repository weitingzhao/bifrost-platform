import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => null }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => null }))
import type { ReactNode } from 'react'
import progress from '@/api/__fixtures__/agentrt-progress.stg.json'
import events from '@/api/__fixtures__/agentrt-events-M-2.stg.json'
import { ProgressPage } from '../../progress/ProgressPage'
import { GoalForm } from '../GoalForm'
import { TaskTimeline } from '../TaskTimeline'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED } from '../useRuntimePlan'
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
    // A runtime that holds no plan here: the panes say so; the board is retired and never read (2.5 ⑤).
    if (path.includes('/agentrt/plan?')) return json({ error: 'not_found', message: 'no such record' }, 404)
    if (path.includes('/agentrt/progress')) return json(progress)
    if (path.includes('/agentrt/events?')) return json(events)
    if (path.includes('/agentrt/artifacts/')) return new Response('# Report\nDone <script>unsafe()</script>', { headers: { 'Content-Type': 'text/markdown' } })
    if (path.includes('/approvals')) return json({ items: [] })
    if (path.includes('/questions')) return json({ questions: [] })
    if (path.includes('/agent/threads')) return json({ threads: [], hosts: [] })
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
    expect(await screen.findByText('M-6 created · View goal')).toBeInTheDocument()
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
  it('names the repositories checked: one as repo, several as repos, none not at all', async () => {
    const requests: Record<string, unknown>[] = []
    vi.stubGlobal('fetch', vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      requests.push(JSON.parse(init!.body as string))
      return json({ ...goalResult, repos: ['bifrost-platform', 'bifrost-trade-infra'] })
    }))
    wrap(<GoalForm />)
    const platform = screen.getByRole('checkbox', { name: 'bifrost-platform' })
    const infra = screen.getByRole('checkbox', { name: 'bifrost-trade-infra' })
    expect(platform).toBeChecked()
    expect(infra).not.toBeChecked()
    enter('Test the probe in both')
    fireEvent.click(infra)
    submit()
    await screen.findByText('M-6 created · View goal')
    expect(requests[0]).toMatchObject({ goal: 'Test the probe in both', repos: ['bifrost-platform', 'bifrost-trade-infra'] })
    expect(requests[0]).not.toHaveProperty('repo')
    enter('Only infra')
    fireEvent.click(platform)
    submit()
    await waitFor(() => expect(requests).toHaveLength(2))
    expect(requests[1]).toMatchObject({ goal: 'Only infra', repo: 'bifrost-trade-infra' })
    expect(requests[1]).not.toHaveProperty('repos')
    enter('Nowhere')
    fireEvent.click(infra)
    expect(screen.getByRole('button', { name: 'Submit' })).toBeDisabled()
  })
  it('generates a new key when the repositories change after a failure', async () => {
    const requests: { idempotency_key: string }[] = []
    vi.stubGlobal('fetch', vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      requests.push(JSON.parse(init!.body as string))
      throw new Error('Network disconnected')
    }))
    wrap(<GoalForm />)
    enter('A goal')
    submit()
    await screen.findByRole('alert')
    fireEvent.click(screen.getByRole('checkbox', { name: 'bifrost-trade-infra' }))
    submit()
    await waitFor(() => expect(requests).toHaveLength(2))
    expect(requests[1]!.idempotency_key).not.toBe(requests[0]!.idempotency_key)
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

describe('Merged Progress page', () => {
  it('redirects the former Agents route and renders hosts and the published plan', async () => {
    expect(SHELL_NAV.map(n => n.id).slice(-2)).toEqual(['infrastructure', 'records'])
    expect(resolveConsoleHash('agents')).toEqual({ location: { kind: 'shell', id: 'work' }, canonical: '#work' })
    wrap(<ProgressPage />)
    expect(await screen.findByText(/mbp-dev · Online/)).toBeInTheDocument()
    expect((await screen.findAllByText(PLAN_NOT_IMPORTED)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(ARCH_NOT_IMPORTED)).length).toBeGreaterThan(0)
    expect(vi.mocked(fetch).mock.calls.some(call => String(call[0]).includes('/agent/progress'))).toBe(false)
    // What waits on the Owner is For you's; new goals are given on Chief of staff (W-31 2.2b).
    expect(screen.queryByText('All on Needs you')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Chief of staff' })).toHaveAttribute('href', '#chief')
    // Finished work is in Records (W-31 2.2b).
    expect(screen.getByRole('link', { name: 'Records › Work history' })).toHaveAttribute('href', '#records?tab=work')
  })
  it('shows source failures and an incomplete count', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'unavailable' }, 503)))
    wrap(<ProgressPage />)
    expect((await screen.findAllByText(/Could not load agent runtime:/)).length).toBeGreaterThan(0)
    // The plan could not be read from the runtime: unavailable in the overview and the plan pane, not the board's (2.5 slice 2).
    expect(await screen.findAllByText('Plan unavailable: Plan: unavailable')).toHaveLength(2)
    expect(await screen.findAllByText('Architecture unavailable: Agent runtime: unavailable')).toHaveLength(2)
  })
  it('shows empty data and an unpublished plan', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL) => String(url).includes('/agentrt/plan?') ? json({ error: 'not_found', message: 'no such record' }, 404)
      : json(String(url).includes('/agentrt/') ? { generated_at: 'now' } : { document: null })))
    wrap(<ProgressPage />)
    expect(await screen.findByText('No goals yet.')).toBeInTheDocument()
    expect(screen.getByText('No hosts reported.')).toBeInTheDocument()
    expect((await screen.findAllByText(PLAN_NOT_IMPORTED)).length).toBeGreaterThan(0)
    expect((await screen.findAllByText(ARCH_NOT_IMPORTED)).length).toBeGreaterThan(0)
  })
})
