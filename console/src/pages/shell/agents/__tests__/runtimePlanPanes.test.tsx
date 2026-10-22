import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import moved from '@/api/__fixtures__/agentrt-plan-moved.testdb.json'
import planApprovals from '@/api/__fixtures__/agentrt-plan-approvals.testdb.json'
// signoffs: the same ledger with done rules waiting on the Owner's sign-off (agentrtapi TestOwnerSignsAStepOff).
import planSignoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
// GET /progress with an architecture root, as store.Progress answered on a test database: four rows,
// ticked true (two gates passed on stg, a thread's note), false (no pass on stg), unknown with no gate
// test (the Owner's note) and unknown with nothing recorded for prod (W-31 2.5 ⑤ G2). Captured once by a
// temporary test in api/internal/agentrt/store (newKernelWorld; the rows made with CreatePlanNode and their
// rules set under a plan_changed event; SeeDeploy for stg; two gate verifications; PostPlanNote twice;
// store.Progress marshalled as agentrtapi's GET /progress writes it), then deleted.
import archFixture from '@/api/__fixtures__/agentrt-progress-arch.testdb.json'
import poolsFixture from '../../owner/__tests__/fixtures/pools-utc.json'
import { IMPORT_HEAD_PAGES, runtimeArch } from '@/api/agentRuntimePlan'
import rootOnly from '@/api/__fixtures__/agentrt-plan-root-only.testdb.json'
import { STREAM_PAGE, planStream, streamPage } from '@/test/agentrtPlanStream'
import { ProgressOverview } from '../../progress/ProgressOverview'
import { AgentsPlan } from '../AgentsPlan'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED, archRead, useRuntimeArch, useRuntimePlan } from '../useRuntimePlan'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { unavailableLine } from '@/test/lastRead'

// The plan panes read the runtime's plan (2.5 slice 2): GET /plan?root=R2, the import request its head
// names, and GET /progress's counts, as the platform answered them on a test database (agentrtapi over
// the round-2 ledger fixture), and the architecture table from GET /progress (2.5 ⑤ G2). The board is
// retired: a root the runtime does not hold is said to be missing, never read from the board.

const source = vi.hoisted(() => ({
  plan: 'imported' as 'imported' | 'moved' | 'root-only' | 'approvals' | 'signoffs' | 'absent' | 'fail', progressFails: false, arch: false,
  // The plan stream: as imported; a cursor that stays put; more pages than are read for the head, none of them with it.
  events: 'imported' as 'imported' | 'stuck' | 'endless', artifact: 'ok' as 'ok' | 'fail' | 'not json',
}))
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
const stuck = planStream(2 * STREAM_PAGE, false), endless = planStream((IMPORT_HEAD_PAGES + 10) * STREAM_PAGE, false)
// approvals: the same round with done rules naming requests (agentrtapi TestPlanStepsNameTheirApprovals); its counts,
// stream and request are the imported round's: the same ledger.
const fixture = () => source.plan === 'moved' ? moved : source.plan === 'root-only' ? rootOnly
  : source.plan === 'approvals' ? { ...imported, plan: planApprovals.plan }
  : source.plan === 'signoffs' ? { ...imported, plan: planSignoffs.plan } : imported
