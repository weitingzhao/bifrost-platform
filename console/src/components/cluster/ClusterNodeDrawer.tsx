import { Button } from '@bifrost/ui'
import type { ClusterNode, ComputeWorkloadStatus, NodePowerResponse } from '@/api/types'
import { NodeObservedStatePanel } from '@/components/cluster/NodeObservedStatePanel'
import { WizardProcedureSteps } from '@/components/cluster/WizardProcedureSteps'
import { StatusLamp } from '@/components/StatusLamp'
import {
  computeShutdownWizardSteps,
  maintenanceWizardSteps,
} from '@/lib/cluster/nodeWizard'

interface ClusterNodeDrawerProps {
  open: boolean
  node: ClusterNode | null
  power: NodePowerResponse | undefined
  powerLoading: boolean
  powerError: string | null
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  onClose: () => void
  onCordon: () => void
  onUncordon: () => void
  onDrain: () => void
  onWake?: () => void
  onPowerOff?: () => void
  onScaleWorkload?: (workload: ComputeWorkloadStatus, replicas: number) => void
}

function powerStateLabel(state: string | undefined): string {
  if (state === 'online') return 'Online'
  if (state === 'offline') return 'Offline'
  return state ?? 'Unknown'
}

export function ClusterNodeDrawer({
  open,
  node,
  power,
  powerLoading,
  powerError,
  canOperate,
  canAdmin,
  actionPending,
  onClose,
  onCordon,
  onUncordon,
  onDrain,
  onWake,
  onPowerOff,
  onScaleWorkload,
}: ClusterNodeDrawerProps) {
  if (!open || node == null) return null

  const computeManaged = node.compute_managed === true
  const offline = power?.power_state === 'offline' || node.status !== 'Ready'
  const online = power?.power_state === 'online' || node.status === 'Ready'
  const wizardSteps = computeManaged
    ? computeShutdownWizardSteps(node, power)
    : maintenanceWizardSteps(node, power)
  const procedureLabel = computeManaged ? 'Compute off' : 'Maintain'

  return (
    <aside
      className="bay-detail-drawer panel-elevated cluster-drawer"
      role="dialog"
      aria-label="Node detail"
    >
      <header className="bay-detail-drawer-header">
        <div>
          <h3 className="m-0 text-sm font-semibold font-mono-tabular">{node.name}</h3>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {computeManaged ? 'On-demand compute · WOL power policy' : 'K3s node lifecycle (P2)'}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={onClose}>
          Close
        </Button>
      </header>

      <div className="bay-detail-drawer-body flex flex-col gap-4">
        <NodeObservedStatePanel
          node={node}
          power={powerLoading && power == null ? undefined : power}
          layout="column"
        />

        <WizardProcedureSteps steps={wizardSteps} flowLabel={procedureLabel} />

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Manual actions
          </h4>
          <p className="m-0 mb-2 text-dense-meta text-[var(--muted-foreground)]">
            Jump to a specific API call — procedure above shows the recommended order.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={!canOperate || actionPending || node.unschedulable === true}
              onClick={onCordon}
            >
              Cordon
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!canOperate || actionPending || node.unschedulable !== true}
              onClick={onUncordon}
            >
              Uncordon
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!canAdmin || actionPending}
              onClick={onDrain}
            >
              Drain
            </Button>
          </div>
          {!canOperate && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Cordon and uncordon require operator token.
            </p>
          )}
          {canOperate && !canAdmin && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Drain requires admin token.
            </p>
          )}
        </section>

        {computeManaged && (
          <section>
            <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Power detail
            </h4>
            {powerLoading && power == null ? (
              <p className="m-0 text-[var(--muted-foreground)]">Loading power status…</p>
            ) : powerError != null ? (
              <p className="m-0 text-[var(--destructive)]">{powerError}</p>
            ) : (
              <ul className="m-0 list-none space-y-1 text-[var(--text-dense)]">
                <li>
                  <StatusLamp value={power?.reachability ?? node.reachability} kind="reach" /> Probe{' '}
                  <code className="font-mono-tabular">{power?.node_status ?? node.status}</code>
                  {' · '}
                  Power <code className="font-mono-tabular">{powerStateLabel(power?.power_state)}</code>
                </li>
                {power?.wol_mac != null && power.wol_mac !== '' && (
                  <li>
                    WOL MAC <code className="font-mono-tabular">{power.wol_mac}</code>
                  </li>
                )}
                {power != null && (
                  <li>
                    Pending pods{' '}
                    <code className="font-mono-tabular">{power.pending_compute_pods}</code>
                    {' · '}
                    User pods{' '}
                    <code className="font-mono-tabular">{power.user_pods_on_node}</code>
                  </li>
                )}
              </ul>
            )}
            <div className="mt-3 flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!canOperate || actionPending || online || onWake == null}
                onClick={onWake}
              >
                Wake (WOL)
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!canAdmin || actionPending || offline || onPowerOff == null}
                onClick={onPowerOff}
              >
                Power off
              </Button>
            </div>
          </section>
        )}

        {computeManaged && power?.workloads != null && power.workloads.length > 0 && onScaleWorkload != null && (
          <section>
            <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Compute workloads
            </h4>
            <ul className="m-0 list-none space-y-3">
              {power.workloads.map(w => (
                <li
                  key={`${w.namespace}/${w.name}`}
                  className="rounded border border-[var(--border)] bg-[var(--background)] px-3 py-2"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{w.label}</span>
                    <code className="text-[var(--text-dense-meta)] font-mono-tabular">
                      {w.namespace}/{w.name}
                    </code>
                  </div>
                  <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    Replicas{' '}
                    <code className="font-mono-tabular">
                      {w.ready_replicas}/{w.replicas}
                    </code>
                  </p>
                  <div className="mt-2 flex gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!canOperate || actionPending || w.replicas >= 1}
                      onClick={() => onScaleWorkload(w, 1)}
                    >
                      Scale up
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!canOperate || actionPending || w.replicas === 0}
                      onClick={() => onScaleWorkload(w, 0)}
                    >
                      Scale down
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </aside>
  )
}
