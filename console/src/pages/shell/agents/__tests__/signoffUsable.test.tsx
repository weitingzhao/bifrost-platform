import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/agentrt-plan-r2.prod.json'
import { plainTitle, signoffWaits } from '@/api/agentRuntimePlan'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { AgentsPlan } from '../AgentsPlan'
import { SignoffWaiting, SignoffWaitingLink } from '../SignoffWaiting'

// The Owner 2026-10-21, after #78: 「这个页面根本没有达到可用的级别」. He could not find the steps to sign
// (2.4-1, 2.4-2, 2.3-4, 2.5-1, 2.5-2, 2.5-3): no step number shown, three steps titled alike, "**" in titles, a
// ledger line a screen long. The fixture is PROD's GET /plan?root=R2 as the platform answered it on 2026-10-21
// (read through the platform MCP's get_plan): ten steps wait on an owner_signoff, none signed.

type Node = { plan_node_id: string; parent_plan_node_id: string | null; node_kind: string; label: string; title: string; state: string | null
  done_rule_json?: unknown; signoffs?: unknown[] }
const nodes = prod.plan.nodes as unknown as Node[]
const WAITING = ['2.2-28', '2.3-2', '2.3-3', '2.3-4', '2.4-1', '2.4-2', '2.5-1', '2.5-2', '2.5-3', '2.5-4']
const OWNERS = ['2.4-1', '2.4-2', '2.3-4', '2.5-1', '2.5-2', '2.5-3']

const auth = vi.hoisted(() => ({ caps: { authenticated: true, role: 'admin', can_operate: true, can_admin: true } as AuthCapabilities }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps) }
})

function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
let plan: { root: string; nodes: Node[] } = prod.plan as unknown as { root: string; nodes: Node[] }
const signed: string[] = []
// The round's counts as /progress gives them, from the same nodes (the plan pane reads both).
function progress() {
  const items = nodes.filter(n => n.node_kind === 'item')
  return { plan: [{ plan_node_id: 'R2', kind: 'round', title: '第 2 轮', items: items.map(i => {
    const steps = nodes.filter(n => n.parent_plan_node_id === i.plan_node_id && n.state !== 'dropped')
    return { plan_node_id: i.plan_node_id, label: i.label, title: i.title,
      count: { done: steps.filter(s => s.state === 'done').length, total: steps.length, state: 'doing' } }
  }) }] }
}
const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  if (url.includes('/signoff') && init?.method === 'POST') {
    const node = decodeURIComponent(url.split('/plan/')[1]!.split('/signoff')[0]!)
    const ref = (JSON.parse(String(init.body)) as { ref: string }).ref
    signed.push(`${node} ${ref}`)
    // The platform records it; the plan read next carries it.
    plan = { ...plan, nodes: plan.nodes.map(n => n.plan_node_id === node ? { ...n, signoffs: [{ ref, by: 'owner', at: '2026-10-21T23:00:00Z', event_id: 1 }] } : n) }
    return json({ plan_node_id: node, ref, by: 'owner', at: '2026-10-21T23:00:00Z', event_id: 1, already_signed: false })
  }
  if (url.includes('/agentrt/plan?')) return json(plan)
  if (url.includes('/agentrt/events?stream=plan')) return json([])
  if (url.includes('/agentrt/progress')) return json(progress())
  if (url.includes('/agentrt/events')) return json([])
  throw new Error(`Unexpected URL ${url}`)
})
function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { client, ...render(<QueryClientProvider client={client}><PlatformAuthProvider>{node}</PlatformAuthProvider></QueryClientProvider>) }
}
beforeEach(() => {
  plan = prod.plan as unknown as { root: string; nodes: Node[] }
  signed.length = 0
  const mem = new Map<string, string>([[PLATFORM_TOKEN_KEY, 'fixture-admin-token']])
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    get length() { return mem.size }, clear: () => mem.clear(), getItem: (k: string) => mem.get(k) ?? null,
    key: (i: number) => [...mem.keys()][i] ?? null, removeItem: (k: string) => { mem.delete(k) }, setItem: (k: string, v: string) => { mem.set(k, v) },
  } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear() })

