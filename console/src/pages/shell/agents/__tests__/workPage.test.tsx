import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import missionFixture from '@/api/__fixtures__/agentrt-mission-M-2.prod.json'
import tasksFixture from '@/api/__fixtures__/agentrt-tasks-M-2.prod.json'
import eventsFixture from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { OwnerConsolePage } from '@/pages/shell/owner/OwnerConsolePage'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => 'owner-token' }))
// Code health draws a @bifrost/ui Dialog, which jsdom here cannot render (a second React); that its page opens is enough.
vi.mock('@/pages/CodeHealthPage', () => ({ CodeHealthPage: () => <p data-testid="code-health-page">Code health page</p> }))

/*
 * R5:5.5-2 (the Owner 2026-11-01 03:3x: Work was 17.7 screens, 28 blocks, "太长了"): Work opens on one summary card and
 * four tabs (Design v3.6 WorkPage); everything else that was on the page is a step away and is neither drawn nor read
 * until the address names it. Every block that was there is still reachable, and every address Work took still works.
 */

/** The PROD R4 answer renumbered to round 5, with 5.1 two steps done and one in progress, and 5.5 one in progress. */
function planView(round: number) {
  const v = JSON.parse(JSON.stringify(prod).replace(/"R4"/g, `"R${round}"`).replace(/R4:4\./g, `R${round}:${round}.`)
    .replace(/"4\.(\d+(?:-\d+)?)"/g, `"${round}.$1"`).replace(/plan:R4/g, `plan:R${round}`))
  for (const [id, state] of [['1-1', 'done'], ['1-2', 'done'], ['1-3', 'active'], ['5-1', 'active']]) {
    const node = v.nodes.find((n: { plan_node_id: string }) => n.plan_node_id === `R${round}:${round}.${id}`)
    if (node) node.state = state
  }
  return v
}
const progressBody = { ...progressFixture, plan: [{ plan_node_id: 'R4', kind: 'round', title: '第 4 轮', items: [] }, { plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }] }
const auth = { token: 'owner-token', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }

let client: QueryClient
let fetcher: ReturnType<typeof vi.fn>
let failing: RegExp | null
/** R4 as PROD answered on 2026-11-01: fifteen steps with an external_verified done rule, so R4 is an acceptance root. */
let acceptanceR4: boolean
/** While set, GET /progress does not answer (a cold load: the rounds are being read); its call releases them. */
let holdProgress: (() => void) | null
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const urls = () => fetcher.mock.calls.map(([u]) => String(u))
/** What moved a step away reads: goal cards, release runs and records, the coordinator's mailbox. */
const STEP_AWAY = /\/agentrt\/missions\/|\/agentrt\/tasks\?mission=|\/agentrt\/events\?stream=mission|\/landing\/missions\/|\/delivery\/pipelines|\/releases\?limit=|\/messages\/thread/

