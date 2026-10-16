import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'

/** The real probe hook: its last OK answer stays in `probeReach`, so `readFailed` has to say the read is not current (review 222 B-R1). */
const status = vi.hoisted(() => vi.fn())
vi.mock('@/api/network', async importOriginal => ({ ...(await importOriginal<object>()), fetchIbGatewayStatus: status }))

function run() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { client, ...renderHook(() => useIbGatewayLiveProbe(), { wrapper }) }
}

afterEach(() => {
  onlineManager.setOnline(true)
  status.mockReset()
})

describe('probe readFailed', () => {
  it('is false on a good read, true when the next read fails, and the old OK is still in probeReach', async () => {
    status.mockResolvedValue({ reachability: 'ok', reachable: true })
    const { result, client } = run()
    await waitFor(() => expect(result.current.probeReach).toBe('ok'))
    expect(result.current.readFailed).toBe(false)
    status.mockRejectedValue(new Error('HTTP 502'))
    await client.refetchQueries({ queryKey: ['ib-gateway', 'live-probe', 'status'] }).catch(() => undefined)
    await waitFor(() => expect(result.current.readFailed).toBe(true))
    expect(result.current.probeReach).toBe('ok')
  })

  it('is true while a refresh is paused offline', async () => {
    status.mockResolvedValue({ reachability: 'ok', reachable: true })
    const { result, client } = run()
    await waitFor(() => expect(result.current.probeReach).toBe('ok'))
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: ['ib-gateway', 'live-probe', 'status'] })
    await waitFor(() => expect(result.current.readFailed).toBe(true))
  })
})
