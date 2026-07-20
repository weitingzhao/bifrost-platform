import { DenseTag } from '@bifrost/ui'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePendingDecisionBriefs } from '@/hooks/useDecisionBriefs'
import { OpsSection } from '@/components/layout/OpsSection'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'

/**
 * Control Room projection only — full queue list + Start/Dismiss live in
 * TCC Execution and Agent Desk. Avoid a third full copy of the same handoffs.
 */
export function OperateQueueStrip({
  onOpenAgentDesk,
}: {
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}) {
  const queueQuery = useOperateQueue()
  const briefsQuery = usePendingDecisionBriefs()
  const openCount = queueQuery.data?.open.length ?? 0
  const pendingBriefs = briefsQuery.pendingCount

  if (queueQuery.isLoading && briefsQuery.isLoading) {
    return (
      <OpsSection title="Operate queue" bodyPadding="compact">
        <p className="m-0 text-dense-meta text-muted-foreground">Loading operate queue…</p>
      </OpsSection>
    )
  }

  if (openCount === 0 && pendingBriefs === 0) {
    return null
  }

  const summary =
    pendingBriefs > 0 && openCount > 0
      ? `${openCount} handoff${openCount === 1 ? '' : 's'} + ${pendingBriefs} decision${pendingBriefs === 1 ? '' : 's'} awaiting`
      : pendingBriefs > 0
        ? `${pendingBriefs} decision${pendingBriefs === 1 ? '' : 's'} awaiting`
        : 'Handoffs awaiting Start / Prepare / Dismiss'

  return (
    <OpsSection
      title="Operate queue"
      description="Commander summary — full list and execution are in Daily Ops TCC / Agent Desk."
      bodyPadding="compact"
      overflow="visible"
    >
      <div className="flex flex-wrap items-center gap-2">
        {openCount > 0 && (
          <DenseTag variant="warning">{openCount} open</DenseTag>
        )}
        {pendingBriefs > 0 && (
          <DenseTag variant="danger">{pendingBriefs} decisions</DenseTag>
        )}
        <span className="text-dense-meta text-muted-foreground">{summary}</span>
        {onOpenAgentDesk != null ? (
          <button
            type="button"
            className="ml-auto shrink-0 text-dense-caption font-medium text-primary hover:underline"
            onClick={() =>
              onOpenAgentDesk(
                pendingBriefs > 0 ? { focusDecisionBriefs: true } : undefined,
              )
            }
          >
            Open Agent Desk →
          </button>
        ) : (
          <span className="ml-auto text-dense-caption text-muted-foreground">
            Open Agent Desk
          </span>
        )}
      </div>
    </OpsSection>
  )
}
