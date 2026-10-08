import { Button, SegmentControl } from '@bifrost/ui'
import {
  RequestCordonNode,
  RequestDrainNode,
  RequestPowerOffNode,
  RequestUncordonNode,
} from '@/components/shell/clusterActionRequests'
import type { ClusterNode, NodePowerResponse } from '@/api/clusterTypes'
import { NodeObservedStatePanel } from '@/components/cluster/NodeObservedStatePanel'
import { WizardProcedureSteps } from '@/components/cluster/WizardProcedureSteps'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  currentWizardStep,
  type NodeWizardFlow,
  type WizardAction,
  wizardStepsForFlow,
} from '@/lib/cluster/nodeWizard'
import { useComputeOffCycleHint } from '@/lib/cluster/useComputeOffCycleHint'

export interface ClusterNodeWizardPanelProps {
  flow: NodeWizardFlow
  onFlowChange: (flow: NodeWizardFlow) => void
  nodes: ClusterNode[]
  selectedNodeName: string | null
  onSelectNodeName: (name: string | null) => void
  selectedNode: ClusterNode | null
  power: NodePowerResponse | undefined
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  onWizardAction: (action: WizardAction, context?: { profileId?: string }) => void
  onOpenNodeDetails?: () => void
}

const FLOW_OPTIONS: { value: NodeWizardFlow; label: string }[] = [
  { value: 'maintenance', label: 'Maintain' },
  { value: 'compute_shutdown', label: 'Compute off' },
]

const FLOW_PROCEDURE_LABEL: Record<NodeWizardFlow, string> = {
  maintenance: 'Maintain',
  compute_shutdown: 'Compute off',
}

function actionLabel(action: WizardAction): string {
  switch (action) {
    case 'cordon':
      return 'Cordon node'
    case 'drain':
      return 'Drain node'
    case 'uncordon':
      return 'Uncordon node'
    case 'wake':
      return 'Wake (WOL)'
    case 'poweroff':
      return 'Power off'
    default:
      return 'Continue'
  }
}

function actionRequiresAdmin(action: WizardAction): boolean {
  return action === 'drain' || action === 'poweroff'
}

function actionDisabled(action: WizardAction, canOperate: boolean, canAdmin: boolean): boolean {
  if (actionRequiresAdmin(action)) return !canAdmin
  switch (action) {
    case 'cordon':
    case 'uncordon':
    case 'wake':
      return !canOperate
    default:
      return false
  }
}

function WizardRequest({
  action,
  nodeName,
  disabled,
}: {
  action: WizardAction
  nodeName: string | null
  disabled: boolean
}) {
  if (action === 'cordon' && nodeName != null) return <RequestCordonNode name={nodeName} disabled={disabled} />
  if (action === 'uncordon' && nodeName != null) return <RequestUncordonNode name={nodeName} disabled={disabled} />
  if (action === 'drain' && nodeName != null) return <RequestDrainNode name={nodeName} disabled={disabled} />
  if (action === 'poweroff' && nodeName != null) return <RequestPowerOffNode name={nodeName} disabled={disabled} />
  return null
}

function WizardNextActionBar({
  action,
  canOperate,
  canAdmin,
  actionPending,
  nodeName,
  onWizardAction,
}: {
  action: WizardAction
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  nodeName: string | null
  onWizardAction: (action: WizardAction, context?: { profileId?: string }) => void
}) {
  const blockedByAuth = actionDisabled(action, canOperate, canAdmin)
  const needsAdmin = actionRequiresAdmin(action)
  const request = WizardRequest({
    action,
    nodeName,
    disabled: actionPending || blockedByAuth,
  })

  return (
    <div className="flex min-w-0 flex-col gap-2 border-t border-[var(--table-rule)] pt-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-dense-meta shrink-0 text-[var(--muted-foreground)]">Next action</span>
        {action === 'wake' ? (
          <Button
            size="sm"
            variant={blockedByAuth ? 'outline' : 'default'}
            disabled={actionPending || blockedByAuth}
            onClick={() => onWizardAction(action)}
          >
            {actionPending ? 'Running…' : actionLabel(action)}
          </Button>
        ) : (
          request
        )}
      </div>
      {blockedByAuth && (
        <OpsFeedback
          variant="warning"
          title={needsAdmin ? 'Permission denied — admin required' : 'Permission denied — operator required'}
        >
          {needsAdmin
            ? 'Power off / Drain need an admin token. Use Authenticate in the top bar and paste PLATFORM_ADMIN_TOKEN (operator is not enough).'
            : 'This action needs an operator token. Use Authenticate in the top bar.'}
        </OpsFeedback>
      )}
    </div>
  )
}

