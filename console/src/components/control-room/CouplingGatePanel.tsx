import { Button, StatusLamp } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { flywheelLabel } from '@/components/FocusStrip'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'

interface CouplingGatePanelProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  onOpenProgram: () => void
  onOpenDelivery?: () => void
}

export function CouplingGatePanel({
  context,
  matrices,
  onOpenProgram,
  onOpenDelivery,
}: CouplingGatePanelProps) {
  if (!context) {
    return (
      <section className="page-section panel-elevated px-3 py-3">
        <p className="m-0 text-[var(--muted-foreground)]">Loading coupling state…</p>
      </section>
    )
  }

  const promote = evaluatePromoteStatus(context, matrices)
  const lamp = promote.ready ? 'ok' : promote.blockedByDecision || promote.prodFails ? 'fail' : 'degraded'

  return (
    <section className="page-section panel-elevated px-3 py-3 coupling-gate-panel">
      <header className="mb-2 flex items-center gap-2">
        <StatusLamp value={lamp} kind="reach" />
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Coupling gate
        </h3>
      </header>
      <p className="m-0 text-[var(--text-dense)] font-medium">
        {promote.ready ? 'Promote ready (narrative)' : 'Promote blocked'}
      </p>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Flywheel primary: {flywheelLabel(context.focus.flywheel_primary)}
      </p>
      {!promote.ready && promote.reasons.length > 0 && (
        <ul className="m-0 mt-2 list-disc px-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {promote.reasons.map(r => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      )}
      {context.focus.blocker != null && context.focus.blocker !== '' && (
        <Button variant="ghost" size="sm" className="mt-2 text-[var(--text-dense)]" onClick={onOpenProgram}>
          Open blocker: {context.focus.blocker}
        </Button>
      )}
      {onOpenDelivery != null && (
        <Button variant="ghost" size="sm" className="mt-2 text-[var(--text-dense)]" onClick={onOpenDelivery}>
          Open Delivery
        </Button>
      )}
    </section>
  )
}
