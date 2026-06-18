import type { ClusterWorkload } from '@/api/types'

export interface DeploymentGroup {
  deployment: ClusterWorkload
  pods: ClusterWorkload[]
}

export function podOwnedByDeployment(podName: string, deploymentName: string): boolean {
  return podName === deploymentName || podName.startsWith(`${deploymentName}-`)
}

export function groupWorkloadsByDeployment(workloads: ClusterWorkload[]): {
  groups: DeploymentGroup[]
  orphanPods: ClusterWorkload[]
} {
  const deployments = workloads.filter(w => w.kind === 'Deployment')
  const pods = workloads.filter(w => w.kind === 'Pod')
  const ownersByNameLength = [...deployments].sort((a, b) => b.name.length - a.name.length)

  const podBuckets = new Map<string, ClusterWorkload[]>()
  for (const d of deployments) {
    podBuckets.set(d.name, [])
  }
  const orphanPods: ClusterWorkload[] = []

  for (const pod of pods) {
    const owner = ownersByNameLength.find(d => podOwnedByDeployment(pod.name, d.name))
    if (owner) {
      podBuckets.get(owner.name)!.push(pod)
    } else {
      orphanPods.push(pod)
    }
  }

  const groups: DeploymentGroup[] = deployments
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(d => ({
      deployment: d,
      pods: (podBuckets.get(d.name) ?? []).sort((a, b) => a.name.localeCompare(b.name)),
    }))

  return { groups, orphanPods: orphanPods.sort((a, b) => a.name.localeCompare(b.name)) }
}

export function groupNeedsAttention(group: DeploymentGroup): boolean {
  if (group.deployment.reachability !== 'ok') return true
  return group.pods.some(p => p.reachability !== 'ok')
}
