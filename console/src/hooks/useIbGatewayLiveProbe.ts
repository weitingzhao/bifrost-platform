import { useQuery } from '@tanstack/react-query'
import { fetchIbGatewayStatus } from '@/api/network'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'

export type IbGatewayLiveProbeState = {
  status: IbGatewayStatusResponse | undefined
  isLoading: boolean
  /** The latest read failed or is paused offline; `probeReach` may still show the last answer that arrived. */
  readFailed: boolean
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

/** `enabled` false reads nothing (a block that is not on screen). */
export function useIbGatewayLiveProbe(refetchIntervalMs = 30_000, enabled = true): IbGatewayLiveProbeState {
  const statusQ = useQuery({
    queryKey: ['ib-gateway', 'live-probe', 'status'],
    queryFn: fetchIbGatewayStatus,
    enabled,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })

  const status = statusQ.data
  const isConfigured = status?.error == null || status.reachable === true

  return {
    status,
    isLoading: statusQ.isLoading,
    readFailed: statusQ.isError || statusQ.isPaused,
    isConfigured,
    probeReach: probeReach(status, statusQ.isError),
    summary: status?.summary ?? (statusQ.isLoading ? 'Probing ib-gateway via platform-api…' : status?.hint ?? status?.error ?? '…'),
    refetch: () => void statusQ.refetch(),
  }
}
