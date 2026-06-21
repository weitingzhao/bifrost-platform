import { useState } from 'react'
import { DenseTag } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { milestoneStatusVariant } from '@/components/FocusStrip'
import type { SessionDelta } from '@/lib/briefing/sessionDiff'
import { isEmptyDelta } from '@/lib/briefing/sessionDiff'

interface SessionDeltaPanelProps {
  delta: SessionDelta | null
  hasBaseline: boolean
}

export function SessionDeltaPanel({ delta, hasBaseline }: SessionDeltaPanelProps) {
  const [expanded, setExpanded] = useState(false)

  if (!hasBaseline) {
    return (
      <section className="page-section panel-elevated px-4 py-2">
        <div className="flex items-center gap-2">
          <p className="briefing-section-kicker m-0">0 · Since your last session</p>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            First session — snapshot saved on briefing copy.
          </span>
        </div>
      </section>
    )
  }

  if (delta == null) return null

  if (isEmptyDelta(delta)) {
    return (
      <section className="page-section panel-elevated px-4 py-2">
        <div className="flex items-center gap-2">
          <p className="briefing-section-kicker m-0">0 · Since your last session</p>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No changes · {delta.timeSince}
          </span>
        </div>
      </section>
    )
  }

  const changeCount = countChanges(delta)
  const summaryChips = buildSummaryChips(delta)

  return (
    <section className="page-section panel-elevated px-4 py-2">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left"
        onClick={() => setExpanded(v => !v)}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        )}
        <span className="briefing-section-kicker m-0">0 · Since your last session</span>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {changeCount} changes · {delta.timeSince}
        </span>
        <span className="ml-auto flex flex-wrap items-center gap-1.5">
          {summaryChips.map(chip => (
            <DenseTag key={chip.label} variant={chip.variant} className="text-[10px]">
              {chip.label}
            </DenseTag>
          ))}
        </span>
      </button>

      {expanded && (
        <div className="mt-2 grid grid-cols-[repeat(auto-fit,minmax(280px,1fr))] gap-2">
          {delta.spineVersionChanged && (
            <DeltaChip label="Spine" detail={`${delta.spineVersionFrom} → ${delta.spineVersionTo}`} />
          )}
          {delta.focusChanged != null && (
            <DeltaChip label="Focus" detail={delta.focusChanged.to} truncate />
          )}
          {delta.blockerChanged != null && (
            <DeltaChip label="Blocker" detail={`${delta.blockerChanged.from ?? 'none'} → ${delta.blockerChanged.to ?? 'none'}`} />
          )}
          {delta.milestoneChanges.map(mc => (
            <div key={mc.id} className="flex items-center gap-1.5 text-[var(--text-dense-meta)]">
              <code className="font-mono-tabular text-[10px]">{mc.id}</code>
              <DenseTag variant={milestoneStatusVariant(mc.from)} className="text-[10px]">{mc.from}</DenseTag>
              <span className="text-[var(--muted-foreground)]">→</span>
              <DenseTag variant={milestoneStatusVariant(mc.to)} className="text-[10px]">{mc.to}</DenseTag>
            </div>
          ))}
          {delta.trackChanges.map(tc => (
            <DeltaChip key={tc.trackId} label={tc.label} detail={`${tc.doneFrom}→${tc.doneTo}/${tc.total} done`} />
          ))}
          {delta.clusterChanges != null && (
            <ClusterChip changes={delta.clusterChanges} />
          )}
          {delta.matrixChanges.length > 0 && (
            <MatrixSummaryChip changes={delta.matrixChanges} />
          )}
          {delta.newAuditRecords.length > 0 && (
            <DeltaChip label="Actions" detail={`${delta.newAuditRecords.length} new`} />
          )}
        </div>
      )}
    </section>
  )
}

function DeltaChip({ label, detail, truncate }: { label: string; detail: string; truncate?: boolean }) {
  return (
    <div className="flex items-center gap-1.5 text-[var(--text-dense-meta)]">
      <span className="font-semibold text-[var(--foreground)]">{label}:</span>
      <span className={`text-[var(--muted-foreground)] ${truncate ? 'truncate max-w-[220px]' : ''}`}>{detail}</span>
    </div>
  )
}

function ClusterChip({ changes }: { changes: NonNullable<SessionDelta['clusterChanges']> }) {
  const parts: string[] = []
  if (changes.failingPodsDelta !== 0) {
    parts.push(`pods ${changes.failingPodsDelta > 0 ? '+' : ''}${changes.failingPodsDelta}`)
  }
  if (changes.nodesReadyDelta !== 0) {
    parts.push(`nodes ${changes.nodesReadyFrom}→${changes.nodesReadyTo}`)
  }
  if (changes.reachabilityChanged) {
    parts.push(`${changes.reachabilityFrom}→${changes.reachabilityTo}`)
  }
  return <DeltaChip label="Cluster" detail={parts.join(', ')} />
}

function MatrixSummaryChip({ changes }: { changes: SessionDelta['matrixChanges'] }) {
  const recovered = changes.filter(m => m.to === 'ok').length
  const degraded = changes.filter(m => m.from === 'ok' && m.to !== 'ok').length
  const parts: string[] = []
  if (recovered > 0) parts.push(`${recovered} recovered`)
  if (degraded > 0) parts.push(`${degraded} degraded`)
  if (parts.length === 0) parts.push(`${changes.length} changed`)
  return <DeltaChip label="Matrix" detail={parts.join(', ')} />
}

type ChipVariant = 'success' | 'warning' | 'danger' | 'neutral' | 'category'

function buildSummaryChips(delta: SessionDelta): { label: string; variant: ChipVariant }[] {
  const chips: { label: string; variant: ChipVariant }[] = []
  if (delta.spineVersionChanged) chips.push({ label: 'Spine', variant: 'neutral' })
  if (delta.milestoneChanges.length > 0) chips.push({ label: `${delta.milestoneChanges.length} milestone`, variant: 'category' })
  if (delta.trackChanges.length > 0) chips.push({ label: `${delta.trackChanges.length} track`, variant: 'success' })
  if (delta.matrixChanges.length > 0) {
    const allOk = delta.matrixChanges.every(m => m.to === 'ok')
    chips.push({ label: `${delta.matrixChanges.length} probe`, variant: allOk ? 'success' : 'warning' })
  }
  if (delta.clusterChanges != null) {
    const variant = delta.clusterChanges.failingPodsDelta > 0 ? 'danger' : 'success'
    chips.push({ label: 'Cluster', variant })
  }
  if (delta.newAuditRecords.length > 0) chips.push({ label: `${delta.newAuditRecords.length} action`, variant: 'neutral' })
  return chips
}

function countChanges(delta: SessionDelta): number {
  let count = 0
  if (delta.spineVersionChanged) count++
  if (delta.focusChanged) count++
  if (delta.blockerChanged) count++
  count += delta.milestoneChanges.length
  count += delta.matrixChanges.length
  count += delta.trackChanges.length
  if (delta.clusterChanges != null) count++
  count += delta.newAuditRecords.length
  return count
}
