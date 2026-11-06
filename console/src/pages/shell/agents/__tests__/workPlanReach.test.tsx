import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { PlanModuleRows } from '../PlanModuleRows'
import { WorkPlanView } from '../WorkPlanView'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

/*
 * R5:5.5-1 「点得到」(the Owner 2026-11-01 03:3x): Work → Plan was fixed on R4, with no control to reach another round,
 * so round 5's modules and their detail pages could be opened only by editing the address. Work now shows the newest
 * round GET /progress lists, an address still names any round, the earlier rounds are one menu away and loaded only
 * when chosen, a module's whole row opens its detail, and the detail's ‹ Work goes back to where the row was.
 */

/** The PROD R4 answer (2026-10-27) renumbered to round 5, as planModuleDetail.test.tsx does. */
function planView(round: number): unknown {
  return JSON.parse(JSON.stringify(prod).replace(/"R4"/g, `"R${round}"`).replace(/R4:4\./g, `R${round}:${round}.`)
    .replace(/"4\.(\d+(?:-\d+)?)"/g, `"${round}.$1"`).replace(/plan:R4/g, `plan:R${round}`))
}
type TreeNode = { plan_node_id: string; parent_plan_node_id: string | null; node_kind: string; label: string; title: string; state: string | null; done_rule_json: unknown }
/**
 * GET /plan?root=Rn, the tree Work's sign-off count reads for every round (useSignoffWaits, SignoffWaiting's read). As
 * PROD answered on 2026-11-01: fifteen of R4's steps have an external_verified done rule (an adopted acceptance binding),
 * so R4 is an acceptance root, and its sign-off queue on Work's Sign-offs tab reads R4's plan view; R2, R3 and R5 have none.
 */
function planTree(round: number) {
  const nodes = (planView(round) as { nodes: TreeNode[] }).nodes.map(({ plan_node_id, parent_plan_node_id, node_kind, label, title, state, done_rule_json }) =>
    ({ plan_node_id, parent_plan_node_id, node_kind, label, title, state, done_rule_json }))
  if (round === 4) nodes.filter(n => n.node_kind === 'step').slice(0, 15).forEach(n => { n.done_rule_json = { external_verified: { binding: `acc-${n.label}`, version: 1 } } })
  return { root: `R${round}`, nodes }
}
/** GET /progress as the platform answers it: every root the runtime holds (progress.go planView). */
const progressBody = (ids = ['R2', 'R3', 'R4', 'R5']) => ({ plan: [{ plan_node_id: 'ARCH', kind: 'arch_root', title: 'Architecture', rows: [] },
  ...ids.map(id => ({ plan_node_id: id, kind: 'round', title: `第 ${id.slice(1)} 轮`, items: [], count: { done: 0, total: 0 } }))] })

