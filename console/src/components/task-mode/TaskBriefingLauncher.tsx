import { useState } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { Check, ChevronDown, ChevronRight, ClipboardList } from 'lucide-react'
import { markBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import { workIntentById } from '@/lib/briefing/workIntents'
import { componentLineForTaskMode, trackTypeForTaskMode } from '@/lib/briefing/briefingViewTabs'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { InlineBriefingPackResult, LaneOption } from '@/hooks/useInlineBriefingPack'
import type { QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'

function laneReach(
  progress: LaneOption['progress'],
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (progress == null) return 'unknown'
  if (progress.percent === 100) return 'ok'
  if (progress.percent > 0) return 'degraded'
  return 'unknown'
}

function queueItemReach(status: QueueItemStatus): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch (status) {
    case 'done':
    case 'closed':
      return 'ok'
    case 'in_progress':
    case 'next':
    case 'ready_for_signoff':
      return 'degraded'
    case 'issue':
    case 'blocked':
      return 'fail'
    default:
      return 'unknown'
  }
}

function statusLabel(status: QueueItemStatus): string {
  if (status === 'ready_for_signoff') return 'sign-off'
  return status.replace('_', ' ')
}

function QueueRow({ item }: { item: QueueItem }) {
  return (
    <li className="flex items-center gap-2 px-2 py-1.5">
      <StatusLamp value={queueItemReach(item.status)} kind="reach" />
      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)]">
        {item.label}
      </span>
      {item.progress != null && item.progress.total > 0 && (
        <span className="font-mono text-[var(--text-dense-caption)] text-muted-foreground">
          {item.progress.done}/{item.progress.total}
        </span>
      )}
      <span className="shrink-0 font-mono text-[var(--text-dense-caption)] uppercase text-muted-foreground">
        {statusLabel(item.status)}
      </span>
    </li>
  )
}

export type TaskBriefingLauncherProps = {
  mode: TaskModeDef
  programId?: string
  inlinePack: InlineBriefingPackResult
  onBriefingOpened?: () => void
  onOpenFullBriefing?: (opts?: BriefingUrlState) => void
}

export function TaskBriefingLauncher({
  mode,
  programId,
  inlinePack,
  onBriefingOpened,
  onOpenFullBriefing,
}: TaskBriefingLauncherProps) {
  const dev = mode.dev
  if (dev == null) return null

  const resolvedProgramId = programId ?? dev.programId
  const {
    isReady,
    copied,
    copyToClipboard,
    track,
    laneOptions,
    selectedLaneId,
    selectLane,
    activeQueue,
    completedQueue,
    selectedLane: laneMeta,
    intent,
  } = inlinePack

  const [completedOpen, setCompletedOpen] = useState(false)

  const briefingOpts: BriefingUrlState = {
    view: componentLineForTaskMode(mode.id),
    trackType: trackTypeForTaskMode(mode.id),
    track: dev.briefingTrack,
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

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      {/* Header */}
      <div className="flex flex-wrap items-center gap-2">
        <ClipboardList size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Agent Briefing</span>
        {track != null && <DenseTag variant="neutral">Track · {track}</DenseTag>}
        {intentLabel != null && <DenseTag variant="info">Intent · {intentLabel}</DenseTag>}
      </div>

      {/* Lane selector */}
      {laneOptions.length > 1 && (
        <div className="mt-2.5">
          <p className="m-0 mb-1.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground">
            Select lane
          </p>
          <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3 lg:grid-cols-4">
            {laneOptions.map(({ lane, progress }) => {
              const selected = selectedLaneId === lane.id
              const reach = laneReach(progress)
              return (
                <button
                  key={lane.id}
                  type="button"
                  className={[
                    'flex flex-col rounded-md border px-2.5 py-2 text-left transition-colors',
                    selected
                      ? 'border-primary bg-primary/8 ring-1 ring-primary/25'
                      : 'border-border bg-card hover:bg-secondary/80',
                  ].join(' ')}
                  onClick={() => selectLane(lane.id)}
                >
                  <div className="flex items-center gap-1.5">
                    <StatusLamp value={reach} kind="reach" />
                    <span className="min-w-0 truncate text-[var(--text-dense-label)] font-semibold">
                      {lane.shortLabel}
                    </span>
                  </div>
                  {progress != null && (
                    <div className="mt-1">
                      <div className="flex items-center justify-between text-[var(--text-dense-caption)] text-muted-foreground">
                        <span>
                          {progress.done}/{progress.total}
                        </span>
                        <span>{progress.percent}%</span>
                      </div>
                      <div className="mt-0.5 h-1 w-full overflow-hidden rounded-full bg-border">
                        <div
                          className="h-full rounded-full bg-primary transition-all"
                          style={{ width: `${progress.percent}%` }}
                        />
                      </div>
                    </div>
                  )}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Task queue for selected lane */}
      {laneMeta != null && (activeQueue.length > 0 || completedQueue.length > 0) && (
        <div className="mt-2.5 overflow-hidden rounded-md border border-border/60">
          <header className="flex items-center justify-between border-b border-border/60 bg-background px-2.5 py-1.5">
            <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
              Queue · {laneMeta.shortLabel}
            </span>
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              {activeQueue.length} active
              {completedQueue.length > 0 ? ` · ${completedQueue.length} done` : ''}
            </span>
          </header>
          <ul className="m-0 flex list-none flex-col divide-y divide-border/40 p-0">
            {activeQueue.map(item => (
              <QueueRow key={item.id} item={item} />
            ))}
            {completedQueue.length > 0 && (
              <li>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[var(--text-dense-caption)] text-muted-foreground hover:bg-secondary/40"
                  onClick={() => setCompletedOpen(v => !v)}
                >
                  <Check size={12} className="text-success" />
                  <span>{completedQueue.length} completed</span>
                  {completedOpen ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                </button>
                {completedOpen && (
                  <ul className="m-0 flex list-none flex-col divide-y divide-border/40 p-0 opacity-70">
                    {completedQueue.map(item => (
                      <QueueRow key={item.id} item={item} />
                    ))}
                  </ul>
                )}
              </li>
            )}
          </ul>
        </div>
      )}

      {/* Lane description when no queue */}
      {laneMeta != null && activeQueue.length === 0 && completedQueue.length === 0 && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-muted-foreground">
          {laneMeta.label} — {laneMeta.description}
        </p>
      )}

      {/* Copy + secondary link */}
      <div className="mt-2.5 flex flex-wrap items-center gap-2">
        <Button
          variant={copied ? 'default' : 'secondary'}
          size="xs"
          disabled={!isReady || copied}
          onClick={() => void handleCopy()}
        >
          {ctaLabel}
        </Button>
        {onOpenFullBriefing != null && (
          <button
            type="button"
            className="text-[var(--text-dense-meta)] text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => onOpenFullBriefing(briefingOpts)}
          >
            Open full Briefing page →
          </button>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
        Pack is scoped to the selected lane. Paste into a new Cursor chat.
      </p>
    </div>
  )
}
