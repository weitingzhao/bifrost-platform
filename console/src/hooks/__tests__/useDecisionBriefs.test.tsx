import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DecisionBrief } from '@/api/operateBriefs'
import {
  isHeldDecisionBrief,
  isPendingDecisionBrief,
  useDecisionBriefs,
  usePendingDecisionBriefs,
} from '@/hooks/useDecisionBriefs'

function brief(overrides: Partial<DecisionBrief> = {}): DecisionBrief {
  return {
    id: 'brief-1',
    item_id: 'item-1',
    title: 'Satellite dev degraded',
    created_at: '2026-07-18T00:00:00Z',
    fleet_signal: 'degraded',
    fleet_detail: 'partial',
    item_age: '5m',
    fix_scope: null,
    risk_level: 'low',
    suggestion: 'RUN',
    suggestion_reason: 'auto-recover candidate',
    full_brief: 'full text',
    ...overrides,
  }
}

describe('isPendingDecisionBrief', () => {
  it('is pending when there is no decision yet', () => {
    expect(isPendingDecisionBrief(brief({ decision: undefined }))).toBe(true)
    expect(isPendingDecisionBrief(brief({ decision: '' }))).toBe(true)
  })

  it('is not pending once approved or dismissed', () => {
    expect(isPendingDecisionBrief(brief({ decision: 'approved_run' }))).toBe(false)
    expect(isPendingDecisionBrief(brief({ decision: 'dismissed' }))).toBe(false)
  })

  it('treats hold without hold_until as still pending', () => {
    expect(isPendingDecisionBrief(brief({ decision: 'hold', hold_until: undefined }))).toBe(true)
    expect(isPendingDecisionBrief(brief({ decision: 'hold', hold_until: '' }))).toBe(true)
  })

  it('treats hold with a future hold_until as pending, past as resolved', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    const past = new Date(Date.now() - 60_000).toISOString()
    expect(isPendingDecisionBrief(brief({ decision: 'hold', hold_until: future }))).toBe(true)
    expect(isPendingDecisionBrief(brief({ decision: 'hold', hold_until: past }))).toBe(false)
  })
})

describe('isHeldDecisionBrief', () => {
  it('is true only for an active hold', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    expect(isHeldDecisionBrief(brief({ decision: 'hold', hold_until: future }))).toBe(true)
    expect(isHeldDecisionBrief(brief({ decision: 'approved_run' }))).toBe(false)
    expect(isHeldDecisionBrief(brief({ decision: undefined }))).toBe(false)
  })
})

function makeWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useDecisionBriefs / usePendingDecisionBriefs', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches briefs from /api/v1/operate/briefs', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify([brief()]), { status: 200 }),
    )

    const { result } = renderHook(() => useDecisionBriefs(), { wrapper: makeWrapper() })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetch).toHaveBeenCalledWith('/api/v1/operate/briefs')
    expect(result.current.data).toHaveLength(1)
  })

  it('filters out already-decided briefs from the pending list', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify([
          brief({ id: 'a', decision: undefined }),
          brief({ id: 'b', decision: 'approved_run' }),
        ]),
        { status: 200 },
      ),
    )

    const { result } = renderHook(() => usePendingDecisionBriefs(), { wrapper: makeWrapper() })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.pendingCount).toBe(1)
    expect(result.current.pending[0].id).toBe('a')
  })
})
