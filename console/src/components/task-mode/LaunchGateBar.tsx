import { cn } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import {
  launchVerdictToSignal,
  readinessAnchorDomId,
  type LaunchCheckpoint,
  type LaunchVerdict,
} from '@/lib/task-mode/satelliteLaunchVerdict'

export type LaunchGateBarProps = {
  verdict: LaunchVerdict
  checkpoints: LaunchCheckpoint[]
  /** row = full-width strip; column = Task CC 1/4 Launch rail */
  layout?: 'row' | 'column'
  /** Agent Fix — only for NO_GO env blocks (not auth). */
  onAgentFix?: () => void
  agentFixPending?: boolean
  agentFixActive?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
  agentFixActiveLabel?: string
  onOpenAgentDesk?: () => void
  /** Primary launch CTA when GO; when NO_GO shows blocked outline control. */
  onLaunch?: () => void
  launchLabel?: string
  blockedLabel?: string
  launchPending?: boolean
  launchDisabled?: boolean
  launchDisabledReason?: string
  onOpenDetail?: () => void
  detailLabel?: string
  onOpenActiveRun?: () => void
  openActiveRunLabel?: string
}

function scrollToReadinessAnchor(anchor: NonNullable<LaunchCheckpoint['readinessAnchor']>) {
  const el = document.getElementById(readinessAnchorDomId(anchor))
  if (el == null) return
  el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  el.classList.add('ring-1', 'ring-primary/50')
  window.setTimeout(() => {
    el.classList.remove('ring-1', 'ring-primary/50')
  }, 1200)
}

/**
 * Task CC launch gate — live Go/No-Go lamps + Fix/Deploy CTA.
 * Checkpoint lamps map 1:1 onto Environment Readiness / Recent panels (click to focus).
 */
