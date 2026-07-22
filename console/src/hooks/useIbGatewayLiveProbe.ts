import { useQuery } from '@tanstack/react-query'
import { fetchIbGatewayStatus } from '@/api/network'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'

export type IbGatewayLiveProbeState = {
  status: IbGatewayStatusResponse | undefined
  isLoading: boolean
  isConfigured: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
  refetch: () => void
}

function probeReach(status: IbGatewayStatusResponse | undefined, statusError: boolean): IbGatewayLiveProbeState['probeReach'] {
  if (status == null && statusError) return 'unknown'
  if (status?.error != null && status.error !== '') {
    if (status.hint != null) return 'unknown'
    return 'fail'
  }
  const reach = status?.reachability
  if (reach === 'ok') return 'ok'
  if (reach === 'degraded') return 'degraded'
  if (reach === 'fail') return 'fail'
  if (status?.reachable === true) return 'ok'
  return 'unknown'
}

export function useIbGatewayLiveProbe(refetchIntervalMs = 30_000): IbGatewayLiveProbeState {
  const statusQ = useQuery({
    queryKey: ['ib-gateway', 'live-probe', 'status'],
    queryFn: fetchIbGatewayStatus,
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
    summary: status?.summary ?? (statusQ.isLoading ? 'Probing ib-gateway via platform-api…' : status?.hint ?? status?.error ?? '…'),
    refetch: () => void statusQ.refetch(),
  }
}
