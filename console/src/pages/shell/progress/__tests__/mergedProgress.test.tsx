import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => null }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => null }))
import runtimeFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import roadmap from '@/api/__fixtures__/board-roadmap-round-arch.json'
import boardFixture from './fixtures/sample-progress-w62.json'
import eventsFixture from '@/api/__fixtures__/agentrt-events-across.testdb.json'
import poolsFixture from '../../owner/__tests__/fixtures/pools-utc.json'
import questionFixture from '../../../../../../api/internal/questions/testdata/contract/question-open.json'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { parseRuntimeProgress, parseRuntimeTask } from '@/api/agentRuntime'
import type { Question } from '@/api/questions'
import { ConsoleNavBadgeMark } from '@/components/shell/ConsoleNavSlotItem'
import { mergedWaitingCount, runtimeWaiting } from '../../needs-you/mergedNeedsYou'
import { needsYouBadge } from '../../needs-you/needsYouModel'
import { useNeedsYou } from '../../needs-you/useNeedsYou'
import { NeedsYouPage } from '../../needs-you/NeedsYouPage'
import { ProgressPage } from '../ProgressPage'
import { ProgressHistory } from '../ProgressHistory'
import { whoseMove } from '../mergedProgressModel'
import { TaskRow } from '../RuntimeWork'

const clients: QueryClient[] = []
const question = questionFixture.body as Question
const request = { id: `question:${question.id}`, kind: 'plan_change', title: 'Decide the plan change', since: question.created_at,
  action: { where: 'console', question_id: question.id, options: question.options } }
function json(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
function Badge() { const { count } = useNeedsYou(); const badge = needsYouBadge(count); return <aside aria-label="Sidebar">{badge && <ConsoleNavBadgeMark badge={badge} />}</aside> }
function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
  return render(<QueryClientProvider client={client}><Badge /><NeedsYouPage /><ProgressPage /></QueryClientProvider>)
}
function mockSources(fail = '') {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (fail && url.includes(fail)) return json({ error: 'source offline' }, 503)
    if (url.includes('/agentrt/progress')) return json({ ...runtimeFixture, needs_you: { count: 2, items: [request, { ...request, id: 'future', kind: 'future_kind', title: 'Future request', action: {} }] } })
    if (url.includes('/agent/progress')) return json({ document: { ...boardFixture, roadmap, owner_now: [
      ...boardFixture.owner_now, { name: 'Already an approval', approval: { id: 'appr_aaaaaaaaaaaaaaaa', number: 1 } },
    ] }, generated_at: runtimeFixture.generated_at, received_at: new Date().toISOString() })
    if (url.includes('/approvals')) return json(buildApprovalListResponse([buildPlatformApproval({ id: 'appr_aaaaaaaaaaaaaaaa' })]))
    if (url.includes('/questions')) return json({ questions: [questionFixture.body] })
    if (url.includes('/agent/threads')) return json({ threads: [], hosts: [] })
    if (url.includes('/lineage')) return json({ threads: [], errors: [] })
    if (url.includes('/self-health')) return json({ overall: 'ok', probes: [] })
    if (url.includes('/checklist/signals')) return json({ signals: [] })
    if (url.includes('/telemetry/alerts')) return json({ alerts: [] })
    if (url.includes('/agentrt/events?stream=')) return json([])
    if (url.includes('/agentrt/events?')) return json(eventsFixture)
    if (url.includes('/agentrt/pools')) return json(poolsFixture)
    if (url.includes('/api/v1/releases')) return json({ status: {}, records: [] })
    throw new Error(`Unexpected URL ${url}`)
  }))
}
beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => values.set(k, v), removeItem: (k: string) => values.delete(k), clear: () => values.clear(),
  } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals() })

