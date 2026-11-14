import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { questionHref } from '@/lib/shell/consoleRoutes'
import { NowClock } from '../../progress/progressClock'
import { PlanModuleDetail } from '../PlanModuleDetail'
import { moduleOf } from '../moduleDetailModel'
import { allRequests, AT, externalWork, openRequests, progress, Q_OPEN, Q_RULE, questions, r5, r5Raw, W3, type Raw } from './moduleDetailFixture'

/*
 * The module detail laid out as Design v3.6 ItemDetail (R5:5.5-3): its head card, the steps as a timeline, and the
 * acceptance, dependencies and live work beside them, on the R5 answer of planModuleDetail.test (what each part says
 * and where it is read: WORK_V36_3_CLAIMS). The detail's own claims are tested there.
 */

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

let client: QueryClient
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
/** The shell's Needs-you reads, as ConsolePage's useNeedsYou leaves them in the cache. */
function seedShell() {
  client.setQueryData(['questions', 'all'], questions())
  client.setQueryData(['approvals', 'open'], openRequests())
  client.setQueryData(['approvals', 'all'], allRequests())
  client.setQueryData(['agent-runtime-progress'], progress())
}
function wrap(children: ReactNode) {
  return <QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>{children}</NowClock></PlatformAuthContext.Provider></QueryClientProvider>
}
function detail(view = r5(), node = 'R5:5.1') {
  const opened = moduleOf(view, node)!
  return render(wrap(<PlanModuleDetail view={view} item={opened.item} step={opened.step} stale={false} />))
}
const region = (name: string) => screen.getByRole('region', { name })
const steps = () => [...region('Steps of 5.1').querySelectorAll<HTMLElement>('[data-step-row]')]
const step = (label: string) => steps().find(li => li.dataset.stepRow === label)!

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(AT))
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  // The detail fetches nothing: any request fails.
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 599 })))
})
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('module detail laid out as Design v3.6 ItemDetail (R5:5.5-3)', () => {
  const nodeOf = (raw: Raw, nid: string) => raw.nodes.find(n => n.plan_node_id === nid)!
  /** The R5 answer with 5.6 not in the adopted view document: its node has no view, and no item, edge or wait names it. */
  function without56(): PlanSnapshot {
    type Edge = { from: { node: string }; to: { node: string } }
    const raw = r5Raw() as Raw & { items: { plan_node_id: string }[]; edges: Edge[]; waiting_on: { edge: Edge }[] }
    const touches = (e: Edge) => e.from.node === 'R5:5.6' || e.to.node === 'R5:5.6'
    delete nodeOf(raw, 'R5:5.6').view
    Object.assign(raw, { items: raw.items.filter(i => i.plan_node_id !== 'R5:5.6'), edges: raw.edges.filter(e => !touches(e)), waiting_on: raw.waiting_on.filter(w => !touches(w.edge)) })
    return parsePlanSnapshot(raw, 'R5')
  }
  const order = (labels: string[]) => labels.map(name => region(name)).every((el, i, all) => i === 0 || (all[i - 1].compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0)

  it('opens on a head card: the module and its place on the mainline, steps to go, now, next, who is next responsible and the ETA', () => {
    seedShell()
    detail()
    const head = region('Summary of 5.1')
    expect(head.querySelector('[data-kicker]')).toHaveTextContent(/^5\.1 · Mainline 1 of 4$/)
    expect(within(head).getByTitle("The plan view's mainline position (mainline.current), not a verified milestone")).toHaveTextContent('Current')
    expect(within(head).getByRole('heading', { level: 3 })).toHaveTextContent('交房')
    for (const name of ['Steps to go', 'Now', 'Next in plan order', 'Next responsible', 'ETA']) expect(head).toContainElement(region(name))
    // Now names the step in progress and opens it; its work is on the timeline, not repeated here.
    expect(within(region('Now')).getByRole('link', { name: 'Step 5.1-3 in progress' })).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1-3')
    expect(region('Now')).not.toHaveTextContent('Module detail page')
    expect(region('Next responsible')).toHaveTextContent('crew_seat · coordinator (plan default)')
    expect(head).toContainElement(screen.getByLabelText('Read as of'))
  })

  it('writes "of N" only when the module\'s place on the mainline path is its mainline order, and no mainline for a support item', () => {
    seedShell()
    detail(r5(), 'R5:5.4')
    expect(region('Summary of 5.4').querySelector('[data-kicker]')).toHaveTextContent(/^5\.4 · Mainline 3 of 4$/)
    cleanup()
    const raw = r5Raw()
    Object.assign((nodeOf(raw, 'R5:5.4').view as Record<string, unknown>), { mainline_order: 7 })
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'), 'R5:5.4')
    expect(region('Summary of 5.4').querySelector('[data-kicker]')).toHaveTextContent(/^5\.4 · Mainline 7$/)
    cleanup()
    seedShell()
    detail(r5(), 'R5:5.6')
    expect(region('Summary of 5.6').querySelector('[data-kicker]')).toHaveTextContent(/^5\.6 · Support$/)
  })

  it('lays the steps out as a timeline: the step in progress open with its done rule and external work, the others one line each', () => {
    seedShell()
    detail()
    expect(steps().filter(li => li.dataset.stepOpen).map(li => li.dataset.stepRow)).toEqual(['5.1-3'])
    const open = step('5.1-3')
    expect(open).toHaveTextContent(`Done whenAll of these hold:Question ${Q_OPEN} is answered “a”Request #7 is approved and executedRequest #8 is approved and executed`)
    expect(open).toHaveTextContent('External work on this stepModule detail page · codex · Registered')
    for (const label of ['5.1-1', '5.1-4', '5.1-6']) {
      expect(step(label)).not.toHaveTextContent('Done when')
      expect(step(label)).not.toHaveTextContent('External work on this step')
    }
  })

  it('opens the step an address names too: its done rule in words and the question its rule waits on', () => {
    seedShell()
    detail(r5(), 'R5:5.1-5')
    expect(steps().filter(li => li.dataset.stepOpen).map(li => li.dataset.stepRow)).toEqual(['5.1-3', '5.1-5'])
    const named = step('5.1-5')
    expect(named).toHaveAttribute('aria-current', 'step')
    expect(document.activeElement).toBe(named)
    expect(named).toHaveTextContent(`Done whenQuestion ${Q_RULE} is answered “a”`)
    expect(within(named).getByRole('link', { name: `Question ${Q_RULE}` })).toHaveAttribute('href', questionHref(Q_RULE))
    cleanup()
    seedShell()
    detail(r5(), 'R5:5.1-4')
    expect(step('5.1-4')).toHaveTextContent('Done whenNo done rule yet. The runtime does not move this step on evidence; it changes only by a recorded event.')
  })

  it('says no step is in progress, and Unknown when a step has no state, in the head card', () => {
    const raw = r5Raw()
    nodeOf(raw, 'R5:5.1-3').state = 'planned'
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    expect(region('Summary of 5.1')).toContainElement(region('Now'))
    expect(region('Now')).toHaveTextContent('No step in progress.')
    expect(steps().filter(li => li.dataset.stepOpen)).toHaveLength(0)
    cleanup()
    nodeOf(raw, 'R5:5.1-6').state = null
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    expect(region('Now')).toHaveTextContent("No step in progress by the runtime's states; 1 step without a state (Unknown).")
    expect(step('5.1-6').querySelector('[data-step-state]')).toHaveTextContent('Unknown')
  })

  it('shows the acceptance criteria of the adopted view with what their bindings read; none bound is Unknown, not 0 alone', () => {
    seedShell()
    detail()
    const acceptance = region('Acceptance of 5.1')
    expect(acceptance).toHaveTextContent('Acceptance · 3')
    expect(acceptance.querySelector('[data-acceptance-count]')).toHaveTextContent(/^0 verified · 3 Unknown$/)
    expect(within(acceptance).getAllByText('Not bound to a trusted source · Unknown')).toHaveLength(3)
    expect(acceptance).toHaveTextContent('P0 全身份权限实证通过')
    // What a criterion requires, not what was found: folded until asked.
    const toggle = within(acceptance).getAllByRole('button', { name: /evidence required/ })[0]
    expect(toggle).toHaveTextContent('4 evidence required +')
    expect(acceptance).not.toHaveTextContent('P0 逐行五项权限结果')
    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(acceptance).toHaveTextContent('P0 逐行五项权限结果')
    cleanup()
    // PV2 readings by criterion id: one false is not verified, all true is verified, an unknown among them is Unknown.
    const raw = r5Raw()
    const reading = (rid: string, value: string, binding: string) => ({ id: rid, binding, atom: 'main_effective', value, source_value: value, freshness: 'Fresh', source_id: 's', source_version: 1,
      evidence: null, last_success: '', last_known_value: '', reason: `read ${value}` })
    nodeOf(raw, 'R5:5.1').acceptance_status = [reading('landing-p0', 'true', 'b1'), reading('landing-prod', 'false', 'b2'), reading('landing-star', 'true', 'b3'), reading('landing-star', 'unknown', 'b4')]
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    const read = region('Acceptance of 5.1')
    expect(read.querySelector('[data-acceptance-count]')).toHaveTextContent(/^1 verified · 1 not verified · 1 Unknown$/)
    expect(read.querySelector('[data-criterion="landing-p0"]')).toHaveTextContent('Verified · main_effective · Fresh · read true')
    expect(read.querySelector('[data-criterion="landing-prod"]')).toHaveAttribute('data-verdict', 'not verified')
    expect(read.querySelector('[data-criterion="landing-star"]')).toHaveAttribute('data-verdict', 'unknown')
    expect(read.querySelector('[data-criterion="landing-star"]')).toHaveTextContent('Unknown · main_effective · Fresh · read unknown')
  })

  it('says what the module waits on and what in the round\'s view waits on it, by milestone; a wait waiting_on no longer holds is said as no wait recorded', () => {
    seedShell()
    detail()
    expect(region('5.1 waits on')).toHaveTextContent('No dependency waiting')
    const waits = region('Waits on 5.1')
    const group = (heading: string) => waits.querySelector<HTMLElement>(`[data-dependents="${heading}"]`)!
    expect([...waits.querySelectorAll<HTMLElement>('[data-dependents]')].map(g => g.dataset.dependents)).toEqual(['On PROD', 'Accepted', 'Related'])
    expect(within(group('On PROD')).getAllByRole('link').map(a => a.textContent)).toEqual(['5.2', '5.3', '5.4', '5.5', '5.8'])
    expect(group('On PROD')).toHaveTextContent('On PRODMilestone Unknown')
    expect(within(group('On PROD')).getByRole('link', { name: '5.2' })).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.2')
    // The reason is the view document's own words (the fixture's, written in round 4).
    expect(group('On PROD')).toHaveTextContent('for implementation — 第二波实施在 4.1 上 PROD 后开始')
    expect(group('Related')).toHaveTextContent('Relateddoes not wait')
    expect(within(group('Related')).getAllByRole('link').map(a => a.textContent)).toEqual(['5.5', '5.6'])
    expect(waits).toHaveTextContent("Edges declared in R5's adopted plan view; other rounds' plans are not read here.")
    cleanup()
    // 5.10 waits on 5.1 accepted; once waiting_on no longer holds that edge, no wait is recorded (never "verified":
    // the plan view has no field that says so).
    const raw = r5Raw() as Raw & { waiting_on: { edge: { from: { node: string }; to: { node: string } } }[] }
    raw.waiting_on = raw.waiting_on.filter(w => !(w.edge.from.node === 'R5:5.10' && w.edge.to.node === 'R5:5.1'))
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    expect(region('Waits on 5.1').querySelector('[data-dependents="Accepted"]')).toHaveTextContent('AcceptedNo wait recorded')
    expect(region('Dependencies of 5.1')).not.toHaveTextContent(/verified/i)
    cleanup()
    seedShell()
    detail(r5(), 'R5:5.4')
    const own = region('5.4 waits on')
    expect(within(own).getByRole('link', { name: '5.1 on PROD' })).toHaveAttribute('href', '#work?section=plan&root=R5&node=R5%3A5.1')
    expect(own).toHaveTextContent('第二波实施门槛；不依赖 4.2 完成')
    expect(own).toHaveTextContent('Milestone verification Unknown')
    expect(region('Waits on 5.4').querySelector('[data-dependents="Accepted"]')).toHaveTextContent('5.10')
    cleanup()
    // Nothing in the view names 5.10 as what it waits on.
    seedShell()
    detail(r5(), 'R5:5.10')
    expect(region('Waits on 5.10')).toHaveTextContent("Nothing in R5's adopted plan view waits on 5.10.")
  })

  it('says Unknown, with the reason, for what the view says of a module it does not list', () => {
    seedShell()
    detail(without56(), 'R5:5.6')
    const why = 'this module is not in the adopted plan view'
    expect(region('Next responsible')).toHaveTextContent(`Unknown · ${why}`)
    expect(region('Acceptance of 5.6')).toHaveTextContent(`Acceptance Unknown · ${why}`)
    expect(region('Acceptance of 5.6')).not.toHaveTextContent('verified')
    expect(region('5.6 waits on')).toHaveTextContent(`Dependencies Unknown · ${why}`)
    expect(region('Waits on 5.6')).toHaveTextContent(`Unknown · ${why}`)
    expect(region('Steps of 5.6').querySelector('[data-estimate]')).toHaveTextContent(`Plan estimate Unknown · ${why}`)
  })

  it('gives the plan\'s own estimate under the steps, said not to be measured, and leaves the ETA Unknown', () => {
    seedShell()
    detail()
    const estimate = region('Steps of 5.1').querySelector('[data-estimate]')!
    expect(estimate).toHaveTextContent('Plan estimatework days Unknown · 1 review round · confidence low')
    expect(estimate).toHaveTextContent("the adopted plan view's own estimate (source: work), not measured")
    expect(region('ETA')).toHaveTextContent('Unknown')
  })

  it('lists the registered work on the module\'s steps, and never says nobody works when none is registered', () => {
    // 5.1-3 has its registered work (the fixture's); 5.2-1, another module's step, has work running.
    const raw = r5Raw()
    externalWork(raw, 'R5:5.2-1', { description: { ...(raw.external_work[0].description as Record<string, unknown>), task: 'Release lanes' } })
    seedShell()
    detail(parsePlanSnapshot(raw, 'R5'))
    const live = region('Live on 5.1')
    expect(live).toHaveTextContent('Live on 5.1 · 1 registered')
    expect(within(live).getAllByRole('listitem')).toHaveLength(1)
    expect(live).toHaveTextContent('codex')
    expect(live).toHaveTextContent('Module detail page')
    expect(live).not.toHaveTextContent('Release lanes')
    cleanup()
    const none = () => { const bare = r5Raw(); bare.external_work = bare.external_work.filter(w => w.external_work_id !== W3); bare.working = []; return parsePlanSnapshot(bare, 'R5') }
    seedShell()
    detail(none())
    expect(region('Live on 5.1')).toHaveTextContent('Live on 5.1 · 0 registered')
    expect(region('Live on 5.1')).toHaveTextContent('No registered work on 5.1 in this snapshot.')
    expect(region('Live on 5.1')).toHaveTextContent('Unregistered work is not seen (R5 coverage: 0 unregistered · Not reconciled).')
    cleanup()
    seedShell()
    const view = none()
    render(wrap(<PlanModuleDetail view={view} item={moduleOf(view, 'R5:5.1')!.item} step="" stale />))
    expect(region('Live on 5.1')).toHaveTextContent('Live on 5.1 · last read 0 registered')
    expect(region('Live on 5.1')).toHaveTextContent('Current activity is Unknown. Last read contained no registered work on 5.1.')
  })

  it('puts the head card first, then the steps, then what needs the Owner, acceptance, dependencies and live work, and the activity last', () => {
    seedShell()
    detail()
    expect(order(['Summary of 5.1', 'Steps of 5.1', 'Your actions found', 'Needs you now', 'Acceptance of 5.1', 'Dependencies of 5.1', 'Live on 5.1', 'Recent activity on 5.1'])).toBe(true)
  })
})
