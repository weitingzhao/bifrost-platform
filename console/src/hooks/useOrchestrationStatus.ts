import { useQuery } from '@tanstack/react-query'
import {
  fetchOrchestrationStatus,
  isResearchProxyError,
  type OrchestrationScheduleRow,
  type OrchestrationStatusData,
} from '@/api/researchEngine'

export const ORCHESTRATION_STATUS_QUERY_KEY = [
  'research',
  'orchestration',
  'status',
] as const

export function useOrchestrationStatus(opts?: { enabled?: boolean }) {
  const q = useQuery({
    queryKey: ORCHESTRATION_STATUS_QUERY_KEY,
    queryFn: () => fetchOrchestrationStatus(),
    enabled: opts?.enabled !== false,
    refetchInterval: 30_000,
    staleTime: 10_000,
    retry: 1,
  })

  const data: OrchestrationStatusData | null =
    q.data != null && !isResearchProxyError(q.data) && q.data.ok !== false
      ? q.data.data
      : null

  const byName = new Map<string, OrchestrationScheduleRow>()
  for (const row of data?.schedules ?? []) {
    byName.set(row.name, row)
  }

  return {
    data,
    byName,
    isLoading: q.isLoading,
    isError: q.isError || (q.data != null && isResearchProxyError(q.data)),
    refetch: () => void q.refetch(),
  }
}