const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  if (url.includes('/agentrt/plan?')) {
    if (source.plan === 'absent') return json(imported.absent.body, imported.absent.status)
    if (source.plan === 'fail') return json({ error: 'unavailable', message: 'the runtime is not connected' }, 503)
    return json(fixture().plan)
  }
  if (url.includes('/agentrt/events?stream=plan')) {
    if (source.events === 'stuck') return json(streamPage(stuck, '?after_seq=0'))
    return json(streamPage(source.events === 'endless' ? endless : source.plan === 'root-only' ? rootOnly.events : imported.events, url))
  }
  // An escaped id named no row on the platform until 2026-10-21 (PROD answered sha256%3A… with this 404).
  if (url.includes('/agentrt/artifacts/') && /%3A/i.test(url)) return json({ error: 'not_found', message: 'no such record' }, 404)
  if (url.includes('/agentrt/artifacts/')) return source.artifact === 'fail' ? json({ error: 'internal', message: 'internal error' }, 500)
    : new Response(source.artifact === 'not json' ? '<html>proxy</html>' : JSON.stringify(imported.request))
  if (url.includes('/agentrt/progress')) {
    if (source.progressFails) return json({ error: 'unavailable' }, 503)
    // A runtime that holds the round and, once imported, the architecture root beside it.
    const progress = fixture().progress
    return json(source.arch ? { ...progress, plan: [...progress.plan, ...archFixture.plan.filter(r => r.kind === 'arch_root')] } : progress)
  }
  if (url.includes('/agentrt/pools')) return json(poolsFixture)
  if (url.includes('/auth/capabilities')) return json({ authenticated: false })
  throw new Error(`Unexpected URL ${url}`)
})
function Overview() {
  return <ProgressOverview count={{ state: 'known', count: 0 }} plan={useRuntimePlan()} arch={useRuntimeArch()} failed={false} />
}
const boardRead = () => fetchMock.mock.calls.some(call => String(call[0]).includes('/agent/progress'))
const clients: QueryClient[] = []
function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  return { client, ...render(<QueryClientProvider client={client}>{node}</QueryClientProvider>) }
}
beforeEach(() => {
  source.plan = 'imported'
  source.progressFails = false
  source.arch = false
  source.events = 'imported'
  source.artifact = 'ok'
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals(); fetchMock.mockClear() })

const boardOnly = /2\.2 接口层与 agentd/
const counted = (f: typeof imported | typeof moved) => f.progress.plan[0]

