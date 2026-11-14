import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import { fetchImportRequest, fetchRuntimePlan } from '@/api/agentRuntimePlan'
import { CrewOverview } from '../CrewOverview'
import { DEVELOPMENT_REVIEW, developmentRead, R4_HREF } from '../developmentModel'
import { useRuntimePlan } from '../../agents/useRuntimePlan'

vi.mock('@/api/agentRuntime', async original => ({ ...await original<object>(), fetchRuntimeProgress: vi.fn() }))
vi.mock('@/api/agentRuntimePlan', async original => ({ ...await original<object>(), fetchRuntimePlan: vi.fn(), fetchImportRequest: vi.fn() }))
const NOW = Date.now()
function progress(done = 1, state = 'active') {
  return parseRuntimeProgress({ generated_at: new Date(NOW).toISOString(), work: { missions: [{ mission_id: 'M-143', state: 'done', title: 'Verified implementation' }] }, plan: [
    { kind: 'round', plan_node_id: 'R4', items: [{ label: '4.1', count: { done, total: 6, manual: 1, stopped: 0, state } }] },
    { kind: 'round', plan_node_id: 'R5', items: [] },
  ] })
}
const doc = (root = 'R4') => ({ root, nodes: [
  { plan_node_id: root, node_kind: 'round', title: root },
  ...(root === 'R4' ? [{ plan_node_id: 'R4:4.1', parent_plan_node_id: 'R4', node_kind: 'item', label: '4.1', title: 'Automatic landing' }] : []),
] })
let client: QueryClient
beforeEach(() => {
  vi.clearAllMocks()
  window.location.hash = '#crew'
  vi.mocked(fetchRuntimeProgress).mockResolvedValue(progress())
  vi.mocked(fetchRuntimePlan).mockImplementation(async root => ({ found: true, doc: doc(root) }))
  vi.mocked(fetchImportRequest).mockResolvedValue(null)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => { cleanup(); client.clear() })
const show = (now = NOW) => render(<QueryClientProvider client={client}><CrewOverview now={now} /></QueryClientProvider>)

describe('Crew development evidence', () => {
  it('keeps historical proof, current observations and full-chain acceptance distinct', async () => {
    show()
    expect(await screen.findByText(/Plan state: doing · 1\/6/)).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Next breakthrough' })).toHaveTextContent('main was merged manually')
    expect(screen.getByRole('region', { name: 'Next breakthrough' })).toHaveTextContent('Full-chain proof not established')
    expect(screen.getAllByText(new RegExp(DEVELOPMENT_REVIEW)).length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: 'M-143 · Verified implementation' })).toHaveAttribute('href', '#work?goal=M-143')
    expect(screen.getByRole('link', { name: /Inspect Round 4 steps/ })).toHaveAttribute('href', R4_HREF)
    expect(screen.queryByText(/55 planned|100%|maturity: /i)).not.toBeInTheDocument()
    expect(screen.getByText(/ROI improvement is not yet established/)).toBeInTheDocument()
  })
  it('refreshes the shared runtime data without promoting a capability when all steps are done', async () => {
    show()
    await screen.findByText(/Plan state: doing · 1\/6/)
    vi.mocked(fetchRuntimeProgress).mockResolvedValue(progress(6, 'done'))
    fireEvent.click(screen.getByRole('button', { name: 'Refresh evidence' }))
    expect(await screen.findByText(/Plan state: done · 6\/6/)).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Next breakthrough' })).toHaveTextContent('Full-chain proof not established')
    expect(client.getQueryData(['agent-runtime-progress'])).toEqual(progress(6, 'done'))
  })
  it('clears cached current claims after a refresh fails but retains historical M-143 evidence', async () => {
    show()
    await screen.findByText(/Plan state: doing · 1\/6/)
    vi.mocked(fetchRuntimeProgress).mockRejectedValue(new Error('viewer token required'))
    fireEvent.click(screen.getByRole('button', { name: 'Refresh evidence' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('viewer token required')
    expect(screen.queryByText(/Plan state: doing/)).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'M-143 · Verified implementation' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'M-143 · engineering collaboration' })).toBeInTheDocument()
  })
  it('does not turn a plan read failure into a capability result', async () => {
    vi.mocked(fetchRuntimePlan).mockRejectedValue(new Error('plan denied'))
    show()
    expect(await screen.findByRole('alert')).toHaveTextContent('plan denied')
    expect(screen.queryByText(/Plan state: doing/)).not.toBeInTheDocument()
  })
  it('distinguishes a missing plan and an empty result window', async () => {
    vi.mocked(fetchRuntimePlan).mockResolvedValue({ found: false })
    show()
    expect(await screen.findByText('Round 4 is not imported in this environment.')).toBeInTheDocument()
    expect(screen.queryByText(/No completed goals in this summary/)).not.toBeInTheDocument()
  })
  it('labels an old server snapshot stale even after a successful network read', async () => {
    vi.mocked(fetchRuntimeProgress).mockResolvedValue({ ...progress(), generated_at: new Date(NOW - 180_000).toISOString() })
    show()
    expect(await screen.findByText(/last complete reading is over two minutes old/)).toBeInTheDocument()
    expect(screen.queryByText(/Plan state: doing/)).not.toBeInTheDocument()
  })
  it('does not substitute missing R4 items or zero goals for missing evidence', async () => {
    const data = progress(); data.work.missions = []
    vi.mocked(fetchRuntimeProgress).mockResolvedValue(data)
    show()
    await screen.findByText(/No completed goals in this summary window/)
    expect(within(screen.getByRole('region', { name: 'Round 4.2' })).getByText('No matching item in this runtime plan.')).toBeInTheDocument()
  })
  it('keeps counts available when immutable imported ledger lines cannot be read', async () => {
    vi.mocked(fetchImportRequest).mockRejectedValue(new Error('ledger denied'))
    show()
    expect(await screen.findByText(/Plan state: doing · 1\/6/)).toBeInTheDocument()
    expect(screen.getByText(/Imported ledger evidence could not be read/)).toHaveTextContent('ledger denied')
  })
})

