import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MatrixResponse } from '@/api/matrixTypes'
import {
  freshQueryData,
  MISSION_STALE_AFTER_MS,
  missionFreshness,
  useMissionSnapshot,
} from '@/hooks/useMissionSnapshot'

const prodMatrix: MatrixResponse = {
  environment: 'prod',
  label: 'Production',
  generated_at: '2026-10-07T00:00:00Z',
  principal: { name: 'viewer', level: 'L0' },
  targets: [
    {
      id: 'api-monitor',
      category: 'trade_read',
      reachability: 'ok',
      auth: 'ok',
      authorization_level: 'L0',
      detail: 'HTTP 200',
    },
  ],
}

/** TD-227: a failing probe must not keep its last good verdict under a fresh stamp. */
describe('mission snapshot stale', () => {
  let matrixOk = true
  let queryClient: QueryClient

  beforeEach(() => {
    matrixOk = true
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url === '/api/v1/matrix') {
          return matrixOk
            ? new Response(JSON.stringify({ matrices: [prodMatrix] }), { status: 200 })
            : new Response('probe down', { status: 502 })
        }
        return new Response('{}', { status: 500 })
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    queryClient.clear()
  })

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }

  it('the matrix query fails after one success → tradeProd is unknown and matrix is listed stale', async () => {
    const { result } = renderHook(() => useMissionSnapshot(), { wrapper })
    await waitFor(() => expect(result.current.snapshot.tradeProd.signal).toBe('ok'))
    expect(result.current.matrices).toHaveLength(1)
    expect(result.current.staleSources).not.toContain('matrix')

    matrixOk = false
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ['cockpit', 'matrix'] })
    })

    await waitFor(() => expect(result.current.snapshot.tradeProd.signal).toBe('unknown'))
    expect(result.current.matrices).toHaveLength(0)
    expect(result.current.staleSources).toContain('matrix')
  })

  it('freshness is the oldest answer, not the newest', () => {
    const now = 1_000_000
    const f = missionFreshness(
      [
        { id: 'cluster', q: { data: {}, isError: false, dataUpdatedAt: now - 1_000 } },
        { id: 'matrix', q: { data: {}, isError: false, dataUpdatedAt: now - MISSION_STALE_AFTER_MS - 1 } },
        { id: 'bridge', q: { data: undefined, isError: true, dataUpdatedAt: 0 } },
      ],
      now,
    )
    expect(f.dataUpdatedAt).toBe(now - MISSION_STALE_AFTER_MS - 1)
    expect(f.staleSources).toEqual(['matrix', 'bridge'])
  })

  it('freshQueryData drops errored or aged-out data and keeps a fresh answer', () => {
    const now = 1_000_000
    expect(freshQueryData({ data: 1, isError: false, dataUpdatedAt: now - 1 }, now)).toBe(1)
    expect(freshQueryData({ data: 1, isError: true, dataUpdatedAt: now - 1 }, now)).toBeUndefined()
    expect(
      freshQueryData({ data: 1, isError: false, dataUpdatedAt: now - MISSION_STALE_AFTER_MS - 1 }, now),
    ).toBeUndefined()
  })
})
