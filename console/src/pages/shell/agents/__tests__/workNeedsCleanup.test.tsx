import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import presenceFixture from '@/api/__fixtures__/agentrt-progress-presence.testdb.json'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { OwnerConsolePage } from '@/pages/shell/owner/OwnerConsolePage'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

/*
 * R5:5.5-4 (PROD 2026-11-01): Work's first screen carried "Host mbp-dev is offline: its last poll was 14 days ago." for
 * two weeks, and two registered external works (TD-457, TD-459) had sent no heartbeat for 50 hours. They are true, and
 * no news to the round: they are one list, Needs cleanup, on Work → Activity, and the summary keeps one entry whose count
 * is that list's. Claims and sources: the R5:5.5-4 hand-in report.
 */

const NOW = Date.parse(prod.as_of)
/**
 * The PROD R4 answer renumbered to round n, with TD-457 registered on n.2-1 and Running, silent for 50 hours: in working,
 * or (underAttempt) only in external_work and the history of a task a runtime attempt runs, as the server leaves it.
 */
function planView(round: number, o: { asOf?: string; working?: boolean; clean?: boolean; underAttempt?: boolean } = {}) {
  const v = JSON.parse(JSON.stringify(prod).replace(/"R4"/g, `"R${round}"`).replace(/R4:4\./g, `R${round}:${round}.`)
    .replace(/"4\.(\d+(?:-\d+)?)"/g, `"${round}.$1"`).replace(/plan:R4/g, `plan:R${round}`))
  const asOf = o.asOf ?? prod.as_of
  const silent = { ...v.external_work[0], external_work_id: 'ew_td457', source_run_key: 'codex-td-457', plan_node_id: `R${round}:${round}.2-1`, state: 'Running',
    type: 'heartbeat', status: 'Unconfirmed', heartbeat_age_s: 50 * 3600, heartbeat_at: new Date(Date.parse(asOf) - 50 * 3600_000).toISOString(),
    ended_at: undefined, exit_code: undefined, report_digest: undefined, report_path_digest: undefined,
    description: { ...v.external_work[0].description, task: 'TD-457: fix the landing gate', branch: 'td/td-457' } }
  if (o.working !== false) Object.assign(v, { working: [silent], external_work: [...v.external_work, silent] })
  if (o.underAttempt) {
    const task = { task_id: 't_01M69PWCTHGN75N160E9F0WBP3', plan_node_id: silent.plan_node_id, mission_id: 'M-251', title: 'Fix the landing gate', state: 'running',
      attempt: { attempt_id: 'a_1', attempt_no: 1, task_id: 't_01M69PWCTHGN75N160E9F0WBP3', mission_id: 'M-251', host_id: 'h1', executor: 'codex', model: 'gpt-6-astra',
        state: 'running', elapsed_s: 600, heartbeat_age_s: 3, status: 'Running', reason: 'implementing', persisted: true },
      external_work: [{ ...silent, task_id: 't_01M69PWCTHGN75N160E9F0WBP3' }] }
    Object.assign(v, { tasks: [...v.tasks, { ...task, attempt: undefined, external_work: undefined }], working: [task],
      external_work: [...v.external_work.filter((w: { external_work_id: string }) => w.external_work_id !== 'ew_td457'), { ...silent, task_id: task.task_id }] })
  }
  // Nothing for the Owner in this round: no Owner step of unknown readiness, no sign-off, no task waiting.
  if (o.clean) Object.assign(v, { owner_later: [], owner_waiting: [] })
  return { ...v, as_of: asOf }
}
/** GET /progress from real rows (W-31 3.5): h1 online, h2 reported offline; two rounds. */
const progressBody = { ...presenceFixture, plan: [{ plan_node_id: 'R4', kind: 'round', title: '第 4 轮', items: [] }, { plan_node_id: 'R5', kind: 'round', title: '第 5 轮', items: [] }] }
const HOST = 'Host h2 is offline: its last poll was 3 days ago.'
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }

