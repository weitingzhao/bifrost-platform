import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster } from '@/api/cluster'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchStgSmoke } from '@/api/promote'
import { fetchSelfHealth, fetchMatrix, isAllMatrices } from '@/api/core'
import { fetchRemediationHealth } from '@/api/remediation'
import { fetchAgentBridge } from '@/api/agentOps'
import type { MatrixResponse } from '@/api/matrixTypes'
import { useNowMs } from '@/hooks/useNowMs'
import { buildMissionSnapshot, type MissionSnapshot } from '@/lib/control-room/missionSignals'

const REFETCH = 20_000

/** A probe whose last answer is older than this (or whose last refetch failed) is stale. */
export const MISSION_STALE_AFTER_MS = 2 * REFETCH

export type MissionSourceId =
  | 'cluster'
  | 'supply-chain'
  | 'stg-smoke'
  | 'self-health'
  | 'runner'
  | 'bridge'
  | 'matrix'

type QueryView<T> = { data: T | undefined; isError: boolean; dataUpdatedAt: number }

/**
 * The data a mission dimension may be judged on: the query's last answer only
 * while it is fresh. TanStack keeps the last success's data when a refetch
 * fails, so without this a failing probe kept its last good verdict (TD-227).
 */
export function freshQueryData<T>(q: QueryView<T>, nowMs: number, maxAgeMs = MISSION_STALE_AFTER_MS): T | undefined {
  if (q.data === undefined || q.isError || q.dataUpdatedAt <= 0) return undefined
  if (nowMs - q.dataUpdatedAt > maxAgeMs) return undefined
  return q.data
}

/**
 * Freshness across the probes: the oldest answer (0 when none has answered)
 * and the probes that are stale — failing, too old, or never answered.
 */
export function missionFreshness(
  sources: { id: MissionSourceId; q: QueryView<unknown> }[],
  nowMs: number,
  maxAgeMs = MISSION_STALE_AFTER_MS,
): { dataUpdatedAt: number; staleSources: MissionSourceId[] } {
  const answered = sources.map(s => s.q.dataUpdatedAt).filter(t => t > 0)
  const staleSources = sources
    .filter(s => freshQueryData(s.q, nowMs, maxAgeMs) === undefined)
    .map(s => s.id)
  return { dataUpdatedAt: answered.length > 0 ? Math.min(...answered) : 0, staleSources }
}

export function useMissionSnapshot(): {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  /** Oldest probe answer across the seven sources (0 = none yet). */
  dataUpdatedAt: number
  /** Probes judged as unknown because they failed, aged out, or never answered. */
  staleSources: MissionSourceId[]
  isLoading: boolean
} {
  const clusterQ = useQuery({ queryKey: ['cockpit', 'cluster'], queryFn: fetchCluster, refetchInterval: REFETCH })
  const supplyQ = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain, refetchInterval: REFETCH })
  const stgQ = useQuery({ queryKey: ['cockpit', 'stg-smoke'], queryFn: fetchStgSmoke, refetchInterval: REFETCH })
  const selfQ = useQuery({ queryKey: ['cockpit', 'self-health'], queryFn: fetchSelfHealth, refetchInterval: REFETCH })
  const runnerQ = useQuery({ queryKey: ['cockpit', 'runner'], queryFn: fetchRemediationHealth, refetchInterval: REFETCH })
  const bridgeQ = useQuery({ queryKey: ['cockpit', 'bridge'], queryFn: fetchAgentBridge, refetchInterval: REFETCH })
  const matrixQ = useQuery({ queryKey: ['cockpit', 'matrix'], queryFn: () => fetchMatrix(), refetchInterval: REFETCH })

  // Ages out a probe that stopped answering without an error (hung, tab paused).
  const nowMs = useNowMs(REFETCH / 2)

  const cluster = freshQueryData(clusterQ, nowMs)
  const supply = freshQueryData(supplyQ, nowMs)
  const stg = freshQueryData(stgQ, nowMs)
  const self = freshQueryData(selfQ, nowMs)
  const runner = freshQueryData(runnerQ, nowMs)
  const bridge = freshQueryData(bridgeQ, nowMs)
  const matrixData = freshQueryData(matrixQ, nowMs)

  const matrices = useMemo((): MatrixResponse[] => {
    if (!matrixData) return []
    return isAllMatrices(matrixData) ? matrixData.matrices : [matrixData]
  }, [matrixData])

  const snapshot = useMemo(
    () => buildMissionSnapshot({ cluster, supply, stg, self, runner, bridge, matrices }),
    [cluster, supply, stg, self, runner, bridge, matrices],
  )

  const isLoading =
    clusterQ.isLoading || supplyQ.isLoading || stgQ.isLoading || selfQ.isLoading || runnerQ.isLoading || bridgeQ.isLoading || matrixQ.isLoading

  const { dataUpdatedAt, staleSources } = missionFreshness(
    [
      { id: 'cluster', q: clusterQ },
      { id: 'supply-chain', q: supplyQ },
      { id: 'stg-smoke', q: stgQ },
      { id: 'self-health', q: selfQ },
      { id: 'runner', q: runnerQ },
      { id: 'bridge', q: bridgeQ },
      { id: 'matrix', q: matrixQ },
    ],
    nowMs,
  )

  return { snapshot, matrices, dataUpdatedAt, staleSources, isLoading }
}
