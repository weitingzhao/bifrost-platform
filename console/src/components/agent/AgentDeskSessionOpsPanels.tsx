import { DeskFoldableSection } from '@/components/agent/DeskFoldableSection'
import { NightlyBriefingPanel } from '@/components/agent/NightlyBriefingPanel'
import { OperateQueueHandoffPanel } from '@/components/agent/OperateQueueHandoffPanel'
import { DecisionBriefPanel } from '@/components/operate/DecisionBriefPanel'
import { usePendingDecisionBriefs } from '@/hooks/useDecisionBriefs'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import type { OperateQueueItem } from '@/api/operateQueueTypes'

export type AgentDeskSessionOpsPanelsProps = {
  mode?: 'operate' | 'review' | 'all'
  onPrepareHandoffAgent?: (item: OperateQueueItem) => void
  onStartHandoffAgent?: (item: OperateQueueItem) => void
  onObserveHandoffJob?: (jobId: string) => void
  onNavigateRecurringSetup?: () => void
  focusHandoffId?: string | null
  onFocusHandoffConsumed?: () => void
  focusDecisionBriefId?: string | null
  onFocusDecisionBriefConsumed?: () => void
}

/**
 * Ops Desk Queue panels — decision briefs and operate queue handoffs (Operate),
 * recently closed handoffs and the nightly agent report (Review).
 */
export function AgentDeskSessionOpsPanels({
  mode = 'all',
  onPrepareHandoffAgent,
  onStartHandoffAgent,
  onObserveHandoffJob,
  onNavigateRecurringSetup,
  focusHandoffId,
  onFocusHandoffConsumed,
  focusDecisionBriefId,
  onFocusDecisionBriefConsumed,
}: AgentDeskSessionOpsPanelsProps) {
  const operateQueueQuery = useOperateQueue()
  const decisionBriefsQuery = usePendingDecisionBriefs({
    enabled: mode !== 'review',
  })

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {mode !== 'review' && (
        <>
          {(decisionBriefsQuery.pendingCount > 0 || decisionBriefsQuery.isLoading) && (
            <DeskFoldableSection
              kicker="Operate"
              title="Decision briefs"
              description="Owner decisions for ambiguous or high-risk queue items before remediation runs."
              defaultExpanded
              badge={
                decisionBriefsQuery.pendingCount > 0
                  ? String(decisionBriefsQuery.pendingCount)
                  : undefined
              }
              badgeVariant="warning"
            >
              <DecisionBriefPanel
                focusBriefId={focusDecisionBriefId}
                onFocusBriefConsumed={onFocusDecisionBriefConsumed}
              />
            </DeskFoldableSection>
          )}
          <DeskFoldableSection
            kicker="Operate"
            title="Operate queue handoffs"
            description="Open handoffs awaiting execution. Open the source or prepare an Agent task; close only after the work is complete."
            defaultExpanded
            badge={
              (operateQueueQuery.data?.open.length ?? 0) > 0
                ? String(operateQueueQuery.data?.open.length)
                : undefined
            }
            badgeVariant="warning"
          >
            <OperateQueueHandoffPanel
              items={operateQueueQuery.data?.open ?? []}
              loading={operateQueueQuery.isLoading}
              onPrepareAgent={onPrepareHandoffAgent}
              onStartAgent={onStartHandoffAgent}
              onObserveJob={onObserveHandoffJob}
              onNavigateSetup={onNavigateRecurringSetup}
              focusHandoffId={focusHandoffId}
              onFocusHandoffConsumed={onFocusHandoffConsumed}
            />
          </DeskFoldableSection>
        </>
      )}

      {mode !== 'operate' && (
        <>
          {(operateQueueQuery.data?.recent_closed.length ?? 0) > 0 && (
            <DeskFoldableSection
              kicker="Review"
              title="Recently closed handoffs"
              description="Verified closure evidence retained by the Operate Queue."
              defaultExpanded={false}
              badge={String(operateQueueQuery.data?.recent_closed.length ?? 0)}
              badgeVariant="success"
            >
              <OperateQueueHandoffPanel
                items={operateQueueQuery.data?.recent_closed ?? []}
                onObserveJob={onObserveHandoffJob}
              />
            </DeskFoldableSection>
          )}
          <DeskFoldableSection
            kicker="Automation"
            title="Nightly agent report & drift proposals"
            description="Layer 1–4 scan from agent host. Owner approval required for fixes."
            defaultExpanded={false}
          >
            <NightlyBriefingPanel />
          </DeskFoldableSection>
        </>
      )}
    </div>
  )
}
