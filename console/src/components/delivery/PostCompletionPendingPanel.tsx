import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  approvePostCompletionItem,
  fetchPendingPostCompletion,
  fetchProgramDetail,
  PROGRAMS_BOARD_QUERY_KEY,
} from '@/api/programs'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export function PostCompletionPendingPanel({ programId }: { programId?: string }) {
  const { canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [approveId, setApproveId] = useState<string | null>(null)

  const pendingQuery = useQuery({
    queryKey: ['programs', 'post-completion', 'pending'],
    queryFn: fetchPendingPostCompletion,
    refetchInterval: 30_000,
  })

  const detailQuery = useQuery({
    queryKey: ['programs', programId ?? ''],
    queryFn: () => fetchProgramDetail(programId!),
    enabled: programId != null,
  })

  const approveMutation = useMutation({
    mutationFn: (itemId: string) => approvePostCompletionItem(itemId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', 'post-completion', 'pending'] })
      if (programId) void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_BOARD_QUERY_KEY })
      setApproveId(null)
    },
  })

  const globalItems = pendingQuery.data?.items ?? []
  const programItems = detailQuery.data?.pending_post_completion_items ?? []
  const items =
    programId != null
      ? programItems
      : globalItems.filter(i => i.status === 'pending_review')

  if (items.length === 0) {
    return null
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-secondary/30 px-3 py-2">
      <p className="text-dense-label font-medium m-0">Post-completion — pending Owner review</p>
      <p className="text-dense-meta text-muted-foreground m-0">
        Approve items before they enter the Operate queue.
      </p>
      <ul className="m-0 flex flex-col gap-2 p-0 list-none">
        {items.map(item => (
          <li key={item.id} className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <span className="font-medium">{item.title}</span>
              {item.description && (
                <p className="text-dense-meta text-muted-foreground m-0 mt-0.5">{item.description}</p>
              )}
              <DenseTag variant="warning" className="mt-1">
                pending_review
              </DenseTag>
            </div>
            {canAdmin && (
              <Button type="button" size="sm" variant="outline" onClick={() => setApproveId(item.id)}>
                Approve for Operate queue
              </Button>
            )}
          </li>
        ))}
      </ul>

      <ConfirmDialog
        open={approveId != null}
        title="Approve operate queue item"
        message="This injects the item into the Operate queue after approval."
        confirmLabel="Approve"
        confirming={approveMutation.isPending}
        onConfirm={() => {
          if (approveId) approveMutation.mutate(approveId)
        }}
        onCancel={() => setApproveId(null)}
      />
    </div>
  )
}
