import type { OpsContextResponse } from '@/api/types'

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
  onOpenProgram?: () => void
}

export function FocusStrip({
  context,
  isLoading,
  matrixUpdatedAt,
  onOpenProgram,
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
