import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import marks from '@/api/__fixtures__/agentrt-progress-marks.testdb.json'
import { parseRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { manualAndStopped, runtimeRound } from '@/api/agentRuntimePlan'
import type { GoalData } from '@/api/agentRuntimeGoals'
import { GoalCard } from '../../progress/GoalCard'
import { stalledStage } from '../../progress/goalCardModel'
import { whoseMove } from '../../progress/mergedProgressModel'
import { ProgressOverview } from '../../progress/ProgressOverview'
import { OwnerCrew } from '../OwnerCrew'
import { OwnerGoals } from '../OwnerGoals'
import { activityCounts, attemptActivity, stalledWords } from '../attemptActivity'
import { ownerVerdict, workNow } from '../ownerModel'

// T24 (design v5 §5.5). The fixture is GET /agentrt/progress from real rows (store TestProgressCarriesT24sMarks, written
// with CONSOLE_MARKS_FIXTURE_WRITE=1): an attempt working on h1; one on h2, a host that stopped polling 10 minutes ago
// (stale, host lost, checkpoint 1, not persisted); one on h3 whose result is computed and not yet persisted (working);
// one on h4 whose lease lapsed while h4 still polls (maybe stuck); a task outside the plan; a step done by a manual record.
const progress: RuntimeProgress = parseRuntimeProgress(marks)
const now = Date.parse(progress.generated_at) + 30_000
const tasks = progress.work.missions.find(m => m.mission_id === 'M-1')!.tasks
const onHost = (host: string) => progress.live.find(a => a.host_id === host)!
const taskOn = (host: string) => tasks.find(t => t.task_id === onHost(host).task_id)!
const offPlan = tasks.find(t => t.off_plan === true)!

const mocks = vi.hoisted(() => ({ actions: vi.fn(), task: vi.fn() }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions }))
vi.mock('@/api/agentRuntime', async original => ({ ...await original<object>(), fetchRuntimeTaskDetail: mocks.task }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.actions.mockResolvedValue({ attempt_id: 'a', actions: [], next: '', unavailable: false })
  mocks.task.mockRejectedValue(new Error('404'))
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { headers: { 'Content-Type': 'application/json' } })))
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } })

