import { RuntimeHosts } from '../../infrastructure/RuntimeHosts'
import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => null }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => null }))
import marks from '@/api/__fixtures__/agentrt-progress-marks.testdb.json'
import mission from '@/api/__fixtures__/agentrt-mission-M-2.prod.json'
import tasks from '@/api/__fixtures__/agentrt-tasks-M-2.prod.json'
import poolsFixture from '../../owner/__tests__/fixtures/pools-utc.json'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { useNeedsYou } from '../../needs-you/useNeedsYou'
import { NeedsYouPage } from '../../needs-you/NeedsYouPage'
import { ProgressPage } from '../ProgressPage'
import { ProgressHistory } from '../ProgressHistory'
import { AttemptActions } from '../AttemptActions'
import { GoalCard } from '../GoalCard'
import { LastRead } from '../../owner/LastRead'
import { useOwnerConsole } from '../../owner/useOwnerConsole'

// Review 488 (W-31 2.5-2): a read that fails after a good one, or before any, is said as failed, and nothing on the
// page goes on telling the last answer as the present. GET /progress is the marks moment the platform answered on a
// test database (agentrtapi T24 marks): four attempts live, one on a lost host, one maybe stuck, two working.

const source = vi.hoisted(() => ({ progress: true, open: true, all: true, threads: true, questions: true }))
function json(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
const down = () => json({ error: 'unavailable', message: 'the agent runtime database is not connected yet; the platform keeps trying' }, 503)
const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  if (url.includes('/agentrt/progress')) return source.progress ? json(marks) : down()
  if (url.includes('/approvals?status=open')) return source.open ? json(buildApprovalListResponse([buildPlatformApproval({ id: 'ap-16', number: 16 })])) : json({ error: 'approvals offline' }, 503)
  if (url.includes('/approvals')) return source.all ? json(buildApprovalListResponse([])) : json({ error: 'approvals offline' }, 503)
  if (url.includes('/questions')) return source.questions ? json({ questions: [] }) : json({ error: 'questions offline' }, 503)
  if (url.includes('/agent/threads')) return source.threads ? json({ threads: [], hosts: [] }) : json({ error: 'threads offline' }, 503)
  if (url.includes('/lineage')) return json({ threads: [], errors: [] })
  if (url.includes('/agentrt/plan?')) return json({ error: 'not_found', message: 'no such root' }, 404)
  if (url.includes('/agentrt/missions/')) return json(mission)
  if (url.includes('/agentrt/tasks?')) return json(tasks)
  if (url.includes('/agentrt/pools')) return json(poolsFixture)
  if (url.includes('/agentrt/events')) return json([])
  if (url.includes('/api/v1/releases')) return json({ status: {}, records: [] })
  // The runtime's answer while its database is not connected (agentrtapi configured): not the "no actions kept" 503.
  if (url.includes('/actions')) return json({ error: 'not_connected', message: 'the agent runtime database is not connected yet; the platform keeps trying' }, 503)
  return down()
})
const clients: QueryClient[] = []
function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
  return { client, ...render(<QueryClientProvider client={client}>{node}</QueryClientProvider>) }
}
beforeEach(() => {
  Object.assign(source, { progress: true, open: true, all: true, threads: true, questions: true })
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { onlineManager.setOnline(true); cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals(); fetchMock.mockClear() })

const runningCard = () => within(screen.getByRole('region', { name: 'Overview' })).getByText('Running').parentElement as HTMLElement

describe('Work while the runtime cannot be read (A1)', () => {
  it('tells what runs and the hosts from a good read, and nothing of it once a read fails', async () => {
    const { client } = show(<><ProgressPage /><section aria-label="Hosts"><RuntimeHosts /></section></>)
    await waitFor(() => expect(within(runningCard()).getByText('2')).toBeInTheDocument())
    expect(within(runningCard()).getByText('Not confirmed running: 1 host lost · 1 maybe stuck')).toBeInTheDocument()
    const hosts = screen.getByRole('region', { name: 'Hosts' })
    expect(within(hosts).getAllByRole('listitem').length).toBeGreaterThan(0)

    source.progress = false
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-progress'] }) })
    // Unknown, as Now and Crew say it, not the last answer's 2 and its lost host.
    await waitFor(() => expect(within(runningCard()).getByText('?')).toBeInTheDocument())
    expect(within(runningCard()).queryByText(/Not confirmed running/)).toBeNull()
    expect(within(hosts).queryAllByRole('listitem')).toHaveLength(0)
    expect(within(hosts).queryByText(/Runtime updated/)).toBeNull()
    expect(within(hosts).getByRole('alert')).toHaveTextContent(/^The agent runtime could not be read; last read \d\d:\d\d \(just now\)\. Error: /)
    expect(screen.getByRole('region', { name: 'Overview' })).toHaveTextContent(/Whose move.*Unknown.*The agent runtime could not be read; last read/)
  })

  it('keeps history as last read and says so, with when', async () => {
    const { client } = show(<ProgressHistory />)
    expect(await screen.findByText('Recent runtime tasks')).toBeInTheDocument()
    source.progress = false
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-progress'] }) })
    expect(await screen.findByText(/^The agent runtime could not be read; last read \d\d:\d\d \(just now\)\. Error: .*Below is what was read then\.$/)).toHaveAttribute('role', 'alert')
    expect(screen.getByText('Recent runtime tasks')).toBeInTheDocument()
    expect(screen.queryByText(/Runtime updated/)).toBeNull()
  })
})

describe('Needs you counts from lists that cannot be read (A2)', () => {
  it('says handled recently is unknown when any list it merges cannot be read, not a count of the others', async () => {
    for (const failing of ['threads', 'questions', 'all'] as const) {
      Object.assign(source, { threads: true, questions: true, all: true, [failing]: false })
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
      const { result, unmount } = renderHook(() => useNeedsYou(), { wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
      await waitFor(() => expect(result.current.recentCount.state).toBe('unknown'))
      unmount()
    }
  })

  it('says a group\'s count is unknown when the approvals list fails after a good read', async () => {
    const { client } = show(<NeedsYouPage />)
    expect(await screen.findByRole('region', { name: 'Decide' })).toBeInTheDocument()
    expect(screen.getByText('Decide · 1')).toBeInTheDocument()
    source.open = false
    await act(async () => { await client.refetchQueries({ queryKey: ['approvals', 'open'] }) })
    expect(await screen.findByText('Decide · ?')).toBeInTheDocument()
    expect(screen.getByText(/Could not read approvals — last tried \d\d:\d\d:\d\d The requests below are from the last read\./)).toBeInTheDocument()
  })

  it('says when the runtime was last read in its group, and that its count is incomplete', async () => {
    const { client } = show(<NeedsYouPage />)
    expect(await screen.findByText(/^Generated: /)).toBeInTheDocument()
    source.progress = false
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-progress'] }) })
    expect(await within(screen.getByRole('region', { name: 'Agent runtime' })).findByText(
      /^The agent runtime could not be read; last read \d\d:\d\d \(just now\)\. Count is incomplete; the requests below are from the last read\.$/)).toBeInTheDocument()
    expect(screen.queryByText(/^Generated: /)).toBeNull()
  })
})

describe('an attempt\'s actions that cannot be read (A4)', () => {
  it('says the read failed and never that there are no actions', async () => {
    const { container } = show(<AttemptActions id="a_1" />)
    const details = container.querySelector('details') as HTMLDetailsElement
    details.open = true
    fireEvent(details, new Event('toggle'))
    expect(await screen.findByText(/the agent runtime database is not connected yet/)).toHaveAttribute('role', 'alert')
    expect(screen.queryByText('no actions yet')).toBeNull()
    expect(screen.queryByText('Reading actions…')).toBeNull()
  })
})

// Review 493: a read paused offline is not a read either, and "not read since this page opened" counts from the
// page's or the card's own opening.
describe('reads paused offline (review 493 B1, C)', () => {
  it('a goal card says it is offline and when its details were read, not that it is loading or current', async () => {
    const { client } = show(<GoalCard id="M-2" updatedAt={0} questions={[]} />)
    await waitFor(() => expect(screen.queryByText('Loading mission details…')).toBeNull())
    expect(screen.queryByRole('alert')).toBeNull()
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: ['agent-runtime-goal', 'M-2'] })
    expect(await screen.findByText(/^Could not load M-2: offline; last read \d\d:\d\d \(just now\)\. Showing the details read then\./)).toHaveAttribute('role', 'alert')
    expect(screen.queryByText('Loading mission details…')).toBeNull()
  })

  it('Work\'s upper cards say the runtime could not be read while its read is paused, not "Reading"', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    const { result } = renderHook(() => useOwnerConsole({ system: false, work: true, chief: false }), {
      wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> })
    await waitFor(() => expect(result.current.runtimeReadAt).not.toBeNull())
    expect(result.current.runtimeError).toBe(false)
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: ['agent-runtime-progress'] })
    await waitFor(() => expect(result.current.runtimeError).toBe(true))
  })

  it('counts "not read since this page opened" from the opening it is given', () => {
    const opened = new Date()
    opened.setHours(10, 0, 0, 0)
    render(<p data-testid="line"><LastRead readAt={null} openedAt={opened.getTime()} /></p>)
    expect(screen.getByTestId('line')).toHaveTextContent(/^not read since this page opened at (\w+ \d+ )?10:00$/)
  })
})
