import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/agentrt-plan-r2.prod.json'
import { plainTitle, signoffNotStarted, signoffWaits } from '@/api/agentRuntimePlan'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { AgentsPlan } from '../AgentsPlan'
import { SignoffWaiting, SignoffWaitingLink } from '../SignoffWaiting'

// The Owner 2026-10-21, after #78: 「这个页面根本没有达到可用的级别」. He could not find the steps to sign
// (2.4-1, 2.4-2, 2.3-4, 2.5-1, 2.5-2, 2.5-3): no step number shown, three steps titled alike, "**" in titles, a
// ledger line a screen long. The fixture is PROD's GET /plan?root=R2 as the platform answered it on 2026-10-21
// (read through the platform MCP's get_plan): ten steps have an owner_signoff, none signed. Seven are active and wait;
// three (2.5-2, 2.5-3, 2.5-4) are planned, their work not started, so they are under Not started and not counted (TD-425,
// the Owner 2026-10-23: 「为什么这个没有完成的工作，目前推到了我的 for you 栏目里？」).

type Node = { plan_node_id: string; parent_plan_node_id: string | null; node_kind: string; label: string; title: string; state: string | null
  done_rule_json?: unknown; signoffs?: unknown[] }
const nodes = prod.plan.nodes as unknown as Node[]
const WAITING = ['2.2-28', '2.3-2', '2.3-3', '2.3-4', '2.4-1', '2.4-2', '2.5-1']
const NOT_STARTED = ['2.5-2', '2.5-3', '2.5-4']
const OWNERS = ['2.4-1', '2.4-2', '2.3-4', '2.5-1', '2.5-2', '2.5-3']

const auth = vi.hoisted(() => ({ caps: { authenticated: true, role: 'admin', can_operate: true, can_admin: true } as AuthCapabilities }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps) }
})

