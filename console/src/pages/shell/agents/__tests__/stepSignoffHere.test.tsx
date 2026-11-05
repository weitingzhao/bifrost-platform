import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import signoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { StepSignoffHere } from '../StepSignoffHere'

// 11-05: the Owner opened R5:5.13-3's own address, read "You sign off …" under Done when and found no button; the only
// one was row 66 of 75 under Sign-offs › Not started, folded. The step card now carries it. The fixture is the GET
// /plan?root=R2 test-database answer stepSignoffs.test.tsx reads: 2.3-2 waits on race (planned), 2.4-1 is signed.

const TOKEN = 'fixture-admin-token'
const ADMIN: AuthCapabilities = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
const auth = vi.hoisted(() => ({ caps: null as AuthCapabilities | null }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps ?? { authenticated: false }) }
})

const sent: { url: string; body: unknown }[] = []
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
const serve = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  if (url.includes('/signoff')) {
    sent.push({ url, body: JSON.parse(String(init?.body)) })
    return json({ ...signoffs.signed, plan_node_id: 'R2:2.3-2', ref: 'race', already_signed: false })
  }
  // GET /plan/{id}: the node as GET /plan lists it (with its sign-offs).
  const m = url.match(/\/agentrt\/plan\/([^/?]+)$/)
  if (m) return json(signoffs.plan.nodes.find(n => n.plan_node_id === decodeURIComponent(m[1])) ?? {})
  return json({})
}
const fetchMock = vi.fn(serve)
const ruleOf = (node: string) => signoffs.plan.nodes.find(n => n.plan_node_id === node)?.done_rule_json
let client: QueryClient
const card = (node: string, state: string, o: { now?: number; viewStale?: boolean } = {}) => <QueryClientProvider client={client}><PlatformAuthProvider>
  <StepSignoffHere node={node} label={node.slice(3)} state={state} rule={ruleOf(node)} now={o.now ?? Date.now()} viewStale={o.viewStale ?? false} /></PlatformAuthProvider></QueryClientProvider>
function show(node: string, state: string, o: { now?: number; viewStale?: boolean } = {}) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(card(node, state, o))
}
beforeEach(() => {
  auth.caps = ADMIN
  const mem = new Map<string, string>([[PLATFORM_TOKEN_KEY, TOKEN]])
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    get length() { return mem.size }, clear: () => mem.clear(), getItem: (k: string) => mem.get(k) ?? null,
    key: (i: number) => [...mem.keys()][i] ?? null, removeItem: (k: string) => { mem.delete(k) }, setItem: (k: string, v: string) => { mem.set(k, String(v)) },
  } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear(); fetchMock.mockImplementation(serve); sent.length = 0 })

describe("the step card's own Sign off", () => {
  it('shows a planned step its Sign off with the warning, and sends it', async () => {
    const { container } = show('R2:2.3-2', 'planned')
    const press = await screen.findByRole('button', { name: 'Sign off' })
    const here = container.querySelector('[data-step-signoff-here="R2:2.3-2"]') as HTMLElement
    expect(within(here).getByText(/^No task on this step ran in Ops/)).toBeInTheDocument()
    await vi.waitFor(() => expect(press).toBeEnabled())
    fireEvent.click(press)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
    await vi.waitFor(() => expect(sent).toEqual([{ url: '/api/v1/agentrt/plan/R2:2.3-2/signoff', body: { ref: 'race' } }]))
  })

  it('shows a signed step its record and no button', async () => {
    const { container } = show('R2:2.4-1', 'active')
    await screen.findByText(/^Signed off exam-2\.4-1 by the Owner, /)
    expect(within(container).queryByRole('button')).toBeNull()
  })

  it('reads nothing and draws nothing for a step whose rule waits on no sign-off, or one closed', async () => {
    const none = show('R2:2.4-2', 'active')
    const dropped = show('R2:2.1-4', 'dropped')
    await new Promise(r => setTimeout(r, 50))
    expect(fetchMock.mock.calls.filter(([u]) => String(u).includes('/agentrt/plan/'))).toEqual([])
    expect(none.container.querySelector('[data-step-signoff-here]')).toBeNull()
    expect(within(dropped.container).queryByRole('button')).toBeNull()
  })

  // Review of R5:5.5-13: a two-minute-old cached step with a hanging refresh kept Confirm sign-off enabled and sent it.
  it('signs nothing from a step read longer ago than twice its poll while its refresh hangs', async () => {
    const step = signoffs.plan.nodes.find(n => n.plan_node_id === 'R2:2.3-2')!
    fetchMock.mockImplementation(async () => new Promise<Response>(() => {}))
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const now = Date.now()
    client.setQueryData(['agent-runtime-plan', 'node', 'R2:2.3-2'], (await import('@/api/agentRuntimePlan')).signoffsByStep({ nodes: [step] }).get('R2:2.3-2'), { updatedAt: now - 120_000 })
    const { container } = render(card('R2:2.3-2', 'planned', { now }))
    const press = await screen.findByRole('button', { name: 'Sign off' })
    expect(press).toBeDisabled()
    expect(within(container).getByText('Refresh the plan before signing.')).toBeInTheDocument()
    fireEvent.click(press)
    expect(screen.queryByRole('button', { name: 'Confirm sign-off' })).toBeNull()
    expect(sent).toEqual([])
  })

  it("voids an open confirmation when the module page's plan view goes stale, and sends nothing", async () => {
    const { rerender } = show('R2:2.3-2', 'planned')
    const press = await screen.findByRole('button', { name: 'Sign off' })
    await vi.waitFor(() => expect(press).toBeEnabled())
    fireEvent.click(press)
    const confirm = screen.getByRole('button', { name: 'Confirm sign-off' })
    expect(confirm).toBeEnabled()
    rerender(card('R2:2.3-2', 'planned', { viewStale: true }))
    expect(screen.getByRole('button', { name: 'Confirm sign-off' })).toBeDisabled()
    expect(screen.getByText('Refresh the plan before signing.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
    // Fresh again, the confirmation stays void: it is reviewed again from the start.
    rerender(card('R2:2.3-2', 'planned', { viewStale: false }))
    expect(screen.getByRole('status')).toHaveTextContent('The sign-in or step changed. Cancel and review this sign-off again.')
    expect(screen.getByRole('button', { name: 'Confirm sign-off' })).toBeDisabled()
    await new Promise(r => setTimeout(r, 20))
    expect(sent).toEqual([])
  })

  it('reads only its own step, never the whole plan', async () => {
    show('R2:2.3-2', 'planned')
    await screen.findByRole('button', { name: 'Sign off' })
    const urls = fetchMock.mock.calls.map(([u]) => String(u))
    expect(urls.filter(u => u.includes('/agentrt/plan'))).toEqual(['/api/v1/agentrt/plan/R2:2.3-2'])
  })
})
