import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parsePinShare } from '@/api/pinShare'
import { PinShareLine } from '../PinShareLine'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

// GET /pin-share as the platform answers it (store.PinShare): 31 implement tasks in the last 7 days, 23 pinned to the
// coordinator, 20 of those with no pin_reason in their task_proposed event.
const answer = {
  since: '2026-10-30T17:30:00Z', read_at: '2026-11-06T17:30:00Z', days: 7, implement: 31, pinned: 23, unexplained: 20,
  summary: 'Implement tasks pinned to the coordinator, last 7 days: 23 of 31 (20 without a reason)',
}
function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><PinShareLine /></QueryClientProvider>)
}
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined } })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('the coordinator pin share line (R5:5.7-8 slice 1)', () => {
  it('reads the counts and refuses an answer without its sentence or whose counts do not fit', () => {
    const s = parsePinShare(answer)
    expect([s.pinned, s.implement, s.unexplained, s.days]).toEqual([23, 31, 20, 7])
    expect(parsePinShare({ ...answer, implement: 0, pinned: 0, unexplained: 0 }).pinned).toBe(0)
    expect(() => parsePinShare({ ...answer, summary: '' })).toThrow(/missing or mistyped/)
    expect(() => parsePinShare({ ...answer, pinned: '23' })).toThrow(/missing or mistyped/)
    expect(() => parsePinShare({ ...answer, pinned: 40 })).toThrow(/do not add up/)
    expect(() => parsePinShare({ ...answer, unexplained: 24 })).toThrow(/do not add up/)
    expect(() => parsePinShare([answer])).toThrow(/not an object/)
  })

  it('shows the one line with its counts, read-only', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(answer), { status: 200 }))
    vi.stubGlobal('fetch', fetcher)
    show()
    const line = await screen.findByText('Implement tasks pinned to the coordinator, last 7 days: 23 of 31 (20 without a reason)')
    expect(line).toHaveAttribute('data-pin-share', '23/31/20')
    expect(line.getAttribute('title')).toContain('since 2026-10-30T17:30:00Z')
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(String(fetcher.mock.calls[0]?.[0])).toContain('/api/v1/agentrt/pin-share')
    const init = (fetcher.mock.calls[0] as unknown[] | undefined)?.[1] as RequestInit | undefined
    expect(init?.method ?? 'GET').toBe('GET')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('says Unknown and why when the platform has no such route (an older server)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })))
    show()
    const line = await screen.findByText(/^Coordinator pins Unknown · /)
    expect(line).toHaveAttribute('data-pin-share', 'unknown')
    expect(line.textContent).not.toMatch(/\d+ of \d+/)
  })
})