describe('T24: attempts on a stopped host are not doing', () => {
  it('tells each live attempt by what the runtime says of it', () => {
    expect(['h1', 'h2', 'h3', 'h4'].map(h => attemptActivity(onHost(h)))).toEqual(['running', 'host_lost', 'running', 'maybe_stuck'])
    expect(activityCounts(progress.live)).toBe('2 running · 1 host lost · 1 maybe stuck')
    expect(stalledWords(onHost('h2'))).toBe('Host lost: host h2 last polled 10 minutes ago')
    expect(stalledWords(onHost('h4'))).toBe('Maybe stuck: the lease lapsed and was not renewed')
    expect(stalledWords(onHost('h1'))).toBe('')
    // A platform before statuses (status ''): by its state, a stale one maybe stuck (review 471 C1).
    expect(attemptActivity({ ...onHost('h1'), status: '' })).toBe('running')
    expect(attemptActivity({ ...onHost('h1'), status: '', state: 'stale' })).toBe('maybe_stuck')
    // A stale attempt is maybe stuck whatever status it carries; an unknown status is not running either.
    expect(attemptActivity({ ...onHost('h1'), state: 'stale', status: 'working' })).toBe('maybe_stuck')
    expect(attemptActivity({ ...onHost('h1'), status: 'unknown', reason: 'the attempt names no host the runtime knows' })).toBe('unknown')
    expect(stalledWords({ ...onHost('h1'), status: 'unknown', reason: 'the attempt names no host the runtime knows' }))
      .toBe('Status unknown: the attempt names no host the runtime knows')
  })

  it('counts on the progress overview only the attempts running, says which are not, and how many done steps are manual', () => {
    const plan = { from: 'runtime' as const, round: runtimeRound({ items: [{ key: '2.1', name: 'data layer', steps: [] }] }, progress.plan, 'R2', null) }
    render(<QueryClientProvider client={client()}><ProgressOverview count={{ state: 'known', count: 0 }} runtime={progress} plan={plan} arch={{ from: 'absent' }} failed={false} /></QueryClientProvider>)
    const running = screen.getByText('Running').parentElement!
    expect(within(running).getByText('2')).toBeInTheDocument()
    expect(within(running).getByText('Not confirmed running: 1 host lost · 1 maybe stuck')).toBeInTheDocument()
    expect(screen.getByText('1 of the done steps manual')).toBeInTheDocument()
  })

  it('counts on Work and in the verdict only the attempts running', () => {
    const work = workNow(progress)!
    expect(work).toMatchObject({ running: 2, stalled: 2, stalledWords: '1 host lost · 1 maybe stuck' })
    const base = { yours: { state: 'known' as const, count: 0 }, approvals: 0, lane: 'release lane free', nodes: { ready: 5, total: 5 } }
    // Not counted as running is not called stopped: its executor may still run (T22 ②; review 471 B1).
    expect(ownerVerdict({ ...base, running: work.running, stalled: work.stalled, stalledWords: work.stalledWords }).line)
      .toBe('Nothing to approve · 2 agents running, 2 agents not confirmed running (1 host lost · 1 maybe stuck) · release lane free · 5 of 5 nodes ready')
    expect(ownerVerdict({ ...base, running: 0, stalled: 1 }).line).toContain(' · 1 agent not confirmed running · ')
    expect(ownerVerdict({ ...base, running: 0, stalled: 0 }).line).toContain(' · agents idle · ')
    expect(ownerVerdict({ ...base, running: 0 }).line).toContain(' · agents idle · ')
  })

  it('says whose move it is from attempts confirmed running and tasks not stalled (review 471 A1)', () => {
    const only = (hosts: string[], extraTasks: string[] = []): RuntimeProgress => {
      const ids = new Set([...hosts.map(h => onHost(h).task_id), ...extraTasks])
      return { ...progress, live: progress.live.filter(a => hosts.includes(a.host_id)),
        work: { missions: progress.work.missions.filter(m => m.mission_id === 'M-1').map(m => ({ ...m, tasks: m.tasks.filter(t => ids.has(t.task_id)) })) } }
    }
    const zero = { state: 'known' as const, count: 0 }
    expect(progress.work.missions.find(m => m.mission_id === 'M-1')!.waiting_on).toBe('coordinator')
    expect(whoseMove(zero, only(['h2']))).toBe("Coordinator's move") // only an attempt on a lost host
    expect(whoseMove(zero, only(['h4']))).toBe("Coordinator's move") // only one maybe stuck
    expect(whoseMove(zero, only(['h2', 'h1']))).toBe("Agents' move") // one confirmed running beside it
    expect(whoseMove(zero, only(['h2'], [offPlan.task_id]))).toBe("Agents' move") // a queued task a host can claim
    expect(whoseMove(zero, progress)).toBe("Agents' move")
  })

  it('says a goal stage whose only work is on a lost host is host lost, not running (review 471 B2)', () => {
    const gone = taskOn('h2')
    const data: GoalData = { mission: { mission_id: 'M-1', state: 'active', goal: 'test', created_at: '2026-10-21T17:20:00Z', created_by_actor_id: 'controller' },
      events: [], tasks: [{ ...gone, created_at: '2026-10-21T17:20:00Z', updated_at: '2026-10-21T17:20:00Z', subject_task_id: '' }] }
    const c = client()
    c.setQueryData(['agent-runtime-goal', 'M-1'], data)
    render(<QueryClientProvider client={c}><GoalCard id="M-1" runtime={progress} updatedAt={1} questions={[]} /></QueryClientProvider>)
    const stage = screen.getByRole('listitem', { name: 'Implement: 1 host lost' })
    expect(within(stage).getByText('1 host lost: not confirmed running')).toBeInTheDocument()
    expect(within(stage).getByText('Host lost: host h2 last polled 10 minutes ago')).toBeInTheDocument()
    expect(screen.queryByRole('listitem', { name: 'Implement: running' })).not.toBeInTheDocument()
    expect(screen.getByText('Implementing · 1 host lost')).toBeInTheDocument()
    expect(stalledStage({ label: 'Implement', state: 'done', tasks: data.tasks, completed: 1, dropped: 0 }, progress.live)).toBe('')
  })

  it('keeps a blocked goal\'s lamp failed when its stalled words follow its status (review 472 B2-R1)', () => {
    const gone = taskOn('h2')
    const data: GoalData = { mission: { mission_id: 'M-1', state: 'paused', goal: 'test', created_at: '2026-10-21T17:20:00Z', created_by_actor_id: 'controller' },
      events: [], tasks: [{ ...gone, created_at: '2026-10-21T17:20:00Z', updated_at: '2026-10-21T17:20:00Z', subject_task_id: '' }] }
    const c = client()
    c.setQueryData(['agent-runtime-goal', 'M-1'], data)
    const { container } = render(<QueryClientProvider client={c}><GoalCard id="M-1" runtime={progress} updatedAt={1} questions={[]} /></QueryClientProvider>)
    expect(screen.getByText('Blocked · 1 host lost')).toBeInTheDocument()
    expect(container.querySelector('header .status-lamp')).toHaveClass('lamp-fail')
  })

  it('shows the host that stopped polling with its attempt host lost, not running, and its result not persisted', () => {
    render(<QueryClientProvider client={client()}><OwnerCrew hosts={progress.hosts} live={progress.live} missions={progress.work.missions} now={now} read="ok" /></QueryClientProvider>)
    const row = (host: string) => screen.getAllByRole('listitem').find(li => within(li).queryByText(host, { selector: 'strong' }))!
    expect(within(row('h2')).getByText('1 host lost')).toBeInTheDocument()
    expect(within(row('h2')).getByText('host lost · last poll 10 min ago')).toBeInTheDocument()
    expect(within(row('h1')).getByText('online')).toBeInTheDocument()
    expect(within(row('h5')).getByText('never polled')).toBeInTheDocument()
    expect(within(row('h2')).getByText('Host lost: host h2 last polled 10 minutes ago')).toBeInTheDocument()
    expect(within(row('h2')).getByText(/^Result not persisted: last checkpoint 1 /)).toBeInTheDocument()
    expect(within(row('h2')).queryByText(/\d running/)).not.toBeInTheDocument()
    expect(within(row('h4')).getByText('1 maybe stuck')).toBeInTheDocument()
    expect(within(row('h1')).getByText('1 running')).toBeInTheDocument()
    expect(within(row('h3')).getByText('1 running')).toBeInTheDocument()
    expect(within(row('h5')).getByText('Idle')).toBeInTheDocument()
    expect(screen.getByText('Crew · 3 of 8 hosts online')).toBeInTheDocument()
  })
})