let client: QueryClient
let fetcher: ReturnType<typeof vi.fn>
let failing: RegExp | null
let view: (round: number) => unknown
let progress: typeof progressBody
/** While set, GET /progress never answers: a refresh that hangs, neither failing nor answering. */
let hang: boolean
/** While set, GET /plan?root= never answers. */
let hangTrees: boolean
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const urls = () => fetcher.mock.calls.map(([u]) => String(u))

function page(hash: string) {
  window.history.replaceState(null, '', hash)
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><OwnerConsolePage entry="work" /></PlatformAuthContext.Provider></QueryClientProvider>)
}
async function go(hash: string) {
  await act(async () => { window.history.replaceState(null, '', hash); window.dispatchEvent(new Event('hashchange')) })
}
const summary = () => screen.findByRole('region', { name: 'Where the round stands' })
const entry = () => document.querySelector<HTMLAnchorElement>('[data-summary-cleanup]')!

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(NOW)
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  Element.prototype.scrollIntoView = vi.fn()
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  failing = null
  view = round => planView(round)
  progress = progressBody
  hang = false
  hangTrees = false
  fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (failing?.test(url)) return json({ error: 'unavailable', message: 'unavailable' }, 503)
    const pv = /plan-view\?root=R(\d+)$/.exec(url)
    if (pv) return json(view(Number(pv[1])))
    const tree = /\/agentrt\/plan\?root=(R\d+)$/.exec(url)
    if (tree && hangTrees) return new Promise<Response>(() => {})
    if (tree) return json({ root: tree[1], nodes: planView(Number(tree[1].slice(1))).nodes.map((n: Record<string, unknown>) => ({ ...n, signoffs: [] })) })
    if (url.endsWith('/agentrt/progress')) return hang ? new Promise<Response>(() => {}) : json({ ...progress, generated_at: prod.as_of })
    return json({ error: 'not_found', message: 'not in this test' }, 404)
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('Work\'s first screen', () => {
  it('no longer says the offline host; the summary counts what needs cleanup and goes to it, with no request of its own', async () => {
    page('#work')
    const card = await summary()
    // Each group with its reach in the visible words (review of 16435ae7): the round, not a total of two reaches.
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 1 external work without heartbeat in Round 5 ›'))
    expect(entry()).toHaveAttribute('href', '#work?section=activity')
    expect(entry().title).toContain('Hosts the controller reports offline 1: every host of the runtime')
    expect(entry().title).toContain('Registered external work with no heartbeat for over 60 s 1: Round 5 only')
    expect(card).toContainElement(entry())
    // How each group is counted is in "How this is read" too, not in a title alone.
    const read = within(card).getByText('How this is read').closest('details')!
    expect(read).toHaveTextContent('Needs cleanup · Hosts the controller reports offline 1: every host of the runtime')
    expect(read).toHaveTextContent("Needs cleanup · Registered external work with no heartbeat for over 60 s 1: Round 5 only")
    expect(document.body).not.toHaveTextContent(HOST)
    // The other lines of the health notice stay on Work's top.
    expect(screen.getByRole('alert', { name: 'Agent runtime health' })).toHaveTextContent('exec:claude is unavailable')
    // Read from what Work and the shell already read: of the runtime, one plan view, this round's tree, GET /progress.
    await act(async () => { await new Promise(r => setTimeout(r, 50)) })
    expect(new Set(urls().filter(u => u.includes('/agentrt/')))).toEqual(new Set(['/api/v1/agentrt/plan-view?root=R5', '/api/v1/agentrt/plan?root=R5', '/api/v1/agentrt/progress']))
  })

  it('keeps the round an address names on the way to the list', async () => {
    page('#work?root=R4')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 1 external work without heartbeat in Round 4 ›'))
    expect(entry()).toHaveAttribute('href', '#work?section=activity&root=R4')
  })

  // PROD 2026-11-01: TD-457 and TD-459 are registered in R4 while Work opens on R5. Round 5's entry must not read as
  // "1 item" for all there is: it says 0 in Round 5, by name, and R4's are counted on R4.
  it('names the round of its external work: none in Round 5 is said so, not as a total; R4\'s are on R4', async () => {
    view = round => planView(round, { working: round === 4 })
    page('#work')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 0 external works without heartbeat in Round 5 ›'))
    expect(entry().textContent).not.toMatch(/\bitems?\b/)
    await go('#work?section=activity')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent("None in Round 5's plan view."))
    expect(list).toHaveTextContent("Each other round's registered work is on its own Live tab, not read here.")
    cleanup(); client.clear()
    page('#work?root=R4')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 1 external work without heartbeat in Round 4 ›'))
  })
})

