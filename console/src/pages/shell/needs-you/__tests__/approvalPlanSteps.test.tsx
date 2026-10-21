import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import approvals from '@/api/__fixtures__/agentrt-plan-approvals.testdb.json'
import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import { PLAN_ROOT, approvalNumbers, approvalsByStep, stepsOfApproval } from '@/api/agentRuntimePlan'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { ApprovalPlanSteps } from '@/pages/shell/needs-you/ApprovalPlanSteps'
import { PLAN_SECTION_HREF, planStepLine } from '@/pages/shell/progress/progressPart'

// A request says which step of the plan it is the approval for (W-31 2.5 slice 5 gap 4, v4 §20.5 scenario 10). The
// fixture is GET /plan?root=R2 as the platform answered it on a test database (agentrtapi TestPlanStepsNameTheirApprovals,
// written with CONSOLE_PLAN_APPROVALS_FIXTURE_WRITE=1): the round-2 ledger imported, then done rules set through
// POST /plan/proposals on 2.1-3 ({approval_executed: 16}), 2.4-1 (all: 17 beside a question) and 2.4-2 (any: 16, 18).

const plan = approvals.plan
const STEP_16 = 'Plan step 2.1-3 (yours): （你）批 PROD 迁移作业（O10、O11） — item 2.1 数据层：状态库'
const STEP_16_TOO = 'Plan step 2.4-2: （补强）★（你）批准 — item 2.4 缩进用不换行空格'

const source = vi.hoisted(() => ({ plan: 'approvals' as 'approvals' | 'absent' | 'fail' | 'not a tree' }))
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
  const url = String(input)
  if (!url.includes('/agentrt/plan?')) throw new Error(`Unexpected URL ${url}`)
  if (source.plan === 'absent') return json(imported.absent.body, imported.absent.status)
  if (source.plan === 'fail') return json({ error: 'unavailable', message: 'the runtime is not connected' }, 503)
  if (source.plan === 'not a tree') return json({ root: 'R2', nodes: null })
  return json(plan)
})
function show(node: ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}
beforeEach(() => {
  source.plan = 'approvals'
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear() })

describe('the approvals a done rule waits on', () => {
  it('reads every approval_executed number, an atom or nested in all / any, each once', () => {
    expect(approvalNumbers({ approval_executed: 16 })).toEqual([16])
    expect(approvalNumbers({ all: [{ approval_executed: 17 }, { question_answered: { question: 'Q1', option: 'a' } }] })).toEqual([17])
    expect(approvalNumbers({ any: [{ approval_executed: 16 }, { all: [{ approval_executed: 18 }, { approval_executed: 16 }] }] })).toEqual([16, 18])
    // Not an approval: another atom, a gate rule, a number that is not a positive integer, a string, a list at the top.
    for (const rule of [{ manual: 'by hand' }, { gates_passed: [{ test: 'T05' }], env: 'prod' }, { approval_executed: 0 },
      // Past the safe integers, as JSON.parse reads one off the wire (it rounds).
      { approval_executed: 1.5 }, { approval_executed: '16' }, JSON.parse('{"approval_executed":9007199254740993}'), [{ approval_executed: 16 }],
      null, undefined, {}]) {
      expect(approvalNumbers(rule)).toEqual([])
    }
    // Deeper than plan/rule.go reads (8) is passed over.
    let deep: unknown = { approval_executed: 9 }
    for (let i = 0; i < 9; i++) deep = { all: [deep] }
    expect(approvalNumbers(deep)).toEqual([])
  })

  it('finds the steps of the runtime\'s plan whose rule waits on a request', () => {
    expect(stepsOfApproval(plan, 16).map(s => s.id)).toEqual(['R2:2.1-3', 'R2:2.4-2'])
    expect(stepsOfApproval(plan, 16)[0]).toEqual({ id: 'R2:2.1-3', label: '2.1-3', title: '（你）批 PROD 迁移作业（O10、O11）', owner: true,
      item: '2.1', itemTitle: '数据层：状态库' })
    expect(stepsOfApproval(plan, 17).map(s => s.id)).toEqual(['R2:2.4-1'])
    expect(stepsOfApproval(plan, 18).map(s => s.id)).toEqual(['R2:2.4-2'])
    expect(stepsOfApproval(plan, 99)).toEqual([])
    expect(stepsOfApproval(imported.plan, 16)).toEqual([]) // the imported plan's rules are manual atoms only
    expect(Object.fromEntries(approvalsByStep(plan))).toEqual({ 'R2:2.1-3': [16], 'R2:2.4-1': [17], 'R2:2.4-2': [16, 18] })
    expect(planStepLine(stepsOfApproval(plan, 16)[0])).toBe(STEP_16)
  })
})

