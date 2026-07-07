import { useQuery } from '@tanstack/react-query'
import { fetchOperateQueue, OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'

export function useOperateQueue() {
  return useQuery({
    queryKey: OPERATE_QUEUE_QUERY_KEY,
    queryFn: fetchOperateQueue,
    refetchInterval: 30_000,
  })
}