describe('Needs cleanup on Work → Activity', () => {
  it('lists every item the entry counts: the offline host as it was said, and the registered work with no heartbeat', async () => {
    page('#work')
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 1 external work without heartbeat in Round 5 ›'))
    await go(entry().getAttribute('href')!)
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent('Needs cleanup · 1 offline host · 1 external work without heartbeat in Round 5'))
    expect(within(list).getByRole('list', { name: 'Offline hosts' })).toHaveTextContent(HOST)
    const silent = within(list).getByRole('list', { name: 'Registered external work with no heartbeat' })
    expect(within(silent).getAllByRole('listitem')).toHaveLength(1)
    expect(silent).toHaveTextContent('TD-457: fix the landing gate')
    expect(silent).toHaveTextContent('Unconfirmed')
    expect(silent).toHaveTextContent('Last heartbeat · 50h 0m ago')
    expect(list).toHaveTextContent('Hosts the controller reports offline · 1 · every host of the runtime')
    expect(list).toHaveTextContent('Registered external work with no heartbeat for over 60 seconds · 1 · Round 5 only')
    // Said once on the page: the health notice above leaves the host to this list.
    expect(screen.getAllByText(HOST)).toHaveLength(1)
    // The list comes first on Activity, before Now and Crew.
    const regions = screen.getAllByRole('region').map(r => r.getAttribute('aria-label'))
    expect(regions.indexOf('Needs cleanup')).toBeLessThan(regions.indexOf('Now'))
  })

  // Review of 16435ae7: the server lists a record on a task a runtime attempt runs only in external_work and that
  // task's history, not in working; the list read working alone, missed it and said "None".
  it('lists registered work silent on a task a runtime attempt runs, which Live shows under the attempt', async () => {
    view = round => planView(round, { working: false, underAttempt: true })
    page('#work')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent('Needs cleanup: 1 offline host · 1 external work without heartbeat in Round 5 ›'))
    await go('#work?section=activity')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(within(list).getByRole('list', { name: 'Registered external work with no heartbeat' })).toHaveTextContent('TD-457: fix the landing gate'))
    expect(list).not.toHaveTextContent("None in Round 5's plan view.")
  })

  it('says 0, not "all clean", when neither group holds any', async () => {
    view = round => planView(round, { working: false })
    progress = { ...progressBody, hosts: progressBody.hosts.map(h => ({ ...h, reported_offline: false })) }
    page('#work?section=activity')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent('Needs cleanup · 0 offline hosts · 0 external works without heartbeat in Round 5'))
    expect(list).toHaveTextContent('None reported offline.')
    expect(list).toHaveTextContent("None in Round 5's plan view.")
  })
})

