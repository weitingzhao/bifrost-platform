import { Button, StatusLamp } from '@bifrost/ui'
import { BriefingIconBadge, TRACK_ICONS } from '@/lib/briefing/briefingIcons'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { TrackSummary, TrackId } from '@/lib/briefing/workTracks'
import { OpsSection } from '@/components/layout/OpsSection'

interface WorkTracksStripProps {
  tracks: TrackSummary[]
  onOpenBriefing: (opts?: BriefingUrlState) => void
}

function trackReach(t: TrackSummary): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (t.id === 'operate') {
    if (t.issues.length === 0) return 'ok'
    if (t.issues.some(i => i.kind === 'cluster_failing' || i.kind === 'matrix_fail')) return 'fail'
    return 'degraded'
  }
  if (t.progress == null) return 'unknown'
  if (t.progress.percent === 100) return 'ok'
  if (t.progress.percent > 0) return 'degraded'
  return 'unknown'
}

function TrackMiniCard({
  track,
  onOpenBriefing,
}: {
  track: TrackSummary
  onOpenBriefing: (opts?: BriefingUrlState) => void
}) {
  const reach = trackReach(track)

  return (
    <button
      type="button"
      className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-left transition-colors hover:bg-[var(--secondary)]/40"
      onClick={() => onOpenBriefing({ track: track.id })}
    >
      <div className="flex items-center gap-2">
        <BriefingIconBadge icon={TRACK_ICONS[track.id as TrackId]} size="sm" />
        <StatusLamp value={reach} kind="reach" />
        <span className="text-sm font-semibold capitalize">{track.id}</span>
        {track.currentPhase != null && (
          <span className="ml-auto rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
            {track.currentPhase}
          </span>
        )}
      </div>
      <p className="m-0 mt-1 text-dense-meta text-[var(--muted-foreground)]">{track.subtitle}</p>
      {track.progress != null && (
        <p className="m-0 mt-1 font-mono text-dense-caption text-[var(--muted-foreground)]">
          {track.progress.done}/{track.progress.total} · {track.progress.percent}%
        </p>
      )}
      {track.nextStep != null && (
        <p className="m-0 mt-1 text-dense-meta">
          <span className="text-[var(--muted-foreground)]">Next: </span>
          <span className="text-[var(--foreground)]">{track.nextStep}</span>
        </p>
      )}
      {track.id === 'operate' && track.issues.length > 0 && (
        <p className="m-0 mt-1 text-dense-caption text-[var(--muted-foreground)]">
          {track.issues[0].label}
          {track.issues.length > 1 ? ` (+${track.issues.length - 1})` : ''}
        </p>
      )}
    </button>
  )
}

export function WorkTracksStrip({ tracks, onOpenBriefing }: WorkTracksStripProps) {
  return (
    <OpsSection
      title="Work tracks"
      description="Build / migrate / operate progress from spine — open Agent Briefing to pick a lane and generate a session pack."
      actions={
        <Button variant="ghost" size="xs" onClick={() => onOpenBriefing()}>
          Open Agent Briefing
        </Button>
      }
      bodyPadding="default"
      overflow="visible"
    >
      <div className="grid gap-2 sm:grid-cols-3">
        {tracks.map(t => (
          <TrackMiniCard key={t.id} track={t} onOpenBriefing={onOpenBriefing} />
        ))}
      </div>
    </OpsSection>
  )
}