function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
let plan: { root: string; nodes: Node[] } = prod.plan as unknown as { root: string; nodes: Node[] }
// How the plan read answers: as the platform does, failed (503), or held until released.
let planRead: 'ok' | 'fail' | 'late' = 'ok'
let releasePlan = () => {}
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
  if (url.includes('/agentrt/plan?')) {
    if (planRead === 'fail') return json({ error: 'unavailable', message: 'the runtime is not connected' }, 503)
    if (planRead === 'late') await new Promise<void>(resolve => { releasePlan = resolve })
    return json(plan)
  }
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
  planRead = 'ok'
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
  it('lists every started step whose rule waits on an unsigned owner_signoff, by number, with a plain title', () => {
    const waits = signoffWaits(prod.plan)
    const notStarted = signoffNotStarted(prod.plan)
    expect(waits.map(w => w.label)).toEqual(WAITING)
    expect(waits.every(w => w.state === 'active')).toBe(true)
    // The planned ones are kept apart, never in the waits (TD-425).
    expect(notStarted.map(w => w.label)).toEqual(NOT_STARTED)
    expect(notStarted.every(w => w.state === 'planned')).toBe(true)
    const both = [...waits, ...notStarted]
    for (const label of OWNERS) expect(both.map(w => w.label)).toContain(label)
    for (const w of both) expect(w.title).not.toMatch(/\*\*|`/)
    expect(waits.find(w => w.label === '2.4-1')?.refs).toEqual(['chief-exam-pass'])
    // Three steps titled alike are told apart by their numbers.
    expect(both.filter(w => w.title === '（补强）独立复核').map(w => w.label)).toEqual(['2.3-4', '2.4-2', '2.5-2'])
  })

  it('orders them by number whatever order the plan gives, 2.9 before 2.10 and 2.2 before 2.2b', () => {
    expect(signoffWaits({ ...prod.plan, nodes: [...prod.plan.nodes].reverse() }).map(w => w.label)).toEqual(WAITING)
    expect(signoffNotStarted({ ...prod.plan, nodes: [...prod.plan.nodes].reverse() }).map(w => w.label)).toEqual(NOT_STARTED)
    const step = (state: string) => (label: string) => ({ plan_node_id: `R2:${label}`, node_kind: 'step', label, title: label, state, done_rule_json: { owner_signoff: label } })
    const labels = ['2.10-1', '2.2b-1', '2.9-1', '2.2-6', '2.2-10']
    const ordered = ['2.2-6', '2.2-10', '2.2b-1', '2.9-1', '2.10-1']
    expect(signoffWaits({ root: 'R2', nodes: labels.map(step('active')) }).map(w => w.label)).toEqual(ordered)
    expect(signoffNotStarted({ root: 'R2', nodes: labels.map(step('planned')) }).map(w => w.label)).toEqual(ordered)
  })

  it('takes markdown out of a title and keeps one line', () => {
    const raw = nodes.find(n => n.label === '2.3-2')!.title
    expect(raw).toMatch(/^\*\*/)
    expect(plainTitle(raw)).toMatch(/^（你 10-17 10:2x 定：A，做「Gitea 做主」）新仓库接入 Agent 不再手工/)
    expect(plainTitle('a `b` **c**\nsecond line')).toBe('a b c')
  })

  it('shows them at the top of the plan with a Sign off each, and a signed step leaves the block', async () => {
    show(<SignoffWaiting />)
    // Reading first, then the steps.
    expect(await screen.findByRole('heading', { name: 'Waiting for your sign-off · 7' })).toBeInTheDocument()
    const block = screen.getByRole('region', { name: 'Waiting for your sign-off' })
    expect([...block.querySelectorAll('[data-signoff-wait] [data-step-badge]')].map(b => b.textContent)).toEqual(WAITING)
    expect(block.textContent).not.toMatch(/\*\*|`/)
    const row = block.querySelector('[data-signoff-wait="R2:2.4-1"]') as HTMLElement
    const press = within(row).getByRole('button', { name: 'Sign off' })
    await waitFor(() => expect(press).toBeEnabled())
    expect(press).toHaveStyle({ minHeight: '44px' })
    fireEvent.click(press)
    fireEvent.click(within(row).getByRole('button', { name: 'Confirm sign-off' }))
    await waitFor(() => expect(block.querySelector('[data-signoff-wait="R2:2.4-1"]')).toBeNull())
    expect(signed).toEqual(['R2:2.4-1 chief-exam-pass'])
    expect(within(block).getByRole('heading', { name: 'Waiting for your sign-off · 6' })).toBeInTheDocument()
    expect(block.querySelector('[data-signoff-wait="R2:2.4-2"]')).not.toBeNull()
  })

  it('For you links to the block with the count', async () => {
    show(<SignoffWaitingLink />)
    const link = await screen.findByRole('link', { name: '7 steps wait for your sign-off ›' })
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

  it('says the plan could not be read, never that none waits, in the block and on For you (review 502 B1)', async () => {
    planRead = 'fail'
    show(<><SignoffWaiting /><SignoffWaitingLink /></>)
    const block = await screen.findByRole('region', { name: 'Waiting for your sign-off' })
    expect(await within(block).findByRole('alert')).toHaveTextContent('The plan could not be read: which steps wait for your sign-off is not known.')
    expect(block.querySelector('[data-signoff-wait]')).toBeNull()
    const forYou = document.querySelector('p[data-signoff-link]') as HTMLElement
    expect(forYou).toHaveTextContent(/^The plan could not be read: which steps wait for your sign-off is not known\./)
    expect(within(forYou).getByRole('link', { name: 'Work › Plan' })).toHaveAttribute('href', '#work?section=signoff')
  })

  it('is in place while the plan is read, and is brought into view when its steps arrive at #work?section=signoff (review 502 B2)', async () => {
    planRead = 'late'
    window.location.hash = '#work?section=signoff'
    const into = vi.fn()
    Element.prototype.scrollIntoView = into
    show(<SignoffWaiting />)
    // A target from the first paint, so the page's own scroll finds it.
    expect(await screen.findByText('Reading which steps wait for your sign-off…')).toBeInTheDocument()
    expect(document.getElementById('signoff')).not.toBeNull()
    expect(into).not.toHaveBeenCalled()
    // The waits read GET /progress first (which rounds there are), then each round's plan: release the plan once it is asked for.
    await waitFor(() => expect(fetchMock.mock.calls.some(c => String(c[0]).includes('/agentrt/plan?'))).toBe(true))
    releasePlan()
    expect(await screen.findByRole('heading', { name: 'Waiting for your sign-off · 7' })).toBeInTheDocument()
    await waitFor(() => expect(into).toHaveBeenCalledTimes(1))
    expect(into.mock.contexts[0]).toBe(document.getElementById('signoff'))
    window.location.hash = ''
  })
})

