import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import moved from '@/api/__fixtures__/agentrt-plan-moved.testdb.json'
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
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED, useRuntimeArch, useRuntimePlan } from '../useRuntimePlan'

// The plan panes read the runtime's plan (2.5 slice 2): GET /plan?root=R2, the import request its head
// names, and GET /progress's counts, as the platform answered them on a test database (agentrtapi over
// the round-2 ledger fixture), and the architecture table from GET /progress (2.5 ⑤ G2). The board is
// retired: a root the runtime does not hold is said to be missing, never read from the board.

const source = vi.hoisted(() => ({
  plan: 'imported' as 'imported' | 'moved' | 'root-only' | 'absent' | 'fail', progressFails: false, arch: false,
  // The plan stream: as imported; a cursor that stays put; more pages than are read for the head, none of them with it.
  events: 'imported' as 'imported' | 'stuck' | 'endless', artifact: 'ok' as 'ok' | 'fail' | 'not json',
}))
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
const stuck = planStream(2 * STREAM_PAGE, false), endless = planStream((IMPORT_HEAD_PAGES + 10) * STREAM_PAGE, false)
const fixture = () => source.plan === 'moved' ? moved : source.plan === 'root-only' ? rootOnly : imported
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
  if (url.includes('/agentrt/artifacts/')) return source.artifact === 'fail' ? json({ error: 'internal', message: 'internal error' }, 500)
    : new Response(source.artifact === 'not json' ? '<html>proxy</html>' : JSON.stringify(imported.request))
  if (url.includes('/agentrt/progress')) {
    if (source.progressFails) return json({ error: 'unavailable' }, 503)
    // A runtime that holds the round and, once imported, the architecture root beside it.
    const progress = fixture().progress
    return json(source.arch ? { ...progress, plan: [...progress.plan, ...archFixture.plan.filter(r => r.kind === 'arch_root')] } : progress)
  }
  if (url.includes('/agentrt/pools')) return json(poolsFixture)
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
    expect(screen.getByText('2.0 设计对齐：v4 到 v5 · 3/3 · done')).toBeInTheDocument()
    expect(screen.getByText('2.1 数据层：状态库 · 2/5 · doing')).toBeInTheDocument()
    expect(screen.getByText('2026-10-14 01:09')).toBeInTheDocument()
    expect(screen.getAllByText('Ledger line').length).toBeGreaterThan(0)
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
    expect(container).not.toHaveTextContent('✎')
    // This runtime holds no architecture root: said so, not read from the board.
    expect(await screen.findByText(ARCH_NOT_IMPORTED)).toBeInTheDocument()
    expect(boardRead()).toBe(false)
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
    expect(await screen.findByText('Plan unavailable: Plan: unavailable')).toHaveAttribute('role', 'alert')
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
  })

  it('once read from the runtime, a later failed read is an error, not the last plan or the board\'s', async () => {
    const { client } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    source.plan = 'fail'
    await client.refetchQueries({ queryKey: ['agent-runtime-plan'] })
    expect(await screen.findByText('Plan unavailable: Plan: unavailable')).toBeInTheDocument()
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
    expect(await screen.findAllByText('Plan unavailable: Agent runtime: unavailable')).toHaveLength(2)
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(screen.queryByText(/ steps$/)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
  })

  it('a request that fails on a read after a good one makes the plan unavailable in both panes, not the plan read before (review 418)', async () => {
    const { client } = show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const steps = `${counted(imported).count.done}/${counted(imported).count.total} steps`
    expect(screen.getByText(steps)).toBeInTheDocument()
    source.artifact = 'fail'
    await client.refetchQueries({ queryKey: ['agent-runtime-plan-request', 'R2'] })
    expect(await screen.findAllByText('Plan unavailable: Plan import request: internal')).toHaveLength(2)
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(screen.queryByText(steps)).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
    expect(screen.queryByText(PLAN_NOT_IMPORTED)).not.toBeInTheDocument()
  })

  it.each([
    ['the request cannot be read', { artifact: 'fail' }, 'Plan unavailable: Plan import request: internal'],
    ['the request is not one', { artifact: 'not json' }, 'Plan unavailable: Plan import request: invalid artifact.'],
    ['the plan stream\'s cursor does not move on', { events: 'stuck' }, 'Plan unavailable: Plan events: invalid pagination cursor'],
    ['the import head is not within the pages read', { events: 'endless' }, `Plan unavailable: Plan events: no import head in the first ${IMPORT_HEAD_PAGES} pages of plan:R2.`],
  ] as const)('a plan whose import request cannot be had is unavailable: %s', async (_, set, line) => {
    Object.assign(source, set)
    show(<AgentsPlan />)
    expect(await screen.findByText(line)).toHaveAttribute('role', 'alert')
    expect(screen.queryByText('Round 2 · from the agent runtime')).not.toBeInTheDocument()
    expect(screen.queryByText(boardOnly)).not.toBeInTheDocument()
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
    expect(await screen.findByText('Plan unavailable: Plan: unavailable')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText(/ steps$/)).not.toBeInTheDocument())
  })
})