describe('a request and its plan step', () => {
  it('shows each step under the request in a Needs you row, as a link to the plan', async () => {
    show(<ApprovalRow item={buildPlatformApproval({ id: 'ap-16', number: 16 })} planSteps />)
    // The row reads the plan itself; a loaded machine (CI) can take past the 1 s default to answer.
    expect(await screen.findByRole('link', { name: STEP_16 }, { timeout: 5000 })).toHaveAttribute('href', PLAN_SECTION_HREF)
    expect(screen.getByRole('link', { name: STEP_16_TOO })).toHaveAttribute('href', PLAN_SECTION_HREF)
  })

  it('says nothing under a row until the plan is read, when it could not be read, or when not asked to', async () => {
    source.plan = 'fail'
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    show(<ApprovalRow item={buildPlatformApproval({ id: 'ap-16', number: 16 })} planSteps />, client)
    // The read has failed (not still loading) when the row is looked at (review 484 C1).
    await vi.waitFor(() => expect(client.getQueryState(['agent-runtime-plan', PLAN_ROOT])?.status).toBe('error'), { timeout: 5000 })
    expect(screen.queryByRole('link', { name: /^Plan step/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/The plan could not be read/)).not.toBeInTheDocument()
    cleanup()
    fetchMock.mockClear()
    source.plan = 'approvals'
    show(<ApprovalRow item={buildPlatformApproval({ id: 'ap-16', number: 16 })} />)
    expect(screen.queryByRole('link', { name: /^Plan step/ })).not.toBeInTheDocument()
    // The row reads its assessment stream; the plan it does not read.
    expect(fetchMock.mock.calls.map(c => String(c[0])).filter(url => url.includes('/agentrt/plan?'))).toEqual([])
  })

  it('says on the request\'s page which step waits on it, that none does, or that the plan could not be read', async () => {
    show(<ApprovalPlanSteps number={16} />)
    expect(await screen.findByRole('link', { name: STEP_16 })).toHaveAttribute('href', '#work?section=plan')
    expect(screen.getByRole('link', { name: STEP_16_TOO })).toBeInTheDocument()
    cleanup()
    show(<ApprovalPlanSteps number={99} />)
    expect(await screen.findByText('No step of the plan waits on this request.')).toBeInTheDocument()
    cleanup()
    source.plan = 'absent' // the runtime holds no round here: no step waits on it
    show(<ApprovalPlanSteps number={16} />)
    expect(await screen.findByText('No step of the plan waits on this request.')).toBeInTheDocument()
    cleanup()
    for (const answer of ['fail', 'not a tree'] as const) {
      // A failed read, and a 200 that is not the round's tree (review 476 C2), are said as unread, never as no step.
      cleanup()
      source.plan = answer
      show(<ApprovalPlanSteps number={16} />)
      expect(await screen.findByText('The plan could not be read: which step waits on this request is not known.')).toBeInTheDocument()
      expect(screen.queryByText('No step of the plan waits on this request.')).not.toBeInTheDocument()
    }
  })

  it('says nothing for a request without a number', () => {
    const { container } = show(<ApprovalPlanSteps number={undefined} />)
    expect(container).toBeEmptyDOMElement()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
