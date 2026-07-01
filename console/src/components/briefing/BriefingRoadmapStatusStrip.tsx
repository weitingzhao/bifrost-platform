import { DenseTag } from '@bifrost/ui'
import { loadPhase1SignoffState } from '@/lib/briefing/briefingPhase1Delivery'
import { loadPhase2SignoffState } from '@/lib/briefing/briefingPhase2Delivery'
import { loadPhase3SignoffState } from '@/lib/briefing/briefingPhase3Delivery'
import { loadPhase4SignoffState } from '@/lib/briefing/briefingPhase4Delivery'
import { useBriefingSignoffRevision } from '@/lib/briefing/briefingSignoffEvents'

function phaseTag(signed: boolean, label: string) {
  return (
    <DenseTag key={label} variant={signed ? 'success' : 'neutral'}>
      {label}
      {signed ? ' ✓' : ''}
    </DenseTag>
  )
}

export function BriefingRoadmapStatusStrip() {
  useBriefingSignoffRevision()
  const p1 = loadPhase1SignoffState().signedOffAt != null
  const p2 = loadPhase2SignoffState().signedOffAt != null
  const p3 = loadPhase3SignoffState().signedOffAt != null
  const p4 = loadPhase4SignoffState().signedOffAt != null
  const allDone = p1 && p2 && p3 && p4

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Roadmap
      </span>
      {phaseTag(p1, 'P1')}
      {phaseTag(p2, 'P2')}
      {phaseTag(p3, 'P3')}
      {phaseTag(p4, 'P4')}
      {allDone ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          Agent Briefing program complete
        </span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Sign off in panels below
        </span>
      )}
    </div>
  )
}
