import type { ClusterNode, JoinProfile, NodePowerResponse } from '@/api/types'

export type NodeWizardFlow = 'join' | 'maintenance' | 'compute_shutdown'

export type WizardStepStatus = 'done' | 'current' | 'pending' | 'blocked'

export type WizardAction =
  | 'cordon'
  | 'drain'
  | 'uncordon'
  | 'wake'
  | 'poweroff'
  | 'join'
  | 'select_node'
  | 'select_profile'

export interface NodeWizardStep {
  id: string
  label: string
  description: string
  status: WizardStepStatus
  action?: WizardAction
}

export function maintenanceWizardSteps(
  node: ClusterNode | null,
  power?: NodePowerResponse,
): NodeWizardStep[] {
  if (node == null) {
    return [
      {
        id: 'pick-node',
        label: 'Select node',
        description: 'Pick a node from the cluster table or the selector below.',
        status: 'current',
        action: 'select_node',
      },
    ]
  }

  const cordoned = node.unschedulable === true
  const ready = node.status === 'Ready'
  const userPods = power?.user_pods_on_node ?? null
  const drainDone = userPods === 0

  const steps: NodeWizardStep[] = [
    {
      id: 'cordon',
      label: 'Cordon',
      description: 'Stop new pods from scheduling onto this node.',
      status: cordoned ? 'done' : ready ? 'current' : 'blocked',
      action: cordoned ? undefined : 'cordon',
    },
    {
      id: 'drain',
      label: 'Drain',
      description: 'Evict user workloads (DaemonSets remain). Requires admin token.',
      status: !cordoned ? 'pending' : drainDone ? 'done' : 'current',
      action: cordoned && !drainDone ? 'drain' : undefined,
    },
    {
      id: 'uncordon',
      label: 'Uncordon',
      description: 'Re-enable scheduling when maintenance is complete.',
      status: !cordoned ? 'pending' : drainDone ? 'current' : 'pending',
      action: cordoned && drainDone ? 'uncordon' : undefined,
    },
  ]

  return steps
}

export function computeShutdownWizardSteps(
  node: ClusterNode | null,
  power?: NodePowerResponse,
): NodeWizardStep[] {
  if (node == null || node.compute_managed !== true) {
    return [
      {
        id: 'pick-compute',
        label: 'Select compute node',
        description: 'Choose a managed compute node (e.g. gpu-server) from the table.',
        status: 'current',
        action: 'select_node',
      },
    ]
  }

  const cordoned = node.unschedulable === true
  const ready = node.status === 'Ready'
  const offline = power?.power_state === 'offline' || !ready
  const online = power?.power_state === 'online' || ready
  const userPods = power?.user_pods_on_node ?? 0
  const drainDone = userPods === 0

  return [
    {
      id: 'cordon',
      label: 'Cordon',
      description: 'Prevent new GPU/compute workloads from landing on the node.',
      status: cordoned ? 'done' : online ? 'current' : 'blocked',
      action: cordoned ? undefined : 'cordon',
    },
    {
      id: 'drain',
      label: 'Drain',
      description: 'Evict running pods before power off.',
      status: !cordoned ? 'pending' : drainDone ? 'done' : 'current',
      action: cordoned && !drainDone ? 'drain' : undefined,
    },
    {
      id: 'poweroff',
      label: 'Power off',
      description: 'SSH systemctl poweroff on the host (admin). Node should go NotReady.',
      status: !cordoned ? 'pending' : offline ? 'done' : drainDone && online ? 'current' : 'pending',
      action: cordoned && online && drainDone ? 'poweroff' : undefined,
    },
    {
      id: 'wake',
      label: 'Wake (WOL)',
      description: 'Send Wake-on-LAN when you need the node again.',
      status: offline ? 'current' : 'pending',
      action: offline ? 'wake' : undefined,
    },
    {
      id: 'uncordon',
      label: 'Uncordon',
      description: 'Re-enable scheduling after the node is Ready.',
      status: !ready ? 'pending' : cordoned && drainDone ? 'current' : cordoned ? 'pending' : 'done',
      action: ready && cordoned && drainDone ? 'uncordon' : undefined,
    },
  ]
}

export function joinWizardSteps(
  profile: JoinProfile | null,
  joinEnabled: boolean,
  nodeNames: string[],
): NodeWizardStep[] {
  const expected = profile?.expected_node?.trim() ?? ''
  const nodeJoined = expected !== '' && nodeNames.includes(expected)

  return [
    {
      id: 'profile',
      label: 'Select join profile',
      description: 'Configured in clusters.yaml — runs bifrost-trade-infra k3s join script.',
      status: profile != null ? 'done' : 'current',
      action: profile == null ? 'select_profile' : undefined,
    },
    {
      id: 'prereq',
      label: 'Prerequisites',
      description: joinEnabled
        ? 'K3S_TOKEN or ~/.bifrost-k3s-node-token on platform-api host; PLATFORM_NODE_JOIN_ENABLED=1.'
        : 'Set PLATFORM_NODE_JOIN_ENABLED=1 on platform-api and restart.',
      status: !joinEnabled ? 'blocked' : profile != null ? 'done' : 'pending',
    },
    {
      id: 'run',
      label: 'Run join job',
      description: profile != null ? `Execute join for profile "${profile.id}".` : 'Pick a profile first.',
      status: !joinEnabled || profile == null ? 'pending' : nodeJoined ? 'done' : 'current',
      action: joinEnabled && profile != null && !nodeJoined ? 'join' : undefined,
    },
    {
      id: 'verify',
      label: 'Verify node Ready',
      description:
        expected !== ''
          ? `Confirm node "${expected}" appears Ready in the nodes table.`
          : 'Confirm the new node appears in the cluster.',
      status: nodeJoined ? 'done' : profile != null && joinEnabled ? 'current' : 'pending',
    },
  ]
}

export function wizardStepsForFlow(
  flow: NodeWizardFlow,
  node: ClusterNode | null,
  power: NodePowerResponse | undefined,
  profile: JoinProfile | null,
  joinEnabled: boolean,
  nodeNames: string[],
): NodeWizardStep[] {
  switch (flow) {
    case 'maintenance':
      return maintenanceWizardSteps(node, power)
    case 'compute_shutdown':
      return computeShutdownWizardSteps(node, power)
    case 'join':
      return joinWizardSteps(profile, joinEnabled, nodeNames)
  }
}

export function currentWizardStep(steps: NodeWizardStep[]): NodeWizardStep | undefined {
  return steps.find(s => s.status === 'current' || s.status === 'blocked')
}

export function stepStatusLamp(status: WizardStepStatus): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch (status) {
    case 'done':
      return 'ok'
    case 'current':
      return 'degraded'
    case 'blocked':
      return 'fail'
    default:
      return 'unknown'
  }
}
