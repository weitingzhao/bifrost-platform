import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchRemediationJobs } from '@/api/remediation'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePendingDecisionBriefs } from '@/hooks/useDecisionBriefs'
import {
  buildControlRoomBaySignals,
  worstBayScanSignal,
} from '@/lib/control-room/controlRoomBays'
import type { Signal } from '@/lib/control-room/missionSignals'
import { findActiveRemediationJobs } from '@/lib/remediation/remediationJobDisplay'

/**
 * Live Bay Scan aggregate for Mission Control → Control Room sidebar icon.
 * Mirrors Control Room bay lamps (Mission / Launch / Operate / Release / Health).
 */
export function useControlRoomBayNavSignal(): Signal {
  const { snapshot } = useFleetSnapshot()
  const operateQueueQuery = useOperateQueue()
  const briefsQuery = usePendingDecisionBriefs()
  const networkProbe = useNetworkLiveProbe()
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 10_000,
  })
  const activeAgentJobCount = findActiveRemediationJobs(jobsQuery.data?.jobs ?? []).length

  return useMemo(() => {
    const bays = buildControlRoomBaySignals({
      snapshot,
      operateOpenCount: operateQueueQuery.data?.open.length ?? 0,
      pendingBriefCount: briefsQuery.pendingCount,
      activeAgentJobCount,
      networkProbe: networkProbe.probeReach,
      showHealth: true,
    })
    return worstBayScanSignal(bays)
  }, [
    snapshot,
    operateQueueQuery.data?.open.length,
    briefsQuery.pendingCount,
    activeAgentJobCount,
    networkProbe.probeReach,
  ])
}
