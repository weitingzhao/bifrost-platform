import { DenseTag, StatusLamp, cn } from '@bifrost/ui'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  controlRoomVerdictLabel,
  controlRoomVerdictTagVariant,
  type ControlRoomBayId,
  type ControlRoomBaySignal,
} from '@/lib/control-room/controlRoomBays'

export type ControlRoomBayCardsProps = {
  bays: ControlRoomBaySignal[]
  activeBay: ControlRoomBayId | null
  openBayIds: ReadonlySet<ControlRoomBayId>
  onSelectBay: (id: ControlRoomBayId) => void
}

function signalTagVariant(signal: Signal): 'success' | 'warning' | 'danger' | 'neutral' {
  return controlRoomVerdictTagVariant(controlRoomVerdictLabel(signal))
}

function BayCard({
  bay,
  selected,
  open,
  onSelect,
}: {
  bay: ControlRoomBaySignal
  selected: boolean
  open: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'control-room-bay-card flex min-w-[9.5rem] flex-1 flex-col gap-1 rounded-md border px-2.5 py-2 text-left transition-colors',
        selected || open
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)] hover:bg-[var(--accent)]/60',
      )}
      aria-pressed={open}
    >
      <span className="flex items-center gap-1.5">
        <StatusLamp value={bay.signal} kind="reach" />
        <span className="text-[var(--text-dense-caption)] font-medium">{bay.label}</span>
        {open && (
          <DenseTag variant="neutral" className="text-[9px]">
            Open
          </DenseTag>
        )}
      </span>
      <span className="flex items-center gap-1.5">
        <DenseTag variant={signalTagVariant(bay.signal)} className="text-[9px]">
          {controlRoomVerdictLabel(bay.signal)}
        </DenseTag>
      </span>
      <span
        className="line-clamp-2 text-[var(--text-dense-caption)] text-muted-foreground"
        title={bay.reason}
      >
        {bay.reason}
      </span>
    </button>
  )
}

/**
 * Observability-style bay scan cards — sole bay picker; click opens one bay (accordion).
 */
export function ControlRoomBayCards({
  bays,
  activeBay,
  openBayIds,
  onSelectBay,
}: ControlRoomBayCardsProps) {
  return (
    <section className="control-room-bay-cards" aria-label="Bay scan">
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
          Bay scan
        </span>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          Click a bay to open detail below
        </span>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {bays.map(bay => (
          <BayCard
            key={bay.id}
            bay={bay}
            selected={activeBay === bay.id}
            open={openBayIds.has(bay.id)}
            onSelect={() => onSelectBay(bay.id)}
          />
        ))}
      </div>
    </section>
  )
}
