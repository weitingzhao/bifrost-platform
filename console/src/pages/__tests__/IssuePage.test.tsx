import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseIssueDetail } from '@/api/issues'
import { issueHref, questionHref, resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { IssuePage } from '@/pages/IssuePage'

// W-31 BI-1 slice 2: the read-only issue page (DESIGN-crew-intake §5.3, §8).
// The shape is store.IssueFull's; every text is synthetic.

const B = '05851c9c83bf54acb720e9979da3455e3b27a76ef7bc0c0e1b25b44fbb3d0311'
const D = '11adec5e20767a55499ddc89230367e42586f4972869ac5b2b52f1bdbdeaf54a'
const CARD = 'q_0123456789abcdef'

const ISSUE = {
  issue: {
    issue_id: 'BI-3', product: 'screening', kind: 'complaint', title: 'the page is empty', state: 'owner_deciding', waiting_on: 'owner',
    served_person: { id: 'person:p2', name: 'Person two' }, via: { actor: 'agent:dots-p2', channel: 'intake_local' }, priority: 'p2',
    brief: { text_id: 4, sha256: B, confirmed: true },
    design: { text_id: 7, sha256: D, decisions: [{ decision: 'D1', proceed: ['A'] }] },
  },
  texts: [
    { text_id: 1, kind: 'words', person: 'person:p2', channel: 'intake_local', said_at: '2026-10-23T10:00:00Z', body: 'her own words', sha256: 'x', to_requester: false, by: 'agent:dots-p2', at: '' },
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

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// The token store reads localStorage (no token here).
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

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <IssuePage />
    </QueryClientProvider>,
  )
}

describe('the read-only issue page', () => {
  beforeEach(() => {
    installStorage()
    window.location.hash = issueHref('BI-3')
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('is a route of its own under For you', () => {
    expect(issueHref('BI-3')).toBe('#issue?id=BI-3')
    expect(resolveConsoleHash('issue?id=BI-3').location).toEqual({ kind: 'issue' })
    expect(resolveConsoleHash('issue').location).toEqual({ kind: 'shell', id: 'you' })
  })

  it('shows the brief and the design in full with their whole sha256, the cards, and the three voices apart', async () => {
    const fetchMock = vi.fn(async () => json(ISSUE))
    vi.stubGlobal('fetch', fetchMock)
    show()
    await waitFor(() => expect(screen.getByTestId('issue-page').getAttribute('data-issue-state')).toBe('ready'))
    expect(String(fetchMock.mock.calls[0][0])).toBe('/api/v1/agentrt/issues/BI-3')
    expect(screen.getByText('the brief in full')).toBeTruthy()
    expect(screen.getByText('the design in full')).toBeTruthy()
    const hashes = screen.getAllByTestId('issue-hash').map(n => n.textContent)
    expect(hashes).toEqual([B, D])
    expect(screen.getByTestId('issue-decisions').textContent).toContain('D1: the work goes on under A')
    const card = screen.getByRole('link', { name: 'Design decision D1' })
    expect(card.getAttribute('href')).toBe(questionHref(CARD))
    expect(screen.getByTestId('issue-column-Her words').textContent).toContain('her own words')
    expect(screen.getByTestId('issue-column-Her words').textContent).toContain('relayed by agent:dots-p2')
    expect(screen.getByTestId('issue-column-Her words').textContent).not.toContain('what the agent')
    expect(screen.getByTestId("issue-column-The agent's reading").textContent).toContain('what the agent reads')
    expect(screen.getByTestId("issue-column-The agent's proposal").textContent).toContain('what the agent proposes')
    // Read only: nothing on it answers or writes.
    expect(screen.queryByRole('button')).toBeNull()
    expect(fetchMock.mock.calls.every(c => (c as unknown[])[1] == null || ((c as unknown[])[1] as RequestInit).method == null)).toBe(true)
  })

  it('says why when the platform refuses it, and names no issue from a bad link', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: "an issue reader's token required" }, 401)))
    show()
    await waitFor(() => expect(screen.getByTestId('issue-page').getAttribute('data-issue-state')).toBe('unreadable'))
    cleanup()
    window.location.hash = '#issue?id=BI-03'
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    show()
    expect(screen.getByTestId('issue-page').getAttribute('data-issue-state')).toBe('malformed')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('refuses an answer that is not an issue', () => {
    expect(() => parseIssueDetail({ issue: { issue_id: 'W-31' } })).toThrow()
    expect(parseIssueDetail(ISSUE).cards[0].open).toBe(true)
  })
})
