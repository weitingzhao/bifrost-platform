import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useEffect, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryAlertEntry } from '@/api/clusterTypes'
import { planRows, splitAlerts } from '../ownerModel'
import { PLAN_FROM_BOARD, planRead, type PlanRead } from '../../agents/useRuntimePlan'
import planFixture from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import rootOnlyFixture from '@/api/__fixtures__/agentrt-plan-root-only.testdb.json'
import { PLAN_ROOT, parsePlanRequest } from '@/api/agentRuntimePlan'
import type { OwnerConsoleData } from '../useOwnerConsole'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'
import evidenceFixture from './fixtures/standby-evidence.prod.json'
import poolsFixture from './fixtures/pools-utc.json'
import capabilitiesFixture from '@/api/__fixtures__/agentrt-capabilities.testdb.json'
import etaFixture from '@/api/__fixtures__/agentrt-progress-eta.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import type { TelemetryTargetEntry } from '@/api/clusterTypes'
import { parseDaemonSetGaps, parseDownTargets, parseStandbyPods } from '../standbyEvidence'

const NOW = Date.parse('2026-10-16T20:10:43Z')
const standbyKnown = vi.hoisted(() => ({ value: true }))
// The round the board publishes, when a test sets one (CUT-plan-import §6).
const boardRound = vi.hoisted(() => ({ value: undefined as Record<string, unknown> | undefined }))
// The coordinating thread's note as the hook reads it from the mailbox (2.5 G3).
const coordinatorView = vi.hoisted(() => ({ value: undefined as unknown }))
// The time the page is drawn at, when a test sets one; the open goals the runtime gives (2.5 G8).
const nowValue = vi.hoisted(() => ({ value: undefined as number | undefined }))
const missionsValue = vi.hoisted(() => ({ value: [] as unknown[] }))
// Where the plan was read from, when a test sets it; the board otherwise, as while the runtime holds none (2.5 slice 2).
const planValue = vi.hoisted(() => ({ value: undefined as PlanRead | undefined }))

function fake(): OwnerConsoleData {
  const evidence = { nodes: [{ name: 'gpu-server', internalIp: '192.168.10.60' }], pods: parseStandbyPods(evidenceFixture.pods),
    gaps: parseDaemonSetGaps(evidenceFixture.gaps), down: parseDownTargets(evidenceFixture.targets as TelemetryTargetEntry[]) }
  const alerts = splitAlerts(alertsFixture.alerts as TelemetryAlertEntry[], standbyKnown.value ? evidence : null, NOW)
  const card = { id: 'runtime-0', title: 'Look at the goal cards', href: '#needs-you', instruction: 'Needs you · Review the runtime request' }
  return {
    now: nowValue.value ?? NOW,
    needsYou: { count: { state: 'known', count: 1 }, decisionItems: [card], silentItems: [{ id: 't1', title: 'thread', href: '#needs-you', instruction: '', kind: 'silent_thread' }],
      decide: [], openQuestions: [], runtimeItems: [], recentCount: { state: 'known', count: 11 } },
    yours: { state: 'known', count: 2 },
    standbyEvidence: standbyKnown.value ? 'read' : 'unread',
    runtimeError: false,
    runsError: null,
    runsRead: true,
    verdict: { tone: 'attention', headline: '2 things are yours', line: 'Nothing to approve · agents idle · release lane free · 5 of 5 nodes ready' },
    badges: { you: { text: '2', tone: 'attention' }, system: { text: '1', tone: 'fail' }, data: { text: '2', tone: 'muted' } },
    alerts, alertsError: null,
    work: { running: 0, stuck: 0, open: 0, lastAccepted: { mission: 'M-2', at: '2026-10-16T04:17:22Z', openState: '' } },
    last: { mission: 'M-2', at: '2026-10-16T04:17:22Z', status: 'done', state: 'done' },
    lastGoal: 'Add a comment above Repos.',
    coordinator: coordinatorView.value ?? { state: 'read', full: false, note: { body: 'Owner console, first version\nNext: 2.3 inbox', at: new Date(NOW - 7 * 60_000).toISOString() } },
    board: { coordinator: { doing: 'Board text, not shown', next: '' }, roadmap: { round: boardRound.value ?? { n: 2, items: [] } } },
    boardStale: false,
    planSource: planValue.value ?? { from: 'board' },
    planRound: planValue.value === undefined ? boardRound.value ?? { n: 2, items: [] } : planValue.value.from === 'runtime' ? planValue.value.round : undefined,
    plan: planValue.value !== undefined ? planRows(planValue.value.from === 'runtime' ? planValue.value.round : undefined)
      : boardRound.value?.unknown !== undefined ? [] : [{ key: '2.2b', name: 'First slice through', done: 10, total: 10, tone: 'ok', tag: '' }, { key: '2.3', name: 'Inbox', done: 0, total: 4, tone: 'muted', tag: 'next' }],
    arch: [{ state: 'done', what: 'a', now: '' }, { state: 'part', what: 'b', now: '' }],
    hosts: [],
    missions: missionsValue.value,
    controlsReadable: true,
    nodes: [{ name: 'ubt-k3s-02', status: 'Ready', roles: 'worker', internal_ip: '192.168.10.70', version: '', reachability: 'ok', cpu_usage_percent: 4.5, memory_usage_percent: 35.2 },
      { name: 'gpu-server', status: 'NotReady', roles: 'worker', internal_ip: '192.168.10.60', version: '', reachability: 'fail', elastic_mode: 'standby' }],
    nodeCount: { ready: 1, total: 1, standby: 1 }, nodesError: null,
    lane: { state: 'free', text: 'Release window free', detail: '' },
    inFlight: [], records: [{ run: 'r1', pipeline: 'bifrost-deliver-platform-prod', lane: 'platform', env: 'prod', deploys: true, started_at: '', completed_at: '2026-10-16T05:29:00Z', repos: {}, recorded_at: '' }],
    runsLoading: false,
    tiles: [{ name: 'Test copies', tone: 'warn', state: 'Stale', figure: '13 days', detail: 'dev and stg copies of PROD, oldest', href: '#infrastructure' }],
    updatedAt: NOW,
  } as unknown as OwnerConsoleData
}