describe('one Needs you list', () => {
  it('deduplicates runtime questions only while that question is open', () => {
    expect(runtimeWaiting([request], [question])).toEqual([])
    expect(runtimeWaiting([request], [{ ...question, status: 'answered' }])).toMatchObject([{ href: `#questions?id=${question.id}`, kind: 'plan_change', since: question.created_at }])
    expect(runtimeWaiting([{ ...request, action: {}, kind: 'future' }], [question])).toMatchObject([{ title: request.title, kind: 'future' }])
  })
  it('uses the same three-item count in the sidebar and For you\'s waiting section, without the board\'s hand-written requests', async () => {
    mockSources(); wrap()
    // Work no longer previews what waits on the Owner: that is For you's (W-31 2.2b).
    await waitFor(() => expect(document.querySelectorAll('[data-needs-you-count="3"]')).toHaveLength(1))
    expect(within(screen.getByLabelText('Sidebar')).getByText('3')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Agent runtime')).getByText('Future request')).toBeInTheDocument()
    // The board's owner_now has no way in: what waits for the Owner is a card, a request or a runtime item (2.5 slice 3).
    expect(screen.queryByLabelText('Written by the coordinator')).not.toBeInTheDocument()
    expect(screen.queryByText(boardFixture.owner_now[0]!.actions[0]!.text)).not.toBeInTheDocument()
    expect(screen.queryByText('Already an approval')).not.toBeInTheDocument()
    const blocks = Array.from(screen.getByTestId('merged-progress').children)
    expect(blocks.filter(b => b.tagName === 'SECTION').map(b => b.getAttribute('aria-label'))).toEqual(['Goals', 'Overview', 'Plan and architecture', 'Hosts', 'History', 'Commit lineage', 'Code health', 'Governance references'])
    // The question it previewed is listed, to answer, in the waiting section.
    expect(within(screen.getByTestId('needs-you-page')).getAllByText(/Where is the quota/).length).toBeGreaterThan(0)
  })
  it('marks all three counts incomplete when the runtime fails', async () => {
    mockSources('/agentrt/progress'); wrap()
    await waitFor(() => expect(document.querySelector('[data-needs-you-count="Unknown"]')).toBeTruthy())
    expect(within(screen.getByLabelText('Sidebar')).getByText('?')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Agent runtime')).getByText(/^The agent runtime could not be read; not read since this page opened at \d\d:\d\d\. Count is incomplete\.$/)).toBeInTheDocument()
  })
  it('keeps the count whole when the board fails: it is not a source of what waits for the Owner', async () => {
    mockSources('/agent/progress'); wrap()
    await waitFor(() => expect(document.querySelectorAll('[data-needs-you-count="3"]')).toHaveLength(1))
    expect(within(screen.getByLabelText('Sidebar')).getByText('3')).toBeInTheDocument()
  })
  it('makes a failed source outrank loading, and counts the complete list', () => {
    expect(mergedWaitingCount([{ state: 'loading' }, { state: 'unknown', reason: 'offline' }])).toEqual({ state: 'unknown', reason: 'offline' })
    expect(mergedWaitingCount([{ state: 'known', count: 3 }, { state: 'known', count: 2 }])).toEqual({ state: 'known', count: 5 })
  })
})

describe('whose move (A8), from the runtime alone (2.5 slice 4)', () => {
  const zero = { state: 'known', count: 0 } as const
  const runtime = parseRuntimeProgress({ ...runtimeFixture, live: [], work: { missions: [] } })
  it('is the Owner when a complete list has requests, before any agent or coordinator', () => {
    expect(whoseMove({ state: 'known', count: 1 }, runtime)).toBe("Owner's move")
    expect(whoseMove({ state: 'known', count: 1 }, parseRuntimeProgress({ live: [{ attempt_id: 'a' }] }))).toBe("Owner's move")
  })
  it('is the agents when an attempt is live', () => {
    const live = parseRuntimeProgress({ live: [{ attempt_id: 'a' }] })
    expect(whoseMove(zero, live)).toBe("Agents' move")
  })
  it.each(['proposed', 'ready', 'leased', 'running', 'submitted', 'verifying', 'rework'])('is the agents when a task is %s', state => {
    const withTask = parseRuntimeProgress({ work: { missions: [{ tasks: [{ state }] }] } })
    expect(whoseMove(zero, withTask)).toBe("Agents' move")
  })
  it.each(['accepted', 'blocked', 'cancelled'])('is not the agents when the only task is %s', state => {
    const withTask = parseRuntimeProgress({ work: { missions: [{ tasks: [{ state }] }] } })
    expect(whoseMove(zero, withTask)).toBe('All done')
  })
  it('is the coordinator when a goal waits on it, then all done when nothing is left', () => {
    // M-3 of the STG fixture is blocked and waits on the coordinator; with its queued and checked tasks taken out, nothing is the agents'.
    const stg = parseRuntimeProgress(runtimeFixture)
    const waiting = { ...stg, live: [], work: { missions: stg.work.missions.filter(m => m.waiting_on === 'coordinator').map(m => ({ ...m, tasks: m.tasks.filter(t => !['proposed', 'ready', 'leased', 'running', 'submitted', 'verifying', 'rework'].includes(t.state)) })) } }
    expect(waiting.work.missions.length).toBeGreaterThan(0)
    expect(whoseMove(zero, waiting)).toBe("Coordinator's move")
    expect(whoseMove(zero, runtime)).toBe('All done')
  })
  it('is unknown on a failed or loading source even with an Owner request', () => {
    expect(whoseMove({ state: 'known', count: 1 }, runtime, true)).toBe('Unknown')
    expect(whoseMove({ state: 'loading' }, runtime)).toBe('Unknown')
    expect(whoseMove(zero, undefined)).toBe('Unknown')
  })
})

describe('Progress with the board stopped (2.5 slice 4)', () => {
  it('builds History from the runtime\'s events and shows no board lines', async () => {
    mockSources('/agent/progress'); wrap()
    // Finished work is in Records › Work history (W-31 2.2b); Work's History points there.
    expect(within(screen.getByRole('region', { name: 'History' })).getByRole('link', { name: 'Records › Work history' })).toHaveAttribute('href', '#records?tab=work')
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    render(<QueryClientProvider client={client}><ProgressHistory /></QueryClientProvider>)
    const history = screen.getByRole('region', { name: 'Work history' })
    expect(await within(history).findByText('Host h1 had a report refused: it named a local path (H8)')).toBeInTheDocument()
    expect(within(history).getByText('An executor failed t_01M5B8AZ5TQWK0H9F14562Y9XX three times in a row; it is not offered that task again')).toBeInTheDocument()
    for (const gone of ['Done today', 'Activity', 'Finished works', 'Coordinator works']) expect(screen.queryByText(gone)).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Coordinator' })).not.toBeInTheDocument()
    expect(within(history).queryByText(/Board updated/)).not.toBeInTheDocument()
    expect(screen.getByTestId('merged-progress')).not.toHaveTextContent('✎')
    const overview = screen.getByRole('region', { name: 'Overview' })
    expect(await within(overview).findByText('claude · weekly')).toBeInTheDocument()
  })
})

describe('task disclosure defaults', () => {
  it.each([['running', true], ['awaiting_owner', true], ['ready', false], ['verifying', false], ['accepted', false]] as const)(
    'expands %s only when it is running or waiting for the Owner', (state, expanded) => {
      mockSources()
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
      render(<QueryClientProvider client={client}><TaskRow mission="M-2" task={parseRuntimeTask({ task_id: 't', title: 'Fixture task', state })} /></QueryClientProvider>)
      expect(screen.getByRole('button', { name: /Fixture task/ })).toHaveAttribute('aria-expanded', String(expanded))
    },
  )
})
