import { CapabilityEvidence } from '../CapabilityEvidence'
import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import moved from '@/api/__fixtures__/agentrt-plan-moved.testdb.json'
import planApprovals from '@/api/__fixtures__/agentrt-plan-approvals.testdb.json'
// signoffs: the same ledger with done rules waiting on the Owner's sign-off (agentrtapi TestOwnerSignsAStepOff).
import planSignoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
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
import { ARCH_NOT_IMPORTED, archRead } from '../useRuntimePlan'
import { parseRuntimeProgress } from '@/api/agentRuntime'

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



describe('the architecture table from the runtime (2.5 ⑤ G2)', () => {
  const rows = archFixture.plan.find(r => r.kind === 'arch_root')!.rows!

  it('shows each row ticked by its gate tests, why, and its latest note', async () => {
    source.arch = true
    const { container } = show(<CapabilityEvidence />)
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
