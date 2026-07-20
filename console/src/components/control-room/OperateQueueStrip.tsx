import { DenseTag } from '@bifrost/ui'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { OpsSection } from '@/components/layout/OpsSection'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'

export function OperateQueueStrip({
  onOpenAgentDesk,
}: {
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}) {
  const queueQuery = useOperateQueue()
  const openItems = queueQuery.data?.open ?? []

  if (queueQuery.isLoading) {
    return (
      <OpsSection
        title="Operate queue"
        description="Open handoff items (diagnosis projection). Execute in Agent Desk."
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
      description="Diagnosis projection of open handoffs — Start / Prepare / Dismiss in Agent Desk."
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
            {onOpenAgentDesk != null ? (
              <button
                type="button"
                className="shrink-0 text-dense-caption font-medium text-primary hover:underline"
                onClick={() => onOpenAgentDesk({ focusHandoffId: item.id })}
              >
                Open in Agent Desk
              </button>
            ) : (
              <span className="text-dense-caption text-muted-foreground">Open in Agent Desk</span>
            )}
          </li>
        ))}
      </ul>
    </OpsSection>
  )
}
