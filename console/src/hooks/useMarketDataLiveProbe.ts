import { useQuery } from '@tanstack/react-query'
import { fetchMarketDataStatus } from '@/api/network'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'

export type MarketDataLiveProbeState = {
  status: MarketDataStatusResponse | undefined
  isLoading: boolean
  isConfigured: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
  refetch: () => void
}

function probeReach(
  status: MarketDataStatusResponse | undefined,
  statusError: boolean,
): MarketDataLiveProbeState['probeReach'] {
  if (status == null && statusError) return 'unknown'
  if (status?.error != null && status.error !== '') {
    if (status.hint != null && status.reachable !== true) return 'unknown'
    return 'fail'
  }
  const reach = status?.reachability
  if (reach === 'ok') return 'ok'
  if (reach === 'degraded') return 'degraded'
  if (reach === 'fail') return 'fail'
  if (status?.reachable === true) return 'ok'
  return 'unknown'
}

export function useMarketDataLiveProbe(refetchIntervalMs = 30_000): MarketDataLiveProbeState {
  const statusQ = useQuery({
    queryKey: ['market-data', 'live-probe', 'status'],
    queryFn: fetchMarketDataStatus,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })

  const status = statusQ.data
  const isConfigured = status?.error == null || status.reachable === true

  return {
    status,
    isLoading: statusQ.isLoading,
    isConfigured,
    probeReach: probeReach(status, statusQ.isError),
    summary:
      status?.summary ??
      (statusQ.isLoading
        ? 'Probing market-data via platform-api…'
        : (status?.hint ?? status?.error ?? '…')),
    refetch: () => void statusQ.refetch(),
  }
}
