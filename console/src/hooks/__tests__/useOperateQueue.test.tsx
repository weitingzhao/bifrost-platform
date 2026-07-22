import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OperateQueueResponse } from '@/api/operateQueueTypes'
import { useOperateQueue } from '@/hooks/useOperateQueue'

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useOperateQueue', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches /api/v1/operate/queue and exposes open/recent_closed items', async () => {
    const payload: OperateQueueResponse = {
      open: [
        {
          id: 'item-1',
          program_id: 'prog-1',
          title: 'Fix satellite dev',
          status: 'open',
          created_at: '2026-07-18T00:00:00Z',
        },
      ],
      recent_closed: [],
    }
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(payload), { status: 200 }),
    )

    const { result } = renderHook(() => useOperateQueue(), { wrapper: makeWrapper() })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(fetch).toHaveBeenCalledWith('/api/v1/operate/queue')
    expect(result.current.data?.open).toHaveLength(1)
    expect(result.current.data?.open[0].title).toBe('Fix satellite dev')
  })

  it('surfaces a parsed error when the response is not ok', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: 'boom' }), { status: 500 }),
    )

    const { result } = renderHook(() => useOperateQueue(), { wrapper: makeWrapper() })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect((result.current.error as Error).message).toMatch(/boom/)
  })
})
