import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-signoff.generated.json'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// 2026-10-26: PV2 (platform 32821e76) added acceptance_status and current_validity to every item node and
// verification to external work; the closed PV1 parser refused the whole response and Work → Plan on
// ops.bifrost.lan showed "unsupported or incomplete response". The fixture is that PROD response.
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
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={{ token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
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
  it('renders Not started plainly in Later and counts only Ready sign-offs', async () => {
    reply = fixture
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
    show()
    const needs = await screen.findByRole('region', { name: 'Needs you' })
    expect(needs).toHaveTextContent('Needs you · 1 sign-off ready')
    expect(needs).toHaveTextContent(`acceptance ${fixture.owner_waiting[0].acceptance} · plan v1`)
    expect(needs).toHaveTextContent('Blocked')
    expect(needs).toHaveTextContent('Later · 2')
    const later = needs.querySelector('details')!
    expect(later).toHaveTextContent('Not started')
    expect(later).toHaveTextContent('Work for this step has not started; no candidate evidence yet')
    expect(within(later).queryByRole('button', { name: 'Sign off' })).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(needs).toHaveTextContent('Submitted report · work-633')
    expect(needs).toHaveTextContent('verification:v_fixture')
    expect(screen.getAllByRole('checkbox').every(box => !(box as HTMLInputElement).checked)).toBe(true)
  })
  it('does not count later sign-offs when none are Ready', async () => {
    reply = { ...fixture, owner_waiting: [] }
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
    show()
    const needs = await screen.findByRole('region', { name: 'Needs you' })
    expect(needs).toHaveTextContent('Needs you · Unknown')
    expect(needs).toHaveTextContent('Later · 2')
    expect(needs).not.toHaveTextContent(/\d+ sign-offs? ready/)
    expect(within(needs).queryByRole('button', { name: 'Sign off' })).toBeNull()
  })
})
