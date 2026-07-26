import { StatusLamp, cn } from '@bifrost/ui'
import type { ReactNode } from 'react'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  controlRoomBayCountsLabel,
  controlRoomVerdictLabel,
  controlRoomVerdictTagVariant,
  formatControlRoomFreshness,
  type ControlRoomBayId,
  type ControlRoomBaySignal,
} from '@/lib/control-room/controlRoomBays'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'

export type ControlRoomVerdictStripProps = {
  missionSignal: Signal
  primaryCause: string
  dataUpdatedAt: number
  bays: ControlRoomBaySignal[]
  isLoading?: boolean
  /** Page-level actions (Launch Release / Deploy Satellite) — always discoverable. */
  actions?: ReactNode
  /** Click a non-ok bay chip → open/scroll that bay (Summary→Detail continuity). */
  onSelectBay?: (id: ControlRoomBayId) => void
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
  actions,
  onSelectBay,
}: ControlRoomVerdictStripProps) {
  const label = isLoading ? 'PROBING' : controlRoomVerdictLabel(missionSignal)
  const counts = controlRoomBayCountsLabel(bays)
  const attentionBays = bays.filter(b => b.signal !== 'ok' && b.signal !== 'unknown')

  return (
    <OpsVerdictStrip
      className="control-room-verdict"
      ariaLabel="Mission verdict"
      title="MISSION VERDICT"
      lamp={isLoading ? 'unknown' : missionSignal}
      tagLabel={label}
      tagVariant={controlRoomVerdictTagVariant(label)}
      summary={
        <span title={primaryCause}>
          {isLoading ? 'Aggregating mission probes…' : primaryCause}
        </span>
      }
      actions={actions}
      meta={
        <>
          {onSelectBay != null && attentionBays.length > 0 ? (
            <span className="inline-flex flex-wrap items-center gap-1.5">
              <span>{counts || '—'}</span>
              {attentionBays.map(bay => (
                <button
                  key={bay.id}
                  type="button"
                  className={cn(
                    'inline-flex items-center gap-1 rounded px-1 py-0.5 hover:bg-primary/10',
                    bay.signal === 'fail' ? 'text-destructive' : 'text-warning',
                  )}
                  title={`${bay.label}: ${bay.reason} — open bay`}
                  onClick={() => onSelectBay(bay.id)}
                >
                  <StatusLamp value={bay.signal} kind="reach" />
                  <span className="font-medium">{bay.label}</span>
                </button>
              ))}
            </span>
          ) : (
            <span>{counts || '—'}</span>
          )}
          <span className="font-mono-tabular">
            freshness {formatControlRoomFreshness(dataUpdatedAt)}
          </span>
        </>
      }
    />
  )
}
