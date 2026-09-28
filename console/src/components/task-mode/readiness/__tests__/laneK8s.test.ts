import { describe, expect, it } from 'vitest'
import type { ClusterSummary } from '@/api/clusterTypes'
import { laneK8s, namespacePods } from '../utils'

function cluster(over: Partial<ClusterSummary> = {}): ClusterSummary {
  return {
    cluster_id: 'k3s',
    label: 'k3s',
    distribution: 'k3s',
    api_server: 'https://192.168.10.73:6443',
    kubeconfig_path: '',
    reachability: 'ok',
    detail: 'cluster API reachable',
    nodes_ready: 5,
    nodes_total: 5,
    failing_pods: 0,
    failing_pod_details: [],
    running_pods: 100,
    pending_pods: 0,
    generated_at: '2026-09-28T02:40:00Z',
    ...over,
  }
}

const researchJobFailed = cluster({
  reachability: 'degraded',
  detail: 'cluster API reachable; 1 failing pods',
  failing_pods: 1,
  failing_pod_details: [
    { namespace: 'research', name: 'research-harness-1-abc', phase: 'Failed', reason: 'Error' },
  ],
})

describe('namespacePods', () => {
  it('does not degrade a namespace over a pod failing in another one', () => {
    // 2026-09-28: one failed pod elsewhere held Rocket and Trade launch NO-GO.
    const s = namespacePods(researchJobFailed, 'bifrost-prod')
    expect(s.signal).toBe('ok')
    expect(s.detail).toBe('bifrost-prod OK · 1 failing elsewhere')
  })

  it('degrades the namespace the failing pod is in', () => {
    expect(namespacePods(researchJobFailed, 'research').signal).toBe('degraded')
  })

  it('degrades on nodes not ready', () => {
    const c = cluster({ reachability: 'degraded', nodes_ready: 4 })
    expect(namespacePods(c, 'bifrost-prod').signal).toBe('degraded')
  })

  it('degrades on an elastic node degraded', () => {
    const c = cluster({ reachability: 'degraded', elastic_degraded: 1 })
    expect(namespacePods(c, 'bifrost-prod').signal).toBe('degraded')
  })

  it('fails when the cluster API is down', () => {
    expect(namespacePods(cluster({ reachability: 'fail' }), 'bifrost-prod').signal).toBe('fail')
  })
})

describe('laneK8s', () => {
  it('judges the lane on its namespace and keeps the cluster line as context', () => {
    const s = laneK8s(researchJobFailed, 'bifrost-prod')
    expect(s.signal).toBe('ok')
    expect(s.detail).toContain('1 failing pods')
    expect(s.detail).toContain('bifrost-prod OK')
  })
})
