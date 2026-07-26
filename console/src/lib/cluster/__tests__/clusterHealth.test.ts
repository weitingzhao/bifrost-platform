import { describe, expect, it } from 'vitest'
import type { ClusterSummary } from '@/api/clusterTypes'
import { deriveClusterVerdict } from '@/lib/cluster/clusterHealth'

function fixture(overrides: Partial<ClusterSummary> = {}): ClusterSummary {
  return {
    cluster_id: 'default',
    label: 'Default cluster',
    distribution: 'k3s',
    api_server: 'https://127.0.0.1:6443',
    kubeconfig_path: '/tmp/kubeconfig',
    reachability: 'ok',
    detail: 'All systems nominal',
    nodes_ready: 3,
    nodes_total: 3,
    failing_pods: 0,
    running_pods: 10,
    pending_pods: 0,
    generated_at: '2026-07-24T00:00:00Z',
    ...overrides,
  }
}

const baseInput = {
  summary: fixture(),
  unreachable: false,
  showBootstrapActions: false,
  summaryFailed: false,
  isProbing: false,
}

describe('deriveClusterVerdict', () => {
  it('probing → unknown / PROBING', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: undefined,
      isProbing: true,
    })
    expect(v.lamp).toBe('unknown')
    expect(v.tagLabel).toBe('PROBING')
    expect(v.tagVariant).toBe('neutral')
    expect(v.summaryLine).toMatch(/Loading/i)
  })

  it('unreachable → fail / UNREACHABLE', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      unreachable: true,
      summary: fixture({ detail: 'kubeconfig missing' }),
    })
    expect(v.lamp).toBe('fail')
    expect(v.tagLabel).toBe('UNREACHABLE')
    expect(v.tagVariant).toBe('danger')
    expect(v.summaryLine).toBe('kubeconfig missing')
  })

  it('summaryFailed → fail / UNREACHABLE', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: undefined,
      summaryFailed: true,
    })
    expect(v.lamp).toBe('fail')
    expect(v.tagLabel).toBe('UNREACHABLE')
    expect(v.summaryLine).toBe('Cluster unreachable')
  })

  it('reachability fail → fail / FAIL', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({ reachability: 'fail', detail: 'API server down' }),
    })
    expect(v.lamp).toBe('fail')
    expect(v.tagLabel).toBe('FAIL')
    expect(v.summaryLine).toBe('API server down')
  })

  it('bootstrap → degraded / BOOTSTRAP (before failing pods)', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      showBootstrapActions: true,
      summary: fixture({ failing_pods: 2 }),
    })
    expect(v.lamp).toBe('degraded')
    expect(v.tagLabel).toBe('BOOTSTRAP')
    expect(v.tagVariant).toBe('warning')
    expect(v.summaryLine).toContain('One-time bootstrap required')
    expect(v.summaryLine).toContain('2 failing pods')
  })

  it('bootstrap without failing pods has no failing note', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      showBootstrapActions: true,
      summary: fixture({ failing_pods: 0 }),
    })
    expect(v.tagLabel).toBe('BOOTSTRAP')
    expect(v.summaryLine).not.toContain('failing')
  })

  it('failing_pods → fail / N FAILING', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({ failing_pods: 2 }),
    })
    expect(v.lamp).toBe('fail')
    expect(v.tagLabel).toBe('2 FAILING')
    expect(v.tagVariant).toBe('danger')
    expect(v.summaryLine).toBe('2 failing pods')
    expect(v.evidenceLine).toContain('3/3 nodes')
    expect(v.evidenceLine).toContain('2 failing pods')
  })

  it('nodes partial → degraded / DEGRADED', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({
        reachability: 'ok',
        nodes_ready: 2,
        nodes_total: 3,
        detail: 'One node NotReady',
      }),
    })
    expect(v.lamp).toBe('degraded')
    expect(v.tagLabel).toBe('DEGRADED')
    expect(v.tagVariant).toBe('warning')
    expect(v.summaryLine).toBe('2/3 core nodes ready')
    expect(v.evidenceLine).toBe('2/3 nodes')
  })

  it('reachability degraded → degraded / DEGRADED', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({
        reachability: 'degraded',
        detail: 'Partial API pressure',
        failing_pods: 0,
      }),
    })
    expect(v.lamp).toBe('degraded')
    expect(v.tagLabel).toBe('DEGRADED')
  })

  it('elastic_degraded alone → degraded / DEGRADED (not false READY)', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({
        reachability: 'ok',
        nodes_ready: 3,
        nodes_total: 3,
        failing_pods: 0,
        elastic_degraded: 2,
      }),
    })
    expect(v.lamp).toBe('degraded')
    expect(v.tagLabel).toBe('DEGRADED')
    expect(v.tagVariant).toBe('warning')
    expect(v.summaryLine).toBe('2 elastic degraded')
    expect(v.evidenceLine).toContain('3/3 nodes')
    expect(v.evidenceLine).toContain('2 elastic degraded')
  })

  it('failing pods wins over elastic_degraded', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      summary: fixture({
        failing_pods: 1,
        elastic_degraded: 2,
      }),
    })
    expect(v.tagLabel).toBe('1 FAILING')
    expect(v.lamp).toBe('fail')
    expect(v.evidenceLine).toContain('1 failing pod')
    expect(v.evidenceLine).toContain('2 elastic degraded')
  })

  it('healthy → ok / READY', () => {
    const v = deriveClusterVerdict(baseInput)
    expect(v.lamp).toBe('ok')
    expect(v.tagLabel).toBe('READY')
    expect(v.tagVariant).toBe('success')
    expect(v.summaryLine).toBe('All checks pass')
    expect(v.evidenceLine).toBe('3/3 nodes')
  })

  it('unreachable wins over bootstrap', () => {
    const v = deriveClusterVerdict({
      ...baseInput,
      unreachable: true,
      showBootstrapActions: true,
    })
    expect(v.tagLabel).toBe('UNREACHABLE')
  })
})
