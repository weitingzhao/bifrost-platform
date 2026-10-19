import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress, parseRuntimeTaskDetail } from '@/api/agentRuntime'
import stg from '@/api/__fixtures__/agentrt-progress.stg.json'
import taskFixture from '@/api/__fixtures__/agentrt-task.stg.json'
const mocks = vi.hoisted(() => ({ actions: vi.fn(), task: vi.fn() }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions }))
vi.mock('@/api/agentRuntime', async original => ({ ...await original<object>(), fetchRuntimeTaskDetail: mocks.task }))
import { OwnerCrew, type CrewRead } from '../OwnerCrew'
import { crewRows, duration, firstLine } from '../crewModel'

// The hosts are STG's as the platform answered (2026-10-15); the live attempts carry the fields of
// store.ProgressAttempt (api/internal/agentrt/store/progress.go); a task's detail is GET /agentrt/tasks/{id}
// as STG answered it, its review titled as accept.go names one ("review t_<subject>").
const at = Date.parse('2026-10-15T23:29:00Z')
const detail = (over: Record<string, unknown>) => parseRuntimeTaskDetail({ ...taskFixture, ...over })
const details: Record<string, ReturnType<typeof detail>> = {
  t_impl: detail({ task_id: 't_impl', title: 'Hosts card on Work', role: 'implement', mission_id: 'M-7', subject_task_id: null }),
  t_rev: detail({ task_id: 't_rev', title: 'review t_impl', role: 'review', mission_id: 'M-7', subject_task_id: 't_impl' }),
  t_far: detail({ task_id: 't_far', title: 'A task of the 101st mission', role: 'implement', mission_id: 'M-101', subject_task_id: null }),
}
function progress(live: object[], extraHosts: object[] = []) {
  const missions = [{ mission_id: 'M-7', title: 'Crew view', goal: 'Show which agent works on each host\nand on what', state: 'active', tasks: [
    { task_id: 't_impl', title: 'Hosts card on Work', role: 'implement', state: 'running' },
    { task_id: 't_rev', title: 'review t_impl', role: 'review', state: 'running' }] }]
  return parseRuntimeProgress({ ...stg, hosts: [...stg.hosts, ...extraHosts], live, work: { missions } })
}
const attempt = (id: string, task: string, mission: string, over: object = {}) => ({ attempt_id: id, attempt_no: 1, task_id: task, mission_id: mission,
  host_id: 'mbp-dev', executor: 'exec:codex', model: 'gpt-x', state: 'running', elapsed_s: 754, heartbeat_age_s: 4, status: 'running', reason: '', ...over })
function show(p: ReturnType<typeof progress>, read: CrewRead = 'ok') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><OwnerCrew hosts={p.hosts} live={p.live} missions={p.work.missions} now={at} read={read} /></QueryClientProvider>)
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.actions.mockResolvedValue({ attempt_id: 'a', actions: [], next: '', unavailable: false })
  mocks.task.mockImplementation(async (id: string) => { if (!details[id]) throw new Error('404'); return details[id] })
})
afterEach(cleanup)

describe('crew', () => {
  it('shows each host, the busy one first, with what each attempt does and for which goal', async () => {
    show(progress([attempt('a_1', 't_impl', 'M-7')]))
    const hosts = screen.getAllByRole('listitem')
    expect(within(hosts[0]).getByText('mbp-dev')).toBeInTheDocument()
    expect(within(hosts[0]).getByText('1 running')).toBeInTheDocument()
    expect(within(hosts[0]).getByText(/Hosts card on Work/)).toBeInTheDocument()
    expect(within(hosts[0]).getByText('Implement')).toBeInTheDocument()
    expect(within(hosts[0]).getByText(/codex \(gpt-x\) · M-7 Crew view · running 12 min/)).toBeInTheDocument()
    expect(within(hosts[1]).getByText('gate-b')).toBeInTheDocument()
    expect(within(hosts[1]).getByText('Idle')).toBeInTheDocument()
    expect(within(hosts[1]).getByText(/last poll/)).toBeInTheDocument()
    expect(screen.getByText('Crew · 1 of 2 hosts online')).toBeInTheDocument()
    expect(await screen.findByText('no actions yet')).toBeInTheDocument()
    expect(screen.queryByText('codex · mbp-dev')).not.toBeInTheDocument() // the action line does not say who again
  })
  it('names a review by what it reviews (review 289 B3)', async () => {
    show(progress([attempt('a_2', 't_rev', 'M-7', { executor: 'exec:claude', model: '' })]))
    expect(await screen.findByText(/Hosts card on Work/)).toBeInTheDocument()
    expect(screen.getByText('Review')).toBeInTheDocument()
    expect(screen.queryByText(/review t_impl/)).not.toBeInTheDocument()
  })
  it('reads the task of a mission the progress did not list, and says when it cannot (review 289 B2)', async () => {
    show(progress([attempt('a_3', 't_far', 'M-101'), attempt('a_4', 't_gone', 'M-102', { host_id: 'agent-x', heartbeat_age_s: 300, elapsed_s: 4000 })]))
    expect(await screen.findByText(/A task of the 101st mission/)).toBeInTheDocument()
    expect(await screen.findByText(/its task could not be read/)).toBeInTheDocument()
    expect(screen.getByText(/t_gone/)).toBeInTheDocument()
    expect(screen.getByText(/no heartbeat for 5 min/)).toBeInTheDocument()
    expect(screen.getByText('not a registered host')).toBeInTheDocument()
    expect(screen.getByText(/running 1 h 6 min/)).toBeInTheDocument()
  })
  it('says nothing of hosts while the runtime is not read (review 289 B1)', () => {
    const p = progress([])
    show(p, 'loading')
    expect(screen.getByText('Reading the agent runtime…')).toBeInTheDocument()
    expect(screen.queryByText(/No agent host/)).not.toBeInTheDocument()
    expect(screen.queryByText('Idle')).not.toBeInTheDocument()
    cleanup()
    show(parseRuntimeProgress({}), 'error')
    expect(screen.getByRole('alert')).toHaveTextContent('could not be read')
    expect(screen.queryByText(/No agent host/)).not.toBeInTheDocument()
    cleanup()
    show(parseRuntimeProgress({ hosts: [], live: [], work: { missions: [] } }))
    expect(screen.getByText('No agent host has polled this platform.')).toBeInTheDocument()
  })
  it('leaves retired hosts out', () => {
    expect(crewRows(progress([], [{ host_id: 'old', state: 'retired', online: false }]).hosts, []).map(r => r.id)).toEqual(['gate-b', 'mbp-dev'])
  })
  it('says durations and goals as people read them', () => {
    expect([duration(undefined), duration(42), duration(754), duration(3660)]).toEqual(['not started', '42 s', '12 min', '1 h 1 min'])
    expect(firstLine('Show which agent\nworks where')).toBe('Show which agent')
    expect(firstLine('x'.repeat(100))).toHaveLength(80)
  })
})