let client: QueryClient
let fetcher: ReturnType<typeof vi.fn>
let progressReply: { status: number; body: unknown }
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
const urls = () => fetcher.mock.calls.map(([u]) => String(u))
const planViewUrls = () => urls().filter(u => u.includes('/agentrt/plan-view'))
/** The shell's GET /progress, as ConsolePage's useNeedsYou leaves it in the cache on every page. */
const seedShell = (ids?: string[]) => client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress(progressBody(ids)))
function page(hash: string) {
  window.location.hash = hash
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
}
async function go(hash: string) {
  await act(async () => { window.location.hash = hash; window.dispatchEvent(new Event('hashchange')) })
}
/** The Modules card's title, found by a selector: role queries over the whole page are slow here (review: 4 s of 5). */
function modulesTitle(): Element {
  const title = document.querySelector('section[aria-label="Modules"] h3')
  if (title == null) throw new Error('No Modules card yet')
  return title
}
const count = (list: string[], url: string) => list.filter(u => u === url).length
/** Beside Work's tab row (R5:5.5-2): which round this is among the rounds the runtime lists. */
const roundNote = () => document.querySelector<HTMLElement>('[data-round-note]')!

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(prod.as_of))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  progressReply = { status: 200, body: progressBody() }
  fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    const root = /plan-view\?root=R(\d+)(?:&explain=1)?$/.exec(url)
    if (root) return new Response(JSON.stringify(planView(Number(root[1]))), { status: 200 })
    if (url.endsWith('/agentrt/progress')) return new Response(JSON.stringify(progressReply.body), { status: progressReply.status })
    const tree = /agentrt\/plan\?root=R(\d+)$/.exec(url)
    if (tree) return new Response(JSON.stringify(planTree(Number(tree[1]))), { status: 200 })
    return new Response('{}', { status: 599 })
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('Work → Plan opens the newest round', () => {
  it('shows the newest round GET /progress lists when the address names none, and reads no other round', async () => {
    seedShell()
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    const rounds = screen.getByRole('navigation', { name: 'Plan rounds' })
    expect(within(rounds).getByRole('link', { name: /^Round 5/ })).toHaveAttribute('aria-current', 'page')
    expect(roundNote()).toHaveTextContent('newest in the runtime')
    // The shell's read is reused: no request of its own for GET /progress (the summary's sign-off count reads it from the
    // cache too, R5:5.5-2), and no plan view of an earlier round. Besides the plan view, the summary reads this round's
    // GET /plan for its sign-off count, and no other round's (R5:5.5-4), and its pace line reads this round's GET
    // /plan-pace (R5:5.5-13), and its coordinator pin line GET /pin-share (R5:5.7-8), which names no round.
    expect(urls().filter(u => u !== '/api/v1/agentrt/plan?root=R5' && u !== '/api/v1/agentrt/plan-pace?root=R5' && u !== '/api/v1/agentrt/pin-share')).toEqual(planViewUrls())
    expect(urls().some(u => u.endsWith('/agentrt/progress'))).toBe(false)
    expect(new Set(planViewUrls())).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R5&explain=1']))
  })

  it('reads GET /progress itself, once, only when nothing has read it yet', async () => {
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    expect(urls().filter(u => u.endsWith('/agentrt/progress'))).toHaveLength(1)
    expect(planViewUrls().every(u => u.endsWith('root=R5&explain=1'))).toBe(true)
  })

  it('keeps the round an address names, the old round= too, and offers the newest as its own link', async () => {
    seedShell()
    page('#work?section=plan&root=R4')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R4'))
    const rounds = screen.getByRole('navigation', { name: 'Plan rounds' })
    const newest = within(rounds).getByRole('link', { name: /^Round 5/ })
    expect(newest).toHaveAttribute('href', '#work')
    expect(newest).not.toHaveAttribute('aria-current')
    expect(rounds).toHaveTextContent('Round 4')
    expect(roundNote()).toHaveTextContent('an earlier round')
    expect(new Set(planViewUrls())).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R4&explain=1']))
    await go('#work?section=plan&round=R3')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R3'))
  })

  it('lists the earlier rounds in a menu and reads one only when it is chosen', async () => {
    seedShell()
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    const trigger = within(screen.getByRole('navigation', { name: 'Plan rounds' })).getByRole('button', { name: /Earlier rounds/ })
    fireEvent.keyDown(trigger, { key: 'Enter' })
    const items = await screen.findAllByRole('menuitem')
    expect(items.map(i => [i.textContent, i.getAttribute('href')])).toEqual([
      ['Round 4第 4 轮', '#work?root=R4'], ['Round 3第 3 轮', '#work?root=R3'], ['Round 2第 2 轮', '#work?root=R2']])
    expect(planViewUrls().some(u => /root=R[234]$/.test(u))).toBe(false)
    const href = items[0].getAttribute('href')!
    fireEvent.click(items[0])
    await go(href)
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R4'))
    expect(planViewUrls()).toContain('/api/v1/agentrt/plan-view?root=R4&explain=1')
    expect(planViewUrls().some(u => /root=R[23]$/.test(u))).toBe(false)
  })

  it('says the newest round is Unknown with the reason, reads no plan, and retries', async () => {
    progressReply = { status: 503, body: { error: 'unavailable', message: 'progress unavailable' } }
    page('#work')
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Newest round Unknown')
    expect(planViewUrls()).toEqual([])
    expect(screen.queryByRole('region', { name: 'Modules' })).toBeNull()
    progressReply = { status: 200, body: progressBody() }
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
  })

  it('keeps the rounds of the last answer when a later read fails, and says when they were read and why the next failed', async () => {
    seedShell()
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    expect(roundNote()).not.toHaveTextContent('rounds as of')
    progressReply = { status: 503, body: { error: 'unavailable', message: 'progress unavailable' } }
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-progress'] }) })
    await waitFor(() => expect(roundNote()).toHaveTextContent(/rounds as of \d\d:\d\d:\d\d UTC · the latest read failed: /))
    expect(modulesTitle()).toHaveTextContent('Modules · R5')
  })

  it('never reads a progress answer without its plan list as no rounds; says none only when the list holds none', async () => {
    progressReply = { status: 200, body: {} }
    page('#work')
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Newest round Unknown · the runtime's progress answer has no plan list")
    // Other reads (Plan sign-offs) go on without the round: the sentence is about this view only.
    expect(alert).toHaveTextContent('This plan and live view shows no round until it is known')
    cleanup(); client.clear()
    progressReply = { status: 200, body: progressBody([]) }
    page('#work')
    expect(await screen.findByText('The runtime holds no round yet.')).toBeInTheDocument()
    expect(planViewUrls()).toEqual([])
  })

  it('says Unknown, not "no round", when the runtime lists rounds but none has an R<n> id; leaves such ids out beside good ones', async () => {
    progressReply = { status: 200, body: progressBody(['R5b', 'round-6']) }
    page('#work')
    expect(await screen.findByRole('alert')).toHaveTextContent('Newest round Unknown · the runtime lists rounds whose ids are not R<n> (R5b, round-6)')
    expect(screen.queryByText('The runtime holds no round yet.')).toBeNull()
    expect(planViewUrls()).toEqual([])
    cleanup(); client.clear()
    progressReply = { status: 200, body: progressBody(['R4', 'R5', 'R6b']) }
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    expect(roundNote()).toHaveTextContent('ignored, ids not R<n>: R6b')
  })

  it('says it is reading which round is newest once, in the plan\'s place', () => {
    client.setQueryDefaults(['agent-runtime-progress'], { queryFn: () => new Promise(() => {}) })
    fetcher.mockImplementation(() => new Promise(() => {}))
    page('#work')
    expect(screen.getAllByText('Reading which round is newest…')).toHaveLength(1)
    expect(screen.getByRole('status')).toHaveTextContent('Reading which round is newest…')
  })
})

