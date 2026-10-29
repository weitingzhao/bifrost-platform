import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null } })
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/landing/missions/')) return new Response(JSON.stringify(landingBody), { status: landingStatus })
    if (url.includes('/agentrt/missions/')) return new Response(JSON.stringify({ ...mission, state: missionState }))
    if (url.includes('/agentrt/tasks?')) return new Response(JSON.stringify(tasks))
    if (url.includes('/agentrt/events?')) return new Response(JSON.stringify(events))
    throw new Error(`Unexpected fixture request: ${url}`)
  }))
})
afterEach(() => {
  cleanup(); clients.forEach(c => c.clear()); clients.length = 0
  vi.unstubAllGlobals(); vi.restoreAllMocks(); window.history.replaceState(null, '', '/')
})

describe('Landing on the goal detail', () => {
  it('groups roots by repository and gives infra a split/manual path without an approval entry', async () => {
    landingBody = { ...fixture, roots: [
      { ...fixture.roots[0], root: 'infra-root', repo: 'bifrost-trade-infra', reason: 'failed:floor', grant_supported: false, floor: { soft: ['size'], grant_supported: false } },
      { ...fixture.roots[1], repo: 'bifrost-platform' },
    ] }
    mount()
    const block = within(await screen.findByRole('region', { name: 'Landing' }))
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
    const block = within(await screen.findByRole('region', { name: 'Landing' }))
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
    { ...fixture, in_scope: false }, { ...fixture, roots: [] }, {},
  ])('hides out-of-scope, empty and malformed responses %#', async body => {
    landingBody = body
    const { client } = mount()
    await waitFor(() => expect(client.getQueryState(['mission-landing', 'M-2'])?.status).toBe('success'))
    expect(await screen.findByRole('list', { name: 'Goal stages' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Landing' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
  it('does not call an active mission Done when its landing cannot be read from the start (review 675 R-08)', async () => {
    landingStatus = 503
    const { client } = mount()
    await waitFor(() => expect(client.getQueryState(['mission-landing', 'M-2'])?.status).toBe('success'))
    expect(await screen.findByRole('listitem', { name: 'Done: future' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Result' })).not.toBeInTheDocument()
  })
  it('keeps the goal usable when landing is unavailable', async () => {
    landingStatus = 503
    const { client } = mount()
    await waitFor(() => expect(client.getQueryState(['mission-landing', 'M-2'])?.status).toBe('success'))
    expect(await screen.findByRole('list', { name: 'Goal stages' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Landing' })).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
  it('shows unknown reasons verbatim and omits an absent closing round', async () => {
    landingBody = { mission_id: 'M-2', in_scope: true, rule: '', roots: [{ ...fixture.roots[0], reason: 'new_reason' }] }
    mount()
    const block = within(await screen.findByRole('region', { name: 'Landing' }))
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
    const block = within(await screen.findByRole('region', { name: 'Landing' }))
    expect(block.getByText('Parked: window')).toBeVisible()
    expect(block.getByText((_, el) => el?.tagName === 'SPAN' && el.textContent === 'since 2026-10-28T08:00:00Z, told at 2026-10-28T10:00:00Z')).toBeVisible()
    expect(block.getByText('Answer not confirmed: card:7:1:root_failed lost')).toBeVisible()
    expect(block.getByText((_, el) => el?.tagName === 'P' && (el.textContent ?? '').startsWith('Closed as done at 2026-10-28T11:00:00Z: 1 delivered, 0 disposed'))).toBeVisible()
  })
  it('keeps reading accepted tasks until the mission closes, and removes stale landing on failure', async () => {
    const { rerender, ui } = mount()
    await screen.findByRole('region', { name: 'Landing' })
    landingStatus = 500
    rerender(ui(2))
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Landing' })).not.toBeInTheDocument())
    // A failed read leaves the scope unknown: an active mission is not Done (review 675 R-08).
    expect(screen.getByRole('listitem', { name: 'Done: future' })).toBeInTheDocument()
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
  it('shows a root held on the Owner\'s answer and a card waiting to be asked (review 675 R-07)', async () => {
    landingBody = { ...fixture, roots: [{ ...fixture.roots[0], held: { until: '2026-10-29T08:00:00Z', question_id: 'q_0123456789abcdef' }, card_pending: true }] }
    mount()
    const block = within(await screen.findByRole('region', { name: 'Landing' }))
    expect(block.getByText((_, el) => el?.tagName === 'SPAN' && (el.textContent ?? '').startsWith('Held on your answer until 2026-10-29T08:00:00Z'))).toBeVisible()
    expect(block.getByRole('link', { name: 'your answer' })).toHaveAttribute('href', '#questions?id=q_0123456789abcdef')
    expect(block.getByText('A card is waiting to be asked')).toBeVisible()
  })
  it('reads Landing on after the mission closed, so its close and a later reversal show (review 675 R-09)', async () => {
    missionState = 'done'
    landingBody = { ...fixture, closing_round: undefined, roots: fixture.roots.filter(root => root.class === 'delivered') }
    const { rerender, ui } = mount()
    await screen.findByRole('region', { name: 'Landing' })
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
