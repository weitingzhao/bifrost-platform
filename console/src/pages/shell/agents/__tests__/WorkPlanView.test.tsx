import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-handler.json'
import empty from '@/api/__fixtures__/plan-view-handler-empty.json'
import legacy from '@/api/__fixtures__/plan-view-handler-legacy.json'
import page2 from '@/api/__fixtures__/plan-view-handler-page2.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
let client: QueryClient
let reply: unknown
let status: number
let fetcher: ReturnType<typeof vi.fn>
/** The shell's GET /progress (ConsolePage → useNeedsYou), with R4 the newest round: Work shows the newest round the runtime lists. */
const progressR4 = { plan: [{ plan_node_id: 'R4', kind: 'round', title: '第 4 轮', items: [] }] }
const newestR4 = () => parseRuntimeProgress(progressR4)
async function go(hash: string) {
  await act(async () => { window.location.hash = hash; window.dispatchEvent(new Event('hashchange')) })
}
function show(hash = '#work') {
  window.location.hash = hash
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(['agent-runtime-progress'], newestR4())
  return render(<QueryClientProvider client={client}><WorkPlanView /></QueryClientProvider>)
}
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
  reply = fixture; status = 200
  fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    // GET /plan (every round's step sign-offs, the Sign-offs tab's other read): the runtime holds no such tree here.
    if (url.includes('/agentrt/plan?')) return new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })
    if (url.endsWith('/agentrt/progress')) return new Response(JSON.stringify(progressR4), { status: 200 })
    return new Response(JSON.stringify(url.includes('recent_cursor=') ? page2 : reply), { status })
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.useRealTimers(); vi.restoreAllMocks(); onlineManager.setOnline(true) })
/** A fold of the round tab, opened as the Owner opens it; it is drawn once the browser's toggle event arrives. */
async function unfold(title: RegExp) {
  const summary = screen.getByText(title, { selector: 'summary' })
  fireEvent.click(summary)
  await waitFor(() => expect(summary.parentElement!.children.length).toBeGreaterThan(1))
}
const summary = () => screen.findByRole('region', { name: 'Where the round stands' })
// R5:5.5-2: Main line and Needs you became the summary card and the Sign-offs tab; Live work, Recent changes, Waiting
// on and the whole plan moved to their tab or fold. What each says is unchanged and asserted where it now is.
describe('Work plan/live view', () => {
  it('shows main line, next responsible, all plan items, linked waits and partial Owner readiness', async () => {
    show()
    expect(screen.getByRole('status')).toHaveTextContent('Loading plan and live work')
    const main = await summary()
    expect(main).toHaveTextContent('4.1')
    expect(main).toHaveTextContent('Next ·')
    expect(main).toHaveTextContent('(plan default)')
    expect(main).toHaveTextContent('ETA Unknown')
    expect(within(main).getByRole('link', { name: /^Sign-offs:/ })).toHaveAttribute('href', '#work?tab=signoffs')
    await unfold(/^Whole plan/)
    for (const item of fixture.items) expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent(item.title)
    await unfold(/^Dependency waits/)
    expect(screen.getByRole('region', { name: 'Waiting on' })).toHaveTextContent('Unknown')
    await go('#work?tab=signoffs')
    const needs = await screen.findByRole('region', { name: 'Sign-offs' })
    await waitFor(() => expect(needs).toHaveTextContent('Later in Round 4 · 1 · not marked ready'))
    expect(needs).toHaveTextContent('Readiness Unknown')
    await go('#work?tab=live')
    const live = screen.getByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('实现全计划与现场视图')
    expect(live).toHaveTextContent('codex')
    expect(live).toHaveTextContent('openai · External work')
    expect(live).toHaveTextContent('Last heartbeat')
    expect(live).toHaveTextContent('Not reconciled')
    // Every tab reads the same plan view; the sign-offs also read the round's GET /plan, as SignoffWaiting did, and take
    // the rounds from the shell's GET /progress in the cache: no progress read of Work's own. Live's Unlinked sessions
    // (R5:5.5-12 §3.3) read GET /agent/threads under the query Needs you polls in the shell: here, without the shell, once.
    // The summary's lines read the round's GET /plan-pace and GET /pin-share (R5:5.7-8).
    expect(new Set(fetcher.mock.calls.map(([url]) => String(url)))).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R4&explain=1', '/api/v1/agentrt/plan?root=R4', '/api/v1/agent/threads', '/api/v1/agentrt/plan-pace?root=R4', '/api/v1/agentrt/pin-share']))
  })
  it('shows runtime attempts on a historical root and preserves the old round parameter', async () => {
    reply = legacy
    show('#work?section=plan&round=R2&tab=live')
    const live = await screen.findByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Runtime attempt')
    expect(live).toHaveTextContent('exec:cursor')
    expect(live).toHaveTextContent('Host · h1')
    expect(live).toHaveTextContent('Linked external history · 1 records')
    expect(within(within(live).getByRole('list', { name: 'Work now' })).getAllByRole('listitem')).toHaveLength(1)
    await go('#work?section=plan&round=R2')
    await unfold(/^Whole plan/)
    expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent('Unclassified')
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/agentrt/plan-view?root=R2&explain=1')
  })
  it('distinguishes a real zero from loading, while keeping Owner readiness unknown', async () => {
    reply = empty; show('#work?root=R3&tab=live')
    expect(screen.queryByText('No active work in this snapshot.')).not.toBeInTheDocument()
    expect(await screen.findByText('No active work in this snapshot.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^Live · 0 registered/ })).toHaveAttribute('aria-current', 'page')
    await go('#work?root=R3&tab=signoffs')
    const needs = screen.getByRole('region', { name: 'Sign-offs' })
    await waitFor(() => expect(needs).toHaveTextContent('Ready for your sign-off now'))
    expect(needs).toHaveTextContent('Later in Round 3 · 0')
  })
  it('keeps submitted external work separate from plan completion', async () => {
    show('#work?tab=live'); const live = await screen.findByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Finished today · 1')
    expect(live).toHaveTextContent('Submitted')
    expect(live).toHaveTextContent('Recently submitted · 1 of 1')
    expect(live).toHaveTextContent('Execution ended; this does not mean the plan is complete.')
    expect(screen.getByRole('region', { name: 'Where the round stands' })).toHaveTextContent('4.1')
  })
  it('keeps the last successful snapshot visibly stale after a failed refresh, recovers on retry', async () => {
    show('#work?tab=live'); await summary()
    status = 503; reply = { error: 'unavailable' }
    await act(async () => { await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] }) })
    expect(await screen.findByRole('alert')).toHaveTextContent('Stale · showing the last successful snapshot')
    expect(screen.getByRole('region', { name: 'Live work' })).toHaveTextContent('实现全计划与现场视图')
    expect(screen.getByRole('region', { name: 'Where the round stands' })).toHaveTextContent(/Stale · last read \d\d:\d\d:\d\d/)
    expect(screen.getByRole('link', { name: /^Live · last read 1 registered/ })).toBeInTheDocument()
    reply = fixture; status = 200
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })
  it.each([404, 503])('does not turn HTTP %s into an empty plan', async code => {
    status = code; reply = { error: 'unavailable' }; show('#work?tab=live')
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
    window.location.hash = '#work?tab=live'
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    client.setQueryData(['agent-runtime-plan-view', 'R4'], parsePlanSnapshot(fixture, 'R4'))
    client.setQueryData(['agent-runtime-progress'], newestR4())
    render(<QueryClientProvider client={client}><WorkPlanView /></QueryClientProvider>)
    const live = screen.getByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Running')
    onlineManager.setOnline(false)
    await act(async () => { await vi.advanceTimersByTimeAsync(61_000) })
    expect(live).toHaveTextContent('Unconfirmed')
  })
  it('loads cursor pages without duplicating changes or moving the history boundary', async () => {
    show('#work?tab=changes'); await screen.findByRole('region', { name: 'Recent changes' })
    const history = screen.getByRole('region', { name: 'Recent changes' })
    // Eight first, of the twenty read (one page); the rest of the page, then the next page, on request.
    expect(within(history).getAllByRole('listitem')).toHaveLength(8)
    expect(history).toHaveTextContent(`Recent changes · 8 of ${fixture.recent_changes.length} read`)
    expect(history).toHaveTextContent('older ones load on request')
    expect(screen.queryByRole('button', { name: 'Load more changes' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: `Show ${fixture.recent_changes.length - 8} more` }))
    expect(within(history).getAllByRole('listitem')).toHaveLength(fixture.recent_changes.length)
    fireEvent.click(screen.getByRole('button', { name: 'Load more changes' }))
    await waitFor(() => expect(within(history).getAllByRole('listitem')).toHaveLength(fixture.recent_changes.length + page2.recent_changes.length))
    expect(history).toHaveTextContent(fixture.recent_as_of)
    fireEvent.click(screen.getByRole('button', { name: 'Latest changes' }))
    await waitFor(() => expect(within(history).getAllByRole('listitem')).toHaveLength(8))
  })
  it('locates a selected step after data arrives and follows another root without old data leakage', async () => {
    const scroll = vi.fn(); Element.prototype.scrollIntoView = scroll
    show('#work?section=plan&root=R4&node=R4%3A4.1-2')
    await screen.findByRole('region', { name: 'Module 4.1' })
    // A step of a module opens the module on its own page (R5:5.5-3), the step open on its timeline and brought into view.
    const row = document.getElementById('module-detail-step-R4:4.1-2')!
    expect(row).toHaveAttribute('aria-current', 'step')
    await waitFor(() => expect(scroll.mock.contexts).toContain(row))
    expect(screen.queryByRole('region', { name: 'Modules' })).toBeNull()
    reply = legacy
    await act(async () => { window.location.hash = '#work?section=plan&root=R2&node=R2%3A1'; window.dispatchEvent(new Event('hashchange')) })
    expect(await screen.findByRole('region', { name: 'Module 1' })).toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: 'Module path' })).getByRole('link', { name: 'Round 2' })).toHaveAttribute('href', '#work?root=R2')
    expect(screen.queryByText('实现全计划与现场视图')).not.toBeInTheDocument()
    // A node with no module of its own (the round itself) opens the whole plan's fold and is found there.
    await act(async () => { window.location.hash = '#work?section=plan&root=R2&node=R2'; window.dispatchEvent(new Event('hashchange')) })
    await waitFor(() => expect(screen.getByRole('region', { name: 'Whole plan' })).toHaveTextContent('Whole plan · R2'))
    expect(screen.queryByRole('region', { name: 'Module 1' })).toBeNull()
  })
})
