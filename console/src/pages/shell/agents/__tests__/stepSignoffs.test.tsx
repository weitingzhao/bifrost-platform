import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'
import signoffs from '@/api/__fixtures__/agentrt-plan-signoffs.testdb.json'
import { PLAN_ROOT, runtimeRound, signoffRefs, signoffsByStep, unsignedRefs } from '@/api/agentRuntimePlan'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { fromPlanWithRequest } from '@/api/agentRuntimePlanView'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { CAPABILITIES_QUERY_KEY } from '@/hooks/platformAuthContext'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { SIGN_IN_ABSENT, SIGN_IN_OPERATOR } from '@/pages/decisionSignIn'
import { streamPage } from '@/test/agentrtPlanStream'
import { AgentsPlan } from '../AgentsPlan'
import { StepSignoffControls } from '../StepSignoffs'
import { SIGNOFF_PRESS_AGAIN, signedLine } from '../stepSignoffText'

// The Owner signs a step off (the Owner 2026-10-21 「加一个只给我用的「签收这一步」路由和 Console 按钮」). The fixture is
// GET /plan?root=R2 as the platform answered it on a test database (agentrtapi TestOwnerSignsAStepOff, written with
// CONSOLE_PLAN_SIGNOFFS_FIXTURE_WRITE=1): the round-2 ledger imported, then done rules set through POST /plan/proposals:
// 2.4-1 waits on owner_signoff exam-2.4-1 and is signed (not yet moved), 2.1-3 on a or b (active), 2.3-2 on race
// (planned), 2.1-4 on d (dropped), 2.3-1 on late (done at import); `signed` is POST /plan/R2:2.4-1/signoff's answer.

const plan = signoffs.plan
const TOKEN = 'fixture-admin-token'
const ADMIN: AuthCapabilities = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
const OPERATOR: AuthCapabilities = { authenticated: true, role: 'operator', can_operate: true, can_admin: false }

const auth = vi.hoisted(() => ({ caps: null as AuthCapabilities | null }))
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => auth.caps ?? { authenticated: false }) }
})

function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
type Sent = { url: string; method: string; authorization: string | null; body: unknown }
const sent: Sent[] = []
let answer: () => Response = () => json({})
const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = String(input)
  if (url.includes('/signoff')) {
    sent.push({ url, method: init?.method ?? 'GET', authorization: new Headers(init?.headers).get('Authorization'), body: JSON.parse(String(init?.body)) })
    return answer()
  }
  // The plan pane's reads: this plan, and the imported round's stream, request and counts (the same ledger).
  if (url.includes('/agentrt/plan?')) return json(plan)
  if (url.includes('/agentrt/events?stream=plan')) return json(streamPage(imported.events, url))
  if (url.includes('/agentrt/artifacts/')) return new Response(JSON.stringify(imported.request))
  if (url.includes('/agentrt/progress')) return json(imported.progress)
  throw new Error(`Unexpected URL ${url}`)
})

function installStorage(token: string): void {
  const mem = new Map<string, string>(token ? [[PLATFORM_TOKEN_KEY, token]] : [])
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    get length() { return mem.size },
    clear: () => mem.clear(),
    getItem: (key: string) => mem.get(key) ?? null,
    key: (index: number) => [...mem.keys()][index] ?? null,
    removeItem: (key: string) => { mem.delete(key) },
    setItem: (key: string, value: string) => { mem.set(key, String(value)) },
  } })
}
function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { client, ...render(<QueryClientProvider client={client}><PlatformAuthProvider>{node}</PlatformAuthProvider></QueryClientProvider>) }
}
const stepOf = (node: string) => {
  const s = signoffsByStep(plan).get(node)
  if (s == null) throw new Error(`no sign-off on ${node}`)
  return s
}
/** The answer the platform gave a sign-off, for another step and ref. */
const answerFor = (node: string, ref: string, already = false) => json({ ...signoffs.signed, plan_node_id: node, ref, already_signed: already })

beforeEach(() => {
  auth.caps = ADMIN
  installStorage(TOKEN)
  answer = () => answerFor('R2:2.3-2', 'race')
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); fetchMock.mockClear(); sent.length = 0 })

