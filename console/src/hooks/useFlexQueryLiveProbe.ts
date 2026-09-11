import { useQuery } from '@tanstack/react-query'
import { fetchFlexQueryStatus } from '@/api/network'
import { fetchDataHusbandry } from '@/api/dataHusbandry'
import { layerVerdictToLamp } from '@/lib/research/researchHealthCopy'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'

export type FlexQueryLiveProbeState = {
  status: MarketDataStatusResponse | undefined
  isLoading: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
  /**
   * The plugin's own `flex_batch` husbandry lane — whether the day-end ingest
   * landed, not whether the port answered. `probeReach` cannot see a failing
   * ingest: through the 2026-09-08..09-10 `[1003] Statement is not available`
   * outage the plugin stayed reachable and the nav icon stayed green.
   */
  batch: {
    verdict: string | undefined
    detail: string | undefined
    lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  }
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
  // Same key as useResearchEngineLiveProbe — one request feeds both lamps.
  const husbandryQ = useQuery({
    queryKey: ['data-husbandry'],
    queryFn: fetchDataHusbandry,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
  const status = statusQ.data
  const lane = husbandryQ.data?.lanes.find(l => l.id === 'flex_batch')
  return {
    status,
    isLoading: statusQ.isLoading,
    probeReach: probeReach(status, statusQ.isError),
    summary:
      status?.summary ??
      (statusQ.isLoading
        ? 'Probing flex-query via platform-api…'
        : (status?.hint ?? status?.error ?? '…')),
    batch: {
      verdict: lane?.verdict,
      detail: lane?.detail,
      lamp: husbandryQ.isLoading ? 'unknown' : layerVerdictToLamp(lane?.verdict),
    },
    refetch: () => {
      void statusQ.refetch()
      void husbandryQ.refetch()
    },
  }
}
