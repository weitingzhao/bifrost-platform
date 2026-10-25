import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseIssueDetail } from '@/api/issues'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { issueHref, questionHref, resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { IssuePage } from '@/pages/IssuePage'

// W-31 BI-1 slice 2: the read-only issue page (DESIGN-crew-intake §5.3, §8),
// under the real sign-in provider (review 524 A1). The shape is
// store.IssueFull's; every text is synthetic.

const B = '05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311'
const D = '11adec5e20767a55499ddc89230367e42586f4972869ac5b2b52f1bdbdeaf54a'
const CARD = 'q_0123456789abcdef'
const PRIVATE = 'PRIVATE-WORDS-524'

const ISSUE = {
  issue: {
    issue_id: 'BI-3', product: 'screening', kind: 'complaint', title: 'the page is empty', state: 'owner_deciding', waiting_on: 'owner',
    served_person: { id: 'person:p2', name: 'Person two' }, via: { actor: 'agent:dots-p2', channel: 'intake_local' }, priority: 'p2',
    brief: { text_id: 4, sha256: B, confirmed: true },
    design: { text_id: 7, sha256: D, decisions: [{ decision: 'D1', proceed: ['A'] }] },
    next: { by: 'crew', what: 'close it declined: the Owner declined it on his card ' + CARD },
  },
  texts: [
    { text_id: 1, kind: 'words', person: 'person:p2', channel: 'intake_local', said_at: '2026-10-23T10:00:00Z', body: PRIVATE, sha256: 'x', to_requester: false, by: 'agent:dots-p2', at: '' },
    { text_id: 2, kind: 'reading', channel: 'intake_local', body: 'what the agent reads', sha256: 'x', to_requester: false, by: 'agent:dots-p2', at: '' },
    { text_id: 3, kind: 'proposal', channel: 'intake_local', body: 'what the agent proposes', sha256: 'x', to_requester: false, by: 'agent:dots-p2', at: '' },
    { text_id: 4, kind: 'brief', channel: 'thread', body: 'the brief in full', sha256: B, to_requester: true, by: 'thread:crew', at: '' },
    { text_id: 5, kind: 'response', person: 'person:p2', channel: 'intake_local', body: 'yes, that is it', sha256: 'x', to_requester: false, by: 'agent:dots-p2', at: '' },
    { text_id: 7, kind: 'design', channel: 'thread', body: 'the design in full', sha256: D, to_requester: false, by: 'thread:crew', at: '' },
  ],
  asks: [{ ask_id: 6, purpose: 'confirm_brief', person: 'person:p2', bound_text_id: 4, bound_sha256: B, verdict: 'confirmed', words_text_id: 5,
    answered_by: 'agent:dots-p2', answer_channel: 'intake_local' }],
  cards: [{ question_id: CARD, purpose: 'design_decision', decision: 'D1', brief_sha256: B, design_sha256: D, open: true }],
}

const ROLES: Record<string, string> = { 'admin-fixture': 'admin', 'viewer-fixture': 'viewer', 'satellite-fixture': 'operator' }

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// The token store reads localStorage.
function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear: () => mem.clear(),
    getItem: key => mem.get(key) ?? null,
    key: index => [...mem.keys()][index] ?? null,
    removeItem: key => {
      mem.delete(key)
    },
    setItem: (key, value) => {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

type IssueAnswer = (token: string, signal: AbortSignal | null | undefined) => Promise<Response>

/** The platform: capabilities by token; the issue for the admin's token only (the readers list), unless answer says otherwise. */
function platform(answer?: IssueAnswer) {
  const issueCalls: Array<{ token: string; signal: AbortSignal | null | undefined }> = []
  const fetchMock = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
    const token = (new Headers(init?.headers).get('Authorization') ?? '').replace(/^Bearer /, '')
    if (String(url).includes('/auth/capabilities')) {
      const role = ROLES[token]
      return json({ authenticated: role != null, principal: role ?? '', role: role ?? 'viewer', can_operate: role === 'admin' || role === 'operator', can_admin: role === 'admin' })
    }
    if (String(url).includes('/agentrt/issues/')) {
      issueCalls.push({ token, signal: init?.signal })
      if (answer) return answer(token, init?.signal)
      return token === 'admin-fixture' ? json(ISSUE) : json({ error: "an issue reader's token required" }, 401)
    }
    throw new Error('unexpected request ' + String(url))
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, issueCalls }
}

function Login() {
  const auth = usePlatformAuth()
  return (
    <div>
      <button onClick={() => auth.setToken('viewer-fixture')}>as viewer</button>
      <button onClick={() => auth.setToken('satellite-fixture')}>as trade-satellite</button>
      <button onClick={auth.signOut}>sign out</button>
    </div>
  )
}

function show() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <PlatformAuthProvider>
        <Login />
        <IssuePage />
      </PlatformAuthProvider>
    </QueryClientProvider>,
  )
  return qc
}

