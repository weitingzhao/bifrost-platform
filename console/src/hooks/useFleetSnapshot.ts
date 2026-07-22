import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster } from '@/api/cluster'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchStgSmoke } from '@/api/promote'
import { fetchSelfHealth, fetchMatrix, isAllMatrices } from '@/api/core'
import { fetchRemediationHealth } from '@/api/remediation'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchIbGatewayStatus } from '@/api/network'
import type { MatrixResponse } from '@/api/matrixTypes'
import { buildFleetSnapshot } from '@/lib/control-room/buildFleetSnapshot'
import {
  normalizeViewerEnv,
  type FleetSnapshot,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { buildMissionSnapshot, type MissionSnapshot } from '@/lib/control-room/missionSignals'

const REFETCH = 20_000

/**
 * Mac seat readiness for Engineer row (not a board column).
 * Prod/STG viewers cannot reach Mac 127.0.0.1 — only mark ready when bridge reports ok
 * AND viewer is on a Mac-adjacent seat (dev / dev-local).
 */
export function resolveGroundBridgeReady(
  viewerEnv: FleetViewerEnv,
  bridgeStatus: string | undefined,
): boolean {
  if (viewerEnv === 'prod' || viewerEnv === 'stg') return false
  return bridgeStatus === 'ok'
}

export function useFleetSnapshot(): {
  fleet: FleetSnapshot
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  viewerEnv: FleetViewerEnv
  /** True until self-health returns viewer_env (avoid wrong seat badge flash). */
  viewerEnvLoading: boolean
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
  const ibGatewayQ = useQuery({
    queryKey: ['cockpit', 'ib-gateway'],
    queryFn: fetchIbGatewayStatus,
    refetchInterval: REFETCH,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQ.data
    if (!data) return []
    return isAllMatrices(data) ? data.matrices : [data]
  }, [matrixQ.data])

  // Avoid DEV→PROD badge flash: wait for self-health payload before showing a seat label.
  const viewerEnvLoading = selfQ.data == null

  const viewerEnv = useMemo(
    () => normalizeViewerEnv(selfQ.data?.viewer_env ?? import.meta.env.VITE_OPS_VIEWER_ENV),
    [selfQ.data?.viewer_env],
  )

  const groundBridgeReady = useMemo(
    () => resolveGroundBridgeReady(viewerEnv, bridgeQ.data?.satellite_probe_bridge?.status),
    [viewerEnv, bridgeQ.data?.satellite_probe_bridge?.status],
  )

  const fleet = useMemo(
    () =>
      buildFleetSnapshot({
        viewerEnv,
        cluster: clusterQ.data,
        supply: supplyQ.data,
        stg: stgQ.data,
        self: selfQ.data,
        runner: runnerQ.data,
        bridge: bridgeQ.data,
        matrices,
        groundBridgeReady,
        ibGateway: ibGatewayQ.data,
      }),
    [
      viewerEnv,
      clusterQ.data,
      supplyQ.data,
      stgQ.data,
      selfQ.data,
      runnerQ.data,
      bridgeQ.data,
      matrices,
      groundBridgeReady,
      ibGatewayQ.data,
    ],
  )

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
    clusterQ.isLoading ||
    supplyQ.isLoading ||
    stgQ.isLoading ||
    selfQ.isLoading ||
    runnerQ.isLoading ||
    bridgeQ.isLoading ||
    matrixQ.isLoading ||
    ibGatewayQ.isLoading

  const dataUpdatedAt = Math.max(
    clusterQ.dataUpdatedAt,
    supplyQ.dataUpdatedAt,
    stgQ.dataUpdatedAt,
    selfQ.dataUpdatedAt,
    runnerQ.dataUpdatedAt,
    bridgeQ.dataUpdatedAt,
    matrixQ.dataUpdatedAt,
    ibGatewayQ.dataUpdatedAt,
  )

  return { fleet, snapshot, matrices, viewerEnv, viewerEnvLoading, dataUpdatedAt, isLoading }
}
