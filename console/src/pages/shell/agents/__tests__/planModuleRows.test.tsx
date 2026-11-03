import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { PlanModuleRows } from '../PlanModuleRows'
import { PlanTree } from '../PlanTree'
import { WorkPlanView } from '../WorkPlanView'
import { moduleRows, toGoText, type ModuleRow } from '../planModuleModel'
import { stepRows } from '../stepStateModel'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

type Progress = { state: string; done: number; total: number; stopped: number; basis: string }
type RawNode = { plan_node_id: string; parent_plan_node_id: string | null; state: string | null; done_rule_json: Record<string, unknown>; mission_id: string | null; execution_progress: Progress }
type Raw = { nodes: RawNode[]; tasks: Record<string, unknown>[]; external_work: Record<string, unknown>[]; mainline: { path: { plan_node_id: string; execution_state?: string }[]; current: { execution_state?: string } | null } }

const TASK = 't_01ABCDEFGHJKMNPQRSTVWXYZ01'
const QUESTION = 'q_0123456789abcdef'

/**
 * The PROD R4 response (2026-10-27, every step planned) moved on as the controller would move it: 4.1 has two steps
 * done, one active, one dropped, one stopped and one with no state; 4.10's only step is done. Each item's and the
 * round's execution_progress is recounted the way store/plan CountSteps counts, and 4.1's steps are listed in reverse
 * so that only their ordinals give the plan order.
 */
function advanced(): PlanSnapshot {
  const input = structuredClone(prod) as unknown as Raw
  const node = (id: string) => input.nodes.find(n => n.plan_node_id === id)!
  const set = (id: string, state: string | null, rule: Record<string, unknown> = {}) => { node(id).state = state; node(id).done_rule_json = rule }
  set('R4:4.1-1', 'done', { task_accepted: TASK })
  set('R4:4.1-2', 'done')
  node('R4:4.1-2').mission_id = 'M-8'
  set('R4:4.1-3', 'active', { all: [{ approval_executed: 42 }, { question_answered: { question: QUESTION, option: 'yes' } }, { any: [{ owner_signoff: 'land-4.1' }, { step_landed: {} }] }] })
  set('R4:4.1-4', 'dropped')
  set('R4:4.1-5', 'stopped', { question_answered: { question: 'Q1', option: 'a' } })
  set('R4:4.1-6', null)
  set('R4:4.10-1', 'done', { step_deployed: { env: 'prod' } })
  node('R4:4.1').execution_progress = { ...node('R4:4.1').execution_progress, state: 'active', done: 2, total: 5, stopped: 1 }
  node('R4:4.10').execution_progress = { ...node('R4:4.10').execution_progress, state: 'done', done: 1, total: 1, stopped: 0 }
  node('R4').execution_progress = { ...node('R4').execution_progress, state: 'active', done: 3, total: 54, stopped: 1 }
  input.mainline.path[0].execution_state = 'active'
  input.mainline.current!.execution_state = 'active'
  const steps41 = input.nodes.filter(n => n.parent_plan_node_id === 'R4:4.1')
  input.nodes = [...input.nodes.filter(n => n.parent_plan_node_id !== 'R4:4.1'), ...steps41.reverse()]
  input.tasks = [{ task_id: TASK, plan_node_id: 'R4:4.1-1', mission_id: 'M-7', title: 'Cut the landing rules', state: 'accepted', delivery: 'Delivered on STG',
    landing: { stream: 'landing', operation: 'op_17', phase: 'landed', outcome: 'landed', reason: '', attempt: 'a_1', result: 'ok', main_commit: '9f4c873e1234567890abcdef1234567890abcdef', run: 'ci-1', observed_at: '2026-10-27T02:40:00Z', basis: 'landing stream' } }]
  input.external_work = [...input.external_work, { ...input.external_work[0], external_work_id: 'ew_step', plan_node_id: 'R4:4.1-3',
    description: { ...(input.external_work[0].description as Record<string, unknown>), task: 'Rehearse case 3 on STG' } }]
  return parsePlanSnapshot(input, 'R4')
}
const row = (rows: ModuleRow[], label: string) => rows.find(r => r.label === label)!
const progress = (view: PlanSnapshot, id: string) => view.nodes.find(n => n.plan_node_id === id)!.execution_progress

