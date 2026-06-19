import type { ClusterNode, NodePowerResponse } from '@/api/types'

export type ObservedStateLamp = 'ok' | 'degraded' | 'fail' | 'unknown'

export interface NodeObservedStateRow {
  id: string
  label: string
  value: string
  lamp: ObservedStateLamp
  detail?: string
}

/** Read-only facts about the node — not workflow steps. */
export function buildNodeObservedStateRows(
  node: ClusterNode | null,
  power: NodePowerResponse | undefined,
  options?: { includePower?: boolean },
): NodeObservedStateRow[] {
  if (node == null) {
    return [
      {
        id: 'node',
        label: 'Node',
        value: 'Not selected',
        lamp: 'unknown',
        detail: 'Pick a node to see live cluster state.',
      },
    ]
  }

  const rows: NodeObservedStateRow[] = []
  const cordoned = node.unschedulable === true
  const ready = node.status === 'Ready'

  rows.push({
    id: 'readiness',
    label: 'Readiness',
    value: node.status,
    lamp: ready ? 'ok' : node.reachability === 'fail' ? 'fail' : 'degraded',
    detail: ready ? 'Kubelet reports Ready' : 'Node is not Ready',
  })

  rows.push({
    id: 'scheduling',
    label: 'Scheduling',
    value: cordoned ? 'Cordoned' : 'Open',
    lamp: cordoned ? 'degraded' : 'ok',
    detail: cordoned
      ? 'Unschedulable — no new pods will be placed here'
      : 'Schedulable — new pods may land on this node',
  })

  if (power != null) {
    const pods = power.user_pods_on_node
    rows.push({
      id: 'user-pods',
      label: 'User pods',
      value: String(pods),
      lamp: pods === 0 ? 'ok' : 'degraded',
      detail:
        pods === 0
          ? 'No evictable user workloads on this node'
          : 'Run drain to evict user workloads (DaemonSets remain)',
    })
  } else if (node.compute_managed === true) {
    rows.push({
      id: 'user-pods',
      label: 'User pods',
      value: '—',
      lamp: 'unknown',
      detail: 'Power probe pending or unavailable',
    })
  }

  if (options?.includePower !== false && node.compute_managed === true && power != null) {
    const ps = power.power_state
    const online = ps === 'online'
    rows.push({
      id: 'power',
      label: 'Host power',
      value: online ? 'Online' : ps === 'offline' ? 'Offline' : ps ?? 'Unknown',
      lamp: online ? 'ok' : ps === 'offline' ? 'unknown' : 'unknown',
      detail: online ? 'Host reachable for SSH / poweroff' : 'Host may be powered down',
    })
  }

  return rows
}

export function procedureStepStatusLabel(status: 'done' | 'current' | 'pending' | 'blocked'): string {
  switch (status) {
    case 'done':
      return 'Done'
    case 'current':
      return 'Next'
    case 'blocked':
      return 'Blocked'
    default:
      return 'Pending'
  }
}
