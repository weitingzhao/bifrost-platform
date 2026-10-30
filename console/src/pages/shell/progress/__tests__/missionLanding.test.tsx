import '@testing-library/jest-dom/vitest'
import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import mission from '@/api/__fixtures__/agentrt-mission-M-2.prod.json'
import tasks from '@/api/__fixtures__/agentrt-tasks-M-2.prod.json'
import events from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import fixture from '@/api/__fixtures__/mission-landing.json'
import { GoalCard } from '../GoalCard'
import { goalTaskId, landingReason } from '../goalCardModel'

const clients: QueryClient[] = []
let landingBody: unknown
let landingStatus: number
let missionState: string
let landingResponse: (() => Promise<Response>) | undefined
async function readLanding() {
  await screen.findByText(/\d+ delivered, \d+ disposed, \d+ unresolved/)
  return screen.getByRole('region', { name: 'Landing' })
}
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  const ui = (updatedAt: number) => <QueryClientProvider client={client}><GoalCard id="M-2" updatedAt={updatedAt} questions={[]} /></QueryClientProvider>
  return { ...render(ui(1)), client, ui }
}
beforeEach(() => {
  landingBody = fixture
  landingStatus = 200
  missionState = 'active'
  landingResponse = undefined
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null } })
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/landing/missions/')) return landingResponse ? landingResponse() : new Response(JSON.stringify(landingBody), { status: landingStatus })
    if (url.includes('/agentrt/missions/')) return new Response(JSON.stringify({ ...mission, state: missionState }))
    if (url.includes('/agentrt/tasks?')) return new Response(JSON.stringify(tasks))
    if (url.includes('/agentrt/events?')) return new Response(JSON.stringify(events))
    throw new Error(`Unexpected fixture request: ${url}`)
  }))
})
afterEach(() => {
  cleanup(); clients.forEach(c => c.clear()); clients.length = 0
  onlineManager.setOnline(true)
  vi.unstubAllGlobals(); vi.restoreAllMocks(); window.history.replaceState(null, '', '/')
})

