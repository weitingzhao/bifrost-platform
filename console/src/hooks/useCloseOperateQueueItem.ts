import { useMutation, useQueryClient } from '@tanstack/react-query'
import { closeOperateQueueItem, OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'
import type { CloseOperateQueueRequest } from '@/api/operateQueueTypes'

export function useCloseOperateQueueItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ itemId, body }: { itemId: string; body: CloseOperateQueueRequest }) =>
      closeOperateQueueItem(itemId, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
    },
  })
}