describe('Work\'s plan reads with an acceptance root (PROD 2026-11-01)', () => {
  // R5:5.5-2: SignoffWaiting is no longer on Work; R4's acceptance sign-offs are read on the Sign-offs tab only.
  // R5:5.5-4: so are the earlier rounds' trees: the round tab reads its own round's GET /plan and no other.
  it('reads the newest round for Work and R4 for its acceptance sign-offs only on Sign-offs: one plan view each, none for R2 or R3', async () => {
    seedShell()
    page('#work')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    const trees = () => new Set(urls().filter(u => u.includes('/agentrt/plan?')))
    await waitFor(() => expect(document.querySelector('[data-summary-signoffs]')).toHaveTextContent('Sign-offs: 0 ready · 13 later · Round 5 only · other rounds on the Sign-offs tab'))
    expect(trees()).toEqual(new Set(['/api/v1/agentrt/plan?root=R5']))
    // Not having read R4's tree, the round tab cannot know R4 is an acceptance root, and does not say anything of it.
    expect(document.querySelector('[data-summary-signoffs]')).not.toHaveTextContent('R4')
    expect(new Set(planViewUrls())).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R5&explain=1']))
    await go('#work?tab=signoffs')
    // The Sign-offs tab reads every round's tree (the Owner 2026-10-22), as SignoffWaiting did, and counts them all.
    await waitFor(() => expect(trees()).toEqual(new Set(['R2', 'R3', 'R4', 'R5'].map(r => `/api/v1/agentrt/plan?root=${r}`))))
    await waitFor(() => expect(document.querySelector('[data-summary-signoffs]')).toHaveTextContent('R4 acceptance not counted here'))
    await waitFor(() => expect(planViewUrls()).toContain('/api/v1/agentrt/plan-view?root=R4&explain=1'))
    await waitFor(() => expect(screen.queryByText('Reading acceptance sign-offs…')).toBeNull())
    expect(count(planViewUrls(), '/api/v1/agentrt/plan-view?root=R5&explain=1')).toBe(1)
    expect(count(planViewUrls(), '/api/v1/agentrt/plan-view?root=R4&explain=1')).toBe(1)
    expect(new Set(planViewUrls())).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R4&explain=1', '/api/v1/agentrt/plan-view?root=R5&explain=1']))
  })
})

