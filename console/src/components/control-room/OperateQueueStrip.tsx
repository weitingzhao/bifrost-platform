import { Button, DenseTag } from '@bifrost/ui'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { useCloseOperateQueueItem } from '@/hooks/useCloseOperateQueueItem'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { OpsSection } from '@/components/layout/OpsSection'

export function OperateQueueStrip() {
  const queueQuery = useOperateQueue()
  const closeMutation = useCloseOperateQueueItem()
  const { canOperate } = usePlatformAuth()
  const openItems = queueQuery.data?.open ?? []

  if (queueQuery.isLoading) {
    return (
      <OpsSection
        title="Operate queue"
        description="Open handoff items from approved post-completion (Projection layer · D11)."
        bodyPadding="compact"
      >
        <p className="m-0 text-dense-meta text-muted-foreground">Loading operate queue…</p>
      </OpsSection>
    )
  }

  if (openItems.length === 0) {
    return null
  }

  return (
    <OpsSection
      title="Operate queue"
      description="Owner-approved post-completion items ready for day-to-day ops work."
      bodyPadding="compact"
      overflow="visible"
    >
      <ul className="m-0 flex list-none flex-col gap-2 p-0">
        {openItems.map(item => (
          <li
            key={item.id}
            className="flex flex-wrap items-start justify-between gap-2 rounded-md border border-border bg-background px-3 py-2"
          >
            <div className="min-w-0">
              <p className="m-0 text-dense-label font-medium">{item.title}</p>
              {item.description && (
                <p className="m-0 mt-0.5 text-dense-meta text-muted-foreground">{item.description}</p>
              )}
              <div className="mt-1 flex flex-wrap items-center gap-1.5">
                <DenseTag variant="warning">open</DenseTag>
                <span className="text-dense-caption text-muted-foreground">{item.program_id}</span>
                {item.lane && (
                  <DenseTag variant="category">{item.lane}</DenseTag>
                )}
              </div>
            </div>
            {canOperate && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={closeMutation.isPending}
                onClick={() => closeMutation.mutate(item.id)}
              >
                Resolve
              </Button>
            )}
          </li>
        ))}
      </ul>
    </OpsSection>
  )
}