describe('module rows model', () => {
  it('reads the PROD R4 response as it is: every step planned, 0 done, every step to go', () => {
    const view = parsePlanSnapshot(prod, 'R4')
    const rows = moduleRows(view)
    expect(rows.map(r => r.label)).toEqual(['4.1', '4.2', '4.4', '4.10', '4.3', '4.5', '4.6', '4.7', '4.8', '4.9'])
    for (const r of rows) {
      expect(r.done).toBe(0)
      expect(r.total).toBe(progress(view, r.id).total)
      expect(r.toGo).toBe(r.total)
      expect(r.cells.every(c => c.state === 'planned' && c.rule.length === 0)).toBe(true)
    }
    expect(rows.reduce((sum, r) => sum + r.toGo, 0)).toBe(55)
    expect(row(rows, '4.1').cells.map(c => c.label)).toEqual(['4.1-1', '4.1-2', '4.1-3', '4.1-4', '4.1-5', '4.1-6'])
    expect(toGoText(row(rows, '4.1'))).toBe('6 steps to go')
    expect(toGoText(row(rows, '4.10'))).toBe('1 step to go')
    expect(rows.filter(r => r.current).map(r => r.label)).toEqual(['4.1'])
    expect(row(rows, '4.1').lane).toBe('Mainline 1')
    expect(row(rows, '4.9').lane).toBe('Branch')
    // No task, request or question is bound to an R4 step yet: the only record each step opens is its place in the plan.
    expect(row(rows, '4.1').cells[0].links).toEqual([{ kind: 'node', href: '#work?section=plan&root=R4&node=R4%3A4.1-1', text: 'Open in module 4.1' }])
  })

  it('says what each item waits on from the blocking edges not verified yet', () => {
    const rows = moduleRows(parsePlanSnapshot(prod, 'R4'))
    expect(row(rows, '4.1').waits).toEqual([])
    expect(row(rows, '4.2').waits.map(w => w.label)).toEqual(['4.1 on PROD'])
    expect(row(rows, '4.7').waits.map(w => w.label)).toEqual(['4.2 · second_repo', '4.5 · pv.accepted'])
    expect(row(rows, '4.10').waits.map(w => w.label)).toEqual(['4.1 accepted', '4.2 accepted', '4.4 accepted', '4.6 accepted', '4.7 accepted'])
    expect(row(rows, '4.2').waits[0]).toMatchObject({ href: '#work?section=plan&root=R4&node=R4%3A4.1', status: 'Unknown' })
  })

  it('counts done, active, stopped and unknown steps, leaves dropped ones out, and agrees with execution_progress', () => {
    const view = advanced()
    // Judged at the snapshot's time: 4.1-3 is active with no task and external work submitted, its verification Unknown:
    // nothing running, queued or held on it, the submitted work a note (R5:5.5-10: most external work is never verified).
    const rows = stepRows(view, { failed: null, now: Date.parse(prod.as_of) })
    const r41 = row(rows, '4.1')
    expect(r41.cells.map(c => [c.label, c.state])).toEqual([['4.1-1', 'done'], ['4.1-2', 'done'], ['4.1-3', 'idle'], ['4.1-5', 'stopped'], ['4.1-6', 'unknown']])
    expect(r41).toMatchObject({ total: 5, done: 2, active: 1, idle: 1, running: 0, queued: 0, held: 0, stopped: 1, unknown: 1, dropped: 1, toGo: 3 })
    // Not judged (no clock): the active step is Unknown now, never in progress.
    expect(row(moduleRows(view), '4.1').cells[2]).toMatchObject({ label: '4.1-3', step: 'active', state: 'active-unknown' })
    expect(toGoText(r41)).toBe('3 steps to go')
    expect(toGoText(row(rows, '4.10'))).toBe('All steps done')
    for (const r of rows) {
      expect(r.done).toBe(progress(view, r.id).done)
      expect(r.total).toBe(progress(view, r.id).total)
      expect(r.stopped).toBe(progress(view, r.id).stopped)
    }
    expect(toGoText({ total: 0, toGo: 0 })).toBe('No steps')
  })

  it('links each step to the records it has, and only those', () => {
    const cells = row(moduleRows(advanced()), '4.1').cells
    const links = (label: string) => cells.find(c => c.label === label)!.links.map(l => [l.kind, l.href])
    const node = (id: string) => ['node', `#work?section=plan&root=R4&node=${encodeURIComponent(id)}`]
    expect(links('4.1-1')).toEqual([['task', `#work?goal=M-7&task=${TASK}`], node('R4:4.1-1')])
    expect(cells[0].links[0]).toMatchObject({ text: 'Task · Cut the landing rules', note: 'accepted · Delivered on STG · Landing op_17 · landed · landed · main 9f4c873' })
    expect(links('4.1-2')).toEqual([['goal', '#work?goal=M-8'], node('R4:4.1-2')])
    expect(links('4.1-3')).toEqual([['approval', '#approvals?id=42'], ['question', `#questions?id=${QUESTION}`], node('R4:4.1-3')])
    // Q1 is not a server question id: the rule names it, the page cannot open it.
    expect(links('4.1-5')).toEqual([node('R4:4.1-5')])
    expect(cells.find(c => c.label === '4.1-3')!.external).toEqual([expect.stringContaining('Rehearse case 3 on STG')])
  })

  it('writes the done rule in words', () => {
    const cells = row(moduleRows(advanced()), '4.1').cells
    expect(cells[0].rule).toEqual([{ text: 'Task Cut the landing rules is accepted' }])
    expect(cells.find(c => c.label === '4.1-3')!.rule).toEqual([{ text: 'All of these hold:', children: [
      { text: 'Request #42 is approved and executed' },
      { text: `Question ${QUESTION} is answered “yes”` },
      { text: 'Any one of these holds:', children: [{ text: 'You sign off “land-4.1”' }, { text: 'Every accepted task of this step has landed on main, or you declined it and the same change landed from another task of the step' }] },
    ] }])
    expect(row(moduleRows(advanced()), '4.10').cells[0].rule).toEqual([{ text: 'Every landed commit of this step is deployed on PROD' }])
  })
})

