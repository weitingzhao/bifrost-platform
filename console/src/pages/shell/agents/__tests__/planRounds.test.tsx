import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/agentrt-plan-r2.prod.json'
import { PLAN_ROOT, currentRound, planRounds } from '@/api/agentRuntimePlan'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { SignoffWaiting } from '../SignoffWaiting'
import { useRuntimePlan } from '../useRuntimePlan'

// The Owner 2026-10-22: round 3's steps had sign-off rules on PROD, yet the Console showed and listed round 2 only
// (PLAN_ROOT fixed on R2), so they could be neither seen nor signed. The panes now follow the newest round GET /progress
// counts, and the waits read every round. Round 3 here is PROD's round-2 tree renumbered: the same shapes, other ids.

type Node = { plan_node_id: string; parent_plan_node_id: string | null; node_kind: string; label: string; title: string; state: string | null
  done_rule_json?: unknown; signoffs?: unknown[] }
type Doc = { root: string; nodes: Node[] }
const r2 = prod.plan as unknown as Doc
function renumber(doc: Doc, n: number): Doc {
  // A node's id is its round and its label (R3:3.2-28), as the plan view checks.
  const id = (s: string) => s.replace(/^R2(?=:|$)/, `R${n}`).replace(/^(R\d+:)2/, `$1${n}`)
  return { ...doc, root: `R${n}`, nodes: doc.nodes.map(x => ({ ...x, plan_node_id: id(x.plan_node_id),
    parent_plan_node_id: x.parent_plan_node_id === null ? null : id(x.parent_plan_node_id), label: x.label.replace(/^2/, String(n)) })) }
}
const r3 = renumber(r2, 3)
// GET /progress counts each round from its own nodes, as the platform does.
function roundCount(doc: Doc) {
  const items = doc.nodes.filter(n => n.node_kind === 'item')
  return { plan_node_id: doc.root, kind: 'round', title: `第 ${doc.root.slice(1)} 轮`, items: items.map(i => {
    const steps = doc.nodes.filter(n => n.parent_plan_node_id === i.plan_node_id && n.state !== 'dropped')
    return { plan_node_id: i.plan_node_id, label: i.label, title: i.title,
      count: { done: steps.filter(s => s.state === 'done').length, total: steps.length, state: 'doing' } }
  }) }
}

const auth = vi.hoisted(() => ({ caps: { authenticated: true, role: 'admin', can_operate: true, can_admin: true } as AuthCapabilities }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps) }
})

function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
let rounds: Doc[] = [r2, r3]
let progressFails = false
const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  if (url.includes('/agentrt/plan?')) {
    const root = new URL(url, 'http://x').searchParams.get('root')
    const doc = rounds.find(d => d.root === root)
    return doc ? json(doc) : json({ error: 'not_found', message: 'no such root' }, 404)
  }
  if (url.includes('/agentrt/progress')) return progressFails ? json({ error: 'unavailable' }, 503) : json({ plan: rounds.map(roundCount) })
  if (url.includes('/agentrt/events')) return json([])
  throw new Error(`Unexpected URL ${url}`)
})
const planReads = () => fetchMock.mock.calls.map(c => String(c[0])).filter(u => u.includes('/agentrt/plan?'))
function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><PlatformAuthProvider>{node}</PlatformAuthProvider></QueryClientProvider>)
}
function RoundProbe() {
  const read = useRuntimePlan()
  return <p data-testid="round">{read.from === 'runtime' ? `round ${read.round.n}` : read.from}</p>
}
beforeEach(() => {
  rounds = [r2, r3]
  progressFails = false
  const mem = new Map<string, string>([[PLATFORM_TOKEN_KEY, 'fixture-admin-token']])
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    get length() { return mem.size }, clear: () => mem.clear(), getItem: (k: string) => mem.get(k) ?? null,
    key: (i: number) => [...mem.keys()][i] ?? null, removeItem: (k: string) => { mem.delete(k) }, setItem: (k: string, v: string) => { mem.set(k, v) },
  } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear() })

describe('the rounds GET /progress counts', () => {
  it('are its round entries by number, R10 after R9, without the architecture root or an id that is not a round', () => {
    const plan = [{ plan_node_id: 'R10', kind: 'round' }, { plan_node_id: 'ARCH', kind: 'arch_root' }, { plan_node_id: 'R9', kind: 'round' },
      { plan_node_id: 'R2', kind: 'round' }, { plan_node_id: 'X1', kind: 'round' }, { plan_node_id: 'R9', kind: 'round' }]
    expect(planRounds(plan)).toEqual(['R2', 'R9', 'R10'])
    expect(currentRound(plan)).toBe('R10')
  })

  it('fall back to the round the panes always read when GET /progress counts none', () => {
    expect(currentRound([])).toBe(PLAN_ROOT)
    expect(currentRound(undefined)).toBe(PLAN_ROOT)
    expect(currentRound([{ plan_node_id: 'ARCH', kind: 'arch_root' }])).toBe(PLAN_ROOT)
  })
})

describe('the plan pane follows the newest round', () => {
  it('reads round 3 once GET /progress counts it', async () => {
    show(<RoundProbe />)
    expect(await screen.findByText('round 3')).toBeInTheDocument()
    expect(planReads().some(u => u.includes('root=R3'))).toBe(true)
    expect(planReads().some(u => u.includes('root=R2'))).toBe(false)
  })

  it('reads round 2 while it is the only round', async () => {
    rounds = [r2]
    show(<RoundProbe />)
    expect(await screen.findByText('round 2')).toBeInTheDocument()
  })

  it('says the read failed when GET /progress fails, never an empty plan', async () => {
    progressFails = true
    show(<RoundProbe />)
    expect(await screen.findByText('error')).toBeInTheDocument()
  })
})

describe('the steps waiting on the Owner\'s sign-off, from every round', () => {
  it('lists round 2\'s and round 3\'s, round by round, each by number', async () => {
    show(<SignoffWaiting />)
    const block = await screen.findByRole('region', { name: 'Waiting for your sign-off' })
    await waitFor(() => expect(within(block).getByRole('heading', { name: 'Waiting for your sign-off · 20' })).toBeInTheDocument())
    const text = block.textContent ?? ''
    expect(text.indexOf('2.2-28')).toBeGreaterThanOrEqual(0)
    expect(text.indexOf('3.2-28')).toBeGreaterThan(text.lastIndexOf('2.5-4'))
    expect(planReads().some(u => u.includes('root=R2'))).toBe(true)
    expect(planReads().some(u => u.includes('root=R3'))).toBe(true)
  })

  it('lists round 2\'s alone while it is the only round', async () => {
    rounds = [r2]
    show(<SignoffWaiting />)
    expect(await screen.findByRole('heading', { name: 'Waiting for your sign-off · 10' })).toBeInTheDocument()
  })

  it('a round whose plan cannot be read makes the list a failed read, not a shorter list', async () => {
    rounds = [r2, r3]
    const missing = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input: RequestInfo | URL) =>
      String(input).includes('root=R3') ? json({ error: 'unavailable', message: 'down' }, 503) : missing(input))
    show(<SignoffWaiting />)
    await waitFor(() => expect(screen.queryByRole('heading', { name: /Waiting for your sign-off · \d+/ })).toBeNull())
    expect(await screen.findByText(/could not be read|not known/i)).toBeInTheDocument()
    fetchMock.mockImplementation(missing)
  })
})
