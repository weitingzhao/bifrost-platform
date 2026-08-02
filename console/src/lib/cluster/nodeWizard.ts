import type { ClusterNode, JoinProfile, NodePowerResponse } from '@/api/clusterTypes'

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

/**
 * Session hint for Compute off: after the host has been observed offline once,
 * coming back online means "post-wake → Uncordon", not "Power off again".
 */
export type ComputeOffCycleHint = 'pre_poweroff' | 'post_wake'

export function computeShutdownWizardSteps(
  node: ClusterNode | null,
  power?: NodePowerResponse,
  cycleHint: ComputeOffCycleHint = 'pre_poweroff',
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
  const postWake = cycleHint === 'post_wake' && online && ready

  let poweroffStatus: WizardStepStatus
  let poweroffAction: WizardAction | undefined
  if (!cordoned) {
    poweroffStatus = 'pending'
    poweroffAction = undefined
  } else if (offline || postWake) {
    poweroffStatus = 'done'
    poweroffAction = undefined
  } else if (drainDone && online) {
    poweroffStatus = 'current'
    poweroffAction = 'poweroff'
  } else {
    poweroffStatus = 'pending'
    poweroffAction = undefined
  }

  let wakeStatus: WizardStepStatus
  let wakeAction: WizardAction | undefined
  if (offline) {
    wakeStatus = 'current'
    wakeAction = 'wake'
  } else if (postWake) {
    wakeStatus = 'done'
    wakeAction = undefined
  } else {
    wakeStatus = 'pending'
    wakeAction = undefined
  }

  let uncordonStatus: WizardStepStatus
  let uncordonAction: WizardAction | undefined
  if (!cordoned) {
    uncordonStatus = 'done'
    uncordonAction = undefined
  } else if (postWake && drainDone) {
    uncordonStatus = 'current'
    uncordonAction = 'uncordon'
  } else {
    uncordonStatus = 'pending'
    uncordonAction = undefined
  }

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
      status: poweroffStatus,
      action: poweroffAction,
    },
    {
      id: 'wake',
      label: 'Wake (WOL)',
      description: 'Send Wake-on-LAN when you need the node again.',
      status: wakeStatus,
      action: wakeAction,
    },
    {
      id: 'uncordon',
      label: 'Uncordon',
      description: 'Re-enable scheduling after the node is Ready.',
      status: uncordonStatus,
      action: uncordonAction,
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

  let prereqStatus: WizardStepStatus
  let prereqDescription: string
  if (nodeJoined) {
    prereqStatus = 'done'
    prereqDescription =
      expected !== ''
        ? `Node "${expected}" already in cluster — join prerequisites not required.`
        : 'Expected node already in cluster — join prerequisites not required.'
  } else if (!joinEnabled) {
    prereqStatus = 'blocked'
    prereqDescription = 'Set PLATFORM_NODE_JOIN_ENABLED=1 on platform-api and restart.'
  } else if (profile != null) {
    prereqStatus = 'done'
    prereqDescription =
      'K3S_TOKEN or ~/.bifrost-k3s-node-token on platform-api host; PLATFORM_NODE_JOIN_ENABLED=1.'
  } else {
    prereqStatus = 'pending'
    prereqDescription =
      'K3S_TOKEN or ~/.bifrost-k3s-node-token on platform-api host; PLATFORM_NODE_JOIN_ENABLED=1.'
  }

  let runStatus: WizardStepStatus
  let runDescription: string
  if (profile == null) {
    runStatus = 'pending'
    runDescription = 'Pick a profile first.'
  } else if (nodeJoined) {
    runStatus = 'done'
    runDescription = `Node "${profile.expected_node || profile.id}" already present — no join job needed.`
  } else if (!joinEnabled) {
    runStatus = 'pending'
    runDescription = `Execute join for profile "${profile.id}" (enable join first).`
  } else {
    runStatus = 'current'
    runDescription = `Execute join for profile "${profile.id}".`
  }

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
      description: prereqDescription,
      status: prereqStatus,
    },
    {
      id: 'run',
      label: 'Run join job',
      description: runDescription,
      status: runStatus,
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
  computeOffCycleHint: ComputeOffCycleHint = 'pre_poweroff',
): NodeWizardStep[] {
  switch (flow) {
    case 'maintenance':
      return maintenanceWizardSteps(node, power)
    case 'compute_shutdown':
      return computeShutdownWizardSteps(node, power, computeOffCycleHint)
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