describe('Landing on the goal detail', () => {
  it('shows one STG line with batch, budget, blocking time and unreconciled execution', async () => {
    landingBody = { ...fixture, roots: [{ ...fixture.roots[0], reason: 'stg_blocked', stg: {
      batch: 'stg:bifrost-platform:7', try: 2, phase: 'quarantined', budget_used: 2, budget_gen: 3, quarantined: true,
      blocked: { why: 'baseline_moved', since: '2026-10-29T12:00:00Z' },
    } }] }
    mount()
    const line = await screen.findByLabelText('STG release')
    expect(line).toHaveTextContent('STG: quarantined · stg:bifrost-platform:7 · try 2 · 2/3 attempts · generation 3')
    expect(line).toHaveTextContent('baseline moved since 2026-10-29T12:00:00Z · execution unreconciled')
    expect(screen.getAllByLabelText('STG release')).toHaveLength(1)
    expect(screen.getByText('STG release is blocked')).toBeVisible()
  })
  it('groups roots by repository and gives infra a split/manual path without an approval entry', async () => {
    landingBody = { ...fixture, roots: [
      { ...fixture.roots[0], root: 'infra-root', repo: 'bifrost-trade-infra', reason: 'failed:floor', grant_supported: false, floor: { soft: ['size'], grant_supported: false } },
      { ...fixture.roots[1], repo: 'bifrost-platform' },
    ] }
    mount()
    const block = within(await readLanding())
    const repos = block.getAllByRole('heading', { level: 5 })
    expect(repos.map(h => h.textContent)).toEqual(['bifrost-platform', 'bifrost-trade-infra'])
    const infra = within(block.getByRole('region', { name: 'bifrost-trade-infra' }))
    expect(infra.getByText('Outside the infra landing scope. Split the change or have the Owner push it manually.')).toBeVisible()
    expect(infra.queryByText(/Request.*approval/i)).not.toBeInTheDocument()
    expect(within(block.getByRole('region', { name: 'bifrost-platform' })).queryByText('infra-root')).not.toBeInTheDocument()
  })
  it('never renders a grant entry for infra, even if a future response includes a wait reason', async () => {
    landingBody = { ...fixture, roots: [{ ...fixture.roots[0], repo: 'bifrost-trade-infra', reason: 'awaiting_grant' }] }
    mount()
    await screen.findByRole('region', { name: 'bifrost-trade-infra' })
    expect(screen.queryByRole('link', { name: 'Landing authorization material' })).not.toBeInTheDocument()
  })
  it('shows counts, classes, reasons, closing time and existing task/question links', async () => {
    mount()
    const block = within(await readLanding())
    expect(block.getByText(/1 delivered, 1 disposed, 1 unresolved/)).toHaveTextContent('main is being read again until 2026-10-28T10:30:00Z')
    for (const label of ['delivered', 'disposed', 'unresolved', 'Waiting for fresh evidence from main', 'Landed and verified in main', 'The change was reverted']) expect(block.getByText(label)).toBeVisible()
    const task = block.getByRole('link', { name: 'Document how to add a repository to store.Repos' })
    expect(task).toHaveAttribute('href', `#work?goal=M-2&task=${fixture.roots[0]!.root}`)
    expect(block.getByRole('link', { name: 't_delivered' })).toHaveAttribute('href', '#work?goal=M-2&task=t_delivered')
    expect(block.getAllByRole('link', { name: 'Open question' })).toHaveLength(1)
    expect(block.getByRole('link', { name: 'Open question' })).toHaveAttribute('href', '#questions?id=q_0123456789abcdef')
    expect(block.queryByRole('button')).not.toBeInTheDocument()
    window.history.replaceState(null, '', task.getAttribute('href')!)
    fireEvent(window, new Event('hashchange'))
    expect(document.getElementById(goalTaskId('M-2', fixture.roots[0]!.root))).toHaveAttribute('open')
    expect(screen.getByRole('listitem', { name: 'Done: future' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Result' })).not.toBeInTheDocument()
  })
  it.each([
    { ...fixture, in_scope: false }, { ...fixture, roots: [] },
  ])('still hides confirmed out-of-scope and empty responses %#', async body => {
    landingBody = body
    const { client } = mount()
    await waitFor(() => expect(client.getQueryState(['mission-landing', 'M-2'])?.status).toBe('success'))
    expect(await screen.findByRole('list', { name: 'Goal stages' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Landing' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
  it.each([
    ...[429, 500, 502, 503].map(status => ({ reason: String(status), response: async () => new Response('', { status }) })),
    { reason: 'network error', response: async () => { throw new TypeError('Failed to fetch') } },
    { reason: 'invalid JSON response', response: async () => new Response('{') },
    { reason: 'invalid response data', response: async () => new Response('{}') },
    { reason: 'invalid response data', response: async () => new Response(JSON.stringify({ ...fixture, roots: [{ ...fixture.roots[0], repo: 'unreadable' }] })) },
    { reason: 'mission mismatch', response: async () => new Response(JSON.stringify({ ...fixture, mission_id: 'M-3' })) },
  ])('keeps Landing visible as Unknown with a reason and recovers on retry: $reason', async ({ reason, response }) => {
    landingResponse = response
    mount()
    expect(await screen.findByText('Landing status unknown')).toBeVisible()
    const block = within(screen.getByRole('region', { name: 'Landing' }))
    expect(block.getByText(`Couldn't read landing status (${reason})`)).toBeVisible()
    expect(block.getByRole('button', { name: 'Retry' })).toBeEnabled()
    // N-01 replaces the old hidden block / Done: future assertions: unreadable completion is explicitly Unknown.
    expect(screen.getByRole('listitem', { name: 'Done: unknown' })).toHaveTextContent('Unknown')
    const header = screen.getByRole('article', { name: 'Goal M-2' }).querySelector('header')!
    expect(within(header).getByText('Unknown')).toBeVisible()
    expect(header.querySelector('.lamp-unknown')).toBeInTheDocument()
    expect(within(header).queryByText(/Checking|Done|Blocked/)).not.toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Goal stages' })).toBeVisible()
    expect(screen.getByRole('region', { name: 'Done when' })).toBeVisible()
    expect(screen.queryByRole('region', { name: 'Result' })).not.toBeInTheDocument()
    expect(block.queryByRole('region')).not.toBeInTheDocument()
    landingResponse = undefined
    fireEvent.click(block.getByRole('button', { name: 'Retry' }))
    await readLanding()
    expect(screen.queryByText('Landing status unknown')).not.toBeInTheDocument()
    expect(screen.queryByText(`Couldn't read landing status (${reason})`)).not.toBeInTheDocument()
    expect(screen.getByRole('listitem', { name: 'Done: future' })).toBeVisible()
    expect(within(header).getByText('Checking')).toBeVisible()
  })
  it('distinguishes a pending read from a failed read and restores the confirmed scope', async () => {
    let resolve!: (response: Response) => void
    landingResponse = () => new Promise<Response>(done => { resolve = done })
    mount()
    expect(await screen.findByText('Reading landing status…')).toBeVisible()
    expect(screen.queryByText('Landing status unknown')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    await act(async () => resolve(new Response(JSON.stringify({ ...fixture, in_scope: false }))))
    await screen.findByRole('listitem', { name: 'Done: done' })
    expect(screen.queryByRole('region', { name: 'Landing' })).not.toBeInTheDocument()
  })
  it('shows Unknown while a landing read is paused offline', async () => {
    const { client } = mount()
    await readLanding()
    act(() => onlineManager.setOnline(false))
    act(() => { void client.refetchQueries({ queryKey: ['mission-landing', 'M-2'] }) })
    expect(await screen.findByText("Couldn't read landing status (offline)")).toBeVisible()
    expect(screen.getByText('Landing status unknown')).toBeVisible()
    expect(screen.getByRole('listitem', { name: 'Done: unknown' })).toBeVisible()
  })
  it('shows unknown reasons verbatim and omits an absent closing round', async () => {
    landingBody = { mission_id: 'M-2', in_scope: true, rule: '', roots: [{ ...fixture.roots[0], reason: 'new_reason' }] }
    mount()
    const block = within(await readLanding())
    expect(block.getByText('new_reason')).toBeVisible()
    expect(block.queryByText(/main is being read again/)).not.toBeInTheDocument()
  })
  it('shows a park, an unconfirmed card and the close as decided (review 672 R-07, N-01)', async () => {
    landingBody = {
      ...fixture,
      roots: [{ ...fixture.roots[0], reason: 'parked:window', card_alert: 'card:7:1:root_failed lost',
        parked: { since: '2026-10-28T08:00:00Z', shown: '', remind_at: '2026-10-28T10:00:00Z' } }],
      closed: { to: 'done', at: '2026-10-28T11:00:00Z', rule: 'landing:dev v1, stg_release none', round: 1, delivered: 1, disposed: 0, briefs: 0 },
    }
    mount()
    const block = within(await readLanding())
    expect(block.getByText('Parked: window')).toBeVisible()
    expect(block.getByText((_, el) => el?.tagName === 'SPAN' && el.textContent === 'since 2026-10-28T08:00:00Z, told at 2026-10-28T10:00:00Z')).toBeVisible()
    expect(block.getByText('Answer not confirmed: card:7:1:root_failed lost')).toBeVisible()
    expect(block.getByText((_, el) => el?.tagName === 'P' && (el.textContent ?? '').startsWith('Closed as done at 2026-10-28T11:00:00Z: 1 delivered, 0 disposed'))).toBeVisible()
  })
  it('keeps reading accepted tasks until the mission closes, and removes stale landing on failure', async () => {
    const { rerender, ui } = mount()
    await readLanding()
    landingStatus = 500
    rerender(ui(2))
    await screen.findByText('Landing status unknown')
    // Keep the R-08 conservative completion rule, but say Unknown instead of hiding the failure (724 N-01).
    expect(screen.getByRole('listitem', { name: 'Done: unknown' })).toBeInTheDocument()
    expect(screen.queryByText(/1 delivered, 1 disposed, 1 unresolved/)).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Result' })).not.toBeInTheDocument()
    landingStatus = 200
    landingBody = { ...fixture, roots: fixture.roots.filter(root => root.class !== 'unresolved') }
    rerender(ui(3))
    await screen.findByText(/1 delivered, 1 disposed, 0 unresolved/)
    expect(screen.getByRole('listitem', { name: 'Done: future' })).toBeInTheDocument()
    missionState = 'done'
    rerender(ui(4))
    await screen.findByRole('listitem', { name: 'Done: done' })
    expect(screen.getByRole('region', { name: 'Result' })).toBeInTheDocument()
  })
  it.each(['503', 'invalid repository'])('replaces both repositories with Unknown after %s and restores them on Retry', async failure => {
    const delivered = { ...fixture, closing_round: undefined, roots: [
      { ...fixture.roots[1], repo: 'bifrost-platform' },
      { ...fixture.roots[1], root: 'infra-root', repo: 'bifrost-trade-infra' },
    ] }
    landingBody = delivered
    const { rerender, ui } = mount()
    await readLanding()
    for (const repo of ['bifrost-platform', 'bifrost-trade-infra']) expect(screen.getByRole('region', { name: repo })).toHaveTextContent('delivered')
    if (failure === '503') landingStatus = 503
    else landingBody = { ...delivered, roots: [delivered.roots[0], { ...delivered.roots[1], repo: 'unreadable' }] }
    rerender(ui(2))
    await screen.findByText('Landing status unknown')
    const block = within(screen.getByRole('region', { name: 'Landing' }))
    expect(block.getByText(`Couldn't read landing status (${failure === '503' ? '503' : 'invalid response data'})`)).toBeVisible()
    expect(block.queryByRole('region')).not.toBeInTheDocument()
    expect(block.queryByText(/delivered|disposed|unresolved|bifrost-platform/)).not.toBeInTheDocument()
    expect(screen.getByRole('listitem', { name: 'Done: unknown' })).toBeVisible()
    let resolve!: (response: Response) => void
    landingResponse = () => new Promise<Response>(done => { resolve = done })
    fireEvent.click(block.getByRole('button', { name: 'Retry' }))
    expect(await block.findByRole('button', { name: 'Retrying…' })).toBeDisabled()
    expect(block.getByText('Landing status unknown')).toBeVisible()
    expect(block.queryByRole('region')).not.toBeInTheDocument()
    await act(async () => resolve(new Response(JSON.stringify(delivered))))
    expect(await screen.findByText('2 delivered, 0 disposed, 0 unresolved')).toBeVisible()
    for (const repo of ['bifrost-platform', 'bifrost-trade-infra']) expect(screen.getByRole('region', { name: repo })).toHaveTextContent('delivered')
    expect(screen.queryByText('Landing status unknown')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    expect(screen.getByRole('listitem', { name: 'Done: future' })).toBeVisible()
    expect(within(screen.getByRole('article', { name: 'Goal M-2' }).querySelector('header')!).getByText('Checking')).toBeVisible()
  })
  it('shows a root held on the Owner\'s answer and a card waiting to be asked (review 675 R-07)', async () => {
    landingBody = { ...fixture, roots: [{ ...fixture.roots[0], held: { until: '2026-10-29T08:00:00Z', question_id: 'q_0123456789abcdef' }, card_pending: true }] }
    mount()
    const block = within(await readLanding())
    expect(block.getByText((_, el) => el?.tagName === 'SPAN' && (el.textContent ?? '').startsWith('Held on your answer until 2026-10-29T08:00:00Z'))).toBeVisible()
    expect(block.getByRole('link', { name: 'your answer' })).toHaveAttribute('href', '#questions?id=q_0123456789abcdef')
    expect(block.getByText('A card is waiting to be asked')).toBeVisible()
  })
  it('reads Landing on after the mission closed, so its close and a later reversal show (review 675 R-09)', async () => {
    missionState = 'done'
    landingBody = { ...fixture, closing_round: undefined, roots: fixture.roots.filter(root => root.class === 'delivered') }
    const { rerender, ui } = mount()
    await readLanding()
    expect(screen.queryByText(/^Closed /)).not.toBeInTheDocument()
    landingBody = { ...landingBody as object, closed: { to: 'done', at: new Date().toISOString(), rule: 'landing:dev v1, stg_release none', round: 1, delivered: 1, disposed: 0, briefs: 0 } }
    rerender(ui(2))
    expect(await screen.findByText((_, el) => el?.tagName === 'P' && (el.textContent ?? '').startsWith('Closed '))).toBeVisible()
  })
  it.each(['at_main', 'evidence', 'landed_unverified', 'rule', 'in_main_unverified', 'failed:stale', 'failed:floor', 'unclaimed', 'reverted', 'declined', 'owner', 'dropped', 'landed', 'in_main'])('explains %s in English', reason => {
    expect(landingReason(reason)).not.toBe(reason)
  })
  it.each([['unclaimed:stale', 'Waiting to be claimed: not landable as set up'], ['unclaimed:rule', 'Waiting to be claimed: the change is outside'], ['unclaimed:future', 'Waiting to be claimed: future']])('explains a refused claim %s (CUT §11.13)', (reason, start) => {
    expect(landingReason(reason).startsWith(start)).toBe(true)
  })
  it.each(['future_reason', 'toString', '__proto__'])('preserves unknown reason %s', reason => {
    expect(landingReason(reason)).toBe(reason)
  })
})
