import { describe, expect, it } from 'vitest'
import type { ClusterWorkload } from '@/api/clusterTypes'
import {
  formatWorkloadRollout,
  isDeploymentRolloutComplete,
} from '@/lib/cluster/workloadRollout'

function deploy(partial: Partial<ClusterWorkload>): ClusterWorkload {
  return {
    namespace: 'bifrost-prod',
    kind: 'Deployment',
    name: 'account-sync',
    ready: '1/1',
    status: 'Ready',
    restarts: 0,
    age: '1h',
    reachability: 'ok',
    ...partial,
  }
}

describe('workloadRollout', () => {
  it('formats updated/ready/available while Progressing', () => {
    expect(
      formatWorkloadRollout(
        deploy({
          desired_replicas: 2,
          ready_replicas: 2,
          updated_replicas: 1,
          available_replicas: 2,
          status: 'Progressing',
          ready: '2/2',
        }),
      ),
    ).toBe('upd 1/2 · ready 2/2 · avail 2/2 · Progressing')
  })

  it('omits Ready status suffix when complete', () => {
    expect(
      formatWorkloadRollout(
        deploy({
          desired_replicas: 1,
          ready_replicas: 1,
          updated_replicas: 1,
          available_replicas: 1,
          status: 'Ready',
        }),
      ),
    ).toBe('upd 1/1 · ready 1/1 · avail 1/1')
  })

  it('isDeploymentRolloutComplete requires updated+ready+available', () => {
    expect(
      isDeploymentRolloutComplete(
        deploy({
          desired_replicas: 2,
          ready_replicas: 2,
          updated_replicas: 1,
          available_replicas: 2,
          status: 'Progressing',
        }),
      ),
    ).toBe(false)
    expect(
      isDeploymentRolloutComplete(
        deploy({
          desired_replicas: 2,
          ready_replicas: 2,
          updated_replicas: 2,
          available_replicas: 2,
          status: 'Ready',
        }),
      ),
    ).toBe(true)
  })
})
