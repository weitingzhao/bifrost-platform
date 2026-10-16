import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'

/** The real probe hooks: their last OK answer stays in `probeReach`, so `readFailed` has to say the read is not current (review 222 B-R1, 223). */
const net = vi.hoisted(() => ({ ib: vi.fn(), market: vi.fn(), flex: vi.fn() }))
vi.mock('@/api/network', async importOriginal => ({ ...(await importOriginal<object>()),
  fetchIbGatewayStatus: net.ib, fetchMarketDataStatus: net.market, fetchFlexQueryStatus: net.flex }))
vi.mock('@/api/dataHusbandry', () => ({ fetchDataHusbandry: vi.fn(async () => ({ lanes: [] })) }))

const probes = [
  { name: 'IB gateway', hook: useIbGatewayLiveProbe, fetch: net.ib, key: ['ib-gateway', 'live-probe', 'status'] },
  { name: 'market data', hook: useMarketDataLiveProbe, fetch: net.market, key: ['market-data', 'live-probe', 'status'] },
  { name: 'IB Flex', hook: useFlexQueryLiveProbe, fetch: net.flex, key: ['flex-query', 'live-probe', 'status'] },
]

function run(hook: () => { probeReach: string; readFailed: boolean }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { client, ...renderHook(() => hook(), { wrapper }) }
}

afterEach(() => {
  onlineManager.setOnline(true)
  for (const f of Object.values(net)) f.mockReset()
})

describe.each(probes)('$name probe readFailed', ({ hook, fetch, key }) => {
  it('is false on a good read, true when the next read fails, and the old OK is still in probeReach', async () => {
    fetch.mockResolvedValue({ reachability: 'ok', reachable: true })
    const { result, client } = run(hook)
    await waitFor(() => expect(result.current.probeReach).toBe('ok'))
    expect(result.current.readFailed).toBe(false)
    fetch.mockRejectedValue(new Error('HTTP 502'))
    await client.refetchQueries({ queryKey: key }).catch(() => undefined)
    await waitFor(() => expect(result.current.readFailed).toBe(true))
    expect(result.current.probeReach).toBe('ok')
  })

  it('is true while a refresh is paused offline', async () => {
    fetch.mockResolvedValue({ reachability: 'ok', reachable: true })
    const { result, client } = run(hook)
    await waitFor(() => expect(result.current.probeReach).toBe('ok'))
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: key })
    await waitFor(() => expect(result.current.readFailed).toBe(true))
  })
})
