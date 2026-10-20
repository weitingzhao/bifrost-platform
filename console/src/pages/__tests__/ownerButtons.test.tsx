import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import cards from '@/api/__fixtures__/question-cards.testdb.json'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return {
    ...actual,
    fetchAuthCapabilities: vi.fn(async () => ({ authenticated: true, role: 'admin', can_operate: true, can_admin: true })),
  }
})

/**
 * Every button the Owner has on a runtime card, on the card as the runtime makes it (TD-393, the Owner
 * 2026-10-20 「走查不再用「需要本人」，按钮的路径用 Console 测试」): each option, and "None of these" with his
 * words, sends exactly that answer to POST /api/v1/questions/{id}/answer. The fixture is one open card of
 * each kind, made by the runtime's own code on the real question store
 * (api/internal/server/agentrt_ownerbuttons_test.go writes it); what the runtime then does with each answer
 * is that test's half.
 */
type CardFixture = { status: number; body: { id: string; created_at: string; options: { key: string; label: string }[] } }

const KINDS = cards as unknown as Record<string, CardFixture>

const ADMIN_CAPS: AuthCapabilities = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    get length() { return mem.size },
    clear: () => mem.clear(),
    getItem: (key: string) => mem.get(key) ?? null,
    key: (index: number) => [...mem.keys()][index] ?? null,
    removeItem: (key: string) => { mem.delete(key) },
    setItem: (key: string, value: string) => { mem.set(key, String(value)) },
  } })
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** Opens the card's page, presses one button (and writes words for "none"), confirms; the request sent. */
async function press(card: CardFixture, key: string, words = ''): Promise<{ url: string; body: unknown }> {
  vi.setSystemTime(new Date(Date.parse(card.body.created_at) + 60_000))
  const posted: { url: string; body: unknown }[] = []
  vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.includes('/agentrt/events?')) return json([])
    if (url.endsWith('/answer') && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { option: string }
      posted.push({ url, body })
      return json({ id: card.body.id, number: 1, status: 'answered',
        answer: { option: body.option, by: 'admin', at: new Date().toISOString(), channel: 'console', approval: false } })
    }
    if (url.endsWith(`/api/v1/questions/${card.body.id}`)) return json(card.body)
    throw new Error(`unexpected request ${url}`)
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['platform', 'auth', 'capabilities', 'admin-token'], ADMIN_CAPS)
  window.location.hash = `#questions?id=${card.body.id}`
  render(
    <QueryClientProvider client={client}>
      <PlatformAuthProvider>
        <QuestionPage />
      </PlatformAuthProvider>
    </QueryClientProvider>,
  )
  if (key === 'none') {
    fireEvent.click(await screen.findByRole('button', { name: 'None of these' }))
    fireEvent.change(document.querySelector('textarea') as HTMLTextAreaElement, { target: { value: words } })
  } else {
    const label = card.body.options.find(o => o.key === key)?.label ?? ''
    fireEvent.click(await screen.findByRole('button', { name: new RegExp(`^${key} ${escape(label)}`) }))
  }
  fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
  await waitFor(() => expect(posted.length).toBe(1))
  return posted[0]!
}

describe("the Owner's buttons on every kind of runtime card", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    resetAnswerSendStore()
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'admin-token')
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    cleanup()
    resetAnswerSendStore()
    vi.unstubAllGlobals()
    window.location.hash = ''
    vi.useRealTimers()
  })

  it('has one card of each kind the runtime asks', () => {
    expect(Object.keys(KINDS).sort()).toEqual(['blocked', 'mission', 'plan_change', 'plan_import', 'plan_question', 'recorder_import'])
    expect(KINDS.blocked!.body.options.map(o => o.label)).toEqual(['Give it one more round', 'Do not do this task', 'Close mission M-5'])
    expect(KINDS.mission!.body.options.map(o => o.label)).toEqual(['Start the mission', 'Do not start it'])
    expect(KINDS.plan_change!.body.options.map(o => o.label)).toEqual(['Apply the change', 'Do not apply it'])
  })

  for (const [kind, card] of Object.entries(KINDS)) {
    for (const option of card.body.options) {
      it(`${kind}: ${option.key} ${option.label} sends option ${option.key}`, async () => {
        const sent = await press(card, option.key)
        expect(sent.url).toBe(`/api/v1/questions/${card.body.id}/answer`)
        expect(sent.body).toEqual({ channel: 'console', option: option.key })
      })
    }
    it(`${kind}: None of these sends his words`, async () => {
      const sent = await press(card, 'none', 'not this week')
      expect(sent.body).toEqual({ channel: 'console', option: 'none', text: 'not this week' })
    })
  }
})
