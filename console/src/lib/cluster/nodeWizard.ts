import type { ClusterNode, NodePowerResponse } from '@/api/clusterTypes'

export type NodeWizardFlow = 'maintenance' | 'compute_shutdown'

export type WizardStepStatus = 'done' | 'current' | 'pending' | 'blocked'

export type WizardAction =
  | 'cordon'
  | 'drain'
  | 'uncordon'
  | 'wake'
  | 'poweroff'
  | 'select_node'

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

export function wizardStepsForFlow(
  flow: NodeWizardFlow,
  node: ClusterNode | null,
  power: NodePowerResponse | undefined,
  computeOffCycleHint: ComputeOffCycleHint = 'pre_poweroff',
): NodeWizardStep[] {
  switch (flow) {
    case 'maintenance':
      return maintenanceWizardSteps(node, power)
    case 'compute_shutdown':
      return computeShutdownWizardSteps(node, power, computeOffCycleHint)
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
