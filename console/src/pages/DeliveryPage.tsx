import { useState } from 'react'
import { StatusLamp } from '@bifrost/ui'
import type { ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { flywheelLabel } from '@/components/FocusStrip'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'
import {
  ciModeLabel,
  DELIVERY_STRATEGY_BULLETS,
  showGitOpsPlannedBadge,
} from '@/lib/delivery/deliveryPhase'

interface DeliveryPageProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary?: ClusterSummary
  isLoading: boolean
  onOpenMilestones: () => void
  onOpenPromote: () => void
}

export function DeliveryPage({
  context,
  matrices,
  clusterSummary,
  isLoading,
  onOpenMilestones,
  onOpenPromote,
}: DeliveryPageProps) {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)

  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading delivery context…</p>
  }

  const promote = evaluatePromoteStatus(context, matrices)
  const lamp = promote.ready ? 'ok' : promote.blockedByDecision || promote.prodFails ? 'fail' : 'degraded'
  const ciMode = ciModeLabel(context.deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(context.deployment)

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Delivery — CI/CD path</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-3xl">
          Unified view of near-term delivery (Mac CI + compose prod) and target GitOps on K3s. For
          migration milestones see <strong>Milestones</strong>; for release readiness see{' '}
          <strong>Promote</strong>.
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="badge-ui font-mono-tabular">phase: {context.deployment.phase}</span>
          <span className="badge-ui focus-strip-ci-mode">CI: {ciMode}</span>
          {gitOpsPlanned && (
            <span className="badge-ui badge-status-pending">GitOps planned</span>
          )}
        </div>
      </section>

      <section className="page-section panel-elevated px-4 py-3">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Strategy (K3S §5)
        </h3>
        <ul className="m-0 mt-2 list-disc px-5 text-[var(--text-dense)]">
          {DELIVERY_STRATEGY_BULLETS.map(b => (
            <li key={b}>{b}</li>
          ))}
        </ul>
      </section>

      <DeliveryFlow
        context={context}
        selectionId={selectedNodeId}
        clusterReachOk={clusterSummary?.reachability === 'ok'}
        onSelectNode={id => setSelectedNodeId(prev => (prev === id ? null : id))}
      />

      <section className="page-section panel-elevated px-4 py-3 coupling-gate-panel">
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
          Flywheel primary: {flywheelLabel(context.focus.flywheel_primary)} · Both flywheels must pass
          before production cutover.
        </p>
        {!promote.ready && promote.reasons.length > 0 && (
          <ul className="m-0 mt-2 list-disc px-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {promote.reasons.map(r => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          <button type="button" className="btn-ui btn-ui-primary" onClick={onOpenPromote}>
            Open Promote
          </button>
          <button type="button" className="btn-ui btn-ui-ghost" onClick={onOpenMilestones}>
            Open Milestones
          </button>
        </div>
      </section>
    </div>
  )
}
