import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import stg from '@/api/__fixtures__/agentrt-progress.stg.json'
const mocks = vi.hoisted(() => ({ actions: vi.fn() }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions }))
import { OwnerCrew } from '../OwnerCrew'
import { crewRows, duration } from '../crewModel'

// The hosts are STG's as the platform answered (2026-10-15); the live attempts carry the fields of
// store.ProgressAttempt (api/internal/agentrt/store/progress.go).
const at = Date.parse('2026-10-15T23:29:00Z')
function progress(live: object[], extraHosts: object[] = []) {
  const missions = [{ mission_id: 'M-7', title: 'Crew view', state: 'active', tasks: [
    { task_id: 't_impl', title: 'Hosts card on Work', role: 'implement', state: 'running' },
    { task_id: 't_rev', title: 'Review the hosts card', role: 'review', state: 'running' }] }]
  return parseRuntimeProgress({ ...stg, hosts: [...stg.hosts, ...extraHosts], live, work: { missions } })
}
function show(p: ReturnType<typeof progress>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><OwnerCrew hosts={p.hosts} live={p.live} missions={p.work.missions} now={at} /></QueryClientProvider>)
}
beforeEach(() => { vi.clearAllMocks(); mocks.actions.mockResolvedValue({ attempt_id: 'a', actions: [], next: '', unavailable: false }) })
afterEach(cleanup)

describe('crew', () => {
  it('shows each host, the busy one first, with what each attempt does', async () => {
    const p = progress([{ attempt_id: 'a_1', attempt_no: 1, task_id: 't_impl', mission_id: 'M-7', host_id: 'mbp-dev', executor: 'exec:codex',
      model: 'gpt-x', state: 'running', elapsed_s: 754, heartbeat_age_s: 4, status: 'running', reason: '' }])
    show(p)
    const hosts = screen.getAllByRole('listitem')
    expect(within(hosts[0]).getByText('mbp-dev')).toBeInTheDocument()
    expect(within(hosts[0]).getByText('1 running')).toBeInTheDocument()
    expect(within(hosts[0]).getByText(/Hosts card on Work/)).toBeInTheDocument()
    expect(within(hosts[0]).getByText('Implement')).toBeInTheDocument()
    expect(within(hosts[0]).getByText(/codex \(gpt-x\) · M-7 · running 12 min/)).toBeInTheDocument()
    expect(within(hosts[1]).getByText('gate-b')).toBeInTheDocument()
    expect(within(hosts[1]).getByText('Idle')).toBeInTheDocument()
    expect(within(hosts[1]).getByText(/last poll/)).toBeInTheDocument()
    expect(screen.getByText('Crew · 1 of 2 hosts online')).toBeInTheDocument()
    expect(await screen.findByText('no actions yet')).toBeInTheDocument()
    expect(screen.queryByText('codex · mbp-dev')).not.toBeInTheDocument() // the action line does not say who again
  })
  it('marks a late heartbeat and keeps an attempt whose host is not registered', () => {
    show(progress([{ attempt_id: 'a_2', task_id: 't_rev', mission_id: 'M-7', host_id: 'agent-x', executor: 'exec:claude',
      state: 'running', elapsed_s: 4000, heartbeat_age_s: 300, status: 'running', reason: '' }]))
    expect(screen.getByText(/no heartbeat for 5 min/)).toBeInTheDocument()
    expect(screen.getByText('not a registered host')).toBeInTheDocument()
    expect(screen.getByText('Review')).toBeInTheDocument()
    expect(screen.getByText(/claude · M-7 · running 1 h 6 min/)).toBeInTheDocument()
  })
  it('leaves retired hosts out and says when no host polled', () => {
    expect(crewRows(progress([], [{ host_id: 'old', state: 'retired', online: false }]).hosts, []).map(r => r.id)).toEqual(['gate-b', 'mbp-dev'])
    const p = parseRuntimeProgress({ hosts: [], live: [], work: { missions: [] } })
    show(p)
    expect(screen.getByText('No agent host has polled this platform.')).toBeInTheDocument()
    expect(screen.getByText('Crew')).toBeInTheDocument()
  })
  it('says durations as people read them', () => {
    expect([duration(undefined), duration(42), duration(754), duration(3660)]).toEqual(['not started', '42 s', '12 min', '1 h 1 min'])
  })
})
