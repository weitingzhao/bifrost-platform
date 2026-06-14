import type { ClusterSummary, OpsContextResponse } from '@/api/types'
import { ciModeLabel, showGitOpsPlannedBadge } from '@/lib/delivery/deliveryPhase'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'

const STATUS_CLASS: Record<string, string> = {
  CLOSED: 'badge-ui badge-status-closed',
  SIGNED: 'badge-ui badge-status-signed',
  IN_PROGRESS: 'badge-ui badge-status-progress',
  BLOCKED_ON: 'badge-ui badge-status-blocked',
  NOT_STARTED: 'badge-ui badge-status-pending',
  DEPLOYED: 'badge-ui badge-status-deployed',
}

export function milestoneStatusClass(status: string): string {
  return STATUS_CLASS[status] ?? 'badge-ui'
}

export function flywheelLabel(code: string): string {
  if (code === 'A') return 'Flywheel A (Trade / product)'
  if (code === 'B') return 'Flywheel B (Runtime / ops)'
  return code
}

interface FocusStripProps {
  context: OpsContextResponse | undefined
  isLoading: boolean
  matrixUpdatedAt: string | null
  clusterSummary?: ClusterSummary
  clusterLoading?: boolean
  onOpenProgram?: () => void
  onOpenDelivery?: () => void
  onOpenCluster?: () => void
}

export function FocusStrip({
  context,
  isLoading,
  matrixUpdatedAt,
  clusterSummary,
  clusterLoading,
  onOpenProgram,
  onOpenDelivery,
  onOpenCluster,
}: FocusStripProps) {
  if (isLoading) {
    return (
      <div className="focus-strip text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Loading focus…
      </div>
    )
  }
  if (!context) {
    return (
      <div className="focus-strip lamp-fail text-[var(--text-dense-meta)]">
        Ops context unavailable
      </div>
    )
  }

  const { focus, deployment } = context
  const ciMode = ciModeLabel(deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(deployment)
  const clusterKpi = clusterLoading
    ? 'Cluster: …'
    : summarizeCluster(clusterSummary).label
  const clusterReach = clusterLoading
    ? 'unknown'
    : (clusterSummary?.reachability ?? 'unknown')
  const matrixAge =
    matrixUpdatedAt != null
      ? `Matrix ${formatAge(matrixUpdatedAt)}`
      : 'Matrix not refreshed'

  return (
    <div className="focus-strip flex flex-wrap items-center gap-x-3 gap-y-1 text-[var(--text-dense-meta)]">
      <span className="font-medium text-[var(--foreground)]">{focus.headline}</span>
      <span className="text-[var(--muted-foreground)]">·</span>
      <span>
        Track <code className="font-mono-tabular">{deployment.active_track}</code>
      </span>
      <span className="text-[var(--muted-foreground)]">·</span>
      <span>
        Phase <code className="font-mono-tabular">{deployment.phase}</code>
      </span>
      <span className="text-[var(--muted-foreground)]">·</span>
      {onOpenDelivery != null ? (
        <button type="button" className="focus-strip-link focus-strip-ci-mode" onClick={onOpenDelivery}>
          CI: {ciMode}
        </button>
      ) : (
        <span className="focus-strip-ci-mode">CI: {ciMode}</span>
      )}
      {gitOpsPlanned && (
        <>
          <span className="text-[var(--muted-foreground)]">·</span>
          <span className="badge-ui badge-status-pending text-[10px]">GitOps planned</span>
        </>
      )}
      <span className="text-[var(--muted-foreground)]">·</span>
      {onOpenCluster != null ? (
        <button
          type="button"
          className={`focus-strip-link ${clusterReach === 'fail' ? 'lamp-warn' : ''}`}
          onClick={onOpenCluster}
        >
          {clusterKpi}
        </button>
      ) : (
        <span className={clusterReach === 'fail' ? 'lamp-warn' : ''}>{clusterKpi}</span>
      )}
      <span className="text-[var(--muted-foreground)]">·</span>
      <span>{flywheelLabel(focus.flywheel_primary)}</span>
      {focus.blocker != null && focus.blocker !== '' && (
        <>
          <span className="text-[var(--muted-foreground)]">·</span>
          {onOpenProgram != null ? (
            <button
              type="button"
              className="focus-strip-link"
              onClick={onOpenProgram}
            >
              Blocked: {focus.blocker}
            </button>
          ) : (
            <span className="lamp-warn">Blocked: {focus.blocker}</span>
          )}
        </>
      )}
      <span className="text-[var(--muted-foreground)]">·</span>
      <span className="text-[var(--muted-foreground)]">{matrixAge}</span>
    </div>
  )
}

function formatAge(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}
