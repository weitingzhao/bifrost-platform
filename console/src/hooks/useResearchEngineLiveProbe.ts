import { useQuery } from '@tanstack/react-query'
import { fetchDataHusbandry } from '@/api/dataHusbandry'
import {
  fetchOrchestrationStatus,
  fetchSignalHealth,
  isResearchProxyError,
} from '@/api/researchEngine'
import { buildResearchVerdictCopy } from '@/lib/research/researchHealthCopy'

export type ResearchEngineLiveProbeState = {
  isLoading: boolean
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  summary: string
  verdict: string | undefined
  refetch: () => void
}

/** Sidebar Research Engine — research_olap rollup + human copy (not Market/Flex alone). */
export function useResearchEngineLiveProbe(
  refetchIntervalMs = 30_000,
): ResearchEngineLiveProbeState {
  const husbandryQ = useQuery({
    queryKey: ['data-husbandry'],
    queryFn: fetchDataHusbandry,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
  const signalQ = useQuery({
    queryKey: ['research-engine-signal-health'],
    queryFn: fetchSignalHealth,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
  const orchQ = useQuery({
    queryKey: ['research-engine-orchestration'],
    queryFn: fetchOrchestrationStatus,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })

  const market = husbandryQ.data?.lanes.find(l => l.id === 'market_batch')
  const flex = husbandryQ.data?.lanes.find(l => l.id === 'flex_batch')
  const research = husbandryQ.data?.lanes.find(l => l.id === 'research_olap')

  const signalOverall =
    signalQ.data != null && !isResearchProxyError(signalQ.data)
      ? signalQ.data.data.overall
      : null
  const orch =
    orchQ.data != null && !isResearchProxyError(orchQ.data) ? orchQ.data.data : null

  const loading = husbandryQ.isLoading && signalQ.isLoading && orchQ.isLoading
  const copy = buildResearchVerdictCopy({
    loading,
    reachable: true,
    marketVerdict: market?.verdict,
    flexVerdict: flex?.verdict,
    batchVerdict: orch?.verdict ?? research?.verdict,
    batchDetail: orch?.detail ?? research?.detail,
    productOverall: signalOverall ?? undefined,
  })

  return {
    isLoading: loading,
    probeReach: loading ? 'unknown' : copy.lamp,
    summary: copy.navSummary,
    verdict: research?.verdict,
    refetch: () => {
      void husbandryQ.refetch()
      void signalQ.refetch()
      void orchQ.refetch()
    },
  }
}
