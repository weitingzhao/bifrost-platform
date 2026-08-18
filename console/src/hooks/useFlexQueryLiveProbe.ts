import { useQuery } from '@tanstack/react-query'
import { fetchFlexQueryStatus } from '@/api/network'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'

export type FlexQueryLiveProbeState = {
  status: MarketDataStatusResponse | undefined
  isLoading: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
  refetch: () => void
}

function probeReach(
  status: MarketDataStatusResponse | undefined,
  statusError: boolean,
): FlexQueryLiveProbeState['probeReach'] {
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

export function useFlexQueryLiveProbe(refetchIntervalMs = 30_000): FlexQueryLiveProbeState {
  const statusQ = useQuery({
    queryKey: ['flex-query', 'live-probe', 'status'],
    queryFn: fetchFlexQueryStatus,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
  const status = statusQ.data
  return {
    status,
    isLoading: statusQ.isLoading,
    probeReach: probeReach(status, statusQ.isError),
    summary:
      status?.summary ??
      (statusQ.isLoading
        ? 'Probing flex-query via platform-api…'
        : (status?.hint ?? status?.error ?? '…')),
    refetch: () => void statusQ.refetch(),
  }
}
