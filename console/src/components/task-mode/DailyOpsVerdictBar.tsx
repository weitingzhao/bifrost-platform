import { DenseTag } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { ViewerEnvBadge } from '@/components/task-mode/ViewerEnvBadge'
import type { FleetSnapshot, FleetVerdictKind } from '@/lib/control-room/fleetSnapshot'

const VERDICT_VARIANT: Record<
  FleetVerdictKind,
  'success' | 'warning' | 'danger'
> = {
  GO: 'success',
  HOLD: 'warning',
  'NO-GO': 'danger',
}

export function DailyOpsVerdictBar({
  fleet,
  isLoading,
  canOperate,
  agentFixPending,
  onPrimaryCta,
  onNavigate,
}: {
  fleet: FleetSnapshot
  isLoading?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  onPrimaryCta: () => void
  onNavigate: (tabId: string) => void
}) {
  const { verdict, viewerEnv } = fleet
  const cta = verdict.primaryCta

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <ViewerEnvBadge viewerEnv={viewerEnv} isLoading={isLoading} />
        <DenseTag variant={VERDICT_VARIANT[verdict.kind]}>
          {isLoading ? 'Probing…' : verdict.kind}
        </DenseTag>
        <span className="min-w-0 flex-1 text-[var(--text-dense-meta)] text-muted-foreground">
          {verdict.topReason}
        </span>
        {cta.kind === 'agent-fix' && (
          <AgentTriggerButton
            label={cta.label}
            size="xs"
            pending={agentFixPending}
            disabled={!canOperate || agentFixPending}
            title={
              !canOperate
                ? 'Authenticate as operator to run Agent Fix'
                : verdict.worstCell?.detail
            }
            onClick={onPrimaryCta}
          />
        )}
        {cta.kind === 'navigate' && cta.tabId != null && (
          <button
            type="button"
            className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-meta)] font-medium text-primary hover:bg-muted"
            onClick={() => onNavigate(cta.tabId!)}
          >
            {cta.label} →
          </button>
        )}
        {cta.kind === 'none' && (
          <DenseTag variant="success" className="text-[9px]">
            {cta.label}
          </DenseTag>
        )}
      </div>
    </div>
  )
}