function page(hash: string) {
  window.history.replaceState(null, '', hash)
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><OwnerConsolePage entry="work" /></PlatformAuthContext.Provider></QueryClientProvider>)
}
async function go(hash: string) {
  await act(async () => { window.history.replaceState(null, '', hash); window.dispatchEvent(new Event('hashchange')) })
}
const summary = () => screen.findByRole('region', { name: 'Where the round stands' })
const tab = (name: RegExp) => within(screen.getByRole('navigation', { name: 'Work views' })).getByRole('link', { name })

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(prod.as_of))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  Element.prototype.scrollIntoView = vi.fn()
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  failing = null
  acceptanceR4 = false
  holdProgress = null
  let held: Promise<void> | null = null
  fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (failing?.test(url)) return json({ error: 'unavailable', message: 'unavailable' }, 503)
    if (url.endsWith('/agentrt/progress') && holdProgress) {
      held ??= new Promise<void>(release => { holdProgress = release })
      await held
    }
    const view = /plan-view\?root=R(\d+)$/.exec(url)
    if (view) return json({ ...planView(Number(view[1])), as_of: prod.as_of })
    const tree = /\/agentrt\/plan\?root=(R\d+)$/.exec(url)
    if (tree) {
      const nodes = planView(Number(tree[1].slice(1))).nodes.map((n: Record<string, unknown>) => ({ ...n, signoffs: [] }))
      if (acceptanceR4 && tree[1] === 'R4') nodes.filter((n: Record<string, unknown>) => n.node_kind === 'step').slice(0, 15)
        .forEach((n: Record<string, unknown>) => { n.done_rule_json = { external_verified: { binding: `acc-${String(n.label)}`, version: 1 } } })
      return json({ root: tree[1], nodes })
    }
    if (url.endsWith('/agentrt/progress')) return json({ ...progressBody, generated_at: prod.as_of })
    const mission = /\/agentrt\/missions\/(M-\d+)$/.exec(url)
    if (mission) return json({ ...missionFixture, mission_id: mission[1] })
    if (url.includes('/agentrt/tasks?mission=')) return json(tasksFixture)
    if (url.includes('/agentrt/events?stream=mission')) return json(eventsFixture)
    return json({ error: 'not_found', message: 'not in this test' }, 404)
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('Work opens on the round', () => {
  it('draws one summary computed from the reads, the tab row and the round tab, and reads nothing moved a step away', async () => {
    page('#work')
    const card = await summary()
    await waitFor(() => expect(card.querySelector('[data-summary-sentence]')).toHaveTextContent(
      'The mainline stands at 5.1 · runtime steps 2 of 6 done, 1 active. By runtime step state, of the 9 other items, 1 has a step active and 8 have no step started.'))
    expect(card).toHaveTextContent('Round 5 · 10 items · plan v1')
    expect(card).toHaveTextContent('Mainline 5.1 → 5.2 → 5.4 → 5.10')
    await waitFor(() => expect(within(card).getByRole('link', { name: /^Sign-offs:/ })).toHaveTextContent('Sign-offs: 0 ready · 13 later in Round 5'))
    expect(card).toHaveTextContent('13 dependency waits')
    expect(card).toHaveTextContent('ETA Unknown')
    // Thirteen Owner steps' readiness is Unknown: "nothing for you" is not known, so the lamp is grey, not green.
    expect(card.querySelector('[data-summary-tone]')).toHaveAttribute('data-summary-tone', 'unknown')
    expect(card.querySelector('[data-summary-tone]')).toHaveAccessibleName("Not fully known: 13 Owner steps' readiness in Round 5 is Unknown")
    expect(card).toHaveTextContent('“no step started”: every step is planned in the runtime')
    expect(tab(/^Round 5/)).toHaveAttribute('aria-current', 'page')
    expect(tab(/^Sign-offs · 0 ready · 13 later in Round 5/)).toHaveAttribute('href', '#work?tab=signoffs')
    expect(tab(/^Live · 0 registered/)).toHaveAttribute('href', '#work?tab=live')
    expect(tab(/^Changes/)).toHaveAttribute('href', '#work?tab=changes')
    expect(screen.getByRole('region', { name: 'Modules' })).toHaveTextContent('Modules · R5')
    for (const name of ['Sign-offs', 'Live work', 'Recent changes', 'Whole plan', 'Waiting on', 'Now', 'Open goals', 'Crew', 'Quota', 'Release lane', 'Goals', 'Activity', 'History', 'Commit lineage', 'Code health']) {
      expect(screen.queryByRole('region', { name })).toBeNull()
    }
    await act(async () => { await new Promise(r => setTimeout(r, 50)) })
    expect(urls().filter(u => STEP_AWAY.test(u))).toEqual([])
    expect(urls().filter(u => u.includes('plan-view'))).toEqual(['/api/v1/agentrt/plan-view?root=R5'])
  })
})

describe('the tabs', () => {
  it('draw one at a time, keep the tab in the address, and keep a round the address names', async () => {
    page('#work?root=R4')
    await summary()
    await waitFor(() => expect(screen.getByRole('region', { name: 'Modules' })).toHaveTextContent('Modules · R4'))
    expect(tab(/^Live/)).toHaveAttribute('href', '#work?tab=live&root=R4')
    await go(tab(/^Live/).getAttribute('href')!)
    expect(await screen.findByRole('region', { name: 'Live work' })).toHaveTextContent('Recently submitted · 4 of 4')
    expect(screen.queryByRole('region', { name: 'Modules' })).toBeNull()
    expect(tab(/^Live/)).toHaveAttribute('aria-current', 'page')
    expect(tab(/^Round 4/)).toHaveAttribute('href', '#work?root=R4')
    await go('#work?tab=changes&root=R4')
    const changes = await screen.findByRole('region', { name: 'Recent changes' })
    expect(changes).toHaveTextContent('Recent changes · 8 of 20 read')
    expect(changes).toHaveTextContent('The plan view returns 20 changes a page; older ones load on request.')
    expect(screen.queryByRole('region', { name: 'Live work' })).toBeNull()
    await go('#work?tab=signoffs&root=R4')
    const signoffs = await screen.findByRole('region', { name: 'Sign-offs' })
    await waitFor(() => expect(signoffs).toHaveTextContent('Later in Round 4 · 13 · not marked ready'))
    expect(signoffs).toHaveTextContent('Readiness Unknown on 13')
    expect(within(signoffs).getAllByRole('listitem')).toHaveLength(8)
    fireEvent.click(within(signoffs).getByRole('button', { name: 'Show 5 more' }))
    expect(within(signoffs).getAllByRole('listitem')).toHaveLength(13)
    // A module open in the round tab is not carried into another tab.
    await go('#work?section=plan&root=R4&node=R4%3A4.1')
    expect(await screen.findByRole('region', { name: 'Module 4.1' })).toBeInTheDocument()
    expect(tab(/^Changes/)).toHaveAttribute('href', '#work?tab=changes&root=R4')
  })
})