describe('what cannot be read says Unknown, on the entry and the list alike', () => {
  it('a GET /progress that fails leaves the hosts Unknown with why, the rest counted as "at least"', async () => {
    failing = /\/agentrt\/progress$/
    page('#work?root=R5')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent(/^Needs cleanup: offline hosts Unknown: GET \/progress could not be read \(.+\) · 1 external work without heartbeat in Round 5 ›$/))
    await go('#work?section=activity&root=R5')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent('Hosts the controller reports offline · Unknown · every host of the runtime'))
    expect(within(list).getAllByRole('alert')[0]).toHaveTextContent(/^Unknown · GET \/progress could not be read/)
  })

  // Platform review of b48fec12: the shell's last answer two minutes old, its refresh hanging, said "0 offline hosts".
  it('a GET /progress two minutes old leaves the hosts Unknown with its age, on the entry and the list, with no number', async () => {
    const allOnline = { ...progressBody, hosts: progressBody.hosts.map(h => ({ ...h, reported_offline: false })), generated_at: prod.as_of }
    client.setQueryData(['agent-runtime-progress'], (await import('@/api/agentRuntime')).parseRuntimeProgress(allOnline), { updatedAt: NOW - 120_000 })
    hang = true
    page('#work')
    await summary()
    const age = `GET /progress is older than twice the shell's 5 s poll: last answered 2 min ago (${new Date(NOW - 120_000).toISOString()})`
    await waitFor(() => expect(entry()).toHaveTextContent(`Needs cleanup: offline hosts Unknown: ${age} · 1 external work without heartbeat in Round 5 ›`))
    expect(entry()).not.toHaveTextContent(/\d+\+? offline hosts?/)
    await go('#work?section=activity')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent('Hosts the controller reports offline · Unknown · every host of the runtime'))
    expect(within(list).getAllByRole('alert')[0]).toHaveTextContent(`Unknown · ${age}.`)
    expect(list).not.toHaveTextContent('None reported offline.')
    expect(list).not.toHaveTextContent(/\d+\+? offline hosts?/)
  })

  it('a plan view older than 60 s leaves the registered work Unknown, listed as last read and not counted', async () => {
    view = round => planView(round, { asOf: new Date(NOW - 120_000).toISOString() })
    page('#work')
    await summary()
    await waitFor(() => expect(entry()).toHaveTextContent(/^Needs cleanup: 1 offline host · external work without heartbeat in Round 5 Unknown: Round 5's plan view is older than 60 s; as of .+ ›$/))
    // The same old view behind every other count of the page (R5:5.5-4 audit): the sign-offs are Unknown, not "13 later",
    // and the tab under the row says its counts are that snapshot's.
    const asOf = new Date(NOW - 120_000).toISOString()
    const card = await summary()
    expect(within(card).getByRole('link', { name: /^Sign-offs:/ })).toHaveTextContent(`Sign-offs: 0+ ready · later Unknown · Round 5 only · Round 5's plan view is older than 60 s; as of ${asOf}`)
    expect(document.querySelector('[data-tab-stale]')).toHaveTextContent(`As last read · Round 5's plan view is older than 60 s; as of ${asOf}. What this tab counts is that snapshot, not now.`)
    await go('#work?section=activity')
    const list = await screen.findByRole('region', { name: 'Needs cleanup' })
    await waitFor(() => expect(list).toHaveTextContent('Registered external work with no heartbeat for over 60 seconds · Unknown · Round 5 only'))
    expect(list).toHaveTextContent('As last read:')
    expect(within(list).getByRole('list', { name: 'Registered external work with no heartbeat' })).toHaveTextContent('TD-457')
  })
})

describe('the lamp of the round tab', () => {
  it('is green for this round only, never "any round"; the Sign-offs tab, reading every round, may say any round', async () => {
    view = round => planView(round, { clean: true })
    page('#work')
    const card = await summary()
    const lamp = () => card.querySelector('[data-summary-tone]')!
    await waitFor(() => expect(lamp()).toHaveAttribute('data-summary-tone', 'ok'))
    expect(lamp().getAttribute('aria-label')).toBe('Nothing for you, as far as read: in Round 5 only, no step sign-off ready, no acceptance sign-off ready, no task waiting on you, no step stopped; other rounds are read on the Sign-offs tab')
    expect(lamp().getAttribute('aria-label')).not.toContain('any round')
    await go('#work?tab=signoffs')
    await waitFor(() => expect(urls()).toContain('/api/v1/agentrt/plan?root=R4'))
    await waitFor(() => expect(lamp().getAttribute('aria-label')).toContain('no step sign-off ready in any round'))
  })
})

