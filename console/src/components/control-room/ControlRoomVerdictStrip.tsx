import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  controlRoomBayCountsLabel,
  controlRoomVerdictLabel,
  controlRoomVerdictTagVariant,
  formatControlRoomFreshness,
  type ControlRoomBaySignal,
} from '@/lib/control-room/controlRoomBays'

export type ControlRoomVerdictStripProps = {
  missionSignal: Signal
  primaryCause: string
  dataUpdatedAt: number
  bays: ControlRoomBaySignal[]
  isLoading?: boolean
}

/**
 * Observability-style mission verdict strip — scan layer for Control Room.
 */
export function ControlRoomVerdictStrip({
  missionSignal,
  primaryCause,
  dataUpdatedAt,
  bays,
  isLoading = false,
}: ControlRoomVerdictStripProps) {
  const label = isLoading ? 'PROBING' : controlRoomVerdictLabel(missionSignal)
  const counts = controlRoomBayCountsLabel(bays)

  return (
    <section
      className="control-room-verdict page-section panel-elevated px-3 py-2.5"
      aria-label="Mission verdict"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <StatusLamp value={isLoading ? 'unknown' : missionSignal} kind="reach" />
        <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
          MISSION VERDICT
        </span>
        <DenseTag variant={controlRoomVerdictTagVariant(label)} className="text-[10px] font-semibold">
          {label}
        </DenseTag>
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)]" title={primaryCause}>
          {isLoading ? 'Aggregating mission probes…' : primaryCause}
        </span>
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
        <span>{counts || '—'}</span>
        <span className="font-mono-tabular">
          freshness {formatControlRoomFreshness(dataUpdatedAt)}
        </span>
      </div>
    </section>
  )
}