describe('the sign-off count reads every round only when the round list is whole (R5:5.5-2 review)', () => {
  const signoffs = () => document.querySelector('[data-summary-signoffs]')!
  it('says only R2 was read when GET /progress has no plan list, with a round in the address too, and the lamp is not green', async () => {
    progressReply = { status: 200, body: {} }
    // The round tab reads the round the address names and no other: no round list is needed for its count (R5:5.5-4).
    page('#work?root=R5')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    await waitFor(() => expect(signoffs()).toHaveTextContent('Sign-offs: 0 ready · 13 later · Round 5 only · other rounds on the Sign-offs tab'))
    expect(new Set(urls().filter(u => u.includes('/agentrt/plan?')))).toEqual(new Set(['/api/v1/agentrt/plan?root=R5']))
    // The Sign-offs tab reads every round the list holds: with no list, R2 alone, and it says so.
    await go('#work?tab=signoffs&root=R5')
    await waitFor(() => expect(signoffs()).toHaveTextContent('not started Unknown'))
    expect(signoffs()).toHaveTextContent("only R2 read; which rounds exist is Unknown (the runtime's progress answer has no plan list)")
    expect(new Set(urls().filter(u => u.includes('/agentrt/plan?')))).toEqual(new Set(['/api/v1/agentrt/plan?root=R2', '/api/v1/agentrt/plan?root=R5']))
    const lamp = document.querySelector('[data-summary-tone]')!
    expect(lamp).not.toHaveAttribute('data-summary-tone', 'ok')
    expect(lamp.getAttribute('aria-label')).toContain('only R2 read')
  })
  it('names the rounds whose ids are not R<n> as not read, beside the good ones', async () => {
    progressReply = { status: 200, body: progressBody(['R4', 'R5', 'R6b']) }
    page('#work?tab=signoffs')
    await waitFor(() => expect(signoffs()).toHaveTextContent('round R6b not read: its id is not R<n>'))
    expect(signoffs()).toHaveTextContent('Sign-offs: 0+ ready · not started Unknown · 13 later in Round 5')
    expect(urls().some(u => u.includes('root=R6b'))).toBe(false)
  })
})

describe('a module row opens its detail', () => {
  const view = () => parsePlanSnapshot(planView(5), 'R5')
  const row = (label: string) => within(screen.getByRole('list', { name: 'Modules in mainline order' })).getAllByRole('listitem').find(li => li.dataset.module === label)!

  it('opens the module from anywhere on its row, but a step cell or a wait still does its own thing', async () => {
    window.location.hash = '#work?root=R5'
    render(<PlanModuleRows view={view()} selected="" />)
    const r51 = row('5.1')
    expect(r51).toHaveTextContent('›')
    fireEvent.click(within(r51).getByText('Mainline 1'))
    expect(window.location.hash).toBe('#work?section=plan&root=R5&node=R5%3A5.1')
    window.location.hash = '#work?root=R5'
    fireEvent.click(within(r51).getByRole('button', { name: 'Step 5.1-1: not started' }))
    expect(window.location.hash).toBe('#work?root=R5')
    fireEvent.click(within(screen.getByRole('region', { name: 'Step 5.1-1' })).getByText('Done when'))
    expect(window.location.hash).toBe('#work?root=R5')
    const wait = within(row('5.2')).getByRole('link', { name: '5.1 on PROD' })
    expect(wait).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1')
  })

  it('is reached by the keyboard: its link is the row\'s first tab stop and its focus ring is drawn on the row', () => {
    render(<PlanModuleRows view={view()} selected="" />)
    const r51 = row('5.1')
    const link = within(r51).getByRole('link', { name: /^Open module 5\.1 ·/ })
    const tabbable = [...r51.querySelectorAll<HTMLElement>('a[href], button')].filter(el => el.tabIndex >= 0)
    expect(tabbable[0]).toBe(link)
    link.focus()
    expect(document.activeElement).toBe(link)
    expect(r51.className).toMatch(/has-\[\[data-row-link\]:focus-visible\]:outline/)
  })

  it('names a dependency wait, and says no dependency waits rather than nothing blocking', () => {
    render(<PlanModuleRows view={view()} selected="" />)
    expect(row('5.1')).toHaveTextContent('No dependency waiting')
    expect(row('5.1')).not.toHaveTextContent('Nothing blocking')
  })

  it('says Dependencies Unknown, with the server\'s reason, for a module whose view document entry was not read', () => {
    // planfactssnapshot.go: a view document that does not parse leaves artifact_id set, a warning, no edges, no node.view.
    type Raw = { nodes: Record<string, unknown>[]; items: { plan_node_id: string }[]; edges: Edge[]; waiting_on: { edge: Edge }[]; warnings: string[]; artifact_id: string; mainline: Record<string, unknown> }
    type Edge = { from: { node: string }; to: { node: string } }
    const raw = planView(5) as Raw
    const warning = 'Plan root R5: view document unavailable: unexpected end of JSON input'
    raw.nodes.forEach(n => { delete n.view; if (n.node_kind === 'item') n.lane_kind = 'Unclassified' })
    Object.assign(raw, { items: [], edges: [], waiting_on: [], mainline: { ...raw.mainline, path: [], current: null }, warnings: [warning, ...raw.warnings] })
    expect(raw.artifact_id).not.toBe('')
    render(<PlanModuleRows view={parsePlanSnapshot(raw, 'R5')} selected="" />)
    expect(row('5.1')).toHaveTextContent(`Dependencies Unknown · ${warning}`)
    expect(row('5.1')).not.toHaveTextContent('No dependency waiting')
    cleanup()
    // A document that parsed but does not list one module: that row alone is Unknown.
    const one = planView(5) as Raw
    const touches = (e: Edge) => e.from.node === 'R5:5.6' || e.to.node === 'R5:5.6'
    delete one.nodes.find(n => n.plan_node_id === 'R5:5.6')!.view
    Object.assign(one, { items: one.items.filter(i => i.plan_node_id !== 'R5:5.6'), edges: one.edges.filter(e => !touches(e)), waiting_on: one.waiting_on.filter(w => !touches(w.edge)) })
    render(<PlanModuleRows view={parsePlanSnapshot(one, 'R5')} selected="" />)
    expect(row('5.6')).toHaveTextContent('Dependencies Unknown · this module is not in the adopted plan view')
    expect(row('5.1')).toHaveTextContent('No dependency waiting')
  })
})

