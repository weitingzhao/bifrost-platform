import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import mission from '@/api/__fixtures__/agentrt-mission-M-2.prod.json'
import tasks from '@/api/__fixtures__/agentrt-tasks-M-2.prod.json'
import events from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import { fetchRuntimeEvents, parseRuntimeEvents, parseRuntimeProgress } from '@/api/agentRuntime'
import { parseGoalTasks, type GoalData } from '@/api/agentRuntimeGoals'
import type { Question } from '@/api/questions'
import { questionHref, resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { GoalCard } from '../GoalCard'
import { goalDone, goalStages, goalStatus, goalWaitingLinks, missionIds, relative, taskTiming } from '../goalCardModel'
import { whoseMove } from '../mergedProgressModel'
import { ProgressPage } from '../ProgressPage'
vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => <p>Lineage content</p> }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => <p>Health content</p> }))
const full: GoalData = { mission, tasks: parseGoalTasks(tasks), events: parseRuntimeEvents(events) }
const clients: QueryClient[] = []
function wrap(ui: React.ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  clients.push(client)
  return { ...render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>), client }
}
function cut(seq: number): GoalData {
  const subset = events.filter(e => e.seq <= seq)
  const at = subset.at(-1)!.at
  return { mission, events: parseRuntimeEvents(subset), tasks: parseGoalTasks(tasks).filter(t => t.created_at <= at).map(t => {
    const changes = subset.filter(e => e.task_id === t.task_id && e.event_type === 'task_state_changed')
    const last = changes.at(-1)
    return { ...t, state: String(last && 'to' in last.body_json ? last.body_json.to : 'proposed') }
  }) }
}
function mockFetch() {
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    let data: unknown = {}
    if (url.includes('/agentrt/missions/')) data = { ...mission, mission_id: url.split('/').at(-1) }
    else if (url.includes('/agentrt/tasks?')) data = tasks
    else if (url.includes('/agentrt/events?stream=')) data = events
    else if (url.includes('/agentrt/events?')) data = { events: [], next: '0.0' }
    else if (url.includes('/agentrt/artifacts/')) return new Response('Fixture implementation report')
    else if (url.includes('/agentrt/goals') && init?.method === 'POST') data = { mission_id: 'M-20', mission_state: 'active', task_id: 't_new' }
    else if (url.includes('/agentrt/progress')) data = { work: { missions: [{ mission_id: 'M-2' }] }, needs_you: { items: [] } }
    else if (url.includes('/agent/progress')) data = { document: { coordinator: { state: 'idle' } } }
    else if (url.includes('/questions')) data = { questions: [] }
    else if (url.includes('/approvals')) data = { approvals: [] }
    else if (url.includes('/agent/threads')) data = { threads: [], hosts: [] }
    return new Response(JSON.stringify(data), { headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', fetch)
  return fetch
}
beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key), clear: () => values.clear(),
  } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals(); vi.restoreAllMocks(); window.history.replaceState(null, '', '/') })

