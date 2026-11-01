import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-signoff.generated.json'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// 2026-10-26: PV2 (platform 32821e76) added acceptance_status and current_validity to every item node and
// verification to external work; the closed PV1 parser refused the whole response and Work → Plan on
// ops.bifrost.lan showed "unsupported or incomplete response". The fixture is that PROD response.
let reply: unknown
let client: QueryClient
const progressR4 = { plan: [{ plan_node_id: 'R4', kind: 'round', title: '第 4 轮', items: [] }] }
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(prod.as_of))
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    // GET /progress lists R4 (the shell's read); GET /plan holds no step sign-offs here: the sign-offs are plan-view's.
    if (url.endsWith('/agentrt/progress')) return new Response(JSON.stringify(progressR4), { status: 200 })
    if (url.includes('/agentrt/plan?')) return new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })
    return new Response(JSON.stringify(reply), { status: 200 })
  }))
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
function show(hash = '#work') {
  window.location.hash = hash
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  // The shell's GET /progress, with R4 the newest round: Work shows the newest round the runtime lists.
  client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress(progressR4))
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
    expect(await screen.findByRole('region', { name: 'Where the round stands' })).toHaveTextContent('4.1')
    expect(screen.queryByRole('alert')).toBeNull()
    // Each item's acceptance reading is in the whole plan, folded under the module rows.
    fireEvent.click(screen.getByText(/^Whole plan/, { selector: 'summary' }))
    await waitFor(() => expect(screen.getAllByText(/Acceptance Unknown/).length).toBeGreaterThan(0))
  })
  it('renders Not started plainly in Later and counts only Ready sign-offs', async () => {
    reply = fixture
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
    show('#work?tab=signoffs')
    const needs = await screen.findByRole('region', { name: 'Sign-offs' })
    await waitFor(() => expect(needs.querySelector('[data-signoff-ready]')).toHaveAttribute('data-signoff-ready', '1'))
    expect(screen.getByRole('link', { name: /^Sign-offs · 1 ready · 2 later in Round 4/ })).toHaveAttribute('aria-current', 'page')
    const ready = within(needs).getByRole('list', { name: 'Ready for your sign-off' })
    expect(ready).toHaveTextContent(`acceptance ${fixture.owner_waiting[0].acceptance} · plan v1`)
    expect(needs).toHaveTextContent('Later in Round 4 · 2 · not marked ready')
    const later = within(needs).getByRole('list', { name: 'Later sign-offs' })
    expect(later).toHaveTextContent('Blocked')
    expect(later).toHaveTextContent('Not started')
    expect(later).toHaveTextContent('Work for this step has not started; no candidate evidence yet')
    expect(within(later).queryByRole('button', { name: 'Sign off' })).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(ready).toHaveTextContent('Submitted report · work-633')
    expect(ready).toHaveTextContent('verification:v_fixture')
    expect(screen.getAllByRole('checkbox').every(box => !(box as HTMLInputElement).checked)).toBe(true)
  })
  it('does not count later sign-offs when none are Ready', async () => {
    reply = { ...fixture, owner_waiting: [] }
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
    show('#work?tab=signoffs')
    const needs = await screen.findByRole('region', { name: 'Sign-offs' })
    await waitFor(() => expect(needs).toHaveTextContent('Later in Round 4 · 2 · not marked ready'))
    expect(needs.querySelector('[data-signoff-ready]')).toHaveAttribute('data-signoff-ready', '0')
    expect(within(screen.getByRole('region', { name: 'Where the round stands' })).getByRole('link', { name: /^Sign-offs:/ })).toHaveTextContent('Sign-offs: 0 ready · 2 later in Round 4')
    expect(within(needs).queryByRole('button', { name: 'Sign off' })).toBeNull()
  })
})