describe('the module detail goes back to Work', () => {
  // The whole Work → Plan with a module's detail open: role queries are kept off the full page (selectors instead).
  it('shows ‹ Work and Round 5 › 5.1, and back puts the row where it was on the screen', { timeout: 15_000 }, async () => {
    seedShell()
    const scrollBy = vi.fn()
    vi.stubGlobal('scrollBy', scrollBy)
    Element.prototype.scrollIntoView = vi.fn()
    page('#work?root=R5')
    await waitFor(() => expect(modulesTitle()).toHaveTextContent('Modules · R5'))
    const link = () => document.getElementById('module-row-R5:5.1')!
    expect(link()).toHaveAccessibleName(/^Open module 5\.1 ·/)
    // Where the row's link is on the screen: 300 px down when it is clicked, 900 px down once the detail has closed.
    let top = 300
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const y = this.id === 'module-row-R5:5.1' ? top : 0
      return { top: y, bottom: y + 40, left: 0, right: 0, width: 0, height: 40, x: 0, y, toJSON: () => ({}) }
    })
    fireEvent.click(within(link().closest('li')!).getByText('Mainline 1'))
    top = 900
    await go(window.location.hash)
    await waitFor(() => expect(document.querySelector('[data-module-detail="5.1"]')).not.toBeNull())
    const detail = document.querySelector<HTMLElement>('[data-module-detail="5.1"]')!
    expect(detail).toHaveAccessibleName('Module 5.1')
    const crumbs = detail.querySelector<HTMLElement>('nav')!
    expect(crumbs).toHaveAccessibleName('Module path')
    const back = within(crumbs).getByRole('link', { name: 'Back to Work' })
    expect(back).toHaveAttribute('href', '#work?root=R5')
    expect(back).toHaveTextContent('‹ Work')
    expect(within(crumbs).getByRole('link', { name: 'Round 5' })).toHaveAttribute('href', '#work?root=R5')
    expect(within(crumbs).getByText('5.1')).toHaveAttribute('aria-current', 'page')
    await go(back.getAttribute('href')!)
    await waitFor(() => expect(document.querySelector('[data-module-detail="5.1"]')).toBeNull())
    await waitFor(() => expect(scrollBy).toHaveBeenCalledWith({ top: 600, behavior: 'instant' }))
    expect(document.activeElement).toBe(link())
  })
})