// Local review of R5:5.5-4 ("old read said as now"): GET /plan has no timeout, so a refresh that hangs stays a success
// and its old "0 ready" lit the round green. The page judges each read's age on its own clock (readAge.ts).
describe('the sign-off count from a GET /plan that stopped answering', () => {
  const treeOf = (round: number) => ({ found: true, doc: { root: `R${round}`, nodes: planView(round).nodes.map((n: Record<string, unknown>) => ({ ...n, signoffs: [] })) } })
  const seedTree = (round: number, age: number) => client.setQueryData(['agent-runtime-plan', `R${round}`], treeOf(round), { updatedAt: NOW - age })
  const iso = (age: number) => new Date(NOW - age).toISOString()

  it('the round tab: plan view fresh, its GET /plan 31 s old: not green, no "0 ready", and How this is read says why', async () => {
    view = round => planView(round, { clean: true })
    seedTree(5, 31_000)
    hangTrees = true
    page('#work')
    const card = await summary()
    const why = `GET /plan?root=R5 last answered 31 s ago (${iso(31_000)}), older than twice its 15 s poll`
    const link = () => within(card).getByRole('link', { name: /^Sign-offs:/ })
    await waitFor(() => expect(link()).toHaveTextContent(`Sign-offs: 0+ ready · not started Unknown · 0 later · Round 5 only · ${why} · other rounds on the Sign-offs tab`))
    expect(link()).not.toHaveTextContent('0 ready')
    const lamp = card.querySelector('[data-summary-tone]')!
    expect(lamp).toHaveAttribute('data-summary-tone', 'unknown')
    expect(lamp.getAttribute('aria-label')).toContain(why)
    expect(within(screen.getByRole('navigation', { name: 'Work views' })).getByRole('link', { name: /^Sign-offs/ })).toHaveTextContent('Sign-offs · 0+ ready · not started Unknown · 0 later · Round 5 only')
    expect(within(card).getByText('How this is read').closest('details')).toHaveTextContent(`Sign-offs · Unknown: ${why}.`)
  })

  it('the Sign-offs tab, every round: the oldest round\'s GET /plan is named, nothing of it counted', async () => {
    view = round => planView(round, { clean: true })
    seedTree(4, 40_000)
    seedTree(5, 31_000)
    hangTrees = true
    page('#work?tab=signoffs')
    const signoffs = await screen.findByRole('region', { name: 'Sign-offs' })
    const why = `GET /plan?root=R4 last answered 40 s ago (${iso(40_000)}), older than twice its 15 s poll`
    await waitFor(() => expect(within(signoffs).getAllByRole('alert').map(a => a.textContent).join('\n')).toContain(`Sign-offs partly Unknown · ${why}`))
    await waitFor(() => expect(screen.getByRole('region', { name: 'Sign-offs' }).querySelector('[data-signoff-ready]')).toHaveTextContent('0+'))
    expect(within(screen.getByRole('navigation', { name: 'Work views' })).getByRole('link', { name: /^Sign-offs/ })).not.toHaveTextContent('0 ready')
    expect((await summary()).querySelector('[data-summary-tone]')).toHaveAttribute('data-summary-tone', 'unknown')
  })

  it('says no round only from a GET /progress that is now; an old one is as of then, and Needs cleanup gives no number', async () => {
    const empty = { ...progressBody, plan: [], hosts: progressBody.hosts.map(h => ({ ...h, reported_offline: false })), generated_at: prod.as_of }
    client.setQueryData(['agent-runtime-progress'], (await import('@/api/agentRuntime')).parseRuntimeProgress(empty), { updatedAt: NOW - 31_000 })
    hang = true
    page('#work')
    const card = await summary()
    await waitFor(() => expect(card).toHaveTextContent(`The runtime held no round as of ${iso(31_000)}; whether it holds one now is Unknown · GET /progress last answered 31 s ago (${iso(31_000)}).`))
    expect(card).not.toHaveTextContent('The runtime holds no round yet.')
    expect(entry()).not.toHaveTextContent(/\d+\+? (offline hosts?|external works?)/)
    expect(entry()).toHaveTextContent(`which round to read is Unknown: GET /progress last answered 31 s ago (${iso(31_000)}), when it listed no round`)
  })
})