vi.mock('../useOwnerConsole', () => ({ useOwnerConsole: () => fake() }))
const submit = vi.hoisted(() => vi.fn(async () => ({ mission_id: 'M-9', mission_state: 'active', waiting: '', task_id: 't', repo: 'bifrost-platform', base_commit: '', spec_artifact_id: '' })))
vi.mock('@/api/agentRuntime', async importOriginal => ({ ...(await importOriginal<object>()), submitRuntimeGoal: submit }))

const { OwnerConsolePage } = await import('../OwnerConsolePage')
const { isEntryRoute } = await import('@/lib/shell/consoleRoutes')

function entryFromHash() {
  const id = window.location.hash.replace(/^#/, '').split('?')[0] ?? ''
  return isEntryRoute(id) ? id : 'you'
}

/** The shell's one slot for the entries: the hash picks the entry, the component stays mounted. */
function Shell() {
  const [entry, setEntry] = useState(entryFromHash)
  useEffect(() => {
    const sync = () => setEntry(entryFromHash())
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return <OwnerConsolePage entry={entry} />
}

function show(hash = '#you') {
  window.history.replaceState(null, '', hash)
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Shell /></QueryClientProvider>)
}
const headings = () => screen.getAllByRole('heading', { level: 2 }).map(h => h.textContent)
async function go(hash: string, heading: string) {
  window.location.hash = hash
  await waitFor(() => expect(headings()[0]).toBe(heading))
}

beforeEach(() => {
  submit.mockClear()
  standbyKnown.value = true
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => values.set(k, v), removeItem: (k: string) => values.delete(k), clear: () => values.clear(),
  } })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); nowValue.value = undefined; missionsValue.value = []; planValue.value = undefined })

function json(body: unknown) { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
/** GET /agentrt/pools and /capabilities as the platform answers them; anything else is not this page's. */
function stubRuntimeReads() {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/agentrt/pools')) return json(poolsFixture)
    if (url.includes('/agentrt/capabilities')) return json(capabilitiesFixture)
    return json({ error: `not stubbed: ${url}` })
  }))
}

