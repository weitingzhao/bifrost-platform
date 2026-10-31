import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { AssessmentSection } from '@/pages/question/AssessmentPanel'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { assessmentKey, type AssessmentKind, type AssessmentSnapshot } from '@/api/assessment'
import streams from '@/api/__fixtures__/assessment-streams.testdb.json'

const owner = { token: 'test-owner', caps: { role: 'admin' }, canAdmin: true, capsLoading: false, capsError: false } as PlatformAuthContextValue
const requested = [{ event_type: 'note', at: '2026-10-30T12:00:00Z', body_json: { assessment_requested: { kind: 'card', id: '463' } } }]
const reply = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

function show(kind: AssessmentKind, snapshot: AssessmentSnapshot = { events: [] }, closed = false, auth = owner) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const section = (current: AssessmentSnapshot) => <PlatformAuthContext.Provider value={auth}><QueryClientProvider client={client}>
    <AssessmentSection kind={kind} id="463" snapshot={current} closed={closed} ruleKey={null} label={key => key} />
  </QueryClientProvider></PlatformAuthContext.Provider>
  const view = render(section(snapshot))
  return { client, refresh: (current: AssessmentSnapshot) => view.rerender(section(current)) }
}

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => 'test-owner', setItem: () => {}, removeItem: () => {} } })
})

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('Owner opts in to assessment', () => {
  it.each(['card', 'approval'] as const)('%s stays quiet until clicked, requests once, then shows progress and the result', async kind => {
    let finish!: (response: Response) => void
    let events: unknown = requested
    const fetch = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        expect(url).toBe(`/api/v1/agentrt/assessments/${kind}/463`)
        return new Promise<Response>(resolve => { finish = resolve })
      }
      return Promise.resolve(reply(events))
    })
    vi.stubGlobal('fetch', fetch)
    const { client } = show(kind)
    const ask = screen.getByRole('button', { name: 'Ask for assessment' })
    expect(screen.queryByText(/Recommends /)).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
    fireEvent.click(ask)
    fireEvent.click(ask)
    expect(fetch).toHaveBeenCalledTimes(1)
    finish(reply(requested))
    expect(await screen.findByText(/Requested; waiting/)).toBeTruthy()
    expect(fetch.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
    events = streams.card
    await client.invalidateQueries({ queryKey: assessmentKey(kind, '463') })
    expect(await screen.findByText('Recommends b (high confidence)')).toBeTruthy()
    expect(screen.getByText(/Advice only: it answers nothing and approves nothing/)).toBeTruthy()
    client.clear()
  })

  it('shows existing results without any assessment request', () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    show('card', { events: streams.card })
    expect(screen.getByText('Recommends b (high confidence)')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })

  describe.each(['card', 'approval'] as const)('%s recovers after a failed poll', kind => {
    it.each([
      { source: 'reopened', state: 'ready' },
      { source: 'clicked', state: 'ready' },
      { source: 'reopened', state: 'unavailable' },
      { source: 'clicked', state: 'unavailable' },
    ])('$source assessment shows $state from a refreshed item', async ({ source, state }) => {
      const fetch = vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'POST') return reply(requested)
        throw new Error('temporary network failure')
      })
      vi.stubGlobal('fetch', fetch)
      const { client, refresh } = show(kind, { events: source === 'reopened' ? requested : [] })
      if (source === 'clicked') fireEvent.click(screen.getByRole('button', { name: 'Ask for assessment' }))
      expect(await screen.findByText(/Could not read the AI assessment:.*temporary network failure/)).toBeTruthy()

      const snapshot = { events: state === 'ready' ? streams.card
        : [...requested, { event_type: 'note', body_json: { unavailable: 'assessment worker timed out' } }] }
      const message = state === 'ready' ? 'Recommends b (high confidence)' : 'AI assessment unavailable: assessment worker timed out'
      refresh(snapshot)
      expect(screen.getByText(message)).toBeTruthy()
      expect(screen.queryByText(/temporary network failure/)).toBeNull()
      // The same fresh snapshot remains visible, even though the poll cache is still in error.
      refresh(snapshot)
      expect(screen.getByText(message)).toBeTruthy()
      await client.invalidateQueries({ queryKey: assessmentKey(kind, '463') })
      expect(fetch).toHaveBeenCalledTimes(source === 'clicked' ? 2 : 1)
      client.clear()
    })
  })

  it('shows the rejection reason and does not retry automatically', async () => {
    const fetch = vi.fn(async () => reply({ error: 'state', message: 'question is answered; assessment requires an open question' }, 409))
    vi.stubGlobal('fetch', fetch)
    show('card')
    fireEvent.click(screen.getByRole('button', { name: 'Ask for assessment' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('question is answered'))
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('disables requests for a closed item or a non-Owner and explains why', () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    show('approval', { events: [] }, true)
    expect((screen.getByRole('button', { name: 'Ask for assessment' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/this item is no longer open/)).toBeTruthy()
    cleanup()
    show('card', { events: [] }, false, { ...owner, caps: { ...owner.caps!, role: 'viewer' } })
    expect((screen.getByRole('button', { name: 'Ask for assessment' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/Sign in as Owner/)).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('shows why the runtime is unavailable from the item response', () => {
    vi.stubGlobal('fetch', vi.fn())
    show('card', { error: 'assessment runtime is unavailable' })
    expect(screen.getByText('AI assessment unavailable: assessment runtime is unavailable')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })
})