describe('module rows on the page', () => {
  it('draws one cell per step with a spoken state, one tab stop per bar and the steps to go', () => {
    render(<PlanModuleRows view={advanced()} selected="" />)
    const card = screen.getByRole('region', { name: 'Modules' })
    expect(card).toHaveTextContent('10 modules · 3 / 54 steps done · 51 steps to go')
    const items = within(screen.getByRole('list', { name: 'Modules in mainline order' })).getAllByRole('listitem').filter(li => li.dataset.module)
    expect(items.map(li => li.dataset.module)).toEqual(['4.1', '4.2', '4.4', '4.10', '4.3', '4.5', '4.6', '4.7', '4.8', '4.9'])
    const bar = screen.getByRole('toolbar', { name: 'Steps of 4.1, 3 steps to go' })
    const cells = within(bar).getAllByRole('button')
    expect(cells.map(b => b.getAttribute('aria-label'))).toEqual(['Step 4.1-1: done', 'Step 4.1-2: done', 'Step 4.1-3: active, nothing running', 'Step 4.1-5: stopped', 'Step 4.1-6: unknown'])
    expect(cells.filter(b => b.tabIndex === 0)).toHaveLength(1)
    expect(items[0]).toHaveTextContent('2 / 5 done · 1 idle · 1 stopped · 1 unknown · 1 dropped')
    expect(items[0]).toHaveAttribute('aria-current', 'step')
    expect(items[3]).toHaveTextContent('All steps done')
    expect(within(items[1]).getByRole('link', { name: '4.1 on PROD' })).toHaveAttribute('href', '#work?section=plan&root=R4&node=R4%3A4.1')
    // waiting_on holds only the plan's dependency edges: none is said as no dependency waits, not as nothing blocking.
    expect(items[0]).toHaveTextContent('No dependency waiting')
  })

  it('opens a step from its cell with its state, rule and links, and closes it again', () => {
    render(<PlanModuleRows view={advanced()} selected="" />)
    const cell = screen.getByRole('button', { name: 'Step 4.1-3: active, nothing running' })
    expect(cell).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(cell)
    expect(cell).toHaveAttribute('aria-expanded', 'true')
    const detail = screen.getByRole('region', { name: 'Step 4.1-3' })
    expect(cell).toHaveAttribute('aria-controls', detail.id)
    expect(detail.querySelector('[data-step-now="idle"]')).toHaveTextContent(/^Active · nothing running, read now:/)
    expect(detail.querySelector('[data-step-note="unverified"]')).toHaveTextContent(/^1 external work submitted, not read Verified \(verification Unknown\): “Rehearse case 3 on STG”$/)
    expect(detail).not.toHaveTextContent(/In progress|Active · held/)
    expect(detail).toHaveTextContent('Request #42 is approved and executed')
    expect(detail).toHaveTextContent('Rehearse case 3 on STG')
    expect(within(detail).getByRole('link', { name: 'Request #42' })).toHaveAttribute('href', '#approvals?id=42')
    expect(within(detail).getByRole('link', { name: `Question ${QUESTION}` })).toHaveAttribute('href', `#questions?id=${QUESTION}`)
    expect(within(detail).getByRole('link', { name: 'Open in module 4.1' })).toHaveAttribute('href', '#work?section=plan&root=R4&node=R4%3A4.1-3')
    fireEvent.keyDown(within(detail).getByRole('link', { name: 'Request #42' }), { key: 'Escape' })
    expect(screen.queryByRole('region', { name: 'Step 4.1-3' })).toBeNull()
    expect(document.activeElement).toBe(cell)
    fireEvent.click(cell)
    fireEvent.click(within(screen.getByRole('region', { name: 'Step 4.1-3' })).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('region', { name: 'Step 4.1-3' })).toBeNull()
  })

  it('says Unknown and no rule plainly for a step the runtime holds no state for', () => {
    render(<PlanModuleRows view={advanced()} selected="" />)
    fireEvent.click(screen.getByRole('button', { name: 'Step 4.1-6: unknown' }))
    const detail = screen.getByRole('region', { name: 'Step 4.1-6' })
    expect(detail).toHaveTextContent('Unknown · the runtime holds no state for this step.')
    expect(detail).toHaveTextContent('No done rule yet.')
    expect(within(detail).getAllByRole('link').map(a => a.textContent)).toEqual(['Open in module 4.1'])
  })

  it('moves along a bar with the arrow keys, Home and End', () => {
    render(<PlanModuleRows view={advanced()} selected="" />)
    const cells = within(screen.getByRole('toolbar', { name: /^Steps of 4\.1,/ })).getAllByRole('button')
    cells[0].focus()
    fireEvent.keyDown(cells[0], { key: 'ArrowRight' })
    expect(document.activeElement).toBe(cells[1])
    expect(cells[1].tabIndex).toBe(0)
    expect(cells[0].tabIndex).toBe(-1)
    fireEvent.keyDown(cells[1], { key: 'End' })
    expect(document.activeElement).toBe(cells[4])
    fireEvent.keyDown(cells[4], { key: 'ArrowRight' })
    expect(document.activeElement).toBe(cells[4])
    fireEvent.keyDown(cells[4], { key: 'Home' })
    expect(document.activeElement).toBe(cells[0])
  })

  it('opens the step the address names', () => {
    render(<PlanModuleRows view={advanced()} selected="R4:4.1-5" />)
    expect(screen.getByRole('region', { name: 'Step 4.1-5' })).toHaveTextContent('Stopped')
    expect(screen.getByRole('button', { name: 'Step 4.1-5: stopped' })).toHaveAttribute('tabindex', '0')
  })

  it('writes Unknown in the whole plan for a step with no state', () => {
    render(<PlanTree view={advanced()} selected="R4:4.1-6" />)
    expect(document.getElementById('plan-node-R4:4.1-6')).toHaveTextContent('· Unknown')
  })
})