describe('freshness and offline semantics', () => {
  const q = { data: progress(), isError: false, isPaused: false, error: null, dataUpdatedAt: NOW }
  const plan = { from: 'runtime', round: { n: 4, items: [] } } as const
  it('reports offline even when a cached success exists', () => {
    expect(developmentRead({ ...q, isPaused: true }, { from: 'runtime', round: { n: 4, items: [] } }, NOW, NOW).state).toBe('offline')
  })
  it('reports an offline plan read separately from a request error', () => {
    expect(developmentRead(q, { from: 'error', error: 'offline' }, NOW, NOW).state).toBe('offline')
  })
  it.each(['', 'invalid', new Date(NOW + 180_000).toISOString()])('does not trust missing, malformed or future generated_at (%s)', generated_at => {
    expect(developmentRead({ ...q, data: { ...progress(), generated_at } }, { ...plan, round: { n: 4, items: [] } }, NOW, NOW).state).toBe('unknown')
  })
  it('expires the plan read independently of a fresh progress poll', () => {
    expect(developmentRead(q, { ...plan, round: { n: 4, items: [] } }, NOW - 180_000, NOW).state).toBe('stale')
  })
})

it('Work deep links keep R4 selected when the newest round is R5 and resume latest on navigation', async () => {
  window.location.hash = R4_HREF
  function PlanProbe() { const p = useRuntimePlan(); return <p>{p.from === 'runtime' ? `Round ${p.round.n}` : p.from}</p> }
  render(<QueryClientProvider client={client}><PlanProbe /></QueryClientProvider>)
  expect(await screen.findByText('Round 4')).toBeInTheDocument()
  act(() => { window.location.hash = '#work?section=plan'; window.dispatchEvent(new HashChangeEvent('hashchange')) })
  await waitFor(() => expect(screen.getByText('Round 5')).toBeInTheDocument())
})