describe('goal stages from the production stream', () => {
  it('shows all stages done when older active missions have every task accepted', () => {
    expect(goalDone(full)).toBe(true)
    expect(goalStages(full).map(s => s.state)).toEqual(['done', 'done', 'done', 'done', 'done', 'done'])
    expect(goalStatus(full, false)).toBe('Done')
  })
  it('moves from the accepted plan to implementation, then from checks to review', () => {
    const planAccepted = events.find(e => e.event_type === 'task_state_changed' && e.task_id === tasks.tasks[0]!.task_id && 'to' in e.body_json && e.body_json.to === 'accepted')!
    expect(goalStatus(cut(planAccepted.seq), false)).toBe('Implementing')
    expect(goalStages(cut(planAccepted.seq)).find(s => s.label === 'Implement')?.state).toBe('running')
    expect(goalStatus(cut(46), false)).toBe('Reviewing')
  })
  it('marks a failed review and blocked planning task', () => {
    // A review that fails in the last round blocks the implementation.
    const failed = { ...full, tasks: full.tasks.map(t => t.role === 'implement' ? { ...t, state: 'blocked' } : t),
      events: full.events.map(e => e.event_type === 'verification_recorded' && e.body_json.method === 'review' ? { ...e, body_json: { ...e.body_json, verdict: 'fail' } } : e) }
    expect(goalStages(failed).find(s => s.label === 'Review')?.state).toBe('failed')
    expect(goalDone(failed)).toBe(false)
    const blocked = { ...cut(8), tasks: [{ ...full.tasks[0]!, state: 'blocked' }] }
    expect(goalStatus(blocked, false)).toBe('Blocked')
    expect(goalStages(blocked)[1]?.state).toBe('failed')
  })
  // Review 219 A1: the checks of round 1 are accepted once their result is
  // used, pass or fail; while round 2 runs, Verify and Review are not done.
  it('does not mark checks done from an earlier round', () => {
    const impl = full.tasks.find(t => t.role === 'implement')!
    const firstVerdict = full.events.find(e => e.event_type === 'verification_recorded' && e.body_json.method === 'deterministic')!
    const lease = full.events.filter(e => e.event_type === 'attempt_leased' && e.task_id === impl.task_id).at(-1)!
    const round1 = full.events.filter(e => (e.seq ?? 0) <= (firstVerdict.seq ?? 0))
      .map(e => e === firstVerdict ? { ...e, body_json: { ...e.body_json, verdict: 'fail' } } : e)
    const relet = { ...lease, seq: 900, attempt_id: 'a_round2', at: '2026-10-16T04:20:00Z' }
    const running: GoalData = { ...full, events: [...round1, relet],
      tasks: full.tasks.filter(t => t.role !== 'review').map(t => t === impl ? { ...t, state: 'running' } : t) }
    const stages = goalStages(running)
    expect(stages.find(s => s.label === 'Implement')?.state).toBe('running')
    expect(stages.find(s => s.label === 'Verify')?.state).not.toBe('done')
    expect(stages.find(s => s.label === 'Review')?.state).not.toBe('done')
    expect(goalDone(running)).toBe(false)
    expect(goalStatus(running, false)).toBe('Implementing')
    // Round 2 passes both checks: done again.
    const pass = (method: string, seq: number) => ({ ...firstVerdict, seq, attempt_id: 'a_round2', body_json: { ...firstVerdict.body_json, method, verdict: 'pass' } })
    const passed: GoalData = { ...full, events: [...round1, relet, pass('deterministic', 901), pass('review', 902)], tasks: full.tasks }
    expect(goalStages(passed).map(s => s.state)).toEqual(['done', 'done', 'done', 'done', 'done', 'done'])
  })
  it('skips planning for a mission before the chief of staff', () => {
    expect(goalStages({ ...full, tasks: full.tasks.filter(t => t.role !== 'plan') })[1]?.state).toBe('skipped')
  })
  it('uses attempt start to submission, relative dates and numeric mission order', () => {
    expect(taskTiming(full, full.tasks[0]!, Date.now()).label).toMatch(/^took \d+ s$/)
    expect(relative('2026-10-16T04:11:00Z', Date.parse('2026-10-16T04:14:00Z'))).toBe('3 min ago')
    const runtime = parseRuntimeProgress({ work: { missions: [{ mission_id: 'M-2' }, { mission_id: 'M-12' }] }, recent: { tasks: [{ mission_id: 'M-10' }, { mission_id: 'M-2' }] } })
    expect(missionIds(runtime)).toEqual(['M-12', 'M-10', 'M-2'])
    expect(missionIds(runtime, 'M-20')[0]).toBe('M-20')
  })
})

// T26's first run (PROD M-9): the fifth task failed its review in its last round and was blocked. Once
// the Owner drops it on its card the mission is done; the dropped task's failed review says nothing of
// the goal, and the card counts it apart.
function withFifth(state: string, extra: GoalData['events'] = [], missionState = 'done'): GoalData {
  const impl = full.tasks.find(t => t.role === 'implement')!
  const review = full.events.find(e => e.event_type === 'verification_recorded' && e.body_json.method === 'review')!
  const fifth = { ...impl, task_id: 't_fifth', title: 'Fifth', state }
  const lease = { ...full.events.find(e => e.event_type === 'attempt_leased' && e.task_id === impl.task_id)!, seq: 990, task_id: 't_fifth', attempt_id: 'a_fifth' }
  const failed = { ...review, seq: 991, task_id: 't_fifth', attempt_id: 'a_fifth', body_json: { ...review.body_json, verdict: 'fail' } }
  return { mission: { ...mission, state: missionState }, tasks: [...full.tasks, fifth], events: [...full.events, lease, failed, ...extra] }
}
const dropEvent = { seq: 992, event_type: 'owner_wait_closed', at: '2026-10-20T01:00:00Z', actor_id: 'controller', task_id: 't_fifth',
  attempt_id: '', body_json: { question_id: 'q_9', outcome: 'dropped', answer: 'b' } }
