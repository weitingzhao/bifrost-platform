import type { ReactNode } from 'react'
import type { ClusterSummary, OpsContextResponse } from '@/api/types'
import { ciModeLabel, showGitOpsPlannedBadge } from '@/lib/delivery/deliveryPhase'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import { cn } from '@bifrost/ui'

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
  if (code === 'A') return 'Flywheel A'
  if (code === 'B') return 'Flywheel B'
  return code
}

function parseHeadlineSegments(headline: string): Array<{ id: string; status: string }> {
  return headline.split(' · ').map((segment) => {
    const trimmed = segment.trim()
    const space = trimmed.lastIndexOf(' ')
    if (space <= 0) return { id: trimmed, status: '' }
    return {
      id: trimmed.slice(0, space),
      status: trimmed.slice(space + 1),
    }
  })
}

function MetaChip({
  children,
  onClick,
  className,
  mono,
}: {
  children: ReactNode
  onClick?: () => void
  className?: string
  mono?: boolean
}) {
  const Tag = onClick != null ? 'button' : 'span'
  return (
    <Tag
      type={onClick != null ? 'button' : undefined}
      onClick={onClick}
      className={cn(
        'inline-flex max-w-full items-center gap-1 rounded-md border border-border bg-background px-2 py-0.5 text-[10px] leading-none text-muted-foreground',
        onClick != null &&
          'cursor-pointer text-primary underline-offset-2 hover:border-primary/40 hover:text-primary',
        mono && 'font-mono tabular-nums',
        className,
      )}
    >
      {children}
    </Tag>
  )
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
      <div className="text-[var(--text-dense-meta)] text-muted-foreground">Loading ops context…</div>
    )
  }
  if (!context) {
    return (
      <div className="text-[var(--text-dense-meta)] text-[color:var(--color-lamp-red)]">
        Ops context unavailable
      </div>
    )
  }

  const { focus, deployment } = context
  const ciMode = ciModeLabel(deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(deployment)
  const clusterKpi = clusterLoading ? 'Cluster …' : summarizeCluster(clusterSummary).label
  const clusterReach = clusterLoading ? 'unknown' : (clusterSummary?.reachability ?? 'unknown')
  const matrixAge =
    matrixUpdatedAt != null ? `Matrix ${formatAge(matrixUpdatedAt)}` : 'Matrix not refreshed'
  const headlineSegments = parseHeadlineSegments(focus.headline)

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        {headlineSegments.map(({ id, status }) => (
          <span
            key={`${id}-${status}`}
            className={cn(
              'badge-ui max-w-full truncate text-[10px]',
              status !== '' ? milestoneStatusClass(status) : undefined,
            )}
            title={`${id} ${status}`.trim()}
          >
            {id}
            {status !== '' ? ` ${status}` : ''}
          </span>
        ))}
        {focus.blocker != null && focus.blocker !== '' && (
          <MetaChip
            onClick={onOpenProgram}
            className="border-[color:var(--color-lamp-red)] text-[color:var(--color-lamp-red)]"
          >
            Blocked: {focus.blocker}
          </MetaChip>
        )}
      </div>

      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <MetaChip mono>
          Track <span className="text-foreground">{deployment.active_track}</span>
        </MetaChip>
        <MetaChip mono>
          Phase <span className="text-foreground">{deployment.phase}</span>
        </MetaChip>
        <MetaChip onClick={onOpenDelivery} mono>
          CI: {ciMode}
        </MetaChip>
        {gitOpsPlanned && (
          <span className="badge-ui badge-status-pending text-[10px]">GitOps planned</span>
        )}
        <MetaChip
          onClick={onOpenCluster}
          className={
            clusterReach === 'fail'
              ? 'border-[color:var(--color-lamp-yellow)] text-[color:var(--color-lamp-yellow)]'
              : undefined
          }
        >
          {clusterKpi}
        </MetaChip>
        <MetaChip>{flywheelLabel(focus.flywheel_primary)}</MetaChip>
        <MetaChip className="text-muted-foreground/80">{matrixAge}</MetaChip>
      </div>
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
