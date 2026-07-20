import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  fetchOperateDrainStatus,
  OPERATE_BRIEFS_QUERY_KEY,
  OPERATE_DRAIN_STATUS_QUERY_KEY,
  OPERATE_SWEEP_LAST_KEY,
  postOperateSweep,
  type SweepRequest,
  type SweepResponse,
} from '@/api/operateBriefs'
import { OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'

export function useOperateSweep() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: SweepRequest = {}) => postOperateSweep(body),
    onSuccess: (result: SweepResponse) => {
      queryClient.setQueryData(OPERATE_SWEEP_LAST_KEY, result)
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: OPERATE_BRIEFS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: OPERATE_DRAIN_STATUS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })
}

export function useLastOperateSweep(): SweepResponse | undefined {
  const queryClient = useQueryClient()
  return queryClient.getQueryData<SweepResponse>(OPERATE_SWEEP_LAST_KEY)
}

export function useOperateDrainStatus(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: OPERATE_DRAIN_STATUS_QUERY_KEY,
    queryFn: fetchOperateDrainStatus,
    refetchInterval: 15_000,
    enabled: options?.enabled ?? true,
    retry: false,
  })
}