describe('every address Work took still lands where it did', () => {
  it('section=signoff opens the Sign-offs tab and brings it into view; plan, root, round and node open the round', async () => {
    page('#work?section=signoff')
    const signoffs = await screen.findByRole('region', { name: 'Sign-offs' })
    expect(signoffs).toHaveAttribute('id', 'signoff')
    await waitFor(() => expect(vi.mocked(Element.prototype.scrollIntoView).mock.contexts).toContain(signoffs))
    await go('#work?section=plan&round=R4')
    await waitFor(() => expect(screen.getByRole('region', { name: 'Modules' })).toHaveTextContent('Modules · R4'))
    // section=plan still brings the plan into view (Work's DomainNav "Plan").
    await waitFor(() => expect(vi.mocked(Element.prototype.scrollIntoView).mock.contexts).toContain(document.getElementById('plan')))
    await go('#work?section=plan&root=R5&node=R5%3A5.1-3')
    expect(await screen.findByRole('region', { name: 'Module 5.1' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Modules' })).toBeInTheDocument()
  })

  it('goal= and goal=&task= open the goal cards a step away, with ‹ Work back', async () => {
    page('#work?goal=M-2&task=t_1')
    const goals = await screen.findByRole('region', { name: 'Goals' })
    expect(await within(goals).findByText(/M-2/)).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Where the round stands' })).toBeNull()
    expect(within(screen.getByRole('navigation', { name: 'Work page' })).getByRole('link', { name: 'Back to Work' })).toHaveAttribute('href', '#work')
  })
})

describe('every block that was on Work is still reachable', () => {
  // Each address draws its own part, and only it: the others are a tab or a page away.
  it.each([
    ['#work', ['Where the round stands', 'Modules'], ['Live work', 'Now', 'Goals']],
    ['#work?tab=signoffs', ['Where the round stands', 'Sign-offs'], ['Modules', 'Live work']],
    ['#work?tab=live', ['Where the round stands', 'Live work'], ['Modules', 'Recent changes']],
    ['#work?tab=changes', ['Where the round stands', 'Recent changes'], ['Modules', 'Live work']],
    ['#work?section=goals', ['Goals'], ['Where the round stands', 'Now']],
    ['#work?section=activity', ['Now', 'Open goals', 'Crew', 'Quota', 'Release lane', 'Activity', 'History'], ['Where the round stands', 'Goals']],
    ['#work?section=commit-lineage', ['Commit lineage'], ['Where the round stands', 'Code health']],
    ['#work?section=code-health', ['Code health'], ['Where the round stands', 'Commit lineage']],
  ])('%s', async (hash, regions, elsewhere) => {
    page(hash)
    for (const name of regions) expect(await screen.findByRole('region', { name })).toBeInTheDocument()
    for (const name of elsewhere) expect(screen.queryByRole('region', { name })).toBeNull()
  })

  it('keeps the whole plan and the dependency waits one fold away, and the old Main line facts in the summary', async () => {
    page('#work')
    const card = await summary()
    await waitFor(() => expect(card).toHaveTextContent('Next · Inspect remaining steps and waiting dependencies'))
    expect(card).toHaveTextContent('Mainline basis · Existing controller step states')
    expect(card).toHaveTextContent('Plan version 1 · sha256:')
    for (const [fold, region] of [[/^Whole plan/, 'Whole plan'], [/^Dependency waits · 13/, 'Waiting on']] as const) {
      fireEvent.click(screen.getByText(fold, { selector: 'summary' }))
      expect(await screen.findByRole('region', { name: region })).toBeInTheDocument()
    }
    expect(screen.queryByRole('link', { name: 'Governance references · Blueprint, Standards, MCP and protocols' })).toBeNull()
  })

  it('reads what a page a step away needs only once it is open', async () => {
    page('#work')
    await summary()
    expect(urls().filter(u => STEP_AWAY.test(u))).toEqual([])
    await go('#work?section=activity')
    await screen.findByRole('region', { name: 'Release lane' })
    await waitFor(() => expect(urls().some(u => u.endsWith('/delivery/pipelines'))).toBe(true))
    expect(urls().some(u => u.includes('/releases?limit='))).toBe(true)
    expect(urls().some(u => u.includes('/messages/thread'))).toBe(true)
    await go('#work?section=goals')
    expect(await screen.findByRole('link', { name: 'Governance references · Blueprint, Standards, MCP and protocols' })).toBeInTheDocument()
    await waitFor(() => expect(urls().some(u => /\/agentrt\/missions\/M-\d+$/.test(u))).toBe(true))
  })
})

describe('what cannot be read says Unknown', () => {
  it('a failed GET /plan leaves the step sign-offs out and says so, never 0 of them', async () => {
    failing = /\/agentrt\/plan\?root=/
    page('#work?tab=signoffs')
    const card = await summary()
    await waitFor(() => expect(within(card).getByRole('link', { name: /^Sign-offs:/ })).toHaveTextContent(
      'Sign-offs: 0+ ready · not started Unknown · 13 later in Round 5 · the plan could not be read (GET /plan)'))
    const signoffs = screen.getByRole('region', { name: 'Sign-offs' })
    expect(within(signoffs).getByRole('alert')).toHaveTextContent('Sign-offs partly Unknown · the plan could not be read (GET /plan)')
    expect(signoffs.querySelector('[data-signoff-ready]')).toHaveTextContent('0+')
    expect(card.querySelector('[data-summary-tone]')).toHaveAttribute('data-summary-tone', 'unknown')
  })

  it('a plan view that cannot be read is Unknown with why, and nothing of the round is drawn from nothing', async () => {
    failing = /plan-view/
    page('#work')
    const card = await summary()
    await waitFor(() => expect(within(card).getByRole('alert')).toHaveTextContent('Unknown · Plan view'))
    expect(card).toHaveTextContent('No successful snapshot yet.')
    expect(within(card).getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Modules' })).toBeNull()
    // GET /plan still answers (no step sign-off here); the round's own sign-offs are left out, and the count says so.
    await waitFor(() => expect(tab(/^Sign-offs/)).toHaveTextContent('Sign-offs · 0+ ready · later in Round 5 Unknown'))
    expect(card.querySelector('[data-summary-tone]')).toHaveAttribute('data-summary-tone', 'unknown')
  })
})

describe('a cold load while the rounds are read (R5:5.5-2 review)', () => {
  it('section=signoff says it is reading, not that the runtime holds no round, and raises no alert', async () => {
    holdProgress = () => {}
    page('#work?section=signoff')
    const signoffs = await screen.findByRole('region', { name: 'Sign-offs' })
    const card = await summary()
    expect(card).toHaveTextContent('Reading which round is newest…')
    expect(tab(/^Sign-offs/)).toHaveTextContent('Sign-offs · reading…')
    expect(within(signoffs).queryByRole('alert')).toBeNull()
    expect(within(card).queryByRole('alert')).toBeNull()
    expect(document.body).not.toHaveTextContent('holds no round')
    await act(async () => { holdProgress?.() })
    await waitFor(() => expect(tab(/^Sign-offs/)).toHaveTextContent('Sign-offs · 0 ready · 13 later in Round 5'))
    expect(within(screen.getByRole('region', { name: 'Sign-offs' })).queryByRole('alert')).toBeNull()
  })
})

describe('with an acceptance root beside the newest round (PROD 2026-11-01: R4)', () => {
  it('reads only R5\'s plan view on the round, says R4 is not counted, and reads R4 once on Sign-offs', async () => {
    acceptanceR4 = true
    page('#work')
    const card = await summary()
    await waitFor(() => expect(within(card).getByRole('link', { name: /^Sign-offs:/ })).toHaveTextContent('Sign-offs: 0 ready · 13 later in Round 5 · R4 acceptance not counted here'))
    expect(card.querySelector('[data-summary-tone]')).toHaveAccessibleName(expect.stringContaining('acceptance sign-offs of R4 are not counted here'))
    await act(async () => { await new Promise(r => setTimeout(r, 50)) })
    expect(urls().filter(u => u.includes('plan-view'))).toEqual(['/api/v1/agentrt/plan-view?root=R5'])
    await go('#work?tab=signoffs')
    const signoffs = await screen.findByRole('region', { name: 'Sign-offs' })
    expect(signoffs).toHaveTextContent('Acceptance sign-offs of R4 are below and not counted here.')
    expect(within(signoffs).getByText('Acceptance sign-offs in Round 4')).toBeInTheDocument()
    await waitFor(() => expect(urls().filter(u => u === '/api/v1/agentrt/plan-view?root=R4')).toHaveLength(1))
    await waitFor(() => expect(within(signoffs).queryByText('Reading acceptance sign-offs…')).toBeNull())
    expect(urls().filter(u => u === '/api/v1/agentrt/plan-view?root=R4')).toHaveLength(1)
  })
})
