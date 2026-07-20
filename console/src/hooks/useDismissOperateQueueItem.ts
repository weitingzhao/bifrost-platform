import { useMutation, useQueryClient } from '@tanstack/react-query'
import { dismissOperateQueueItem, OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'
import type { DismissOperateQueueRequest } from '@/api/operateQueueTypes'

export function useDismissOperateQueueItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ itemId, body }: { itemId: string; body: DismissOperateQueueRequest }) =>
      dismissOperateQueueItem(itemId, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
    },
  })
}
