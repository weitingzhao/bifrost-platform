import { describe, expect, it } from 'vitest'
import type { ClusterWorkload } from '@/api/clusterTypes'
import {
  IB_GATEWAY_PLUGIN_NS,
  IB_GATEWAY_WORKLOAD,
  resolveCriticalProcesses,
} from '@/lib/satellite-bus/criticalProcesses'

function wl(
  partial: Partial<ClusterWorkload> & Pick<ClusterWorkload, 'name' | 'namespace'>,
): ClusterWorkload {
  return {
    kind: 'Deployment',
    ready: '1/1',
    status: 'Ready',
    restarts: 0,
    age: '1d',
    reachability: 'ok',
    ...partial,
  }
}

describe('resolveCriticalProcesses', () => {
  it('maps IB Ingestor/Operator/Account Agent to data/ib-gateway when Trade NS has no legacy edge', () => {
    const trade = [
      wl({ name: 'daemon', namespace: 'bifrost-prod', ready: '2/2' }),
      wl({ name: 'account-sync', namespace: 'bifrost-prod' }),
      wl({ name: 'api-massive', namespace: 'bifrost-prod' }),
      wl({ name: 'polygon-ws-ingestor', namespace: 'bifrost-prod' }),
      wl({ name: 'celery-beat', namespace: 'bifrost-prod' }),
      wl({ name: 'celery-worker', namespace: 'bifrost-prod', ready: '0/1', status: 'Pending', reachability: 'degraded' }),
      wl({ name: 'flower', namespace: 'bifrost-prod' }),
    ]
    const plugin = [
      wl({ name: IB_GATEWAY_WORKLOAD, namespace: IB_GATEWAY_PLUGIN_NS, ready: '1/1' }),
    ]

    const rows = resolveCriticalProcesses('bifrost-prod', trade, plugin)
    const byLabel = Object.fromEntries(rows.map(r => [r.label, r]))

    expect(byLabel['IB Ingestor']).toMatchObject({
      name: 'ib-gateway',
      namespace: 'data',
      ready: '1/1',
      reachability: 'ok',
    })
    expect(byLabel['IB Ingestor'].status).toMatch(/IB Gateway plugin/)
    expect(byLabel['IB Operator']).toMatchObject({
      name: 'ib-gateway',
      namespace: 'data',
      ready: '1/1',
    })
    expect(byLabel['IB Account Agent']).toMatchObject({
      name: 'ib-gateway',
      namespace: 'data',
    })
    // Must not confuse account-sync / api-massive / celery-beat
    expect(byLabel['IB Account Agent'].name).not.toBe('account-sync')
    expect(byLabel['Polygon WS Ingestor']).toMatchObject({
      name: 'polygon-ws-ingestor',
      namespace: 'bifrost-prod',
    })
    expect(byLabel['Celery worker']).toMatchObject({
      name: 'celery-worker',
      ready: '0/1',
      reachability: 'degraded',
    })
    expect(byLabel['Flower'].name).toBe('flower')
    expect(byLabel['GsTrading daemon'].ready).toBe('2/2')
  })

  it('prefers Trade-NS legacy edge names over plugin when present', () => {
    const trade = [
      wl({ name: 'ib-ingestor', namespace: 'bifrost-stg', ready: '1/1' }),
      wl({ name: 'ib-operator', namespace: 'bifrost-stg', ready: '1/1' }),
      wl({ name: 'daemon', namespace: 'bifrost-stg' }),
    ]
    const plugin = [wl({ name: IB_GATEWAY_WORKLOAD, namespace: IB_GATEWAY_PLUGIN_NS })]

    const rows = resolveCriticalProcesses('bifrost-stg', trade, plugin)
    const byLabel = Object.fromEntries(rows.map(r => [r.label, r]))

    expect(byLabel['IB Ingestor']).toMatchObject({
      name: 'ib-ingestor',
      namespace: 'bifrost-stg',
      status: 'Ready',
    })
    expect(byLabel['IB Operator'].name).toBe('ib-operator')
    expect(byLabel['IB Operator'].status).not.toMatch(/plugin/)
  })

  it('shows not deployed when neither Trade nor plugin match', () => {
    const rows = resolveCriticalProcesses('bifrost-prod', [], [])
    const ingest = rows.find(r => r.label === 'IB Ingestor')
    expect(ingest).toMatchObject({
      name: '—',
      status: 'not deployed',
      reachability: 'unknown',
    })
  })
})
