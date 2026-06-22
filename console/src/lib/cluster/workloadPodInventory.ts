import type { ClusterNamespace, ClusterWorkload } from '@/api/types'
import { groupWorkloadsByDeployment } from '@/lib/cluster/workloadTree'

export type PodPhaseCounts = {
  running: number
  succeeded: number
  pending: number
  failed: number
  other: number
}

export type NamespacePodInventory = {
  /** All Pod objects in the namespace (from cluster API when available). */
  totalPods: number
  deploymentCount: number
  /** Pods matched to a Deployment by name prefix. */
  deploymentPodCount: number
  /** Pods not owned by any Deployment in this namespace. */
  standalonePodCount: number
  phases: PodPhaseCounts
  failingPods: number
}

function countPhases(pods: ClusterWorkload[]): PodPhaseCounts {
  const phases: PodPhaseCounts = { running: 0, succeeded: 0, pending: 0, failed: 0, other: 0 }
  for (const pod of pods) {
    switch (pod.status) {
      case 'Running':
        phases.running += 1
        break
      case 'Succeeded':
        phases.succeeded += 1
        break
      case 'Pending':
        phases.pending += 1
        break
      case 'Failed':
        phases.failed += 1
        break
      default:
        phases.other += 1
        break
    }
  }
  return phases
}

/** Pod inventory for the selected namespace — aligns chip total with table tabs. */
export function buildNamespacePodInventory(
  workloads: ClusterWorkload[],
  nsMeta: Pick<ClusterNamespace, 'pod_count' | 'failing_pods'> | null | undefined,
): NamespacePodInventory | null {
  if (nsMeta == null && workloads.length === 0) return null

  const pods = workloads.filter(w => w.kind === 'Pod')
  const { groups, orphanPods } = groupWorkloadsByDeployment(workloads)
  const deploymentPodCount = groups.reduce((sum, group) => sum + group.pods.length, 0)

  return {
    totalPods: nsMeta?.pod_count ?? pods.length,
    deploymentCount: groups.length,
    deploymentPodCount,
    standalonePodCount: orphanPods.length,
    phases: countPhases(pods),
    failingPods: nsMeta?.failing_pods ?? 0,
  }
}
