import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '../__fixtures__/landing-card.json'
import { fetchLandingCard, LANDING_KIND_LABELS, parseLandingCard } from '../landingCards'

// Same storage shim as the other API HTTP tests (Node's storage is not jsdom's).
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null } })
})
afterEach(() => vi.unstubAllGlobals())

describe('landing card contract', () => {
  it('keeps a card repository and accepts old responses without one', () => {
    for (const repo of ['bifrost-platform', 'bifrost-trade-infra']) {
      expect(parseLandingCard({ ...fixture, repo })?.repo).toBe(repo)
    }
    expect(parseLandingCard(fixture)).toEqual(fixture)
    for (const repo of ['', 'bifrost-research', null, 7]) expect(parseLandingCard({ ...fixture, repo })).toBeNull()
  })
  it.each(['applied', 'moot', 'refused', 'held', 'acknowledged'])('reads a 200 %s outcome', async outcome => {
    const body = { ...fixture, outcome: { ...fixture.outcome, outcome, ...(outcome === 'held' ? { remind_at: '2026-10-12T08:00:00Z' } : {}) } }
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body))))
    expect(await fetchLandingCard(fixture.question_id)).toEqual(body)
  })

  it.each(Object.keys(LANDING_KIND_LABELS))('reads kind %s and a pending outcome', kind => {
    expect(parseLandingCard({ ...fixture, kind, outcome: null })?.kind).toBe(kind)
  })

  it('accepts repository cards with no task and permits additional fields', () => {
    expect(parseLandingCard({ ...fixture, kind: 'repo_stopped', root: '', op: '', future: true })).not.toBeNull()
  })

  it.each(Object.keys(fixture))('refuses a missing %s', key => {
    const body: Record<string, unknown> = { ...fixture }
    delete body[key]
    expect(parseLandingCard(body)).toBeNull()
  })

  it.each(Object.keys(fixture))('refuses a mistyped %s', key => {
    expect(parseLandingCard({ ...fixture, [key]: [] })).toBeNull()
  })

  it.each(['outcome', 'why', 'choice', 'by', 'answered_at'])('refuses a missing or mistyped outcome.%s', key => {
    const outcome: Record<string, unknown> = { ...fixture.outcome }
    delete outcome[key]
    expect(parseLandingCard({ ...fixture, outcome })).toBeNull()
    expect(parseLandingCard({ ...fixture, outcome: { ...fixture.outcome, [key]: 1 } })).toBeNull()
  })

  it.each([null, [], {}, false, 'card',
    { ...fixture, kind: 'unknown' }, { ...fixture, kind: 'toString' },
    { ...fixture, question_id: 'Work-31' }, { ...fixture, options: { a: 7 } },
    { ...fixture, trigger_seq: -1 }, { ...fixture, stop_generation: 1.5 },
    { ...fixture, trigger_seq: Number.MAX_SAFE_INTEGER + 1 },
    { ...fixture, outcome: { ...fixture.outcome, outcome: 'unknown' } },
    { ...fixture, outcome: { ...fixture.outcome, answered_at: 'yesterday' } },
    { ...fixture, outcome: { ...fixture.outcome, remind_at: 7 } },
    { ...fixture, outcome: { ...fixture.outcome, outcome: 'held' } },
  ])('does not crash on an unreadable shape %#', body => {
    expect(parseLandingCard(body)).toBeNull()
  })

  it('returns null for an ordinary question (404)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    expect(await fetchLandingCard(fixture.question_id)).toBeNull()
  })

  it.each([{}, { ...fixture, question_id: 'q_0000000000000002' }])('rejects malformed or mismatched 200 records', async body => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body))))
    await expect(fetchLandingCard(fixture.question_id)).rejects.toThrow('invalid platform response')
  })

  it.each([401, 500])('keeps HTTP %s distinct from not found', async status => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status })))
    await expect(fetchLandingCard(fixture.question_id)).rejects.toThrow()
  })

  it('passes cancellation and rejects unreadable JSON or a failed read', async () => {
    const fetcher = vi.fn(async () => new Response('{'))
    vi.stubGlobal('fetch', fetcher)
    const controller = new AbortController()
    await expect(fetchLandingCard(fixture.question_id, controller.signal)).rejects.toThrow()
    expect(fetcher).toHaveBeenCalledWith(`/api/v1/agentrt/landing/cards/${fixture.question_id}`, expect.objectContaining({ signal: controller.signal, headers: expect.any(Headers) }))
    fetcher.mockRejectedValueOnce(new TypeError('offline'))
    await expect(fetchLandingCard(fixture.question_id)).rejects.toThrow('offline')
  })
})