describe('T24: the three marks', () => {
  it('reads off plan with its reason and computed not persisted from the runtime', () => {
    expect(offPlan).toMatchObject({ off_plan: true, off_plan_reason: 'T24 drill: an errand outside the plan', status: 'queued' })
    expect(taskOn('h3')).toMatchObject({ computed_not_persisted: true, off_plan: false })
    expect(tasks.filter(t => t.off_plan === true)).toHaveLength(1)
    expect(tasks.filter(t => t.computed_not_persisted === true)).toHaveLength(1)
    // An answer that does not say leaves them unset, never false.
    const old = parseRuntimeProgress({ work: { missions: [{ mission_id: 'M-1', tasks: [{ task_id: 't' }] }] } }).work.missions[0].tasks[0]
    expect(old.off_plan).toBeUndefined()
    expect(old.computed_not_persisted).toBeUndefined()
  })

  it('marks the goal with work off plan on Work, with why on hover', () => {
    render(<OwnerGoals missions={progress.work.missions} now={now} read="ok" />)
    // Why is in the text, not on hover only: a phone has no hover (review 471 B3).
    expect(screen.getByText('1 of 5 tasks off plan').parentElement).toHaveTextContent('1 of 5 tasks off plan T24 drill: an errand outside the plan')
    expect(screen.getAllByText(/off plan/i)).toHaveLength(1) // M-2 has none
    // v4 §20.3 rule 7: while a task is on a lost host the goal's time is unknown, and says which task.
    expect(screen.getByText(`Done by: unknown — task ${onHost('h2').task_id} is host lost: no time can be told until it is heard from`)).toBeInTheDocument()
  })

  it('counts the step done by a manual record as done and marks it manual', () => {
    const round = runtimeRound({ items: [{ key: '2.1', name: 'data layer', steps: [] }] }, progress.plan, 'R2', null)
    expect(round.items[0]).toMatchObject({ done: 1, total: 4, manual: 1, stopped: 0 })
    expect(manualAndStopped(round.items[0])).toBe(' · 1 manual')
    expect(manualAndStopped({ manual: 0, stopped: 2 })).toBe(' · 2 stopped')
    // A count from a platform before manual and stopped leaves them unset.
    const before = runtimeRound({ items: [{ key: '2.1', name: 'data layer', steps: [] }] },
      [{ plan_node_id: 'R2', kind: 'round', items: [{ label: '2.1', count: { done: 1, total: 4, state: 'active' } }] }], 'R2', null)
    expect(before.items[0].manual).toBeUndefined()
    expect(manualAndStopped(before.items[0])).toBe('')
  })

  it('says on the goal card which task is off plan, which attempt is host lost or maybe stuck, and which result is not yet persisted', () => {
    const data: GoalData = { mission: { mission_id: 'M-1', state: 'active', goal: 'test', created_at: '2026-10-21T17:20:00Z', created_by_actor_id: 'controller' },
      events: [], tasks: tasks.map(t => ({ ...t, created_at: '2026-10-21T17:20:00Z', updated_at: '2026-10-21T17:20:00Z', subject_task_id: '' })) }
    const c = client()
    c.setQueryData(['agent-runtime-goal', 'M-1'], data)
    render(<QueryClientProvider client={c}><GoalCard id="M-1" runtime={progress} updatedAt={1} questions={[]} /></QueryClientProvider>)
    const stage = screen.getByRole('listitem', { name: 'Implement: running' })
    expect(within(stage).getByText('Off plan').parentElement).toHaveTextContent('Off plan T24 drill: an errand outside the plan')
    expect(within(stage).getByText('Host lost: host h2 last polled 10 minutes ago')).toBeInTheDocument()
    expect(within(stage).getByText('Maybe stuck: the lease lapsed and was not renewed')).toBeInTheDocument()
    expect(within(stage).getAllByText('Result computed on the host, not yet persisted on the platform')).toHaveLength(1)
    expect(screen.getByText(`implement · ${offPlan.task_id} · ready · off plan`)).toBeInTheDocument()
  })
})
