import { Button, DenseTag } from '@bifrost/ui'
import { ClipboardList } from 'lucide-react'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'

export type TaskBriefingLauncherProps = {
  mode: TaskModeDef
  onOpenBriefing?: (opts?: BriefingUrlState) => void
}

export function TaskBriefingLauncher({ mode, onOpenBriefing }: TaskBriefingLauncherProps) {
  const dev = mode.dev
  if (dev == null || onOpenBriefing == null) return null

  const briefingOpts: BriefingUrlState = {
    track: dev.briefingTrack,
    lane: dev.briefingLane,
    intent: dev.briefingIntent,
    pack: 'compact',
    taskModeContext: {
      modeId: mode.id,
      modeLabel: mode.label,
      loopArchetype: mode.loopArchetype,
      programId: dev.programId,
    },
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <ClipboardList size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Agent Briefing</span>
        {dev.briefingTrack != null && (
          <DenseTag variant="neutral">Track · {dev.briefingTrack}</DenseTag>
        )}
        {dev.briefingIntent != null && (
          <DenseTag variant="info">Intent · {dev.briefingIntent}</DenseTag>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        Opens Briefing with task mode context injected into the pack header.
      </p>
      <Button variant="secondary" size="xs" className="mt-2" onClick={() => onOpenBriefing(briefingOpts)}>
        Open scoped Briefing →
      </Button>
    </div>
  )
}