export function ClusterNodeWizardPanel({
  flow,
  onFlowChange,
  nodes,
  selectedNodeName,
  onSelectNodeName,
  selectedNode,
  power,
  canOperate,
  canAdmin,
  actionPending,
  onWizardAction,
  onOpenNodeDetails,
}: ClusterNodeWizardPanelProps) {
  const computeOffCycleHint = useComputeOffCycleHint(
    selectedNode?.name,
    power?.power_state,
    selectedNode?.status,
    selectedNode?.unschedulable === true,
  )
  const steps = wizardStepsForFlow(flow, selectedNode, power, computeOffCycleHint)
  const current = currentWizardStep(steps)

  const computeNodes = nodes.filter(n => n.compute_managed)
  const pickerNodes = flow === 'compute_shutdown' ? computeNodes : nodes

  return (
    <OpsSection
      title="Node operations wizard"
      description="Observed state (what the cluster reports) and procedure (what to do next) are shown separately."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-dense-meta text-muted-foreground shrink-0">Flow</span>
          <SegmentControl value={flow} onChange={v => onFlowChange(v as NodeWizardFlow)} options={FLOW_OPTIONS} size="sm" />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <span className="text-dense-meta text-muted-foreground shrink-0">Node</span>
          <select
            className="min-w-[12rem] rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 text-dense-body font-mono-tabular outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
            value={selectedNodeName ?? ''}
            onChange={e => {
              const v = e.target.value
              onSelectNodeName(v === '' ? null : v)
            }}
          >
            <option value="">Select a node…</option>
            {pickerNodes.map(n => (
              <option key={n.name} value={n.name}>
                {n.name}
              </option>
            ))}
          </select>
          {flow === 'compute_shutdown' && computeNodes.length === 0 && (
            <span className="text-dense-meta text-[var(--muted-foreground)]">No compute-managed nodes in cluster.</span>
          )}
          {selectedNode != null && onOpenNodeDetails != null && (
            <Button size="sm" variant="outline" onClick={onOpenNodeDetails}>
              Node detail panel
            </Button>
          )}
        </div>

        <div className="grid min-w-0 gap-3 sm:grid-cols-[auto_minmax(0,1fr)]">
          <NodeObservedStatePanel
            node={selectedNode}
            power={power}
            includePower={selectedNode?.compute_managed === true}
            layout="column"
          />
          <div className="flex min-w-0 flex-col gap-3">
            <WizardProcedureSteps steps={steps} flowLabel={FLOW_PROCEDURE_LABEL[flow]} />
            {current?.action != null && current.action !== 'select_node' && (
              <WizardNextActionBar
                action={current.action}
                canOperate={canOperate}
                canAdmin={canAdmin}
                actionPending={actionPending}
                nodeName={selectedNodeName}
                onWizardAction={onWizardAction}
              />
            )}
            {current?.action === 'select_node' && (
              <p className="m-0 border-t border-[var(--table-rule)] pt-3 text-dense-meta text-[var(--muted-foreground)]">
                {current.description}
              </p>
            )}
          </div>
        </div>
      </div>
    </OpsSection>
  )
}