describe('the sign-offs a done rule waits on', () => {
  it('reads every owner_signoff ref, an atom or nested in all / any, each once', () => {
    expect(signoffRefs({ owner_signoff: 'exam-2.4-1' })).toEqual(['exam-2.4-1'])
    expect(signoffRefs({ any: [{ owner_signoff: 'a' }, { all: [{ owner_signoff: 'b' }, { approval_executed: 16 }, { owner_signoff: 'a' }] }] })).toEqual(['a', 'b'])
    for (const rule of [{ owner_signoff: '' }, { owner_signoff: 7 }, { manual: 'by hand' }, { approval_executed: 16 }, [{ owner_signoff: 'a' }], null, {}]) {
      expect(signoffRefs(rule)).toEqual([])
    }
    let deep: unknown = { owner_signoff: 'deep' }
    for (let i = 0; i < 9; i++) deep = { all: [deep] }
    expect(signoffRefs(deep)).toEqual([]) // deeper than plan/rule.go reads (8)
  })

  it('finds each step\'s refs and the sign-offs GET /plan gives it, and which refs are still open', () => {
    const by = signoffsByStep(plan)
    expect([...by.keys()]).toEqual(['R2:2.1-3', 'R2:2.1-4', 'R2:2.3-1', 'R2:2.3-2', 'R2:2.4-1'])
    expect(by.get('R2:2.4-1')).toEqual({ node: 'R2:2.4-1', refs: ['exam-2.4-1'],
      signed: [{ ref: 'exam-2.4-1', by: 'owner', at: plan.nodes.find(n => n.plan_node_id === 'R2:2.4-1')?.signoffs?.[0].at }] })
    expect(unsignedRefs(stepOf('R2:2.4-1'))).toEqual([])
    expect(unsignedRefs(stepOf('R2:2.1-3'))).toEqual(['a', 'b'])
    // The time is said in UTC whatever zone the database answered in.
    expect(signedLine(stepOf('R2:2.4-1').signed[0])).toBe(`Signed off exam-2.4-1 by the Owner, ${signoffs.signed.at.slice(0, 16).replace('T', ' ')}Z`)
    const round = runtimeRound(fromPlanWithRequest(plan, PLAN_ROOT, imported.request as never), parseRuntimeProgress(imported.progress).plan, PLAN_ROOT,
      null, new Map(), by)
    const step = round.items.flatMap(i => i.steps).find(s => s.id === '2.4-1')
    expect(step?.signoffs?.refs).toEqual(['exam-2.4-1'])
    expect(round.items.flatMap(i => i.steps).find(s => s.id === '2.4-2')?.signoffs).toBeUndefined()
  })
})

