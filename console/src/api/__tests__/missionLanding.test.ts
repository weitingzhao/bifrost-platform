import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '../__fixtures__/mission-landing.json'
import { fetchMissionLanding, parseMissionLanding } from '../missionLanding'

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null } })
})
afterEach(() => vi.unstubAllGlobals())

describe('mission landing contract', () => {
  it('accepts STG state and refuses malformed state without breaking old responses', () => {
    const stg = { batch: 'stg:bifrost-platform:1', try: 2, phase: 'quarantined', budget_used: 2, budget_gen: 1, quarantined: true, blocked: { why: 'baseline_moved', since: '2026-10-29T12:00:00Z' } }
    const read = (value: unknown) => parseMissionLanding({ ...fixture, roots: [{ ...fixture.roots[0], stg: value }] })
    expect(read(stg)?.roots[0]?.stg).toEqual(stg)
    expect(parseMissionLanding(fixture)).not.toBeNull()
    for (const value of [null, [], { ...stg, try: -1 }, { ...stg, budget_used: 4 }, { ...stg, budget_gen: 0 }, { ...stg, quarantined: 'true' }, { ...stg, blocked: { why: 'rule', since: 'later' } }]) expect(read(value)).toBeNull()
  })
  it('accepts repository and floor details, preserves old responses, and refuses mistyped additions', () => {
    const root = { ...fixture.roots[0], repo: 'bifrost-trade-infra', grant_supported: false, floor: { soft: ['size'], grant_supported: false } }
    expect(parseMissionLanding({ ...fixture, roots: [root] })?.roots[0]).toEqual(root)
    expect(parseMissionLanding(fixture)).toEqual(fixture)
    for (const changed of [{ repo: '' }, { repo: 'bifrost-research' }, { grant_supported: 'false' }, { floor: [] }, { floor: { soft: 'size' } }]) {
      expect(parseMissionLanding({ ...fixture, roots: [{ ...root, ...changed }] })).toBeNull()
    }
  })
  it('reads every class, the closing round, and additional fields', () => {
    expect(parseMissionLanding(fixture)).toEqual(fixture)
    expect(parseMissionLanding({ ...fixture, future: true })).not.toBeNull()
  })
  it('reads a park, a card alert and the close; rejects them mistyped (review 672 R-07, N-01)', () => {
    const parked = { ...fixture.roots[0], card_alert: 'card:7:1:root_failed lost', parked: { since: '2026-10-28T08:00:00Z', shown: 'ci_stuck', retry_at: '2026-10-28T08:01:00Z' } }
    const closed = { to: 'stopped', at: '2026-10-28T11:00:00Z', rule: 'no landing rule in force', round: 0, delivered: 0, disposed: 1, briefs: 0 }
    expect(parseMissionLanding({ ...fixture, roots: [parked], closed })).not.toBeNull()
    expect(parseMissionLanding({ ...fixture, roots: [{ ...parked, parked: { since: 'soon', shown: '' } }] })).toBeNull()
    expect(parseMissionLanding({ ...fixture, roots: [{ ...parked, card_alert: 1 }] })).toBeNull()
    expect(parseMissionLanding({ ...fixture, closed: { ...closed, to: 'expired' } })).toBeNull()
    expect(parseMissionLanding({ ...fixture, closed: { ...closed, delivered: -1 } })).toBeNull()
  })
  it('reads a hold and a pending card; rejects them mistyped (review 675 R-07)', () => {
    const held = { ...fixture.roots[0], held: { until: '2026-10-29T08:00:00Z', question_id: 'q_0123456789abcdef' }, card_pending: true }
    expect(parseMissionLanding({ ...fixture, roots: [held] })).not.toBeNull()
    expect(parseMissionLanding({ ...fixture, roots: [{ ...held, held: { until: 'later', question_id: 'q_1' } }] })).toBeNull()
    expect(parseMissionLanding({ ...fixture, roots: [{ ...held, card_pending: 'yes' }] })).toBeNull()
  })
  it('accepts an out-of-scope mission and an empty root list without a closing round', () => {
    const body = { mission_id: 'M-2', in_scope: false, rule: '', roots: [] }
    expect(parseMissionLanding(body)).toEqual(body)
    expect(parseMissionLanding({ ...body, in_scope: true })).not.toBeNull()
  })
  it('preserves unknown reason/card words and permits an unclaimed root with no operation', () => {
    const root = { ...fixture.roots[0], op: '', reason: 'future_reason', card_kind: 'future_card' }
    expect(parseMissionLanding({ ...fixture, roots: [root] })?.roots).toEqual([root])
  })
  it.each(['mission_id', 'in_scope', 'rule', 'roots'])('rejects missing or mistyped %s', key => {
    const body: Record<string, unknown> = { ...fixture }
    delete body[key]
    expect(parseMissionLanding(body)).toBeNull()
    expect(parseMissionLanding({ ...fixture, [key]: 1 })).toBeNull()
  })
  it.each(Object.keys(fixture.roots[0]!))('rejects the whole read for missing or mistyped root.%s', key => {
    const root: Record<string, unknown> = { ...fixture.roots[0] }
    delete root[key]
    expect(parseMissionLanding({ ...fixture, roots: [fixture.roots[1], root] })).toBeNull()
    expect(parseMissionLanding({ ...fixture, roots: [{ ...fixture.roots[0], [key]: [] }] })).toBeNull()
  })
  it.each(Object.keys(fixture.closing_round))('rejects missing or mistyped closing_round.%s', key => {
    const round: Record<string, unknown> = { ...fixture.closing_round }
    delete round[key]
    expect(parseMissionLanding({ ...fixture, closing_round: round })).toBeNull()
    expect(parseMissionLanding({ ...fixture, closing_round: { ...fixture.closing_round, [key]: null } })).toBeNull()
  })
  it.each([null, [], {}, false, 'landing',
    { ...fixture, mission_id: '' }, { ...fixture, roots: [null] },
    { ...fixture, roots: [{ ...fixture.roots[0], root: '' }] },
    { ...fixture, roots: [{ ...fixture.roots[0], class: 'unknown' }] },
    { ...fixture, closing_round: null }, { ...fixture, closing_round: [] },
    ...[0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1].map(round => ({ ...fixture, closing_round: { ...fixture.closing_round, round } })),
    { ...fixture, closing_round: { ...fixture.closing_round, at: 'yesterday' } },
    { ...fixture, closing_round: { ...fixture.closing_round, until: 'tomorrow' } },
    { ...fixture, closing_round: { ...fixture.closing_round, until: fixture.closing_round.at } },
    { ...fixture, closing_round: { ...fixture.closing_round, ops: [42] } },
  ])('rejects malformed shapes %# without throwing', body => {
    expect(parseMissionLanding(body)).toBeNull()
  })
  it('reads 200 and forwards the abort signal and authentication headers', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(fixture)))
    vi.stubGlobal('fetch', fetcher)
    const controller = new AbortController()
    expect(await fetchMissionLanding('M-2', controller.signal)).toEqual(fixture)
    expect(fetcher).toHaveBeenCalledWith('/api/v1/agentrt/landing/missions/M-2', expect.objectContaining({ signal: controller.signal, headers: expect.any(Headers) }))
  })
  it.each([201, 204, 301, 401, 403, 404, 429, 500, 502, 503])('preserves HTTP %s as the unreadable reason', async status => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(status === 204 ? null : JSON.stringify(fixture), { status })))
    await expect(fetchMissionLanding('M-2')).rejects.toThrow(`Couldn't read landing status (${status})`)
  })
  it.each([
    ['{', 'invalid JSON response'], ['{}', 'invalid response data'],
    [JSON.stringify({ ...fixture, roots: [{ ...fixture.roots[0], repo: 'unreadable' }] }), 'invalid response data'],
    [JSON.stringify({ ...fixture, mission_id: 'M-3' }), 'mission mismatch'],
  ])('explains malformed or mismatched 200 responses: %s', async (body, reason) => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(body)))
    await expect(fetchMissionLanding('M-2')).rejects.toThrow(`Couldn't read landing status (${reason})`)
  })
  it('explains network failure and preserves cancellation', async () => {
    const aborted = new DOMException('Aborted', 'AbortError')
    const fetcher = vi.fn().mockRejectedValueOnce(new TypeError('Failed to fetch')).mockRejectedValueOnce(aborted)
    vi.stubGlobal('fetch', fetcher)
    await expect(fetchMissionLanding('M-2')).rejects.toThrow("Couldn't read landing status (network error)")
    const controller = new AbortController()
    controller.abort()
    await expect(fetchMissionLanding('M-2', controller.signal)).rejects.toBe(aborted)
  })
})