describe('the steps waiting on the Owner\'s sign-off, on PROD\'s plan', () => {
  it('lists every step whose rule waits on an unsigned owner_signoff, by number, with a plain title', () => {
    const waits = signoffWaits(prod.plan)
    expect(waits.map(w => w.label)).toEqual(WAITING)
    for (const label of OWNERS) expect(waits.map(w => w.label)).toContain(label)
    for (const w of waits) expect(w.title).not.toMatch(/\*\*|`/)
    expect(waits.find(w => w.label === '2.4-1')?.refs).toEqual(['chief-exam-pass'])
    // Three steps titled alike are told apart by their numbers.
    expect(waits.filter(w => w.title === '（补强）独立复核').map(w => w.label)).toEqual(['2.3-4', '2.4-2', '2.5-2'])
  })

  it('orders them by number whatever order the plan gives, 2.9 before 2.10 and 2.2 before 2.2b', () => {
    expect(signoffWaits({ ...prod.plan, nodes: [...prod.plan.nodes].reverse() }).map(w => w.label)).toEqual(WAITING)
    const step = (label: string) => ({ plan_node_id: `R2:${label}`, node_kind: 'step', label, title: label, state: 'planned', done_rule_json: { owner_signoff: label } })
    const doc = { root: 'R2', nodes: ['2.10-1', '2.2b-1', '2.9-1', '2.2-6', '2.2-10'].map(step) }
    expect(signoffWaits(doc).map(w => w.label)).toEqual(['2.2-6', '2.2-10', '2.2b-1', '2.9-1', '2.10-1'])
  })

  it('takes markdown out of a title and keeps one line', () => {
    const raw = nodes.find(n => n.label === '2.3-2')!.title
    expect(raw).toMatch(/^\*\*/)
    expect(plainTitle(raw)).toMatch(/^（你 10-17 10:2x 定：A，做「Gitea 做主」）新仓库接入 Agent 不再手工/)
    expect(plainTitle('a `b` **c**\nsecond line')).toBe('a b c')
  })

  it('shows them at the top of the plan with a Sign off each, and a signed step leaves the block', async () => {
    show(<SignoffWaiting />)
    const block = await screen.findByRole('region', { name: 'Waiting for your sign-off' })
    expect(within(block).getByRole('heading', { name: 'Waiting for your sign-off · 10' })).toBeInTheDocument()
    expect([...block.querySelectorAll('[data-step-badge]')].map(b => b.textContent)).toEqual(WAITING)
    expect(block.textContent).not.toMatch(/\*\*|`/)
    const row = block.querySelector('[data-signoff-wait="R2:2.4-1"]') as HTMLElement
    const press = within(row).getByRole('button', { name: 'Sign off' })
    await waitFor(() => expect(press).toBeEnabled())
    expect(press).toHaveStyle({ minHeight: '44px' })
    fireEvent.click(press)
    fireEvent.click(within(row).getByRole('button', { name: 'Confirm sign-off' }))
    await waitFor(() => expect(block.querySelector('[data-signoff-wait="R2:2.4-1"]')).toBeNull())
    expect(signed).toEqual(['R2:2.4-1 chief-exam-pass'])
    expect(within(block).getByRole('heading', { name: 'Waiting for your sign-off · 9' })).toBeInTheDocument()
    expect(block.querySelector('[data-signoff-wait="R2:2.4-2"]')).not.toBeNull()
  })

  it('For you links to the block with the count', async () => {
    show(<SignoffWaitingLink />)
    const link = await screen.findByRole('link', { name: '10 steps wait for your sign-off ›' })
    expect(link).toHaveAttribute('href', '#work?section=signoff')
  })

  it('numbers every step of the plan with a badge and shows no markdown marks', async () => {
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const steps = nodes.filter(n => n.node_kind === 'step')
    const badges = [...container.querySelectorAll('[data-step] [data-step-badge]')].map(b => b.textContent)
    expect(badges.sort()).toEqual(steps.map(s => s.label).sort())
    for (const li of container.querySelectorAll('[data-step]')) expect(li.querySelector('button[aria-expanded]')?.textContent).not.toMatch(/\*\*|`/)
    expect(container.querySelector('summary')?.textContent).not.toMatch(/\*\*|`/)
  })
})
