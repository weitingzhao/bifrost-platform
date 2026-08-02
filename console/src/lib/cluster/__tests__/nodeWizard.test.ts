import { describe, expect, it } from 'vitest'
import type { ClusterNode, JoinProfile, NodePowerResponse } from '@/api/clusterTypes'
import { computeShutdownWizardSteps, joinWizardSteps } from '@/lib/cluster/nodeWizard'

const gpuProfile: JoinProfile = {
  id: 'gpu-server',
  label: 'K3s agent join — gpu-server (P5a)',
  expected_node: 'gpu-server',
  script: 'join-gpu-server.sh',
}

const gpuNode: ClusterNode = {
  name: 'gpu-server',
  status: 'Ready',
  roles: 'worker',
  version: 'v1.35.5+k3s1',
  internal_ip: '192.168.10.60',
  reachability: 'ok',
  compute_managed: true,
  unschedulable: true,
}

const onlinePower: NodePowerResponse = {
  cluster_id: 'bifrost-bootstrap',
  node_name: 'gpu-server',
  compute_managed: true,
  node_status: 'Ready',
  power_state: 'online',
  user_pods_on_node: 0,
  pending_compute_pods: 0,
  workloads: [],
  reachability: 'ok',
  detail: 'node Ready',
  generated_at: '2026-08-02T00:00:00Z',
}

describe('joinWizardSteps', () => {
  it('marks prereq/run/verify done when expected node already in cluster (join disabled)', () => {
    const steps = joinWizardSteps(gpuProfile, false, ['gpu-server', 'ubt-k3s-01'])
    expect(steps.map(s => [s.id, s.status])).toEqual([
      ['profile', 'done'],
      ['prereq', 'done'],
      ['run', 'done'],
      ['verify', 'done'],
    ])
    expect(steps.find(s => s.id === 'prereq')?.description).toMatch(/already in cluster/i)
    expect(steps.find(s => s.id === 'run')?.description).toMatch(/no join job needed/i)
    expect(steps.find(s => s.id === 'run')?.action).toBeUndefined()
  })

  it('blocks prerequisites when join disabled and node not present', () => {
    const steps = joinWizardSteps(gpuProfile, false, ['ubt-k3s-01'])
    expect(steps.find(s => s.id === 'prereq')?.status).toBe('blocked')
    expect(steps.find(s => s.id === 'run')?.status).toBe('pending')
    expect(steps.find(s => s.id === 'verify')?.status).toBe('pending')
    expect(steps.find(s => s.id === 'run')?.action).toBeUndefined()
  })

  it('offers join action when enabled and node missing', () => {
    const steps = joinWizardSteps(gpuProfile, true, ['ubt-k3s-01'])
    expect(steps.find(s => s.id === 'prereq')?.status).toBe('done')
    expect(steps.find(s => s.id === 'run')?.status).toBe('current')
    expect(steps.find(s => s.id === 'run')?.action).toBe('join')
    expect(steps.find(s => s.id === 'verify')?.status).toBe('current')
  })
})

describe('computeShutdownWizardSteps', () => {
  it('after cordon+drain, only Power off is Next (Uncordon stays pending)', () => {
    const steps = computeShutdownWizardSteps(gpuNode, onlinePower, 'pre_poweroff')
    expect(steps.map(s => [s.id, s.status])).toEqual([
      ['cordon', 'done'],
      ['drain', 'done'],
      ['poweroff', 'current'],
      ['wake', 'pending'],
      ['uncordon', 'pending'],
    ])
    expect(steps.find(s => s.id === 'poweroff')?.action).toBe('poweroff')
    expect(steps.find(s => s.id === 'uncordon')?.action).toBeUndefined()
  })

  it('after wake, Uncordon is Next and Power off stays done', () => {
    const steps = computeShutdownWizardSteps(gpuNode, onlinePower, 'post_wake')
    expect(steps.map(s => [s.id, s.status])).toEqual([
      ['cordon', 'done'],
      ['drain', 'done'],
      ['poweroff', 'done'],
      ['wake', 'done'],
      ['uncordon', 'current'],
    ])
    expect(steps.find(s => s.id === 'uncordon')?.action).toBe('uncordon')
  })
})