describe('a task dropped on its blocked card', () => {
  it('lets the goal be done and counts the dropped task apart', () => {
    const dropped = withFifth('cancelled', [dropEvent])
    expect(goalDone(dropped)).toBe(true)
    expect(goalStatus(dropped, false)).toBe('Done')
    const stages = goalStages(dropped)
    expect(stages.map(s => s.state)).toEqual(['done', 'done', 'done', 'done', 'done', 'done'])
    expect(stages.find(s => s.label === 'Implement')?.dropped).toBe(1)
  })
  it('keeps a task cancelled any other way, and a blocked one, holding the goal', () => {
    expect(goalDone(withFifth('cancelled', [], 'active'))).toBe(false)
    const blocked = withFifth('blocked', [], 'active')
    expect(goalDone(blocked)).toBe(false)
    const runtime = parseRuntimeProgress({ needs_you: { items: [{ kind: 'blocked_task', title: 'Blocked in M-2: Fifth', task_id: 't_fifth',
      mission_id: 'M-2', blocks: { missions: ['M-2'] }, action: { where: 'console', question_id: 'q_9' } }] } })
    expect(goalWaitingLinks('M-2', blocked, [], runtime)).toEqual([{ title: 'Blocked in M-2: Fifth', href: questionHref('q_9') }])
    expect(goalStatus(blocked, true)).toBe('Waiting for you')
  })
  // Review 420 A1: the controller closes such a goal as stopped; read before it does, the card must not
  // call it done (and so stop reading it).
  it('never calls a goal done whose planning alone was accepted', () => {
    const plan = full.tasks.find(t => t.role === 'plan')!
    const impl = full.tasks.find(t => t.role === 'implement')!
    const planned: GoalData = { mission: { ...mission, state: 'active' }, tasks: [{ ...plan, state: 'accepted' }, { ...impl, state: 'cancelled' }],
      events: [...full.events.filter(e => e.task_id === plan.task_id), { ...dropEvent, task_id: impl.task_id }] }
    expect(goalDone(planned)).toBe(false)
    expect(goalStatus(planned, false)).not.toBe('Done')
  })
  it('stops a goal whose plan the Owner dropped', () => {
    const plan = full.tasks.find(t => t.role === 'plan')!
    const stopped: GoalData = { mission: { ...mission, state: 'stopped' }, tasks: [{ ...plan, state: 'cancelled' }],
      events: [...full.events.filter(e => e.task_id === plan.task_id), { ...dropEvent, task_id: plan.task_id }] }
    expect(goalStatus(stopped, false)).toBe('Stopped')
    expect(goalStages(stopped).find(s => s.label === 'Plan')).toMatchObject({ state: 'skipped', dropped: 1 })
  })
  it('says on the card which task was dropped', async () => {
    mockFetch()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['agent-runtime-goal', 'M-2'], withFifth('cancelled', [dropEvent]))
    wrap(<GoalCard id="M-2" updatedAt={1} questions={[]} />, client)
    expect(await screen.findByText('Done', { selector: 'strong.text-lg' })).toBeInTheDocument()
    expect(screen.getByText('1 dropped by you')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Implementation tasks')).getByText(/Fifth · Dropped by you/)).toBeInTheDocument()
  })
})

