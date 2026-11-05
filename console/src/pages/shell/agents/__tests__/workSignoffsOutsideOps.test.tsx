import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import signoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
import { signoffNotStarted } from '@/api/agentRuntimePlan'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { WorkSignoffs } from '../WorkSignoffs'
import type { SignoffCount, SignoffRow } from '../workSummaryModel'

// 11-05: R5:5.13-2 and 5.13-3 ran on owner-run cards (#286–#291), so no task on them was leased, they stayed planned,
// and Work › Sign-offs listed them under Not started with no Sign off anywhere. The platform signs a planned step off
// (store/signoff.go refuses only done, stopped and dropped); the Console held the button back (TD-425). Not started keeps
// them out of Ready and every count, and now folds a Sign off under each, saying Ops shows no work behind it.
// The fixture is the same GET /plan?root=R2 test-database answer stepSignoffs.test.tsx reads: 2.3-2 waits on race, planned.

const plan = signoffs.plan
const TOKEN = 'fixture-admin-token'
const ADMIN: AuthCapabilities = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
const auth = vi.hoisted(() => ({ caps: null as AuthCapabilities | null }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps ?? { authenticated: false }) }
})

type Sent = { url: string; method: string; body: unknown }
const sent: Sent[] = []
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  if (url.includes('/signoff')) {
    sent.push({ url, method: init?.method ?? 'GET', body: JSON.parse(String(init?.body)) })
    return json({ ...signoffs.signed, plan_node_id: 'R2:2.3-2', ref: 'race', already_signed: false })
  }
  return json({})
})

const notStarted: SignoffRow[] = signoffNotStarted(plan).map(w => ({ kind: 'step', node: w.node, label: w.label, title: w.title, wait: w }))
const none = { ready: [], notStarted: [], later: [], tasks: [] }
function count(over: Partial<SignoffCount> = {}): SignoffCount {
  return { ...none, notStarted, uncounted: [], root: 'R2', stepRounds: ['R2'], steps: 'ok', round: 'ok', scope: 'every', lastRead: none, ...over }
}
function show(c: SignoffCount, stale = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><PlatformAuthProvider><WorkSignoffs count={c} view={undefined} stale={stale} /></PlatformAuthProvider></QueryClientProvider>)
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
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear(); sent.length = 0 })

describe('a step not started whose work ran outside Ops', () => {
  it('stays out of Ready and its count, and folds a Sign off that says Ops shows no work behind it', async () => {
    expect(notStarted.map(r => r.node)).toEqual(['R2:2.3-2'])
    const { container } = show(count())
    expect(container.querySelector('[data-signoff-ready]')).toHaveAttribute('data-signoff-ready', '0')
    const row = container.querySelector('[data-signoff-later="R2:2.3-2"]') as HTMLElement
    const fold = within(row).getByText('Done outside Ops? Sign off', { selector: 'summary' })
    expect(fold.closest('details')).not.toHaveAttribute('open')
    expect(within(row).getByText(/^No task on this step ran in Ops, so Ops shows no work behind it\./)).toBeInTheDocument()
    const press = await within(row).findByRole('button', { name: 'Sign off' })
    await vi.waitFor(() => expect(press).toBeEnabled())
    fireEvent.click(press)
    expect(sent).toEqual([])
    fireEvent.click(within(row).getByRole('button', { name: 'Confirm sign-off' }))
    await vi.waitFor(() => expect(sent).toEqual([{ url: '/api/v1/agentrt/plan/R2:2.3-2/signoff', method: 'POST', body: { ref: 'race' } }]))
  })

  it('offers no Sign off while the plan is stale, nor on a row listed as last read', () => {
    const stale = show(count(), true)
    expect(stale.container.querySelector('[data-signoff-later="R2:2.3-2"]')).not.toBeNull()
    expect(stale.container.querySelector('[data-signoff-outside]')).toBeNull()
    cleanup()
    const last = show(count({ notStarted: [], steps: 'ok', lastRead: { ...none, notStarted } }))
    expect(last.container.querySelector('[data-signoff-later="R2:2.3-2"][data-last-read]')).not.toBeNull()
    expect(last.container.querySelector('[data-signoff-outside]')).toBeNull()
  })
})
