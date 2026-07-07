import { useMutation, useQueryClient } from '@tanstack/react-query'
import { closeOperateQueueItem, OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'

export function useCloseOperateQueueItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (itemId: string) => closeOperateQueueItem(itemId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
    },
  })
}
