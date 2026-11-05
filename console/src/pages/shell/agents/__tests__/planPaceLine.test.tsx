import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parsePlanPace } from '@/api/planPace'
import { PlanPaceLine } from '../PlanPaceLine'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// GET /plan-pace as the platform answers it (store.paceOf's sentence for 99 steps at 4, 5, 1 a day).
const answer = {
  root: 'R5', read_at: '2026-11-05T17:30:00Z', timezone: 'America/Chicago', remaining: 99, average: 3.3,
  days: [{ date: '2026-11-02', done: 4 }, { date: '2026-11-03', done: 5 }, { date: '2026-11-04', done: 1 }, { date: '2026-11-05', done: 1, partial: true }],
  eta: { status: 'ok', days: 30, date: '2026-12-05' },
  summary: 'R5: 99 steps left. Last 3 full days: 3.3 steps a day (4, 5, 1). At this pace about 30 days, around 2026-12-05.',
  quiet: [{ from: '2026-11-05T02:03:00Z', to: '2026-11-05T15:47:00Z', minutes: 824 }], quiet_minutes: 824,
  quiet_summary: 'No registered work ran for 13.7 h of the last 24 hours (longest 11-04 20:03 – 11-05 09:47).',
}
function show(root = 'R5') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><PlanPaceLine root={root} /></QueryClientProvider>)
}
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('the round pace line (R5:5.5-13 slice 1)', () => {
  it('reads the answer and refuses one without its sentence or with an unknown ETA', () => {
    const p = parsePlanPace(answer)
    expect(p.remaining).toBe(99)
    expect(p.eta).toEqual({ status: 'ok', days: 30, date: '2026-12-05' })
    expect(p.days[3]).toEqual({ date: '2026-11-05', done: 1, partial: true })
    expect(p.quietMinutes).toBe(824)
    // An older server without the quiet fields still reads.
    const { quiet_minutes: _m, quiet_summary: _s, ...old } = answer
    expect(parsePlanPace(old).quietSummary).toBeUndefined()
    expect(() => parsePlanPace({ ...answer, summary: '' })).toThrow(/missing or mistyped/)
    expect(() => parsePlanPace({ ...answer, eta: { status: 'soon' } })).toThrow(/unknown ETA status/)
    expect(() => parsePlanPace([answer])).toThrow(/not an object/)
  })

  it('shows the platform sentence as it comes, with the days in its title', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(answer), { status: 200 }))
    vi.stubGlobal('fetch', fetcher)
    show()
    const line = await screen.findByText(answer.summary)
    expect(line).toHaveAttribute('data-plan-pace', 'ok')
    expect(line.getAttribute('title')).toContain('11-05: 1 (today so far)')
    expect(String(fetcher.mock.calls[0]?.[0])).toContain('/api/v1/agentrt/plan-pace?root=R5')
    expect(screen.getByText(answer.quiet_summary)).toHaveAttribute('data-plan-quiet', '824')
  })

  it('says Unknown and why when the platform has no such route (an older server)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    show()
    const line = await screen.findByText(/^Pace Unknown · /)
    expect(line).toHaveAttribute('data-plan-pace', 'unknown')
    expect(line.textContent).toMatch(/Plan pace|404|not_found/i)
    expect(line.textContent).not.toMatch(/localStorage/)
  })
})