describe('Work → Plan', () => {
  it('shows the module rows above the whole plan for the PROD R4 response', async () => {
    Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse(prod.as_of))
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('/agentrt/plan-view')
      ? new Response(JSON.stringify(prod), { status: 200 }) : new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    window.location.hash = '#work'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
    // The shell's GET /progress, with R4 the newest round: Work shows the newest round the runtime lists.
    client.setQueryData(['agent-runtime-progress'], parseRuntimeProgress({ plan: [{ plan_node_id: 'R4', kind: 'round', title: '第 4 轮', items: [] }] }))
    render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={{ token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }}><WorkPlanView /></PlatformAuthContext.Provider></QueryClientProvider>)
    const modules = await screen.findByRole('region', { name: 'Modules' })
    expect(modules).toHaveTextContent('10 modules · 0 / 55 steps done · 55 steps to go')
    expect(screen.getByRole('toolbar', { name: 'Steps of 4.1, 6 steps to go' })).toBeInTheDocument()
    // The whole plan is folded below the rows (R5:5.5-2), and drawn once opened.
    const fold = screen.getByText(/^Whole plan/, { selector: 'summary' })
    expect(modules.compareDocumentPosition(fold) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(fold)
    const tree = await screen.findByRole('region', { name: 'Whole plan' })
    expect(modules.compareDocumentPosition(tree) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
    client.clear()
  })
})
