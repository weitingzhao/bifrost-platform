import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useEffect, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryAlertEntry } from '@/api/clusterTypes'
import { splitAlerts } from '../ownerModel'
import type { ArchRead } from '../../agents/useRuntimePlan'
import type { OwnerConsoleData } from '../useOwnerConsole'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'
import evidenceFixture from './fixtures/standby-evidence.prod.json'
import poolsFixture from './fixtures/pools-utc.json'
import capabilitiesFixture from '@/api/__fixtures__/agentrt-capabilities.testdb.json'
import schemaFixture from '@/api/__fixtures__/agentrt-progress-schema.testdb.json'
import presenceFixture from '@/api/__fixtures__/agentrt-progress-presence.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import type { TelemetryTargetEntry } from '@/api/clusterTypes'
import { parseDaemonSetGaps, parseDownTargets, parseStandbyPods } from '../standbyEvidence'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'

vi.mock('../../agents/WorkPlanView', () => ({ WorkPlanView: () => <section data-testid="plan-view" /> }))
// Code health draws a @bifrost/ui Dialog, which jsdom here cannot render (a second React); its own page is all that is asserted.
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => <p data-testid="code-health-page" /> }))

const NOW = Date.parse('2026-10-16T20:10:43Z')
const standbyKnown = vi.hoisted(() => ({ value: true }))
// The architecture table, when a test sets it; absent otherwise, as while the runtime holds no ARCH root (2.5 ⑤ G2).
const archValue = vi.hoisted(() => ({ value: undefined as ArchRead | undefined }))
// The coordinating thread's note as the hook reads it from the mailbox (2.5 G3).
const coordinatorView = vi.hoisted(() => ({ value: undefined as unknown }))
// The time the page is drawn at, when a test sets one; the open goals the runtime gives (2.5 G8).
const nowValue = vi.hoisted(() => ({ value: undefined as number | undefined }))
const missionsValue = vi.hoisted(() => ({ value: [] as unknown[] }))
// The runtime as last read, when a test sets it (TD-411: what stops the controller is said on Work and System).
const runtimeValue = vi.hoisted(() => ({ value: undefined as unknown }))
// Where the plan was read from, when a test sets it; absent otherwise, as while the runtime holds none (2.5 slice 2; the board is retired, ⑤).
// Running, stuck and open goals as the hook reads them from the runtime, when a test sets them (T24).
const workValue = vi.hoisted(() => ({ value: undefined as unknown }))

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
    work: workValue.value ?? { running: 0, stalled: 0, stalledWords: '', stuck: 0, open: 0, lastAccepted: { mission: 'M-2', at: '2026-10-16T04:17:22Z', openState: '' } },
    last: { mission: 'M-2', at: '2026-10-16T04:17:22Z', status: 'done', state: 'done' },
    lastGoal: 'Add a comment above Repos.',
    coordinator: coordinatorView.value ?? { state: 'read', full: false, note: { body: 'Owner console, first version\nNext: 2.3 inbox', at: new Date(NOW - 7 * 60_000).toISOString() } },
    arch: archValue.value ?? { from: 'absent' },
    hosts: [],
    missions: missionsValue.value,
    runtime: runtimeValue.value,
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
const { isEntryRoute, resolveConsoleHash } = await import('@/lib/shell/consoleRoutes')

