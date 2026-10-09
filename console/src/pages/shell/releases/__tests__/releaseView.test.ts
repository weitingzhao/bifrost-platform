import type { DeliveryPipelineRunView, GitOpsApplicationView } from '@/api/deliveryTypes'
import type { ReleaseRecord } from '@/api/releases'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { describe, expect, it } from 'vitest'
import {
  attentionRuns,
  catalogProdApp,
  isReleasePipeline,
  namespaceForLaneEnv,
  prodRollbackApps,
  versionCell,
} from '@/pages/shell/releases/releaseView'

const SHA = 'abcd1234567890abcd1234567890abcd12345678'
const OTHER = 'fedcba9876543210fedcba9876543210fedcba98'

function record(partial: Partial<ReleaseRecord> & Pick<ReleaseRecord, 'run' | 'lane' | 'env'>): ReleaseRecord {
  return {
    pipeline: 'bifrost-deliver-stg',
    deploys: true,
    started_at: '2026-10-01T00:00:00Z',
    completed_at: '2026-10-02T00:00:00Z',
    repos: {},
    recorded_at: '2026-10-02T00:00:00Z',
    ...partial,
  }
}

function app(partial: Partial<GitOpsApplicationView> & Pick<GitOpsApplicationView, 'name'>): GitOpsApplicationView {
  return {
    namespace: 'argocd',
    sync_status: 'Synced',
    health_status: 'Healthy',
    ...partial,
  }
}

function run(partial: Partial<DeliveryPipelineRunView> & Pick<DeliveryPipelineRunView, 'name' | 'status'>): DeliveryPipelineRunView {
  return {
    namespace: 'cicd',
    pipeline: 'bifrost-deliver-stg',
    ...partial,
  }
}

describe('catalogProdApp', () => {
  it('matches the actions catalog PROD name rule', () => {
    expect(catalogProdApp('prod')).toBe(true)
    expect(catalogProdApp('prod-trade')).toBe(true)
    expect(catalogProdApp('bifrost-prod')).toBe(true)
    expect(catalogProdApp('bifrost-platform-prod')).toBe(true)
    expect(catalogProdApp('bifrost-prod-extra')).toBe(true)
    expect(catalogProdApp('bifrost-stg')).toBe(false)
    expect(catalogProdApp('bifrost-platform-stg')).toBe(false)
    expect(catalogProdApp('bifrost-research')).toBe(false)
  })
})

describe('versionCell', () => {
  it('uses the newest deploying release for that lane and environment', () => {
    const cell = versionCell({
      lane: 'platform',
      env: 'stg',
      apps: [],
      records: [
        record({
          run: 'older',
          lane: 'platform',
          env: 'stg',
          completed_at: '2026-10-01T00:00:00Z',
          repos: { 'bifrost-platform': { sha: OTHER, source: 'result' } },
        }),
        record({
          run: 'newer',
          lane: 'platform',
          env: 'stg',
          completed_at: '2026-10-07T00:00:00Z',
          tag: 'v1',
          repos: { 'bifrost-platform': { sha: SHA, source: 'result' } },
        }),
        record({
          run: 'image',
          lane: 'platform',
          env: 'image',
          deploys: false,
          repos: { 'bifrost-platform': { sha: OTHER, source: 'result' } },
        }),
      ],
    })
    expect(cell.text).toBe('v1 · platform abcd123')
    expect(cell.title).toContain('newer')
  })

  it('does not put an image-only record into STG or PROD', () => {
    const records = [
      record({
        run: 'research-image',
        lane: 'research',
        env: 'image',
        deploys: false,
        tag: '2026.10.07',
        repos: { 'bifrost-research': { sha: SHA, source: 'result' } },
      }),
    ]
    expect(versionCell({ records, lane: 'research', env: 'stg', apps: [] }).text).toBe('—')
    expect(versionCell({ records, lane: 'research', env: 'prod', apps: [] }).text).toBe('—')
    expect(versionCell({ records, lane: 'agent', env: 'stg', apps: [] }).text).toBe('—')
    expect(versionCell({ records, lane: 'agent', env: 'prod', apps: [] }).text).toBe('—')
    expect(versionCell({ records, lane: 'market-data', env: 'prod', apps: [] }).text).toBe('—')
  })

  it('renders live image text, including an absent research cell', () => {
    const runningImages = [
      { lane: 'research', env: 'stg', absent: true, text: 'No STG', title: '' },
      { lane: 'research', env: 'prod', text: 'research-api 0.205.0', title: 'research/research-api research-api:0.205.0' },
    ]
    expect(versionCell({ records: [], lane: 'research', env: 'stg', apps: [], runningImages }).text).toBe('No STG')
    const prod = versionCell({ records: [], lane: 'research', env: 'prod', apps: [], runningImages })
    expect(prod.text).toBe('research-api 0.205.0')
    expect(prod.title).toContain('research-api')
    expect(versionCell({ records: [], lane: 'platform', env: 'stg', apps: [], runningImages }).text).toBe('—')
  })

  it('falls back to the Argo revision for Platform and Trade namespaces', () => {
    const namespace = namespaceForLaneEnv('platform', 'prod')
    expect(namespace).toBe(deliveryTargetById('platform-prod').namespace)
    const cell = versionCell({
      records: [],
      lane: 'platform',
      env: 'prod',
      apps: [{ name: 'bifrost-platform-prod', destination_namespace: namespace, revision: OTHER }],
    })
    expect(cell.text).toBe('fedcba9')
    expect(cell.title).toContain('Argo CD revision')
  })
})

describe('prodRollbackApps', () => {
  it('keeps only PROD application names', () => {
    const names = prodRollbackApps([
      app({ name: 'bifrost-research' }),
      app({ name: 'bifrost-prod' }),
      app({ name: 'bifrost-stg' }),
      app({ name: 'bifrost-platform-prod' }),
    ]).map(item => item.name)
    expect(names).toEqual(['bifrost-platform-prod', 'bifrost-prod'])
  })
})

describe('attentionRuns', () => {
  it('keeps running and failed release runs and drops succeeded ones', () => {
    expect(isReleasePipeline('bifrost-deliver-stg')).toBe(true)
    expect(isReleasePipeline('bifrost-build-market-data')).toBe(true)
    expect(isReleasePipeline('bifrost-ci-platform')).toBe(false)
    expect(isReleasePipeline('bifrost-smoke')).toBe(false)
    const rows = attentionRuns([
      {
        pipeline: 'bifrost-deliver-stg',
        runs: [
          run({ name: 'ok', status: 'True', reason: 'Succeeded', completion_time: '2026-10-07T00:00:00Z' }),
          run({ name: 'bad', status: 'False', reason: 'Failed', start_time: '2026-10-06T00:00:00Z' }),
        ],
      },
      {
        pipeline: 'bifrost-deliver-prod',
        runs: [run({ name: 'live', status: 'Unknown', reason: 'Running', start_time: '2026-10-07T03:00:00Z' })],
      },
    ])
    expect(rows.map(row => row.name)).toEqual(['live', 'bad'])
    expect(rows[0]?.kind).toBe('running')
    expect(rows[1]?.kind).toBe('failed')
  })
})