describe('the architecture table from the runtime (2.5 ⑤ G2)', () => {
  const rows = archFixture.plan.find(r => r.kind === 'arch_root')!.rows!

  it('shows each row ticked by its gate tests, why, and its latest note', async () => {
    source.arch = true
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('架构对照 · 1 passed · 1 not yet · 2 unknown')).toBeInTheDocument()
    for (const row of rows) {
      const el = container.querySelector(`[data-arch-row="${row.plan_node_id}"]`) as HTMLElement
      expect(el, row.plan_node_id).toHaveAttribute('data-tick', row.tick)
      expect(el).toHaveTextContent(row.title)
      expect(el).toHaveTextContent(row.reason)
      if (row.note) expect(el).toHaveTextContent(`${row.note.text} · ${row.note.by}`)
      else expect(el).toHaveTextContent('No note on what it is now.')
    }
    expect(container.querySelector('[data-arch-row="ARCH:state-db"]')).toHaveTextContent('passed')
    expect(container.querySelector('[data-arch-row="ARCH:bus"]')).toHaveTextContent('not yet')
    expect(screen.queryByText(ARCH_NOT_IMPORTED)).not.toBeInTheDocument()
    expect(boardRead()).toBe(false)
  })

  it('counts the rows by tick on the overview', async () => {
    source.arch = true
    show(<Overview />)
    expect(await screen.findByText('1/4 passed')).toBeInTheDocument()
    expect(screen.getByText('1/4 not yet')).toBeInTheDocument()
    expect(screen.getByText('2/4 unknown')).toBeInTheDocument()
  })

  it('a read that fails after a good one is unavailable, never the rows read before', async () => {
    source.arch = true
    const { client } = show(<><Overview /><AgentsPlan /></>)
    expect(await screen.findByText('1/4 passed')).toBeInTheDocument()
    source.progressFails = true
    await client.refetchQueries({ queryKey: ['agent-runtime-progress'] })
    expect(await screen.findAllByText('Architecture unavailable: Agent runtime: unavailable')).toHaveLength(2)
    expect(screen.queryByText('1/4 passed')).not.toBeInTheDocument()
    expect(document.querySelector('[data-arch-row]')).toBeNull()
  })

  it('a tick it does not know is unknown, never a pass', () => {
    const odd = runtimeArch([{ kind: 'arch_root', plan_node_id: 'ARCH', title: 't', rows: [
      { plan_node_id: 'ARCH:a', title: 'a', tick: true, reason: '' }, { plan_node_id: 'ARCH:b', title: 'b', tick: 'maybe' },
      { plan_node_id: 'ARCH:c', title: 'c', tick: 'false', note: { text: '' } },
    ] }])
    expect(odd?.rows.map(r => r.tick)).toEqual(['unknown', 'unknown', 'false'])
    expect(odd?.rows[2].note).toBeNull()
    expect(runtimeArch([{ kind: 'round', plan_node_id: 'R2' }])).toBeNull()
  })
})