function entryFromHash() {
  const location = resolveConsoleHash(window.location.hash.slice(1)).location
  const id = location.kind === 'shell' ? location.id : ''
  return isEntryRoute(id) ? id : null
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

const viewer = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
function show(hash = '#you', signIn = false) {
  window.history.replaceState(null, '', hash)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  // Code health and Commit lineage read the sign-in (their own pages on Work since R5:5.5-2); the shell provides it.
  return render(<QueryClientProvider client={client}>{signIn ? <PlatformAuthContext.Provider value={viewer}><Shell /></PlatformAuthContext.Provider> : <Shell />}</QueryClientProvider>)
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
afterEach(() => { cleanup(); vi.unstubAllGlobals(); nowValue.value = undefined; missionsValue.value = []; archValue.value = undefined; runtimeValue.value = undefined; workValue.value = undefined })

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
  it('keeps sign-offs separate from Yours and full waiting information available in Detail', async () => {
    stubRuntimeReads()
    show('#you')
    const yours = screen.getByRole('region', { name: 'Yours' })
    expect(yours.querySelector('[data-signoff-link]')).toBeNull()
    const signoffs = screen.getByRole('region', { name: 'Plan sign-offs' })
    expect(signoffs).toHaveTextContent('separate from Needs you')
    expect(within(signoffs).getByRole('link', { name: /Review steps/ })).toHaveAttribute('href', '#work?section=signoff')
    const detail = screen.getByRole('region', { name: 'Everything waiting on you' }).querySelector('details')!
    expect(detail.open).toBe(false)
    const allWaiting = within(yours).getByRole('link', { name: /All waiting/ })
    allWaiting.addEventListener('click', event => event.preventDefault())
    fireEvent.click(allWaiting)
    await waitFor(() => expect(detail.open).toBe(true))
    detail.open = false
    fireEvent.click(allWaiting)
    expect(detail.open).toBe(true)
    expect(within(detail).getByTestId('needs-you-page')).toBeInTheDocument()
  })
  // TD-411 (STG 2026-10-20: a platform needing 0007 and 0008 on a database at 0006, the controller failing quietly):
  // Work and System say the database misses them, from Progress as a database at 0006 answered it; For you does not.
  it('Work and System say the agent runtime database misses migrations', async () => {
    runtimeValue.value = parseRuntimeProgress(schemaFixture.behind)
    stubRuntimeReads()
    show('#system')
    const text = "The agent runtime's database is missing migrations 0007_gate_tasks, 0008_attempt_prepare_failed"
    expect(screen.getByRole('alert', { name: 'Agent runtime health' })).toHaveTextContent(text)
    await go('#work', 'Work')
    expect(screen.getByRole('alert', { name: 'Agent runtime health' })).toHaveTextContent(text)
    await go('#you', 'For you')
    expect(screen.queryByRole('alert', { name: 'Agent runtime health' })).toBeNull()
  })

  // R5:5.5-4 (PROD 2026-11-01: "Host mbp-dev is offline: its last poll was 14 days ago." on top of Work for two weeks):
  // Work's top leaves an offline host to Needs cleanup on its Activity page; System still says it; the rest stays on Work.
  it('Work leaves an offline host to Needs cleanup, System still says it at its top, and Work keeps every other line', async () => {
    const progress = parseRuntimeProgress(presenceFixture)
    runtimeValue.value = progress
    nowValue.value = Date.parse(progress.generated_at)
    stubRuntimeReads()
    show('#system')
    const host = 'Host h2 is offline: its last poll was'
    expect(screen.getByRole('alert', { name: 'Agent runtime health' })).toHaveTextContent(host)
    await go('#work', 'Work')
    const notice = screen.getByRole('alert', { name: 'Agent runtime health' })
    expect(notice).not.toHaveTextContent(host)
    expect(notice).toHaveTextContent("exec:claude is unavailable: the claude account's pools cool down until")
    expect(notice).toHaveTextContent('No executor can take these jobs now: chief_of_staff')
    expect(document.body).not.toHaveTextContent(host)
  })

  it('For you: the whole verdict, then what is yours and what is worth a look', () => {
    show('#you')
    expect(screen.getByRole('region', { name: 'Verdict' })).toHaveTextContent('2 things are yours')
    expect(headings()).toEqual(['For you'])
  })

  it('each other entry shows its own block, and the verdict is one line that goes to For you', () => {
    show('#work')
    expect(headings()).toEqual(['Work'])
    expect(screen.queryByRole('region', { name: 'Verdict' })).toBeNull()
    expect(screen.getByRole('link', { name: /2 things are yours/ })).toHaveAttribute('href', '#command')
  })

  it('System carries the data and the way into every detail page', () => {
    show('#system')
    expect(headings()).toEqual(['System', 'Storage and copies'])
    expect(screen.queryByRole('region', { name: 'Release lane' })).toBeNull()
    expect(screen.getByRole('region', { name: 'Health' })).toHaveTextContent('critical · yours')
    expect(screen.queryByRole('region', { name: 'Data' })).toBeNull()
  })

  // Navigation, second slice (W-31 2.2b): what was Needs you is For you's lower half, what was Progress is Work's.
  it('For you goes on to everything waiting, and the address can open it there', async () => {
    stubRuntimeReads()
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    show('#you?section=waiting')
    const waiting = screen.getByRole('region', { name: 'Everything waiting on you' })
    expect(waiting).toHaveAttribute('id', 'waiting')
    expect(within(waiting).getByTestId('needs-you-page')).toBeInTheDocument()
    expect(within(waiting).getByRole('region', { name: 'Sign off' })).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Yours' })).getByRole('link', { name: /All waiting/ })).toHaveAttribute('href', '#command?section=waiting')
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    expect(scroll.mock.contexts.some(el => el === waiting)).toBe(true)
  })


  it('the other entries do not carry the merged sections', () => {
    stubRuntimeReads()
    show('#system')
    expect(screen.queryByRole('region', { name: 'Everything waiting on you' })).toBeNull()
    expect(screen.queryByTestId('merged-progress')).toBeNull()
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




  it('puts the aggregate plan view on Work, and the runtime cards with the release lane on its Activity page', async () => {
    show('#work')
    expect(screen.getByTestId('plan-view')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Coordinator' })).toBeNull()
    await go('#work?section=activity', 'Work')
    const now = screen.getByRole('region', { name: 'Coordinator' })
    for (const name of ['Release lane']) {
      expect(now.compareDocumentPosition(screen.getByRole('region', { name })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    expect(screen.queryByTestId('plan-view')).toBeNull()
  })


  it('shows the coordinating thread\'s latest note from the mailbox, not the board', () => {
    show('#work?section=activity')
    const now = screen.getByRole('region', { name: 'Coordinator' })
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
      show('#work?section=activity')
      expect(screen.getByRole('region', { name: 'Coordinator' })).toHaveTextContent(line)
    } finally {
      coordinatorView.value = undefined
    }
  })



})

