import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import fixture from '../__fixtures__/agentrt-capabilities.testdb.json'
import { fetchCapabilities, parseCapabilities } from '../agentRuntimeCapabilities'

// GET /agentrt/capabilities as the handler answered it on a test database (agentrtapi/capabilities.go
// over the store's kernel world with three scores, 2026-10-19): scores, candidates and facts as to_jsonb rows.

beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { vi.unstubAllGlobals() })

describe('capability scores (2.5 G9)', () => {
  it('reads every score, candidate and fact the route gives', () => {
    const c = parseCapabilities(fixture)
    expect(c.truncated).toBe(false)
    expect(c.scores.map(s => [s.executor_actor_id, s.job_id, s.task_class, s.score, s.attempts, s.median_wall_s])).toEqual([
      ['exec:codex', 'implementer', 'go', 0.95, 6, null], ['exec:cursor', 'implementer', 'go', 0.8, 4, 420], ['exec:claude', 'reviewer', 'ts', 0.5, 2, 95]])
    expect(c.scores[0]).toMatchObject({ core: true, source: 'exam', first_pass: 5, accepted: 6, rework: 0, model: 'gpt-x', window_end: fixture.scores[0].window_end })
    expect(c.candidates).toHaveLength(fixture.candidates.length)
    expect(c.candidates[0]).toEqual({ job_candidate_id: 'chief_of_staff/exec:claude/anthropic/*', job_id: 'chief_of_staff', executor_actor_id: 'exec:claude',
      model_provider: 'anthropic', model_pattern: '*', status: 'active', rule_id: 'job:test', qualified_at: null })
    expect(c.facts.find(f => f.capability_fact_id === 'core-author-claude')).toMatchObject({ fact_kind: 'only', subject: 'author_core', source_kind: 'owner',
      rule_id: 'capability-facts', model_provider: '' })
  })
  it.each([
    ['a score above 1', { ...fixture, scores: [{ ...fixture.scores[0], score: 1.2 }] }],
    ['a source of no kind', { ...fixture, scores: [{ ...fixture.scores[0], source: 'guess' }] }],
    ['a score without its window', { ...fixture, scores: [{ ...fixture.scores[0], window_end: null }] }],
    ['a candidate of no status', { ...fixture, candidates: [{ ...fixture.candidates[0], status: 'maybe' }] }],
    ['a fact of no kind', { ...fixture, facts: [{ ...fixture.facts[0], fact_kind: 'likes' }] }],
    ['no truncated flag', { ...fixture, truncated: undefined }],
    ['a list that is not one', { ...fixture, facts: {} }],
  ])('refuses %s rather than show a score nobody measured', (_, value) => {
    expect(() => parseCapabilities(value)).toThrow('Capabilities: invalid platform response.')
  })
  it('reads GET /api/v1/agentrt/capabilities', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(fixture), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    expect((await fetchCapabilities()).scores).toHaveLength(3)
    expect(String((fetch.mock.calls[0] as unknown[])[0])).toBe('/api/v1/agentrt/capabilities')
  })
})
