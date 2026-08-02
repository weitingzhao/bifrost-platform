import { Button, SegmentControl } from '@bifrost/ui'
import type { ClusterNode, JoinProfilesResponse, NodePowerResponse } from '@/api/clusterTypes'
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
  joinProfiles: JoinProfilesResponse | undefined
  selectedJoinProfileId: string | null
  onSelectJoinProfileId: (id: string | null) => void
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  onWizardAction: (action: WizardAction, context?: { profileId?: string }) => void
  onOpenNodeDetails?: () => void
}

const FLOW_OPTIONS: { value: NodeWizardFlow; label: string }[] = [
  { value: 'maintenance', label: 'Maintain' },
  { value: 'compute_shutdown', label: 'Compute off' },
  { value: 'join', label: 'Join node' },
]

const FLOW_PROCEDURE_LABEL: Record<NodeWizardFlow, string> = {
  maintenance: 'Maintain',
  compute_shutdown: 'Compute off',
  join: 'Join node',
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
    case 'join':
      return 'Run join job'
    default:
      return 'Continue'
  }
}

function actionRequiresAdmin(action: WizardAction): boolean {
  return action === 'drain' || action === 'poweroff' || action === 'join'
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

function WizardNextActionBar({
  action,
  canOperate,
  canAdmin,
  actionPending,
  profileId,
  onWizardAction,
}: {
  action: WizardAction
  canOperate: boolean
  canAdmin: boolean
  actionPending: boolean
  profileId?: string
  onWizardAction: (action: WizardAction, context?: { profileId?: string }) => void
}) {
  const blockedByAuth = actionDisabled(action, canOperate, canAdmin)
  const needsAdmin = actionRequiresAdmin(action)

  return (
    <div className="flex min-w-0 flex-col gap-2 border-t border-[var(--border)] pt-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-dense-meta shrink-0 text-[var(--muted-foreground)]">Next action</span>
        <Button
          size="sm"
          variant={blockedByAuth ? 'outline' : 'default'}
          disabled={actionPending || blockedByAuth}
          onClick={() => onWizardAction(action, { profileId })}
        >
          {actionPending ? 'Running…' : actionLabel(action)}
        </Button>
      </div>
      {blockedByAuth && (
        <OpsFeedback
          variant="warning"
          title={needsAdmin ? 'Permission denied — admin required' : 'Permission denied — operator required'}
        >
          {needsAdmin
            ? 'Power off / Drain / Join need an admin token. Use Authenticate in the top bar and paste PLATFORM_ADMIN_TOKEN (operator is not enough).'
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
  joinProfiles,
  selectedJoinProfileId,
  onSelectJoinProfileId,
  canOperate,
  canAdmin,
  actionPending,
  onWizardAction,
  onOpenNodeDetails,
}: ClusterNodeWizardPanelProps) {
  const joinProfile =
    joinProfiles?.profiles.find(p => p.id === selectedJoinProfileId) ??
    joinProfiles?.profiles[0] ??
    null

  const nodeNames = nodes.map(n => n.name)
  const computeOffCycleHint = useComputeOffCycleHint(
    selectedNode?.name,
    power?.power_state,
    selectedNode?.status,
    selectedNode?.unschedulable === true,
  )
  const steps = wizardStepsForFlow(
    flow,
    selectedNode,
    power,
    flow === 'join' ? joinProfile : null,
    joinProfiles?.enabled === true,
    nodeNames,
    computeOffCycleHint,
  )
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

        {flow !== 'join' && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-dense-meta text-muted-foreground shrink-0">Node</span>
            <select
              className="min-w-[12rem] rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 text-dense-body font-mono-tabular"
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
        )}

        {flow === 'join' && joinProfiles != null && joinProfiles.profiles.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-dense-meta text-muted-foreground shrink-0">Profile</span>
            <select
              className="min-w-[16rem] rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 text-dense-body"
              value={selectedJoinProfileId ?? joinProfiles.profiles[0]?.id ?? ''}
              onChange={e => onSelectJoinProfileId(e.target.value || null)}
            >
              {joinProfiles.profiles.map(p => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
            {!joinProfiles.enabled &&
              !(joinProfile?.expected_node && nodeNames.includes(joinProfile.expected_node)) && (
              <span className="text-dense-meta lamp-warn">{joinProfiles.detail ?? 'Join disabled'}</span>
            )}
            {joinProfile?.expected_node && nodeNames.includes(joinProfile.expected_node) && (
              <span className="text-dense-meta text-[var(--muted-foreground)]">
                Already joined — no action needed
              </span>
            )}
          </div>
        )}

        {flow !== 'join' ? (
          <div className="grid min-w-0 gap-3 sm:grid-cols-[auto_minmax(0,1fr)]">
            <NodeObservedStatePanel
              node={selectedNode}
              power={power}
              includePower={selectedNode?.compute_managed === true}
              layout="column"
            />
            <div className="flex min-w-0 flex-col gap-3">
              <WizardProcedureSteps steps={steps} flowLabel={FLOW_PROCEDURE_LABEL[flow]} />
              {current?.action != null &&
                current.action !== 'select_node' &&
                current.action !== 'select_profile' && (
                  <WizardNextActionBar
                    action={current.action}
                    canOperate={canOperate}
                    canAdmin={canAdmin}
                    actionPending={actionPending}
                    profileId={joinProfile?.id}
                    onWizardAction={onWizardAction}
                  />
                )}
              {(current?.action === 'select_node' || current?.action === 'select_profile') && (
                <p className="m-0 border-t border-[var(--border)] pt-3 text-dense-meta text-[var(--muted-foreground)]">
                  {current.description}
                </p>
              )}
            </div>
          </div>
        ) : (
          <>
            <WizardProcedureSteps steps={steps} flowLabel={FLOW_PROCEDURE_LABEL[flow]} />
            {current?.action != null &&
              current.action !== 'select_node' &&
              current.action !== 'select_profile' && (
                <WizardNextActionBar
                  action={current.action}
                  canOperate={canOperate}
                  canAdmin={canAdmin}
                  actionPending={actionPending}
                  profileId={joinProfile?.id}
                  onWizardAction={onWizardAction}
                />
              )}
            {(current?.action === 'select_node' || current?.action === 'select_profile') && (
              <p className="m-0 border-t border-[var(--border)] pt-3 text-dense-meta text-[var(--muted-foreground)]">
                {current.description}
              </p>
            )}
          </>
        )}
      </div>
    </OpsSection>
  )
}