const pageState = () => screen.getByTestId('issue-page').getAttribute('data-issue-state')
const cached = (qc: QueryClient) => JSON.stringify(qc.getQueryCache().getAll().map(q => q.state.data ?? null))

describe('the read-only issue page', () => {
  beforeEach(() => {
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-fixture')
    window.location.hash = issueHref('BI-3')
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('is a route of its own under For you', () => {
    expect(issueHref('BI-3')).toBe('#issue?id=BI-3')
    expect(resolveConsoleHash('issue?id=BI-3').location).toEqual({ kind: 'issue' })
    expect(resolveConsoleHash('issue').location).toEqual({ kind: 'shell', id: 'command' })
  })

  it('shows the brief and the design in full with their whole sha256, the cards, and the three voices apart', async () => {
    const { issueCalls } = platform()
    show()
    await waitFor(() => expect(pageState()).toBe('ready'))
    expect(issueCalls.map(c => c.token)).toEqual(['admin-fixture'])
    expect(screen.getByText('the brief in full')).toBeTruthy()
    expect(screen.getByText('the design in full')).toBeTruthy()
    expect(screen.getAllByTestId('issue-hash').map(n => n.textContent)).toEqual([B, D])
    expect(screen.getByTestId('issue-decisions').textContent).toContain('D1: the work goes on under A')
    expect(screen.getByRole('link', { name: 'Design decision D1' }).getAttribute('href')).toBe(questionHref(CARD))
    expect(screen.getByText(`crew: close it declined: the Owner declined it on his card ${CARD}`)).toBeTruthy()
    const her = screen.getByTestId('issue-column-Her words').textContent
    expect(her).toContain(PRIVATE)
    expect(her).toContain('relayed by agent:dots-p2')
    expect(her).not.toContain('what the agent')
    expect(screen.getByTestId("issue-column-The agent's reading").textContent).toContain('what the agent reads')
    expect(screen.getByTestId("issue-column-The agent's proposal").textContent).toContain('what the agent proposes')
    // Read only: nothing on it answers or writes.
    expect(within(screen.getByTestId('issue-page')).queryByRole('button')).toBeNull()
  })

  // Review 524 A1: what one sign-in read is gone the moment another (or none)
  // takes its place: not in the page, not in the query cache.
  for (const [step, button, refused] of [
    ['to a viewer', 'as viewer', true],
    ['to trade-satellite', 'as trade-satellite', true],
    ['to signed out', 'sign out', false],
  ] as const) {
    it(`drops the text when the sign-in changes ${step}`, async () => {
      const { issueCalls } = platform()
      const qc = show()
      await screen.findByText(PRIVATE)
      fireEvent.click(screen.getByText(button))
      expect(screen.queryByText(PRIVATE)).toBeNull()
      expect(cached(qc)).not.toContain(PRIVATE)
      await waitFor(() => expect(pageState()).toBe(refused ? 'unreadable' : 'signed-out'))
      expect(screen.queryByText(PRIVATE)).toBeNull()
      expect(cached(qc)).not.toContain(PRIVATE)
      expect(issueCalls.length).toBe(refused ? 2 : 1)
      if (refused) expect(issueCalls[1].token).not.toBe('admin-fixture')
    })
  }

  it('never fills in a late answer to the last sign-in', async () => {
    let release: (() => void) | undefined
    const { issueCalls } = platform(token => {
      if (token !== 'admin-fixture') return Promise.resolve(json({ error: "an issue reader's token required" }, 401))
      // The admin's read is slow; it answers after the sign-in changed, whatever the abort said.
      return new Promise<Response>(resolve => {
        release = () => resolve(json(ISSUE))
      })
    })
    const qc = show()
    await waitFor(() => expect(issueCalls.length).toBe(1))
    fireEvent.click(screen.getByText('as viewer'))
    await waitFor(() => expect(pageState()).toBe('unreadable'))
    expect(issueCalls[0].signal?.aborted).toBe(true)
    await act(async () => {
      release?.()
      await new Promise(r => setTimeout(r, 20))
    })
    expect(screen.queryByText(PRIVATE)).toBeNull()
    expect(cached(qc)).not.toContain(PRIVATE)
    expect(pageState()).toBe('unreadable')
  })

  it('names no issue from a bad link', () => {
    window.location.hash = '#issue?id=BI-03'
    const { issueCalls } = platform()
    show()
    expect(pageState()).toBe('malformed')
    expect(issueCalls.length).toBe(0)
  })

  it('refuses an answer that is not an issue', () => {
    expect(() => parseIssueDetail({ issue: { issue_id: 'W-31' } })).toThrow()
    expect(parseIssueDetail(ISSUE).cards[0].open).toBe(true)
  })
})
