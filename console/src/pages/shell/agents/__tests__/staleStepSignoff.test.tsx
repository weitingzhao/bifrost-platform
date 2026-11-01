import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import signoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { WorkPlanView } from '../WorkPlanView'

/*
 * R5:5.5-4, review of 13191da1: a step sign-off listed as last read (its GET /plan older than twice its 15 s poll, or
 * its refresh failed) kept an enabled Sign off and sent the POST: the reason not to sign reached acceptance sign-offs
 * only. The fixture is GET /plan?root=R2 from a test database (stepSignoffs.test.tsx): 2.1-3, active, waits on the
 * Owner's sign-off a or b.
 */

const admin: PlatformAuthContextValue = { token: 'fixture-owner', caps: { authenticated: true, role: 'admin', can_admin: true, can_operate: true }, capsLoading: false, capsError: false, canAdmin: true, canOperate: true, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
const NOW = Date.parse('2026-10-21T21:00:00Z')
const progress = { plan: [{ plan_node_id: 'R2', kind: 'round', title: '第 2 轮', items: [] }] }
let client: QueryClient
let posts: string[]
/** How GET /plan answers: with the tree, with 503 (a release restart), or never (a refresh that hangs). */
let tree: 'ok' | 'fail' | 'hang'
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => {}, removeItem: () => {} } })
  window.location.hash = '#work?tab=signoffs&root=R2'
  vi.spyOn(Date, 'now').mockReturnValue(NOW)
  posts = []
  tree = 'ok'
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.endsWith('/signoff')) { posts.push(String(init?.body)); return json({ plan_node_id: 'R2:2.1-3', ref: 'a', by: 'owner', at: new Date(NOW).toISOString(), already_signed: false, event_id: 9 }) }
    if (url.includes('/agentrt/plan?root=R2')) return tree === 'hang' ? new Promise<Response>(() => {}) : tree === 'fail' ? json({ error: 'unavailable', message: 'platform restarting' }, 503) : json(signoffs.plan)
    if (url.endsWith('/agentrt/progress')) return json(progress)
    return json({ error: 'not_found', message: 'not in this test' }, 404)
  }))
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

function show() {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={admin}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
}
const region = () => screen.getByRole('region', { name: 'Sign-offs' })

it('offers the step\'s Sign off while its GET /plan is now', async () => {
  show()
  await waitFor(() => expect(within(region()).getByRole('list', { name: 'Ready for your sign-off' })).toBeInTheDocument())
  const row = region().querySelector('[data-signoff-wait="R2:2.1-3"]') as HTMLElement
  expect(row).not.toHaveAttribute('data-last-read')
  expect(within(row).getByRole('button', { name: 'Sign off a' })).toBeEnabled()
})

it.each([
  ['31 s old, its refresh hanging', async () => {
    tree = 'hang'
    // The tree answered at NOW; the page's clock moves 31 s on, past twice the 15 s poll.
    vi.mocked(Date.now).mockReturnValue(NOW + 31_000)
    await act(async () => { await new Promise(r => setTimeout(r, 1_100)) })
  }, 'GET /plan?root=R2 last answered 31 s ago'],
  ['whose refresh failed', async () => {
    tree = 'fail'
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-plan'] }) })
  }, 'the plan could not be read (GET /plan)'],
])('lists the step from a GET /plan %s as last read: Sign off disabled, why said, no POST', async (_, age, why) => {
  show()
  await waitFor(() => expect(within(region()).getByRole('list', { name: 'Ready for your sign-off' })).toBeInTheDocument())
  await age()
  const last = await within(region()).findByRole('list', { name: 'Sign-offs as last read' })
  expect(within(region()).queryByRole('list', { name: 'Ready for your sign-off' })).toBeNull()
  expect(within(region()).getAllByRole('alert').map(a => a.textContent).join('\n')).toContain(why)
  const row = last.querySelector('[data-signoff-wait="R2:2.1-3"]') as HTMLElement
  expect(row).toHaveAttribute('data-last-read', 'true')
  for (const name of ['Sign off a', 'Sign off b']) {
    const button = within(row).getByRole('button', { name })
    expect(button).toBeDisabled()
    fireEvent.click(button)
  }
  expect(within(row).getAllByText('Refresh the plan before signing.')).toHaveLength(2)
  expect(within(row).queryByRole('button', { name: 'Confirm sign-off' })).toBeNull()
  expect(posts).toEqual([])
}, 10_000)