describe('the plan pane on Progress', () => {
  it('reads an imported plan from the runtime: its items, counts, each step\'s ledger date and line, nothing kept by hand', async () => {
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    expect(screen.getByText('From the agent runtime; each architecture row is ticked by its gate tests')).toBeInTheDocument()
    // Imported done steps are done by their manual atom: counted as done and marked manual (v5 §5.1.2, T24).
    expect(screen.getByText('2.0 设计对齐：v4 到 v5 · 3/3 · 3 manual · done')).toBeInTheDocument()
    expect(screen.getByText('2.1 数据层：状态库 · 2/5 · 2 manual · doing')).toBeInTheDocument()
    expect(screen.getByText('2.4 缩进用不换行空格 · 0/2 · doing')).toBeInTheDocument()
    expect(screen.getByText('2026-10-14 01:09')).toBeInTheDocument()
    expect(screen.getAllByText('Ledger line').length).toBeGreaterThan(0)
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
    expect(container).not.toHaveTextContent('✎')
    // This runtime holds no architecture root: said so, not read from the board.
    expect(await screen.findByText(ARCH_NOT_IMPORTED)).toBeInTheDocument()
    expect(boardRead()).toBe(false)
  })

  it('links a step done by a request to that request, by its number (v4 §20.5 scenario 10)', async () => {
    source.plan = 'approvals'
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const links = [...container.querySelectorAll('a[data-step-approval]')].map(a => [a.textContent, a.getAttribute('href')])
    expect(links).toEqual([['Request #16', '#approvals?id=16'], ['Request #17', '#approvals?id=17'], ['Request #16', '#approvals?id=16'],
      ['Request #18', '#approvals?id=18']])
  })

  it('says the plan is not in the runtime while it holds none (404), and never reads the board', async () => {
    source.plan = 'absent'
    show(<AgentsPlan />)
    expect(await screen.findByText(PLAN_NOT_IMPORTED)).toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(boardRead()).toBe(false)
  })

  it('shows a failed read as failed, never the board\'s plan', async () => {
    source.plan = 'fail'
    show(<AgentsPlan />)
    expect(await screen.findByText(unavailableLine('Plan unavailable: Plan: unavailable', 'never'))).toHaveAttribute('role', 'alert')
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
  })

  it('once read from the runtime, a later failed read is an error, not the last plan or the board\'s', async () => {
    const { client } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    source.plan = 'fail'
    await client.refetchQueries({ queryKey: ['agent-runtime-plan'] })
    // The plan was read whole before: the line says when (review 488 B1).
    expect(await screen.findByText(unavailableLine('Plan unavailable: Plan: unavailable', 'last'))).toBeInTheDocument()
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  it('shows each step\'s Owner and ★ marks from the runtime, whatever its title says (review 416 C2)', async () => {
    show(<AgentsPlan />)
    await screen.findByText('Round 2 · from the agent runtime')
    // R2:2.1-5 is an effect step whose title carries no ★; R2:2.1-3 is the Owner's.
    // The title and the ledger line under it are in the same row.
    const effect = screen.getAllByText(/本机测试库可用/)[0].closest('li')!
    expect(within(effect).getByTitle('An effect step (effect_step)')).toHaveTextContent('★')
    expect(within(effect).queryByTitle("The Owner's step (owner_step)")).not.toBeInTheDocument()
    const owners = screen.getAllByText(/批 PROD 迁移作业/)[0].closest('li')!
    expect(within(owners).getByTitle("The Owner's step (owner_step)")).toHaveTextContent('Owner')
    expect(within(owners).queryByTitle('An effect step (effect_step)')).not.toBeInTheDocument()
  })

  it('shows a round with no items as empty, with zero counts, not as unavailable', async () => {
    source.plan = 'root-only'
    show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText("No items in the runtime's plan.")).toBeInTheDocument()
    expect(screen.getByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    expect(screen.getByText('0/0 items')).toBeInTheDocument()
    expect(screen.getByText('0/0 steps')).toBeInTheDocument()
    expect(screen.queryByText(/Plan unavailable/)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  it('counts that fail after a good read make the plan unavailable in both panes, not the last counts (review 416 B2)', async () => {
    const { client } = show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    expect(screen.getByText(`${counted(imported).count.done}/${counted(imported).count.total} steps`)).toBeInTheDocument()
    source.progressFails = true
    await client.refetchQueries({ queryKey: ['agent-runtime-progress'] })
    expect(await screen.findAllByText(unavailableLine('Plan unavailable: Agent runtime: unavailable'))).toHaveLength(2)
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(screen.queryByText(/ steps$/)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  // 2026-10-21 PROD: the import request was read by sha256%3A…, the platform answered 404, and the whole plan, with the
  // Owner's Sign off, was "Plan unavailable". The request only gives ledger lines and imported steps' dates.
  it('reads the import request by its artifact id, colon kept, and shows each step\'s ledger line', async () => {
    show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const reads = fetchMock.mock.calls.map(c => String(c[0])).filter(url => url.includes('/agentrt/artifacts/'))
    expect(reads.length).toBeGreaterThan(0)
    for (const url of reads) expect(url).toMatch(/\/api\/v1\/agentrt\/artifacts\/sha256:[0-9a-f]{64}$/)
    expect(screen.getAllByText('Ledger line').length).toBeGreaterThan(0)
    expect(screen.queryByText(/The ledger lines could not be read/)).not.toBeInTheDocument()
  })

  it('a request that fails on a read after a good one leaves the plan standing, without the lines read before (review 418)', async () => {
    const { client } = show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const steps = `${counted(imported).count.done}/${counted(imported).count.total} steps`
    expect(screen.getByText(steps)).toBeInTheDocument()
    source.artifact = 'fail'
    await client.refetchQueries({ queryKey: ['agent-runtime-plan-request', 'R2'] })
    expect(await screen.findByText(/^The ledger lines could not be read: Plan import request: internal\. The steps are shown without them/)).toBeInTheDocument()
    expect(screen.getByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    expect(screen.getByText(steps)).toBeInTheDocument()
    expect(screen.queryByText('Ledger line')).not.toBeInTheDocument()
    expect(screen.queryByText(/Plan unavailable/)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  it.each([
    ['the request cannot be read', { artifact: 'fail' }, 'Plan import request: internal'],
    ['the request is not one', { artifact: 'not json' }, 'Plan import request: invalid artifact'],
    ['the plan stream\'s cursor does not move on', { events: 'stuck' }, 'Plan events: invalid pagination cursor'],
    ['the import head is not within the pages read', { events: 'endless' }, `Plan events: no import head in the first ${IMPORT_HEAD_PAGES} pages of plan:R2`],
  ] as const)('a plan whose import request cannot be had stands without its ledger lines and imported dates: %s', async (_, set, reason) => {
    Object.assign(source, set)
    show(<AgentsPlan />)
    const escaped = reason.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    expect(await screen.findByText(new RegExp(`^The ledger lines could not be read: ${escaped}\\. The steps are shown without them`))).toHaveAttribute('role', 'status')
    expect(screen.getByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    expect(screen.getByText('2.1 数据层：状态库 · 2/5 · 2 manual · doing')).toBeInTheDocument()
    expect(screen.queryByText('Ledger line')).not.toBeInTheDocument()
    // The ledger's date of an imported step is not known, and the import's own moment is not told in its place.
    expect(screen.queryByText('2026-10-14 01:09')).not.toBeInTheDocument()
    expect(screen.queryByText(/Plan unavailable/)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  it('keeps the Owner\'s Sign off when the import request cannot be read (PROD 2026-10-21)', async () => {
    source.plan = 'signoffs'
    source.artifact = 'fail'
    show(<PlatformAuthProvider><AgentsPlan /></PlatformAuthProvider>)
    expect(await screen.findByText(/^The ledger lines could not be read: /)).toBeInTheDocument()
    // In the block at the top and under each step of the plan.
    for (const name of ['Sign off a', 'Sign off b', 'Sign off']) expect(screen.getAllByRole('button', { name }).length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: 'Waiting for your sign-off · 2' })).toBeInTheDocument()
    expect(screen.getByText(/^Signed off exam-2\.4-1 by the Owner, /)).toBeInTheDocument()
    expect(screen.queryByText(/Plan unavailable/)).not.toBeInTheDocument()
  })

  it('a plan whose counts cannot be read is an error too', async () => {
    source.progressFails = true
    show(<AgentsPlan />)
    expect(await screen.findByText(/^Plan unavailable: Agent runtime:/)).toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })
})

describe('the round progress on the overview', () => {
  it.each([['imported', imported], ['moved on', moved]] as const)('counts as GET /progress does: %s', async (name, f) => {
    source.plan = name === 'imported' ? 'imported' : 'moved'
    show(<Overview />)
    const c = counted(f)
    const card = (await screen.findByText(`${c.count.done}/${c.count.total} steps`)).closest('div.flex') as HTMLElement
    const doneItems = c.items.filter(i => i.count.state === 'done').length
    expect(within(card).getByText(`${doneItems}/${c.items.length} items`)).toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
  })

  it('says the plan is not in the runtime while it holds none; a failed read is unavailable', async () => {
    source.plan = 'absent'
    const first = show(<Overview />)
    expect(await screen.findByText(PLAN_NOT_IMPORTED)).toBeInTheDocument()
    expect(screen.queryByText(/ steps$/)).not.toBeInTheDocument()
    first.unmount()
    source.plan = 'fail'
    show(<Overview />)
    expect(await screen.findByText(unavailableLine('Plan unavailable: Plan: unavailable'))).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText(/ steps$/)).not.toBeInTheDocument())
  })
})

describe('the architecture table from the runtime (2.5 ⑤ G2)', () => {
  const rows = archFixture.plan.find(r => r.kind === 'arch_root')!.rows!

  it('shows each row ticked by its gate tests, why, and its latest note', async () => {
    source.arch = true
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('架构对照 · 1 passed · 1 not yet · 1 no gate yet · 1 unknown')).toBeInTheDocument()
    for (const row of rows) {
      const el = container.querySelector(`[data-arch-row="${row.plan_node_id}"]`) as HTMLElement
      // A row with no gate yet is that, not unknown (TD-397).
      expect(el, row.plan_node_id).toHaveAttribute('data-tick', row.reason === 'no gate test yet' ? 'nogate' : row.tick)
      expect(el).toHaveTextContent(row.title)
      expect(el).toHaveTextContent(row.reason)
      if (row.note) {
        expect(el).toHaveTextContent(`${row.note.text} · ${row.note.by}, ${row.note.at.slice(0, 16).replace('T', ' ')}Z`)
        expect(el.querySelector(`[title="${row.note.at}"]`)).not.toBeNull()
      }
      else expect(el).toHaveTextContent('No note on what it is now.')
    }
    expect(container.querySelector('[data-arch-row="ARCH:state-db"]')).toHaveTextContent('passed')
    expect(container.querySelector('[data-arch-row="ARCH:bus"]')).toHaveTextContent('not yet')
    expect(container.querySelector('[data-tick="nogate"]')).toHaveTextContent('no gate yet')
    expect(screen.queryByText(ARCH_NOT_IMPORTED)).not.toBeInTheDocument()
    expect(boardRead()).toBe(false)
  })

  it('counts the rows by tick on the overview', async () => {
    source.arch = true
    show(<Overview />)
    expect(await screen.findByText('1/4 passed')).toBeInTheDocument()
    expect(screen.getByText('1/4 not yet')).toBeInTheDocument()
    expect(screen.getByText('1/4 no gate yet')).toBeInTheDocument()
    expect(screen.getByText('1/4 unknown')).toBeInTheDocument()
  })

  it('a read that fails after a good one is unavailable, never the rows read before', async () => {
    source.arch = true
    const { client } = show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText('1/4 passed')).toBeInTheDocument()
    source.progressFails = true
    await client.refetchQueries({ queryKey: ['agent-runtime-progress'] })
    expect(await screen.findAllByText(unavailableLine('Architecture unavailable: Agent runtime: unavailable'))).toHaveLength(2)
    expect(screen.queryByText('1/4 passed')).not.toBeInTheDocument()
    expect(document.querySelector('[data-arch-row]')).toBeNull()
  })

  it('a tick it does not know is unknown, never a pass', () => {
    const odd = runtimeArch([{ kind: 'arch_root', plan_node_id: 'ARCH', title: 't', rows: [
      { plan_node_id: 'ARCH:a', title: 'a', tick: true, reason: '' }, { plan_node_id: 'ARCH:b', title: 'b', tick: 'maybe' },
      { plan_node_id: 'ARCH:c', title: 'c', tick: 'false', note: { text: '' } }, { plan_node_id: 'ARCH:d', title: 'd' },
    ] }])
    expect(odd?.rows.map(r => r.tick)).toEqual(['unknown', 'unknown', 'false', 'unknown'])
    expect(odd?.rows[2].note).toBeNull()
    expect(runtimeArch([{ kind: 'round', plan_node_id: 'R2' }])).toBeNull()
  })
})

// Review 431 B1, B2: what is read decides what is said. An answer that is not a progress answer is a failed
// read, never "not imported" or a table of no rows; a plan read empty is not imported; a paused read is
// offline, with or without an answer before it.
describe('reading the architecture table (review 431)', () => {
  const ok = (data: unknown) => ({ data: parseRuntimeProgress(data), error: null, isError: false, isPaused: false })
  it('is a failed read when the answer has no plan list, or its architecture root no rows list', () => {
    for (const body of [{}, { plan: null }, { plan: {} }]) {
      expect(archRead(ok(body)), JSON.stringify(body)).toEqual({ from: 'error', error: 'Agent runtime: the progress answer has no plan list' })
    }
    expect(archRead(ok({ plan: [{ kind: 'arch_root', plan_node_id: 'ARCH' }] }))).toEqual({ from: 'error', error: 'Agent runtime: the architecture root has no rows list' })
    expect(archRead(ok({ plan: [] }))).toEqual({ from: 'absent' })
    expect(archRead(ok({ plan: [{ kind: 'arch_root', plan_node_id: 'ARCH', rows: [] }] }))).toMatchObject({ from: 'runtime', table: { rows: [] } })
  })
  it('is offline when paused, with or without an answer read before', () => {
    const answer = parseRuntimeProgress(archFixture)
    expect(archRead({ data: answer, error: null, isError: false, isPaused: true })).toEqual({ from: 'error', error: 'offline' })
    expect(archRead({ data: undefined, error: null, isError: false, isPaused: true })).toEqual({ from: 'error', error: 'offline' })
    expect(archRead({ data: undefined, error: null, isError: false, isPaused: false })).toEqual({ from: 'loading' })
  })
})
