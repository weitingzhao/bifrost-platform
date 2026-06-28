import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchCluster,
  fetchSupplyChain,
  fetchStgSmoke,
  fetchSelfHealth,
  fetchRemediationHealth,
  fetchAgentBridge,
  fetchMatrix,
  isAllMatrices,
} from '@/api/platform'
import type { MatrixResponse } from '@/api/types'
import { buildMissionSnapshot, type MissionSnapshot } from '@/lib/control-room/missionSignals'

const REFETCH = 20_000

export function useMissionSnapshot(): {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  dataUpdatedAt: number
  isLoading: boolean
} {
  const clusterQ = useQuery({ queryKey: ['cockpit', 'cluster'], queryFn: fetchCluster, refetchInterval: REFETCH })
  const supplyQ = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain, refetchInterval: REFETCH })
  const stgQ = useQuery({ queryKey: ['cockpit', 'stg-smoke'], queryFn: fetchStgSmoke, refetchInterval: REFETCH })
  const selfQ = useQuery({ queryKey: ['cockpit', 'self-health'], queryFn: fetchSelfHealth, refetchInterval: REFETCH })
  const runnerQ = useQuery({ queryKey: ['cockpit', 'runner'], queryFn: fetchRemediationHealth, refetchInterval: REFETCH })
  const bridgeQ = useQuery({ queryKey: ['cockpit', 'bridge'], queryFn: fetchAgentBridge, refetchInterval: REFETCH })
  const matrixQ = useQuery({ queryKey: ['cockpit', 'matrix'], queryFn: () => fetchMatrix(), refetchInterval: REFETCH })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQ.data
    if (!data) return []
    return isAllMatrices(data) ? data.matrices : [data]
  }, [matrixQ.data])

  const snapshot = useMemo(
    () =>
      buildMissionSnapshot({
        cluster: clusterQ.data,
        supply: supplyQ.data,
        stg: stgQ.data,
        self: selfQ.data,
        runner: runnerQ.data,
        bridge: bridgeQ.data,
        matrices,
      }),
    [clusterQ.data, supplyQ.data, stgQ.data, selfQ.data, runnerQ.data, bridgeQ.data, matrices],
  )

  const isLoading =
    clusterQ.isLoading || supplyQ.isLoading || stgQ.isLoading || selfQ.isLoading || runnerQ.isLoading || bridgeQ.isLoading || matrixQ.isLoading

  const dataUpdatedAt = Math.max(
    clusterQ.dataUpdatedAt,
    supplyQ.dataUpdatedAt,
    stgQ.dataUpdatedAt,
    selfQ.dataUpdatedAt,
    runnerQ.dataUpdatedAt,
    bridgeQ.dataUpdatedAt,
    matrixQ.dataUpdatedAt,
  )

  return { snapshot, matrices, dataUpdatedAt, isLoading }
}
