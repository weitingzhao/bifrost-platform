import { Button, DenseTag } from '@bifrost/ui'
import { ClipboardList } from 'lucide-react'
import { BriefingStatusBadge } from '@/components/briefing/BriefingStatusChrome'
import { markBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import { resolveSessionLaneFocus } from '@/lib/task-mode/sessionLaneFocus'
import { workIntentById } from '@/lib/briefing/workIntents'
import { componentLineForTaskMode, trackTypeForTaskMode } from '@/lib/briefing/briefingViewTabs'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import { queueItemToBriefingStatus } from '@/lib/briefing/briefingStatus'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'

export type TaskBriefingLauncherProps = {
  mode: TaskModeDef
  programId?: string
  inlinePack: InlineBriefingPackResult
  hasActiveSession?: boolean
  programSigned?: number
  programPhaseCount?: number
  onBriefingOpened?: () => void
  onOpenFullBriefing?: (opts?: BriefingUrlState) => void
  onNavigate?: (tabId: string) => void
}

export function TaskBriefingLauncher({
  mode,
  programId,
  inlinePack,
  hasActiveSession = false,
  programSigned = 0,
  programPhaseCount = 0,
  onBriefingOpened,
  onOpenFullBriefing,
  onNavigate,
}: TaskBriefingLauncherProps) {
  const { programsReleasedFor } = useDeliveryProgramClosure()
  const dev = mode.dev
  if (dev == null) return null

  const resolvedProgramId = programId ?? dev.programId
  const {
    isReady,
    copied,
    copyToClipboard,
    copyError,
    track,
    selectedLaneId,
    laneQueue,
    selectedLane: laneMeta,
    intent,
  } = inlinePack
  const focusLane = selectedLaneId ?? dev.briefingLane

  const focus = resolveSessionLaneFocus({
    queue: laneQueue,
    hasActiveSession,
    hasProgram: resolvedProgramId != null && resolvedProgramId !== '',
    programsReleased: focusLane != null ? programsReleasedFor(focusLane) : undefined,
  })

  const catalogLoading =
    laneMeta != null && laneMeta.description === 'Lane catalog loading…'

  const briefingOpts: BriefingUrlState = {
    view: laneMeta?.componentLine ?? componentLineForTaskMode(mode.id),
    trackType: laneMeta?.trackType ?? trackTypeForTaskMode(mode.id),
    track: laneMeta?.track ?? dev.briefingTrack,
    lane: selectedLaneId ?? dev.briefingLane,
    intent: intent ?? dev.briefingIntent,
    pack: 'compact',
    taskModeContext: {
      modeId: mode.id,
      modeLabel: mode.label,
      loopArchetype: mode.loopArchetype,
      programId: resolvedProgramId,
    },
  }

  const handleCopy = async () => {
    const ok = await copyToClipboard()
    if (!ok) return
    markBriefingOpened(mode.id, resolvedProgramId)
    onBriefingOpened?.()
  }

  const ctaLabel = copied ? 'Copied ✓' : isReady ? 'Copy session pack' : 'Preparing…'
  const intentLabel = intent != null ? workIntentById(intent).shortLabel : undefined
  const progressLabel =
    focus.progress != null ? `${focus.progress.done}/${focus.progress.total}` : null

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <ClipboardList size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Briefing</span>
        {track != null && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Track · {track}
          </span>
        )}
        {intentLabel != null && <DenseTag variant="info">Intent · {intentLabel}</DenseTag>}
      </div>

      <div className="mt-2.5 overflow-hidden rounded-md border border-border/60 bg-background">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 px-2.5 py-1.5">
          <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
            Session lane
          </span>
          <span className="flex items-center gap-1.5">
            <BriefingStatusBadge status={focus.status} />
            {progressLabel != null && (
              <span className="font-mono text-[var(--text-dense-caption)] text-muted-foreground">
                {progressLabel}
              </span>
            )}
          </span>
        </header>
        <div className="flex flex-col gap-1 px-2.5 py-2">
          {hasActiveSession && catalogLoading ? (
            <span className="text-[var(--text-dense-label)] font-semibold text-muted-foreground">
              Loading lane catalog…
            </span>
          ) : hasActiveSession && laneMeta != null ? (
            <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <span className="text-[var(--text-dense-label)] font-semibold">
                {laneMeta.shortLabel}
              </span>
              <span className="font-mono text-[var(--text-dense-caption)] text-muted-foreground">
                {laneMeta.id}
              </span>
              {resolvedProgramId != null && programPhaseCount > 0 && (
                <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                  · program {programSigned}/{programPhaseCount} signed
                </span>
              )}
            </div>
          ) : (
            <span className="text-[var(--text-dense-label)] font-semibold">No Active Session</span>
          )}
          <p className="m-0 text-[var(--text-dense-meta)]">{focus.line}</p>
          {focus.nextItem != null && (
            <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
              Next:{' '}
              <span className="text-foreground">{focus.nextItem.label}</span>
              {' · '}
              {queueItemToBriefingStatus(focus.nextItem.status) === 'blocked'
                ? 'blocked'
                : 'pending'}
            </p>
          )}
        </div>
      </div>

      <div className="mt-2.5 flex flex-wrap items-center gap-2">
        {hasActiveSession && (
          <Button
            variant={copied ? 'default' : 'secondary'}
            size="xs"
            disabled={!isReady || copied}
            onClick={() => void handleCopy()}
          >
            {ctaLabel}
          </Button>
        )}
        {focus.kind === 'signoff' && onNavigate != null && (
          <Button variant="secondary" size="xs" onClick={() => onNavigate('delivery-board')}>
            Delivery →
          </Button>
        )}
        {onOpenFullBriefing != null && (
          <button
            type="button"
            className="text-[var(--text-dense-meta)] text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => onOpenFullBriefing(briefingOpts)}
          >
            {hasActiveSession ? 'Change lane in Briefing →' : 'Full Briefing →'}
          </button>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
        {hasActiveSession
          ? 'Pack is scoped to the Active Session lane. Paste into a new Cursor chat.'
          : 'TCC follows the Active Session — pick a lane in Agent Briefing first.'}
      </p>
      {copyError != null && (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-destructive">{copyError}</p>
      )}
    </div>
  )
}
