import { StatusLamp } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { flywheelLabel } from '@/components/FocusStrip'
import { CouplingGatePanel } from '@/components/control-room/CouplingGatePanel'
import { baysForFlywheel, getBay } from '@/lib/control-room/bayRegistry'
import { lampForBay, summaryForBay } from '@/lib/control-room/matrixSummary'

export type ControlRoomSelection =
  | { kind: 'bay'; id: string }
  | { kind: 'milestone'; id: string }
  | null

interface BayCardProps {
  bayId: string
  matrices: MatrixResponse[]
  context: OpsContextResponse | undefined
  selected: boolean
  onSelect: (bayId: string) => void
}

function BayCard({ bayId, matrices, context, selected, onSelect }: BayCardProps) {
  const bay = getBay(bayId)
  if (!bay) return null

  const lamp = lampForBay(bayId, matrices, context)
  const summary = summaryForBay(bayId, matrices)

  return (
    <button
      type="button"
      className={['bay-card', selected ? 'bay-card--selected' : ''].filter(Boolean).join(' ')}
      onClick={() => onSelect(bayId)}
    >
      <StatusLamp value={lamp} kind="reach" />
      <span className="bay-card-label">{bay.label}</span>
      {summary != null && (
        <span className="bay-card-count font-mono-tabular text-[var(--muted-foreground)]">
          {summary.ok}/{summary.total}
        </span>
      )}
    </button>
  )
}

interface DualFlywheelPanelProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  selection: ControlRoomSelection
  onSelectBay: (bayId: string) => void
  onOpenProgram: () => void
}

export function DualFlywheelPanel({
  context,
  matrices,
  selection,
  onSelectBay,
  onOpenProgram,
}: DualFlywheelPanelProps) {
  const flywheelA = baysForFlywheel('A')
  const flywheelB = baysForFlywheel('B')

  return (
    <section className="page-section panel-elevated px-3 py-3">
      <header className="mb-3">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Dual flywheel bays
        </h3>
        {context != null && (
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Primary: {flywheelLabel(context.focus.flywheel_primary)}
          </p>
        )}
      </header>
      <div className="dual-flywheel-grid">
        <div className="flywheel-column">
          <div className="flywheel-column-title">A — Product</div>
          <div className="bay-stack">
            {flywheelA.map(b => (
              <BayCard
                key={b.id}
                bayId={b.id}
                matrices={matrices}
                context={context}
                selected={selection?.kind === 'bay' && selection.id === b.id}
                onSelect={onSelectBay}
              />
            ))}
          </div>
        </div>
        <div className="flywheel-column flywheel-column--coupling">
          <CouplingGatePanel context={context} matrices={matrices} onOpenProgram={onOpenProgram} />
        </div>
        <div className="flywheel-column">
          <div className="flywheel-column-title">B — Runtime</div>
          <div className="bay-stack">
            {flywheelB.map(b => (
              <BayCard
                key={b.id}
                bayId={b.id}
                matrices={matrices}
                context={context}
                selected={selection?.kind === 'bay' && selection.id === b.id}
                onSelect={onSelectBay}
              />
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}
