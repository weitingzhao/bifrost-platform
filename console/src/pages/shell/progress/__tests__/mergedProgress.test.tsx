import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('@/pages/CommitLineagePage', () => ({ CommitLineagePage: () => null }))
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => null }))
import runtimeFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import roadmap from '@/api/__fixtures__/board-roadmap-round-arch.json'
import boardFixture from './fixtures/sample-progress-w62.json'
import questionFixture from '../../../../../../api/internal/questions/testdata/contract/question-open.json'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { parseRuntimeProgress, parseRuntimeTask } from '@/api/agentRuntime'
import { parseProgressDocument } from '@/api/agentProgress'
import type { Question } from '@/api/questions'
import { ConsoleNavBadgeMark } from '@/components/shell/ConsoleNavSlotItem'
import { coordinatorWaiting, mergedWaitingCount, runtimeWaiting } from '../../needs-you/mergedNeedsYou'
import { needsYouBadge } from '../../needs-you/needsYouModel'
import { useNeedsYou } from '../../needs-you/useNeedsYou'
import { NeedsYouPage } from '../../needs-you/NeedsYouPage'
import { ProgressPage } from '../ProgressPage'
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
    if (url.includes('/agentrt/events')) return json([])
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
  it('keeps coordinator fixture requests and skips every item carrying an approval', () => {
    const board = parseProgressDocument(boardFixture)!
    expect(coordinatorWaiting(board.owner_now!)).toHaveLength(1)
    expect(coordinatorWaiting([...board.owner_now!, { name: 'Duplicate', approval: { id: 'appr_aaaaaaaaaaaaaaaa' } }])).toHaveLength(1)
    expect(coordinatorWaiting(board.owner_now!)[0]!.instruction).toContain(boardFixture.owner_now[0]!.actions[0]!.text)
  })
  it('uses the same four-item count in the sidebar, home and Progress, renders both new groups', async () => {
    mockSources(); wrap()
    await waitFor(() => expect(document.querySelectorAll('[data-needs-you-count="4"]')).toHaveLength(2))
    expect(within(screen.getByLabelText('Sidebar')).getByText('4')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Agent runtime')).getByText('Future request')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Written by the coordinator')).getByText(boardFixture.owner_now[0]!.actions[0]!.text)).toBeInTheDocument()
    expect(screen.queryByText('Already an approval')).not.toBeInTheDocument()
    expect(screen.getByText(/Commands are not published to the platform/)).toBeInTheDocument()
    const blocks = Array.from(screen.getByTestId('merged-progress').children)
    expect(blocks.map(b => b.getAttribute('aria-label'))).toEqual(['Needs you', 'Overview', 'Give the agents a goal', 'Running', 'Plan and architecture', 'Hosts', 'History', 'Commit lineage', 'Code health'])
    const preview = within(blocks[0] as HTMLElement)
    expect(preview.getAllByRole('listitem')).toHaveLength(4)
    expect(preview.getByRole('link', { name: /Where is the quota/ })).toHaveAttribute('href', `#questions?id=${question.id}`)
  })
  it.each(['/agentrt/progress', '/agent/progress'])('marks all three counts incomplete when %s fails', async source => {
    mockSources(source); wrap()
    expect(await screen.findByText('Needs you: Unknown — count is incomplete')).toBeInTheDocument()
    expect(within(screen.getByLabelText('Sidebar')).getByText('?')).toBeInTheDocument()
    expect(screen.getByText(/Count is incomplete:/)).toBeInTheDocument()
    const label = source.includes('agentrt') ? 'Agent runtime' : 'Written by the coordinator'
    expect(within(screen.getByLabelText(label)).getByText(/Could not load/)).toBeInTheDocument()
  })
  it('makes a failed source outrank loading, and counts the complete list', () => {
    expect(mergedWaitingCount([{ state: 'loading' }, { state: 'unknown', reason: 'offline' }])).toEqual({ state: 'unknown', reason: 'offline' })
    expect(mergedWaitingCount([{ state: 'known', count: 3 }, { state: 'known', count: 2 }])).toEqual({ state: 'known', count: 5 })
  })
})

describe('whose move (A8)', () => {
  const zero = { state: 'known', count: 0 } as const
  const runtime = parseRuntimeProgress({ ...runtimeFixture, live: [], work: { missions: [] } })
  const board = { coordinator: { state: 'idle' } }
  it('is the Owner when a complete list has requests, before any agent or coordinator', () => {
    expect(whoseMove({ state: 'known', count: 1 }, runtime, { coordinator: { state: 'working' } })).toBe("Owner's move")
    expect(whoseMove({ state: 'known', count: 1 }, runtime, null)).toBe("Owner's move")
  })
  it('is the agents when an attempt is live', () => {
    const live = parseRuntimeProgress({ live: [{ attempt_id: 'a' }] })
    expect(whoseMove(zero, live, board)).toBe("Agents' move")
  })
  it.each(['proposed', 'ready', 'leased', 'running', 'submitted', 'verifying', 'rework'])('is the agents when a task is %s', state => {
    const withTask = parseRuntimeProgress({ work: { missions: [{ tasks: [{ state }] }] } })
    expect(whoseMove(zero, withTask, board)).toBe("Agents' move")
  })
  it.each(['accepted', 'blocked', 'cancelled'])('is not the agents when the only task is %s', state => {
    const withTask = parseRuntimeProgress({ work: { missions: [{ tasks: [{ state }] }] } })
    expect(whoseMove(zero, withTask, board)).toBe('All done')
  })
  it('is the coordinator when working, then all done when nothing is left', () => {
    expect(whoseMove(zero, runtime, { coordinator: { state: 'working' } })).toBe("Coordinator's move")
    expect(whoseMove(zero, runtime, { coordinator: { doing: 'Reviewing the implementation' } })).toBe("Coordinator's move")
    expect(whoseMove(zero, runtime, board)).toBe('All done')
  })
  it('is unknown on a failed or loading source even with an Owner request', () => {
    expect(whoseMove({ state: 'known', count: 1 }, runtime, board, true)).toBe('Unknown')
    expect(whoseMove({ state: 'loading' }, runtime, board)).toBe('Unknown')
    expect(whoseMove(zero, undefined, board)).toBe('Unknown')
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
