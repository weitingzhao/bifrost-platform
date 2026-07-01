import { DenseTag } from '@bifrost/ui'
import {
  formatGateProjectionLabel,
  formatSpineStatusLabel,
  isHistoricalSpineStatus,
  resolveMilestoneDualLabels,
} from '@/lib/architecture/spineSemantics'

interface MilestoneSpineDualTagsProps {
  milestoneId?: string
  milestoneStatus: string
  gateReady: boolean
  /** When true, always show gate chip for historical milestones (Milestones table). */
  showGateWhenReady?: boolean
}

export function MilestoneSpineDualTags({
  milestoneId,
  milestoneStatus,
  gateReady,
  showGateWhenReady = false,
}: MilestoneSpineDualTagsProps) {
  if (!isHistoricalSpineStatus(milestoneStatus)) {
    return <DenseTag variant="info">{milestoneStatus}</DenseTag>
  }

  const dual = resolveMilestoneDualLabels(milestoneStatus, gateReady)
  if (dual == null) {
    return <DenseTag variant="success">{formatSpineStatusLabel(milestoneStatus)}</DenseTag>
  }

  const showGate = !gateReady || showGateWhenReady

  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {milestoneId != null && (
        <code className="font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {milestoneId}
        </code>
      )}
      <DenseTag variant="success">{dual.spineLabel}</DenseTag>
      {showGate && (
        <DenseTag variant={gateReady ? 'success' : 'warning'}>{dual.gateLabel}</DenseTag>
      )}
    </span>
  )
}

/** Gate-only chip for Promote / Control Room when spine is already labeled elsewhere. */
export function GateProjectionTag({ gateReady }: { gateReady: boolean }) {
  return (
    <DenseTag variant={gateReady ? 'success' : 'warning'}>
      {formatGateProjectionLabel(gateReady)}
    </DenseTag>
  )
}