export function LaunchGateBar({
  verdict,
  checkpoints,
  layout = 'row',
  onAgentFix,
  agentFixPending,
  agentFixActive = false,
  agentFixDisabled,
  agentFixTitle,
  agentFixActiveLabel = 'View agent →',
  onOpenAgentDesk,
  onLaunch,
  launchLabel = 'Agent Deploy',
  blockedLabel = 'Deploy blocked',
  launchPending,
  launchDisabled,
  launchDisabledReason,
  onOpenDetail,
  detailLabel = 'Detail →',
  onOpenActiveRun,
  openActiveRunLabel = 'Open active run →',
}: LaunchGateBarProps) {
  const column = layout === 'column'
  const tone =
    verdict.kind === 'GO'
      ? 'success'
      : verdict.kind === 'IN_FLIGHT'
        ? 'info'
        : agentFixActive || agentFixPending
          ? 'info'
          : 'warning'

  const title =
    verdict.kind === 'NO_GO' && (agentFixActive || agentFixPending)
      ? agentFixPending
        ? 'Starting Agent Fix…'
        : 'Agent Fix running — follow chrome banner'
      : verdict.title

  const canLaunch = verdict.kind === 'GO' && !launchDisabled
  const showFix =
    verdict.kind === 'NO_GO' && verdict.blockKind !== 'auth' && onAgentFix != null
  /** When Fix is already running, local CTA is a shortcut to Desk; primary surface is chrome banner. */
  const fixRunning = agentFixActive && !agentFixPending

  const fixButton = showFix ? (
    <AgentTriggerButton
      label="Agent Fix"
      size="xs"
      pending={agentFixPending}
      active={fixRunning}
      activeLabel={agentFixActiveLabel}
      disabled={agentFixDisabled && !agentFixActive}
      title={
        fixRunning
          ? 'Open Agent Desk — live progress is also in the chrome banner above'
          : (agentFixTitle ?? 'Start Cluster · Remediate')
      }
      onClick={() => {
        if (fixRunning && onOpenAgentDesk != null) {
          onOpenAgentDesk()
          return
        }
        onAgentFix()
      }}
      className={column ? 'w-full justify-center' : undefined}
    />
  ) : null

  const launchOrActive =
    verdict.kind === 'IN_FLIGHT' && onOpenActiveRun != null ? (
      <button
        type="button"
        className="text-[var(--text-dense-caption)] text-primary hover:underline"
        onClick={onOpenActiveRun}
      >
        {openActiveRunLabel}
      </button>
    ) : onLaunch != null ? (
      <AgentTriggerButton
        label={canLaunch ? launchLabel : blockedLabel}
        size="xs"
        pending={launchPending}
        disabled={!canLaunch}
        title={launchDisabledReason ?? (canLaunch ? launchLabel : verdict.disabledReason)}
        onClick={onLaunch}
        className={column ? 'w-full justify-center' : undefined}
      />
    ) : null

  const detailLink =
    onOpenDetail != null ? (
      <button
        type="button"
        className={cn(
          'text-[var(--text-dense-caption)] text-primary hover:underline',
          column && 'text-left',
        )}
        onClick={onOpenDetail}
      >
        {detailLabel}
      </button>
    ) : null

  return (
    <div
      className={cn(
        'ops-feedback',
        `ops-feedback--${tone}`,
        column
          ? 'flex flex-col gap-1.5'
          : 'flex flex-wrap items-center gap-x-3 gap-y-1.5',
      )}
      role="status"
      aria-label="Launch gate"
    >
      {!column && (
        <div className="flex min-w-0 items-center gap-1.5">
          <StatusLamp value={launchVerdictToSignal(verdict.kind)} kind="reach" />
          <span className="text-[var(--text-dense-meta)] font-semibold uppercase tracking-wide">
            Launch
          </span>
        </div>
      )}

      {column && (
        <div className="flex min-w-0 items-center gap-1.5">
          <StatusLamp value={launchVerdictToSignal(verdict.kind)} kind="reach" />
          <span
            className={cn(
              'min-w-0 truncate text-[var(--text-dense-caption)]',
              verdict.kind === 'GO' ? 'text-muted-foreground' : 'font-medium text-warning',
            )}
            title={verdict.detail}
          >
            {title}
          </span>
        </div>
      )}

      <ul
        className={cn(
          'm-0 flex list-none p-0',
          column ? 'flex-col gap-0.5' : 'flex-wrap items-center gap-x-2.5 gap-y-0.5',
        )}
      >
        {checkpoints.map(cp => {
          const lampSignal = cp.signal ?? (cp.ok ? 'ok' : 'fail')
          const canFocus = cp.readinessAnchor != null
          const label = (
            <>
              <StatusLamp value={lampSignal} kind="reach" />
              <span className={cn('truncate', cp.ok ? 'text-muted-foreground' : 'font-medium text-warning')}>
                {cp.label}
                {!cp.ok && cp.detail != null && cp.detail !== '' ? ` · ${cp.detail}` : ''}
              </span>
            </>
          )
          return (
            <li
              key={cp.id}
              className={cn(
                'min-w-0 items-center gap-1 text-[var(--text-dense-caption)]',
                column ? 'flex' : 'inline-flex',
              )}
              title={
                canFocus
                  ? `${cp.detail ?? cp.label} — click to open matching readiness panel`
                  : cp.detail
              }
            >
              {canFocus ? (
                <button
                  type="button"
                  className="inline-flex min-w-0 max-w-full items-center gap-1 rounded px-0.5 hover:bg-primary/10"
                  onClick={() => scrollToReadinessAnchor(cp.readinessAnchor!)}
                >
                  {label}
                </button>
              ) : (
                <span className="inline-flex min-w-0 items-center gap-1">{label}</span>
              )}
            </li>
          )
        })}
      </ul>

      {!column && (
        <p
          className={cn(
            'm-0 min-w-0 flex-1 truncate text-[var(--text-dense-caption)]',
            verdict.kind === 'GO' ? 'text-muted-foreground' : 'font-medium text-warning',
          )}
          title={verdict.detail}
        >
          {title}
        </p>
      )}

      <div
        className={cn(
          'flex',
          column ? 'w-full flex-col gap-1.5' : 'ml-auto flex-wrap items-center gap-2',
        )}
      >
        {fixButton}
        {launchOrActive}
        {detailLink}
      </div>
    </div>
  )
}
