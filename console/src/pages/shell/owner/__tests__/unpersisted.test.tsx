import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import persist from '@/api/__fixtures__/agentrt-progress-persist.testdb.json'
import mission from '@/api/__fixtures__/agentrt-mission-M-2.prod.json'
import tasks from '@/api/__fixtures__/agentrt-tasks-M-2.prod.json'
import events from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import { parseRuntimeEvents, parseRuntimeProgress, type RuntimeAttempt, type RuntimeProgress } from '@/api/agentRuntime'
import { parseGoalTasks } from '@/api/agentRuntimeGoals'
import { GoalCard } from '../../progress/GoalCard'
import { OwnerCrew } from '../OwnerCrew'
import { unpersistedWords } from '../crewModel'

// TD-352 (design T34 ⑤; Gate A STG 2026-10-15 M-5: gate-b checkpointed once and went away for good). The fixture is
// GET /agentrt/progress from real rows (store TestProgressSaysWhatOfAnAttemptIsPersisted, written with
// CONSOLE_PERSIST_FIXTURE_WRITE=1): when the attempt on h2 is stale after checkpoint 1, and when it is lost.
const stale = parseRuntimeProgress(persist.stale)
const lostDoc = parseRuntimeProgress(persist.lost)
const goneTask = persist.gone_task, goneAttempt = persist.gone_attempt
const taskOf = (p: RuntimeProgress) => p.work.missions.flatMap(m => m.tasks).find(t => t.task_id === goneTask)!
const nowOf = (p: RuntimeProgress) => Date.parse(p.generated_at) + 5 * 60_000

const mocks = vi.hoisted(() => ({ actions: vi.fn(), task: vi.fn() }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions }))
vi.mock('@/api/agentRuntime', async original => ({ ...await original<object>(), fetchRuntimeTaskDetail: mocks.task }))

function crew(p: RuntimeProgress) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><OwnerCrew hosts={p.hosts} live={p.live} missions={p.work.missions} now={nowOf(p)} read="ok" /></QueryClientProvider>)
  return screen.getAllByRole('listitem').find(li => within(li).queryByText('h2'))!
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.actions.mockResolvedValue({ attempt_id: 'a', actions: [], next: '', unavailable: false })
  mocks.task.mockRejectedValue(new Error('404'))
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { headers: { 'Content-Type': 'application/json' } })))
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('what of an attempt is persisted', () => {
  it('reads the fields the platform gives a live and a lost attempt', () => {
    const live = stale.live.find(a => a.attempt_id === goneAttempt)!
    expect(live).toMatchObject({ state: 'stale', attempt_no: 1, persisted: false, last_checkpoint_seq: 1 })
    expect(Date.parse(live.last_checkpoint_at!)).toBeGreaterThan(0)
    expect(live.unpersisted_for_s).toBeGreaterThanOrEqual(0)
    expect(taskOf(stale).reason).toMatch(/^the lease lapsed and was not renewed; result not persisted \(last checkpoint 1 at /)
    const lost = taskOf(lostDoc).lost_attempt!
    expect(lost).toMatchObject({ attempt_id: goneAttempt, state: 'lost', persisted: false, last_checkpoint_seq: 1 })
    expect(lost.ended_at).toBeTruthy()
    expect(taskOf(lostDoc).reason).toBe(`ready for a host to claim; ${lost.reason}`)
  })

  it('says a stale, lost or host-lost attempt with its result not persisted, and nothing else', () => {
    const now = Date.parse('2026-10-15T22:30:00Z')
    const a = (over: Partial<RuntimeAttempt>): RuntimeAttempt => ({ attempt_id: 'a', task_id: 't', mission_id: 'M-5', host_id: 'gate-b',
      executor: 'exec:codex', model: '', state: 'stale', status: 'maybe_stuck', reason: '', persisted: false, ...over })
    expect(unpersistedWords(a({ last_checkpoint_seq: 1, last_checkpoint_at: '2026-10-15T22:09:43Z', unpersisted_for_s: 1217 }), now))
      .toBe('Result not persisted: last checkpoint 1 20 min ago, nothing persisted for 20 min')
    expect(unpersistedWords(a({ unpersisted_for_s: 70 }), now)).toBe('Result not persisted: no checkpoint, nothing persisted for 1 min')
    expect(unpersistedWords(a({ state: 'running', status: 'host_lost', unpersisted_for_s: 5 }), now)).toBe('Result not persisted: no checkpoint, nothing persisted for 5 s')
    expect(unpersistedWords(a({ state: 'lost', status: '' }), now)).toBe('Result not persisted: no checkpoint')
    expect(unpersistedWords(a({ persisted: true }), now)).toBe('')
    expect(unpersistedWords(a({ persisted: undefined }), now)).toBe('') // a platform before TD-352 says nothing
    expect(unpersistedWords(a({ state: 'running', status: 'working' }), now)).toBe('')
  })

  it('shows the stale attempt on its host with its result not persisted', () => {
    const h2 = crew(stale)
    expect(within(h2).getByText('1 running')).toBeInTheDocument()
    expect(within(h2).getByText(/^Result not persisted: last checkpoint 1 \d+ min ago, nothing persisted for /)).toBeInTheDocument()
  })

  it('keeps the lost attempt under its host, not done and not running', () => {
    const h2 = crew(lostDoc)
    expect(within(h2).getByText(/^Idle · 1 lost$/)).toBeInTheDocument()
    expect(within(h2).getByText('Lost')).toBeInTheDocument()
    expect(within(h2).getByText(/^Attempt 1 · codex · M-1 · lost \d+ min ago · not done$/)).toBeInTheDocument()
    expect(within(h2).getByText(/^Result not persisted: last checkpoint 1 \d+ min ago, nothing persisted for /)).toBeInTheDocument()
  })

  it('says so on the goal card of the task, as not done', () => {
    // M-2's implementation task (PROD fixture) as the runtime would list it after its attempt was lost on h2.
    const impl = 't_01M51EHZ92V5F2PRB7JCZVHTQ3'
    const lost = { ...persist.lost, work: { missions: [{ mission_id: 'M-2', tasks: [{ ...taskOf(lostDoc), task_id: impl,
      lost_attempt: { ...(persist.lost.work.missions.flatMap(m => m.tasks).find(t => t.task_id === goneTask) as { lost_attempt: object }).lost_attempt, task_id: impl } }] }] } }
    const runtime = parseRuntimeProgress(lost)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['agent-runtime-goal', 'M-2'], { mission: { ...mission, state: 'active' }, events: parseRuntimeEvents(events.filter(e => e.seq <= 26)),
      tasks: parseGoalTasks(tasks).filter(t => t.task_id === impl).map(t => ({ ...t, state: 'ready' })) })
    render(<QueryClientProvider client={client}><GoalCard id="M-2" runtime={runtime} updatedAt={1} questions={[]} /></QueryClientProvider>)
    expect(screen.getByText(/^Attempt 1 lost on h2, not done\. Result not persisted: last checkpoint 1 /)).toBeInTheDocument()
  })
})
