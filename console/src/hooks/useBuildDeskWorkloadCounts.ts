import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster } from '@/api/cluster'
import { fetchContext, fetchMatrix, isAllMatrices } from '@/api/core'
import type { MatrixResponse } from '@/api/matrixTypes'
import {
  computeBuildDeskWorkloadCounts,
  type BuildDeskWorkloadCounts,
} from '@/lib/briefing/buildDeskWorkload'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'
import { useLaneCatalog } from '@/hooks/useLaneCatalog'

/** Sidebar badges for Briefing / In Flight — always enabled (dedupes TanStack keys). */
export function useBuildDeskWorkloadCounts(): BuildDeskWorkloadCounts {
  // Ensure catalog is hydrated before counting Doing lanes (same as Active Session).
  useLaneCatalog()
  const {
    programs,
    releasedByLane,
    programsReady,
  } = useDeliveryProgramClosure()

  const contextQ = useQuery({
    queryKey: ['context'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })
  const matrixQ = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const clusterQ = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQ.data
    if (!data) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQ.data])

  return useMemo(
    () =>
      computeBuildDeskWorkloadCounts({
        programs,
        context: contextQ.data,
        matrices,
        clusterSummary: clusterQ.data,
        releasedByLane,
        programsReady,
      }),
    [programs, contextQ.data, matrices, clusterQ.data, releasedByLane, programsReady],
  )
}
