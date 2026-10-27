import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// 2026-10-26: PV2 (platform 32821e76) added acceptance_status and current_validity to every item node and
// verification to external work; the closed PV1 parser refused the whole response and Work → Plan on
// ops.bifrost.lan showed "unsupported or incomplete response". The fixture is that PROD response.
const sha = 'a'.repeat(64)
function withBindings() {
  const x = structuredClone(prod) as unknown as Record<string, unknown> & typeof prod
  const node = x.nodes.find(n => n.plan_node_id === 'R4:4.1') as Record<string, unknown>
  node.current_validity = 'unknown'
  node.acceptance_status = [{ id: 'landing-p0', binding: 'p0-main', atom: 'main_effective', value: 'true', source_value: 'true', freshness: 'Fresh', source_id: `R4:${sha}`, source_version: 3, evidence: ['bifrost-platform@' + 'b'.repeat(40)], last_success: '2026-10-27T02:40:00Z', last_known_value: 'true', reason: 'Result effective on main' }]
  ;(node.view as Record<string, unknown>).bindings = [{ v: 1, kind: 'agentrt.plan_binding/v1', id: 'p0-main' }]
  x.mainline = { path: x.mainline.path.map(p => ({ ref: p.ref, plan_node_id: p.plan_node_id, milestone_status: 'unknown', current_validity: 'unknown' })), current: null, basis: 'Versioned acceptance observations' } as never
  ;(x.mainline as Record<string, unknown>).current = (x.mainline as { path: unknown[] }).path[0]
  x.next = [{ plan_node_id: 'R4:4.1', action: 'Satisfy unverified acceptance conditions', responsible: { kind: 'owner', actor_ref: 'owner', seat_ref: null, provider: null } }] as never
  x.waiting_on = x.waiting_on.map((w, i) => i === 0 ? { ...w, status: 'Blocked' } : w) as never
  x.owner_waiting = [{ plan_node_id: 'R4:4.1-1', ref: `plan-acceptance:${sha}`, acceptance: 'landing-star', plan_version: 2, evidence: ['sha256:' + sha], reason: 'Owner acceptance signoff', source: 'adopted plan binding', readiness: 'Ready' }] as never
  x.owner_later = [...x.owner_later, { plan_node_id: 'R4:4.1-2', ref: `plan-acceptance:${'c'.repeat(64)}`, acceptance: 'landing-prod', plan_version: 2, evidence: null, reason: 'Owner acceptance signoff', source: 'adopted plan binding', readiness: 'Blocked' }] as never
  x.freshness = { ...x.freshness, verification: 'Partial' }
  x.external_work = x.external_work.map((e, i) => i === 0 ? { ...e, verification: 'Verified' } : e) as never
  return x
}
let reply: unknown
let client: QueryClient
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(prod.as_of))
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(reply), { status: 200 })))
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
function show() {
  window.location.hash = '#work'
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(<QueryClientProvider client={client}><WorkPlanView /></QueryClientProvider>)
}

describe('Work plan view reads PV2 responses', () => {
  it('parses the PROD response of the first PV2 release (no bindings adopted yet)', () => {
    const view = parsePlanSnapshot(prod, 'R4')
    expect(view.mainline.current?.plan_node_id).toBe('R4:4.1')
    expect(view.nodes.find(n => n.plan_node_id === 'R4:4.1')?.current_validity).toBe('unknown')
    expect(view.freshness.verification).toBe('Unknown')
  })
  it('renders the PROD response instead of an error', async () => {
    reply = prod
    show()
    expect(await screen.findByRole('region', { name: 'Main line' })).toHaveTextContent('4.1')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getAllByText(/Acceptance Unknown/).length).toBeGreaterThan(0)
  })
  it('shows verified acceptance, blocked milestones, ready sign-offs and external verification once bindings exist', async () => {
    reply = withBindings()
    show()
    const needs = await screen.findByRole('region', { name: 'Needs you' })
    expect(needs).toHaveTextContent('Needs you · 1 sign-off ready')
    expect(needs).toHaveTextContent('acceptance landing-star · plan v2')
    expect(needs).toHaveTextContent('Blocked')
    expect(screen.getByText(/main_effective · Fresh · Result effective on main/)).toBeInTheDocument()
    expect(screen.getAllByText(/Milestone Blocked/).length).toBeGreaterThan(0)
    expect(screen.getByRole('region', { name: 'Main line' })).toHaveTextContent('Verification Partial')
    expect(screen.getAllByText(/ · Verified/).length).toBeGreaterThan(0)
  })
})
