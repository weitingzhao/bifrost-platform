import { afterEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/agentrt-plan-timeline.go.json'
import { fetchPlanTimeline, parsePlanTimeline } from '@/api/planTimeline'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
afterEach(() => vi.unstubAllGlobals())

// The answers of store.ReadRoundTimeline on the migrated database, as GET /plan-timeline writes them
// (store/console_delivery_fixture_test.go generates the fixture).
const round = wire.cases.find(c => c.name === 'round')!
const oneStep = wire.cases.find(c => c.name === 'one step')!
const clone = <T,>(v: T): T => structuredClone(v)

describe('GET /plan-timeline, closed', () => {
  it("parses Go's answers as they are: a round, and one step of it", () => {
    expect(parsePlanTimeline(round.response, 'R2')).toEqual(round.response)
    expect(parsePlanTimeline(oneStep.response, 'R2', 'R2:2.1-2')).toEqual(oneStep.response)
    const s2 = parsePlanTimeline(round.response, 'R2').steps.find(s => s.plan_node_id === 'R2:2.1-2')!
    expect(s2.events.map(e => e.kind)).toEqual(expect.arrayContaining(['submitted', 'accepted', 'landed', 'on_stg', 'on_prod']))
    expect(s2.cards[0]).toMatchObject({ kind: 'landing_card', via: 'landing_op' })
    expect(s2.cards[0]!.attempts.map(a => a.void_reason ?? '')).toEqual(['expired', ''])
  })

  it('refuses unknown keys at every level', () => {
    const top = { ...clone(round.response), extra: 1 }
    expect(() => parsePlanTimeline(top, 'R2')).toThrow(/at timeline$/)
    const step = clone(round.response); Object.assign(step.steps[3]!, { extra: 1 })
    expect(() => parsePlanTimeline(step, 'R2')).toThrow(/timeline\.steps\[3\]$/)
    const event = clone(round.response); Object.assign(event.steps[3]!.events[0]!, { extra: 1 })
    expect(() => parsePlanTimeline(event, 'R2')).toThrow(/events\[0\]$/)
    const card = clone(round.response); Object.assign(card.steps[3]!.cards[0]!.attempts[1]!.result!, { extra: 1 })
    expect(() => parsePlanTimeline(card, 'R2')).toThrow(/result$/)
  })

  it('refuses a kind or source Go does not name, a missing field, another round and a step not asked for', () => {
    const kind = clone(round.response); kind.steps[3]!.events[0]!.kind = 'deployed'
    expect(() => parsePlanTimeline(kind, 'R2')).toThrow(/kind$/)
    const source = clone(round.response); source.steps[3]!.events[0]!.source = 'guess'
    expect(() => parsePlanTimeline(source, 'R2')).toThrow(/source$/)
    const missing = clone(round.response) as Record<string, unknown>; delete missing.unbound_cards
    expect(() => parsePlanTimeline(missing, 'R2')).toThrow(/unbound_cards$/)
    const at = clone(round.response); at.steps[3]!.events[0]!.at = 'yesterday'
    expect(() => parsePlanTimeline(at, 'R2')).toThrow(/at$/)
    expect(() => parsePlanTimeline(round.response, 'R5')).toThrow(/root$/)
    expect(() => parsePlanTimeline(round.response, 'R2', 'R2:2.1-2')).toThrow(/steps$/)
  })

  it('reads the round, or one step of it, by its query', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(oneStep.response), { status: 200 }))
    vi.stubGlobal('fetch', fetcher)
    const got = await fetchPlanTimeline('R2', 'R2:2.1-2')
    expect(got.steps).toHaveLength(1)
    expect(String((fetcher.mock.calls[0] as unknown[])[0])).toBe('/api/v1/agentrt/plan-timeline?root=R2&node=R2%3A2.1-2')
  })
})
