import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-handler.json'
import empty from '@/api/__fixtures__/plan-view-handler-empty.json'
import legacy from '@/api/__fixtures__/plan-view-handler-legacy.json'
import page2 from '@/api/__fixtures__/plan-view-handler-page2.json'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
let client: QueryClient
let reply: unknown
let status: number
let fetcher: ReturnType<typeof vi.fn>
function show(hash = '#work') {
  window.location.hash = hash
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(<QueryClientProvider client={client}><WorkPlanView /></QueryClientProvider>)
}
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
  reply = fixture; status = 200
  fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    return new Response(JSON.stringify(url.includes('recent_cursor=') ? page2 : reply), { status })
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.useRealTimers(); vi.restoreAllMocks(); onlineManager.setOnline(true) })
describe('Work plan/live view', () => {
  it('shows main line, next responsible, all plan items, linked waits and partial Owner readiness', async () => {
    show()
    expect(screen.getByRole('status')).toHaveTextContent('Loading plan and live work')
    const main = await screen.findByRole('region', { name: 'Main line' })
    expect(main).toHaveTextContent('4.1')
    expect(main).toHaveTextContent('Next ·')
    expect(main).toHaveTextContent('(plan default)')
    expect(main).toHaveTextContent('ETA · Unknown')
    const needs = screen.getByRole('region', { name: 'Needs you' })
    expect(needs).toHaveTextContent('Needs you · Unknown')
    expect(needs).toHaveTextContent('Later · 1')
    expect(within(needs).getByRole('link', { name: 'Plan sign-offs' })).toHaveAttribute('href', '#work?section=signoff')
    for (const item of fixture.items) expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent(item.title)
    const live = screen.getByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('实现全计划与现场视图')
    expect(live).toHaveTextContent('codex · openai')
    expect(live).toHaveTextContent('Last heartbeat')
    expect(live).toHaveTextContent('Not reconciled')
    expect(screen.getByRole('region', { name: 'Waiting on' })).toHaveTextContent('Unknown')
    expect(fetcher.mock.calls.every(([url]) => String(url).includes('/agentrt/plan-view?root=R4'))).toBe(true)
  })
  it('shows runtime attempts on a historical root and preserves the old round parameter', async () => {
    reply = legacy
    show('#work?section=plan&round=R2')
    const live = await screen.findByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Runtime attempt')
    expect(live).toHaveTextContent('exec:cursor')
    expect(live).toHaveTextContent('Host · h1')
    expect(live).toHaveTextContent('Linked external history · 1 records')
    expect(within(live).getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent('Unclassified')
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/agentrt/plan-view?root=R2')
  })
  it('distinguishes a real zero from loading, while keeping Owner readiness unknown', async () => {
    reply = empty; show('#work?root=R3')
    expect(screen.queryByText('No active work in this snapshot.')).not.toBeInTheDocument()
    expect(await screen.findByText('No active work in this snapshot.')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Needs you' })).toHaveTextContent('Needs you · Unknown')
  })
  it('keeps submitted external work separate from plan completion', async () => {
    show(); const live = await screen.findByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Finished today · 1')
    expect(live).toHaveTextContent('Submitted')
    expect(live).toHaveTextContent('Execution ended; this does not mean the plan is complete.')
    expect(screen.getByRole('region', { name: 'Main line' })).toHaveTextContent('4.1')
  })
  it('keeps the last successful snapshot visibly stale after a failed refresh, recovers on retry', async () => {
    show(); await screen.findByRole('region', { name: 'Main line' })
    status = 503; reply = { error: 'unavailable' }
    await act(async () => { await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] }) })
    expect(await screen.findByRole('alert')).toHaveTextContent('Stale · showing the last successful snapshot')
    expect(screen.getByRole('region', { name: 'Live work' })).toHaveTextContent('实现全计划与现场视图')
    expect(screen.getByRole('region', { name: 'Needs you' })).toHaveTextContent('Unknown')
    reply = fixture; status = 200
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })
  it.each([404, 503])('does not turn HTTP %s into an empty plan', async code => {
    status = code; reply = { error: 'unavailable' }; show()
    expect(await screen.findByRole('alert')).toHaveTextContent('Unknown')
    expect(screen.queryByRole('region', { name: 'Live work' })).not.toBeInTheDocument()
  })
  it('rejects malformed successful responses on the page', async () => {
    reply = { schema: fixture.schema, root: 'R4', working: [] }; show()
    expect(await screen.findByRole('alert')).toHaveTextContent('unsupported or incomplete response')
    expect(screen.queryByText('No active work in this snapshot.')).not.toBeInTheDocument()
  })
  it('ages a cached heartbeat beyond 60s without another successful fetch', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse(fixture.as_of))
    window.location.hash = '#work'
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['agent-runtime-plan-view', 'R4'], parsePlanSnapshot(fixture, 'R4'))
    render(<QueryClientProvider client={client}><WorkPlanView /></QueryClientProvider>)
    const live = screen.getByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Running')
    onlineManager.setOnline(false)
    await act(async () => { await vi.advanceTimersByTimeAsync(61_000) })
    expect(live).toHaveTextContent('Unconfirmed')
  })
  it('loads cursor pages without duplicating changes or moving the history boundary', async () => {
    show(); await screen.findByRole('region', { name: 'Recent changes' })
    const history = screen.getByRole('region', { name: 'Recent changes' })
    expect(within(history).getAllByRole('listitem')).toHaveLength(fixture.recent_changes.length)
    fireEvent.click(screen.getByRole('button', { name: 'Load more changes' }))
    await waitFor(() => expect(within(history).getAllByRole('listitem')).toHaveLength(fixture.recent_changes.length + page2.recent_changes.length))
    expect(history).toHaveTextContent(fixture.recent_as_of)
    fireEvent.click(screen.getByRole('button', { name: 'Latest changes' }))
    await waitFor(() => expect(within(history).getAllByRole('listitem')).toHaveLength(fixture.recent_changes.length))
  })
  it('locates a selected step after data arrives and follows another root without old data leakage', async () => {
    const scroll = vi.fn(); Element.prototype.scrollIntoView = scroll
    show('#work?section=plan&root=R4&node=R4%3A4.1-2')
    await screen.findByRole('region', { name: 'Whole plan' })
    const node = document.getElementById('plan-node-R4:4.1-2')!
    expect(node.closest('details')).toHaveAttribute('open')
    await waitFor(() => expect(scroll).toHaveBeenCalled())
    reply = legacy
    await act(async () => { window.location.hash = '#work?section=plan&root=R2&node=R2%3A1'; window.dispatchEvent(new Event('hashchange')) })
    await waitFor(() => expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent('Whole plan · R2'))
    expect(screen.queryByText('实现全计划与现场视图')).not.toBeInTheDocument()
  })
})