describe('Owner Console entries', () => {
  it('For you: the whole verdict, then what is yours and what is worth a look', () => {
    show('#you')
    expect(screen.getByRole('region', { name: 'Verdict' })).toHaveTextContent('2 things are yours')
    expect(headings()).toEqual(['For you'])
  })

  it('each other entry shows its own block, and the verdict is one line that goes to For you', () => {
    show('#work')
    expect(headings()).toEqual(['Work'])
    expect(screen.queryByRole('region', { name: 'Verdict' })).toBeNull()
    expect(screen.getByRole('link', { name: /2 things are yours/ })).toHaveAttribute('href', '#you')
  })

  it('System carries the data and the way into every detail page', () => {
    show('#system')
    expect(headings()).toEqual(['System', 'Data', 'Detail pages'])
    expect(screen.getByRole('region', { name: 'Release lane' })).toHaveTextContent('nobody holds it; nothing delivering')
    expect(screen.getByRole('region', { name: 'Health' })).toHaveTextContent('critical · yours')
    const details = within(screen.getByRole('navigation', { name: 'Detail pages' })).getAllByRole('link')
    expect(details.map(a => a.getAttribute('href'))).toEqual(['#needs-you', '#status', '#data', '#ib', '#releases', '#infrastructure', '#progress', '#records'])
  })

  it('lists what is his, then what is worth a look with whose it is', () => {
    show('#you')
    const yours = screen.getByRole('region', { name: 'Yours' })
    expect(within(yours).getAllByRole('listitem').map(li => li.querySelector('.font-semibold')?.textContent)).toEqual([
      'Log in to TWS for the IB secondary slot', 'Look at the goal cards'])
    expect(yours).not.toHaveTextContent('✎')
    expect(yours).toHaveTextContent('No approvals, questions or agent cards waiting')
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    expect(within(worth).getAllByText('No goal yet')).toHaveLength(4)
    expect(worth).toHaveTextContent('6 DaemonSet alerts that fit gpu-server being on standby')
    expect(worth).toHaveTextContent('Likely the planned standby; not proven')
    expect(worth).toHaveTextContent('1 Claude thread went quiet mid-turn')
    expect(worth).toHaveTextContent('The coordinator checks them')
  })

  it('shows six rows worth a look, and the rest on request', () => {
    standbyKnown.value = false
    show('#you')
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    const total = Number(/Worth a look · (\d+)/.exec(worth.textContent ?? '')?.[1])
    expect(total).toBeGreaterThan(6)
    expect(within(worth).getAllByRole('listitem')).toHaveLength(6)
    fireEvent.click(within(worth).getByRole('button', { name: `Show ${total - 6} more` }))
    expect(within(worth).getAllByRole('listitem')).toHaveLength(total)
  })

  it('Make it a goal opens the chief of staff with a draft in its box; nothing is sent', async () => {
    show('#you')
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    fireEvent.click(within(worth).getAllByRole('button', { name: 'Make it a goal' })[0])
    await waitFor(() => expect(headings()).toEqual(['Chief of staff']))
    expect(window.location.hash).toBe('#chief')
    expect((screen.getByLabelText('Goal') as HTMLTextAreaElement).value).toContain('kube-prometheus-stack-grafana targets down')
    expect(submit).not.toHaveBeenCalled()
  })

  it('a second draft replaces the first, and only Submit sends it', async () => {
    show('#you')
    fireEvent.click(within(screen.getByRole('region', { name: 'Worth a look' })).getAllByRole('button', { name: 'Make it a goal' })[0])
    await waitFor(() => expect(headings()).toEqual(['Chief of staff']))
    await go('#you', 'For you')
    fireEvent.click(within(screen.getByRole('region', { name: 'Worth a look' })).getAllByRole('button', { name: 'Make it a goal' })[1])
    await waitFor(() => expect(headings()).toEqual(['Chief of staff']))
    const box = screen.getByLabelText('Goal') as HTMLTextAreaElement
    expect(box.value).toContain('Job logical-backup-drill-manual-1 failed')
    expect(box.value).not.toContain('grafana')
    expect(submit).not.toHaveBeenCalled()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Submit' })) })
    expect(submit).toHaveBeenCalledTimes(1)
    expect(submit.mock.calls[0][0]).toContain('Job logical-backup-drill-manual-1 failed')
  })

  it('an edited draft and a goal just sent outlive moving to another entry (review 224 B-224-1)', async () => {
    show('#you')
    fireEvent.click(within(screen.getByRole('region', { name: 'Worth a look' })).getAllByRole('button', { name: 'Make it a goal' })[0])
    await waitFor(() => expect(headings()).toEqual(['Chief of staff']))
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'My own words' } })
    await go('#work', 'Work')
    await go('#chief', 'Chief of staff')
    expect((screen.getByLabelText('Goal') as HTMLTextAreaElement).value).toBe('My own words')
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Submit' })) })
    expect(submit).toHaveBeenCalledTimes(1)
    await go('#work', 'Work')
    await go('#chief', 'Chief of staff')
    expect((screen.getByLabelText('Goal') as HTMLTextAreaElement).value).toBe('')
    expect(screen.getByRole('status')).toHaveTextContent('M-9 created')
  })

  it('says why the plan is unavailable, and marks a plan from the runtime as not kept by hand', () => {
    boardRound.value = { unknown: 'PG unavailable', source: 'runtime', items: [] }
    try {
      show('#work')
      const plan = screen.getByRole('region', { name: 'The plan' })
      expect(plan).toHaveTextContent('Plan unavailable: PG unavailable')
      expect(plan).not.toHaveTextContent("No plan on the coordinator's board.")
      expect(plan).not.toHaveTextContent('✎ by hand')
    } finally {
      boardRound.value = undefined
    }
  })

  it('shows the plan from the runtime, counted as GET /progress counts it, with no hand mark and no board note (2.5 slice 2)', () => {
    const read = (data: unknown) => ({ data, error: null, isError: false, isPaused: false })
    planValue.value = planRead(read({ found: true, doc: planFixture.plan }), read(parsePlanRequest(planFixture.request, PLAN_ROOT)), read(parseRuntimeProgress(planFixture.progress)))
    show('#work')
    const plan = screen.getByRole('region', { name: 'The plan' })
    expect(within(plan).getByRole('heading')).toHaveTextContent('The plan · Round 2')
    const rows = within(plan).getAllByRole('listitem').map(li => li.textContent)
    expect(rows).toEqual(planFixture.progress.plan[0].items.map(i => `${i.label}${i.title}${i.count.done}/${i.count.total}`))
    expect(plan).not.toHaveTextContent('✎ by hand')
    expect(plan).not.toHaveTextContent(PLAN_FROM_BOARD)
  })

  it('shows a runtime round with no items as empty, not unavailable and not the board\'s', () => {
    const read = (data: unknown) => ({ data, error: null, isError: false, isPaused: false })
    planValue.value = planRead(read({ found: true, doc: rootOnlyFixture.plan }), read(null), read(parseRuntimeProgress(rootOnlyFixture.progress)))
    show('#work')
    const plan = screen.getByRole('region', { name: 'The plan' })
    expect(plan).toHaveTextContent("No items in the runtime's plan.")
    expect(within(plan).queryByRole('alert')).not.toBeInTheDocument()
    expect(within(plan).queryAllByRole('listitem')).toHaveLength(0)
    expect(plan).not.toHaveTextContent(PLAN_FROM_BOARD)
  })

  it('says the plan is read from the board while the runtime holds none, and marks it kept by hand', () => {
    show('#work')
    const plan = screen.getByRole('region', { name: 'The plan' })
    expect(plan).toHaveTextContent(PLAN_FROM_BOARD)
    expect(plan).toHaveTextContent('✎ by hand')
    expect(plan).toHaveTextContent('2.2b')
  })

  it('shows a failed read of the runtime\'s plan as unavailable, never the board\'s rows', () => {
    planValue.value = { from: 'error', error: 'Plan: unavailable' }
    show('#work')
    const plan = screen.getByRole('region', { name: 'The plan' })
    expect(within(plan).getByRole('alert')).toHaveTextContent('Plan unavailable: Plan: unavailable')
    expect(plan).not.toHaveTextContent('2.2b')
    expect(plan).not.toHaveTextContent('✎ by hand')
    expect(plan).not.toHaveTextContent(PLAN_FROM_BOARD)
  })

  it('shows the coordinating thread\'s latest note from the mailbox, not the board', () => {
    show('#work')
    const now = screen.getByRole('region', { name: 'Now' })
    expect(now).toHaveTextContent('Owner console, first version')
    expect(now).toHaveTextContent('Note 7 min ago')
    expect(now).not.toHaveTextContent('Board text, not shown')
    expect(now).not.toHaveTextContent('✎ by hand')
  })

  it.each([
    [{ state: 'read', note: null, full: false }, 'No note from the coordinator.'],
    [{ state: 'read', note: null, full: true }, 'No note among the latest 50 messages with the coordinator.'],
    [{ state: 'loading' }, 'Reading the mailbox…'],
    [{ state: 'error', error: 'Connect with the Owner token to read the coordinator’s notes.' }, 'Coordinator note unavailable: Connect with the Owner token'],
  ])('says what the coordinator card read: %j', (view, line) => {
    coordinatorView.value = view
    try {
      show('#work')
      expect(screen.getByRole('region', { name: 'Now' })).toHaveTextContent(line)
    } finally {
      coordinatorView.value = undefined
    }
  })

  it('shows each pool\'s last reading from the runtime with when, an old one marked; no hand-kept quota (2.5 G7 A)', async () => {
    stubRuntimeReads()
    nowValue.value = Date.parse('2026-10-19T21:00:00Z')
    show('#work')
    const quota = screen.getByRole('region', { name: 'Quota' })
    expect(await within(quota).findByText('claude · weekly')).toBeInTheDocument()
    expect(within(quota).getAllByText('old')).toHaveLength(3)
    expect(within(quota).getAllByText('read 25 h ago')).toHaveLength(3)
    expect(within(quota).getAllByText('old')[0]).toHaveAttribute('title', 'its window has reset since it was read')
    expect(within(quota).getByText('no reading yet')).toBeInTheDocument()
    expect(within(quota).getByRole('link', { name: /System/ })).toHaveAttribute('href', '#system')
    expect(quota).not.toHaveTextContent('✎')
    expect(screen.getByRole('region', { name: 'The plan' })).toHaveTextContent('✎ by hand')
    expect(screen.getByRole('region', { name: 'Now' })).toHaveTextContent('M-2 Add a comment above Repos.')
  })

  it('gives each open goal when it should be done, as a range with its basis, or unknown with why (2.5 G8)', () => {
    missionsValue.value = parseRuntimeProgress(etaFixture).work.missions
    nowValue.value = Date.parse('2026-10-19T23:35:51Z')
    show('#work')
    const goals = screen.getByRole('region', { name: 'Open goals' })
    expect(goals).toHaveTextContent('Open goals · 2')
    const [m2, m1] = within(goals).getAllByRole('listitem').filter(li => li.parentElement?.parentElement === goals)
    expect(m2).toHaveTextContent('M-2')
    expect(m2).toHaveTextContent('Done by: unknown — the mission is proposed: a wait for a decision has no usual time')
    expect(m1).toHaveTextContent(/M-1 M-1queued · ready for a host to claim/)
    expect(m1).toHaveTextContent(/Expected \d\d:\d\d–\d\d:\d\d, in 3 min to 5 min/)
    expect(m1).toHaveTextContent('implement go: usually 1 min 40 s, from 3 accepted attempts in 30 days')
    expect(m1).not.toHaveTextContent('unknown')
  })

  it('System puts the capability scores beside the quota, from GET /capabilities (2.5 G9)', async () => {
    stubRuntimeReads()
    show('#system')
    const card = screen.getByRole('region', { name: 'Capabilities' })
    const implementer = await within(card).findByRole('region', { name: 'Scores for implementer' })
    expect(within(implementer).getAllByRole('listitem').map(li => li.textContent)).toEqual([
      expect.stringMatching(/^codex \(gpt-x\) · go · core95%exam · 6 attempts: 5 first pass, 6 accepted, 0 rework · window ended/),
      expect.stringMatching(/^cursor \(auto\) · go80%in the job · 4 attempts: 2 first pass, 3 accepted, 1 rework · median 7 min · window ended/),
    ])
    expect(within(card).getByRole('region', { name: 'Scores for reviewer' })).toHaveTextContent('claude (opus) · ts50%')
    expect(within(card).getByText('Candidates · 40')).toBeInTheDocument()
    expect(within(card).getByText('Capability facts · 6')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Quota' })).toBeInTheDocument()
  })
})