describe('Sign off on the plan page', () => {
  it('is offered under each step still waiting on a sign-off, and only there; a signed one says who and when', async () => {
    const { container } = show(<AgentsPlan />)
    expect(await screen.findByText('Round 2 · from the agent runtime')).toBeInTheDocument()
    const under = (node: string) => container.querySelector(`[data-step-signoffs="${node}"]`) as HTMLElement | null
    // a or b: one button for each, named.
    expect(within(under('R2:2.1-3') as HTMLElement).getAllByRole('button').map(b => b.textContent)).toEqual(['Sign off a', 'Sign off b'])
    expect(within(under('R2:2.3-2') as HTMLElement).getByRole('button', { name: 'Sign off' })).toBeEnabled()
    // Signed: the line, no button.
    expect(within(under('R2:2.4-1') as HTMLElement).queryByRole('button')).toBeNull()
    expect(within(under('R2:2.4-1') as HTMLElement).getByText(/^Signed off exam-2\.4-1 by the Owner, /)).toBeInTheDocument()
    // Done (or dropped) without it: nothing to press; a step with no owner_signoff has no sign-off part at all.
    for (const node of ['R2:2.3-1', 'R2:2.1-4']) {
      const part = under(node)
      if (part != null) expect(within(part).queryByRole('button')).toBeNull()
    }
    expect(under('R2:2.4-2')).toBeNull()
    // Under the steps of the plan: a, b and race; the block at the top lists the same three steps again (signoffUsable).
    expect([...container.querySelectorAll('[data-step-signoffs] button')].length).toBe(3)
    expect([...(container.querySelector('#signoff')?.querySelectorAll('[data-signoff-wait]') ?? [])].map(r => r.getAttribute('data-signoff-wait')))
      .toEqual(['R2:2.1-3', 'R2:2.3-2'])
    expect(sent).toEqual([])
  })

  it('asks once, then sends the step and its ref with the admin sign-in and says what was recorded', async () => {
    const { client } = show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
    client.setQueryData(['agent-runtime-plan', PLAN_ROOT], { found: true, doc: plan })
    const press = await screen.findByRole('button', { name: 'Sign off' })
    await vi.waitFor(() => expect(press).toBeEnabled())
    expect(press).toHaveStyle({ minHeight: '44px' })
    fireEvent.click(press)
    expect(sent).toEqual([])
    expect(screen.getByText(/^Sign off step 2\.3-2 \(race\) as the Owner\?/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(sent).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Sign off' }))
    const confirm = screen.getByRole('button', { name: 'Confirm sign-off' })
    expect(confirm).toHaveStyle({ minHeight: '44px' })
    fireEvent.click(confirm)
    expect(await screen.findByRole('status')).toHaveTextContent(`Signed off race by the Owner, ${signoffs.signed.at.slice(0, 16).replace('T', ' ')}Z`)
    expect(sent).toEqual([{ url: '/api/v1/agentrt/plan/R2:2.3-2/signoff', method: 'POST', authorization: `Bearer ${TOKEN}`, body: { ref: 'race' } }])
    // The plan is read again, so the step shows the platform's own record.
    expect(client.getQueryState(['agent-runtime-plan', PLAN_ROOT])?.isInvalidated).toBe(true)
    expect(screen.queryByRole('button', { name: 'Sign off' })).toBeNull()
  })

  it('says a press that found the sign-off already recorded answered the first one', async () => {
    answer = () => answerFor('R2:2.3-2', 'race', true)
    show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
    const press = await screen.findByRole('button', { name: 'Sign off' })
    await vi.waitFor(() => expect(press).toBeEnabled())
    fireEvent.click(press)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/\(recorded before this press\)$/)
  })

  it('names the ref it signs when the rule waits on more than one', async () => {
    show(<StepSignoffControls label="2.1-3" state="active" signoffs={stepOf('R2:2.1-3')} />)
    answer = () => answerFor('R2:2.1-3', 'b')
    const b = await screen.findByRole('button', { name: 'Sign off b' })
    await vi.waitFor(() => expect(b).toBeEnabled())
    fireEvent.click(b)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/^Signed off b by the Owner/)
    expect(sent.map(s => [s.url, s.body])).toEqual([['/api/v1/agentrt/plan/R2:2.1-3/signoff', { ref: 'b' }]])
    expect(screen.getByRole('button', { name: 'Sign off a' })).toBeEnabled()
  })

  it('cannot be pressed unless the Owner is signed in', async () => {
    auth.caps = OPERATOR
    show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
    expect(await screen.findByText(SIGN_IN_OPERATOR)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Sign off' }))
    expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Confirm sign-off' })).toBeNull()
    cleanup()
    installStorage('')
    auth.caps = null
    show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
    expect(await screen.findByText(SIGN_IN_ABSENT)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
    expect(sent).toEqual([])
  })

  it('says why a press was refused, in the platform\'s words, and that pressing again is safe', async () => {
    const refusals: [Response, string | RegExp][] = [
      // As the platform answers them (agentrtapi fail and RequireAny).
      [json({ error: 'state', message: 'store: not allowed in the current state: R2:2.3-2 is dropped; a sign-off would move nothing' }, 409),
        'Sign off: store: not allowed in the current state: R2:2.3-2 is dropped; a sign-off would move nothing'],
      [json({ error: 'invalid', message: 'store: invalid request: the done rule of R2:2.1-3 waits on no sign-off "c"; it waits on a, b' }, 422),
        'Sign off: store: invalid request: the done rule of R2:2.1-3 waits on no sign-off "c"; it waits on a, b'],
      [json({ error: 'admin token required' }, 401), 'Sign off: admin token required'],
      [new Response('<html>proxy</html>', { status: 502 }), 'Sign off: HTTP 502'],
      // A reason that carries the sign-in is hidden whole.
      [json({ error: 'internal', message: `bad bearer ${TOKEN}` }, 500), /^The server returned an error \(HTTP 500\)\. The details are hidden/],
    ]
    for (const [response, said] of refusals) {
      answer = () => response
      show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
      const press = await screen.findByRole('button', { name: 'Sign off' })
      await vi.waitFor(() => expect(press).toBeEnabled())
      fireEvent.click(press)
      fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
      const alert = await screen.findByRole('alert')
      expect(alert).toHaveTextContent(typeof said === 'string' ? `${said} ${SIGNOFF_PRESS_AGAIN}` : said)
      expect(alert).toHaveTextContent(SIGNOFF_PRESS_AGAIN)
      expect(alert).not.toHaveTextContent(TOKEN)
      expect(screen.getByRole('button', { name: 'Sign off' })).toBeEnabled()
      expect(screen.queryByRole('status')).toBeNull()
      cleanup()
    }
  })

  it('cannot be confirmed once the sign-in is no longer the Owner\'s', async () => {
    const { client } = show(<StepSignoffControls label="2.3-2" state="planned" signoffs={stepOf('R2:2.3-2')} />)
    const press = await screen.findByRole('button', { name: 'Sign off' })
    await vi.waitFor(() => expect(press).toBeEnabled())
    fireEvent.click(press)
    expect(screen.getByRole('button', { name: 'Confirm sign-off' })).toBeEnabled()
    // The role is read again and is no longer admin (signed in as another token meanwhile).
    act(() => { client.setQueryData([...CAPABILITIES_QUERY_KEY, TOKEN], OPERATOR) })
    expect(await screen.findByText(SIGN_IN_OPERATOR)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
    expect(screen.getByRole('button', { name: 'Confirm sign-off' })).toBeDisabled()
    expect(sent).toEqual([])
  })

  it('offers nothing on a step that can no longer move', () => {
    for (const state of ['done', 'stopped', 'dropped']) {
      const { container } = show(<StepSignoffControls label="2.3-2" state={state} signoffs={stepOf('R2:2.3-2')} />)
      expect(within(container).queryByRole('button')).toBeNull()
      cleanup()
    }
  })
})