describe('goal cards and source links', () => {
  it('shows four criteria, four exclusions, three decisions, commit, branch and the report', async () => {
    mockFetch()
    wrap(<GoalCard id="M-2" updatedAt={1} questions={[]} />)
    expect(await screen.findByText('Done', { selector: 'strong.text-lg' })).toBeInTheDocument()
    expect(within(screen.getByLabelText('Done when')).getAllByRole('listitem', { hidden: true })).toHaveLength(4)
    expect(within(screen.getByLabelText('Not doing')).getAllByRole('listitem', { hidden: true })).toHaveLength(4)
    expect(within(screen.getByLabelText('Decisions')).getAllByRole('listitem', { hidden: true })).toHaveLength(3)
    expect(screen.getByText('d7ce5db')).toBeInTheDocument()
    expect(screen.getByText(/Branch: refs\/heads\/agent\//)).toBeInTheDocument()
    expect(screen.getByText('Review verdict: pass')).toBeInTheDocument()
    expect(screen.getByText('What the chief of staff wrote').closest('details')).not.toHaveAttribute('open')
    fireEvent.click(screen.getByRole('button', { name: 'Implementation report 1' }))
    expect(await screen.findByText('Fixture implementation report')).toBeInTheDocument()
  })
  it('matches only planning questions and runtime blocks for this mission', () => {
    const q = { id: 'q_123', title: 'Confirm scope', task_id: full.tasks[0]!.task_id, status: 'open' } as Question
    const runtime = parseRuntimeProgress({ needs_you: { items: [{ title: 'Change plan', blocks: { missions: ['M-2'] }, action: {} }] } })
    expect(goalWaitingLinks('M-2', full, [q], runtime)).toEqual([{ title: q.title, href: '#questions?id=q_123' }, { title: 'Change plan', href: '#needs-you' }])
    expect(goalStatus(full, true)).toBe('Waiting for you')
    expect(goalWaitingLinks('M-1', full, [{ ...q, task_id: 'unrelated' }], runtime)).toEqual([])
  })
  it('refreshes open cards with the runtime poll and stops after completion', async () => {
    const fetch = mockFetch()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['agent-runtime-goal', 'M-2'], cut(26))
    const { rerender } = wrap(<GoalCard id="M-2" updatedAt={1} questions={[]} />, client)
    rerender(<QueryClientProvider client={client}><GoalCard id="M-2" updatedAt={2} questions={[]} /></QueryClientProvider>)
    await screen.findByText('Done', { selector: 'strong.text-lg' })
    expect(fetch).toHaveBeenCalledTimes(3)
    rerender(<QueryClientProvider client={client}><GoalCard id="M-2" updatedAt={3} questions={[]} /></QueryClientProvider>)
    expect(fetch).toHaveBeenCalledTimes(3)
  })
  it('does not give the Owner the move for silent threads alone', () => {
    expect(whoseMove({ state: 'known', count: 4 }, parseRuntimeProgress({ live: [{ task_id: 't' }] }), false, 4)).toBe("Agents' move")
    expect(whoseMove({ state: 'known', count: 5 }, parseRuntimeProgress({}), false, 4)).toBe("Owner's move")
  })
})

describe('Progress composition', () => {
  it('collapses reference sections and opens legacy hash targets', async () => {
    mockFetch()
    window.history.replaceState(null, '', '#commit-lineage')
    wrap(<ProgressPage />)
    expect(document.getElementById('commit-lineage')).toHaveAttribute('open')
    expect(document.getElementById('code-health')).not.toHaveAttribute('open')
    expect(screen.getByText('History').closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText('Plan and architecture').closest('details')).not.toHaveAttribute('open')
    expect(resolveConsoleHash('commit-lineage').canonical).toBe('#progress?section=commit-lineage')
    expect(resolveConsoleHash('code-health').canonical).toBe('#progress?section=code-health')
    await act(async () => { window.location.hash = '#progress?section=code-health'; window.dispatchEvent(new Event('hashchange')) })
    expect(document.getElementById('code-health')).toHaveAttribute('open')
  })
  it('puts a submitted card first, clears the form, links and scrolls to the card', async () => {
    mockFetch()
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    wrap(<ProgressPage />)
    await screen.findByLabelText('Goal M-2')
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'A new goal' } })
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }))
    await screen.findByLabelText('Goal M-20')
    expect(screen.getByLabelText('Goal')).toHaveValue('')
    expect(screen.getAllByRole('article')[0]).toHaveAttribute('id', 'goal-M-20')
    expect(screen.getByRole('link', { name: 'M-20 created · View goal' })).toHaveAttribute('href', '#progress?goal=M-20')
    expect(scroll).toHaveBeenCalled()
    expect(screen.getByLabelText('Decisions for you')).toBeInTheDocument()
    expect(screen.getByText('Silent threads · 0').closest('details')).not.toHaveAttribute('open')
  })
})

it('reads the next event page from the last sequence', async () => {
  const fetch = vi.fn(async (url: RequestInfo | URL) => new Response(JSON.stringify(String(url).includes('after_seq=500') ? [{ seq: 501, event_type: 'mission_amended' }] : Array.from({ length: 500 }, (_, i) => ({ seq: i + 1, event_type: 'attempt_started' })))))
  vi.stubGlobal('fetch', fetch)
  expect(await fetchRuntimeEvents('M-2')).toHaveLength(501)
  expect(String(fetch.mock.calls[1]![0])).toContain('after_seq=500')
})

// Review 219 B3: a page that fails after a full one is an error, never a
// shorter stream that looks whole; the card says it could not load.
it('fails when a later event page fails, and the card says so', async () => {
  const page = Array.from({ length: 500 }, (_, i) => ({ seq: i + 1, event_type: 'attempt_started' }))
  vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL) => String(url).includes('after_seq=500')
    ? new Response(JSON.stringify({ error: 'unavailable' }), { status: 503, headers: { 'Content-Type': 'application/json' } })
    : new Response(JSON.stringify(page), { headers: { 'Content-Type': 'application/json' } })))
  await expect(fetchRuntimeEvents('M-2')).rejects.toThrow()
  mockFetch()
  const fetch = vi.mocked(globalThis.fetch)
  fetch.mockImplementation(async (input: RequestInfo | URL) => String(input).includes('/agentrt/events?')
    ? new Response(JSON.stringify({ error: 'unavailable' }), { status: 503, headers: { 'Content-Type': 'application/json' } })
    : new Response(JSON.stringify(String(input).includes('/agentrt/missions/') ? mission : tasks), { headers: { 'Content-Type': 'application/json' } }))
  wrap(<GoalCard id="M-2" questions={[]} updatedAt={0} />)
  expect(await screen.findByText(/could not|unavailable|failed/i)).toBeInTheDocument()
  expect(screen.queryByText('Done')).not.toBeInTheDocument()
})