// TD-425 (the Owner 2026-10-23): the coordinator set sign-off rules ahead of the work on 3.2-2…3.2-6, and For you said
// five steps waited for his sign-off though nobody had started them. A planned step is not counted, anywhere.
describe('a step whose work has not started is not waiting for the Owner\'s sign-off (TD-425)', () => {
  it('is not counted: it is under Not started, folded, by number and title, with no Sign off; a started one keeps its', async () => {
    show(<><SignoffWaiting /><SignoffWaitingLink /></>)
    expect(await screen.findByRole('heading', { name: 'Waiting for your sign-off · 7' })).toBeInTheDocument()
    const block = screen.getByRole('region', { name: 'Waiting for your sign-off' })
    for (const label of NOT_STARTED) expect(block.querySelector(`[data-signoff-wait="R2:${label}"]`)).toBeNull()
    const group = block.querySelector('[data-signoff-not-started-group]') as HTMLDetailsElement
    expect(group.tagName).toBe('DETAILS')
    expect(group.open).toBe(false)
    expect(group.querySelector('summary')).toHaveTextContent(/^Not started · 3$/)
    const rows = [...group.querySelectorAll('[data-signoff-not-started]')]
    expect(rows.map(r => r.getAttribute('data-signoff-not-started'))).toEqual(NOT_STARTED.map(label => `R2:${label}`))
    expect(rows.map(r => r.querySelector('[data-step-badge]')?.textContent)).toEqual(NOT_STARTED)
    for (const [i, label] of NOT_STARTED.entries()) expect(rows[i]).toHaveTextContent(plainTitle(nodes.find(n => n.label === label)!.title))
    // Nothing to press under Not started.
    expect(group.querySelector('[data-signoff-ref]')).toBeNull()
    expect([...group.querySelectorAll('button')].filter(b => /Sign off/.test(b.textContent ?? ''))).toEqual([])
    // A started step keeps its Sign off in the list.
    const started = block.querySelector('[data-signoff-wait="R2:2.5-1"]') as HTMLElement
    await waitFor(() => expect(within(started).getByRole('button', { name: 'Sign off' })).toBeEnabled())
    // For you's total is the list's.
    expect(screen.getByRole('link', { name: '7 steps wait for your sign-off ›' })).toHaveAttribute('href', '#work?section=signoff')
  })

  it('when only steps not started have a sign-off rule, none waits: 0 in the block, nothing on For you, the steps under Not started', async () => {
    // The Owner's 2026-10-23 case: every rule not signed is on a step nobody has started.
    plan = { ...plan, nodes: plan.nodes.map(n => (WAITING.includes(n.label) ? { ...n, state: 'done' } : n)) }
    show(<><SignoffWaiting /><SignoffWaitingLink /></>)
    expect(await screen.findByRole('heading', { name: 'Waiting for your sign-off · 0' })).toBeInTheDocument()
    const block = screen.getByRole('region', { name: 'Waiting for your sign-off' })
    expect(block.querySelector('[data-signoff-wait]')).toBeNull()
    expect(block.querySelector('[data-signoff-not-started-group] summary')).toHaveTextContent(/^Not started · 3$/)
    expect(document.querySelector('[data-signoff-link]')).toBeNull()
  })

  it('moves a step up to the waits once its work starts', () => {
    const started = { ...prod.plan, nodes: prod.plan.nodes.map(n => (n.label === '2.5-2' ? { ...n, state: 'active' } : n)) }
    expect(signoffWaits(started).map(w => w.label)).toEqual([...WAITING, '2.5-2'])
    expect(signoffNotStarted(started).map(w => w.label)).toEqual(['2.5-3', '2.5-4'])
  })
})
