import type { ApprovalItem } from '@/api/approvals'
import type { DeliveryPipelineRunView, GitOpsApplicationView } from '@/api/deliveryTypes'
import type { ReleaseRecord } from '@/api/releases'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { describe, expect, it } from 'vitest'
import {
  RELEASE_WINDOW_FREE,
  approvalHref,
  approvalSubject,
  catalogProdApp,
  formatDuration,
  isReleasePipeline,
  namespaceForLaneEnv,
  pendingReleaseApprovals,
  prodRollbackApps,
  releaseWindowView,
  splitReleaseRuns,
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

describe('splitReleaseRuns', () => {
  it('keeps running and failed release runs and drops succeeded ones', () => {
    expect(isReleasePipeline('bifrost-deliver-stg')).toBe(true)
    expect(isReleasePipeline('bifrost-build-market-data')).toBe(true)
    expect(isReleasePipeline('bifrost-ci-platform')).toBe(false)
    expect(isReleasePipeline('bifrost-smoke')).toBe(false)
    const { inProgress, superseded } = splitReleaseRuns([
      {
        pipeline: 'bifrost-deliver-stg',
        runs: [
          run({ name: 'ok', status: 'True', reason: 'Succeeded', start_time: '2026-10-05T00:00:00Z' }),
          run({ name: 'bad', status: 'False', reason: 'Failed', start_time: '2026-10-06T00:00:00Z' }),
        ],
      },
      {
        pipeline: 'bifrost-deliver-prod',
        runs: [run({ name: 'live', status: 'Unknown', reason: 'Running', start_time: '2026-10-07T03:00:00Z' })],
      },
    ])
    expect(inProgress.map(row => row.name)).toEqual(['live', 'bad'])
    expect(inProgress[0]?.kind).toBe('running')
    expect(inProgress[1]?.kind).toBe('failed')
    expect(superseded).toEqual([])
  })

  it('moves a failed run to history once a later run of the same pipeline succeeded', () => {
    const { inProgress, superseded } = splitReleaseRuns([
      {
        pipeline: 'bifrost-deliver-research',
        runs: [
          run({ name: 'old-cancelled', status: 'False', reason: 'Cancelled', start_time: '2026-10-03T19:16:00Z' }),
          run({ name: 'fixed', status: 'True', reason: 'Succeeded', start_time: '2026-10-10T03:30:00Z' }),
        ],
      },
      {
        pipeline: 'bifrost-build-flex-query',
        runs: [
          run({ name: 'flex-ok', status: 'True', reason: 'Succeeded', start_time: '2026-10-07T00:00:00Z' }),
          run({ name: 'flex-bad', status: 'False', reason: 'Failed', start_time: '2026-10-10T03:51:00Z' }),
        ],
      },
    ])
    expect(inProgress.map(row => row.name)).toEqual(['flex-bad'])
    expect(superseded).toHaveLength(1)
    expect(superseded[0]?.name).toBe('old-cancelled')
    expect(superseded[0]?.supersededBy).toBe('fixed')
  })

  it('counts a release record of the same pipeline as a later success', () => {
    const { inProgress, superseded } = splitReleaseRuns(
      [
        {
          pipeline: 'bifrost-deliver-platform',
          runs: [run({ name: 'plat-bad', status: 'False', reason: 'Failed', start_time: '2026-10-08T02:47:00Z' })],
        },
      ],
      [
        record({
          run: 'plat-later',
          pipeline: 'bifrost-deliver-platform',
          lane: 'platform',
          env: 'stg',
          started_at: '2026-10-08T02:59:00Z',
        }),
        record({
          run: 'other-pipeline',
          pipeline: 'bifrost-deliver-platform-prod',
          lane: 'platform',
          env: 'prod',
          started_at: '2026-10-09T00:00:00Z',
        }),
      ],
    )
    expect(inProgress).toEqual([])
    expect(superseded[0]?.supersededBy).toBe('plat-later')
  })

  it('keeps a failure when the only success is from another pipeline or earlier', () => {
    const { inProgress } = splitReleaseRuns(
      [
        {
          pipeline: 'bifrost-deliver-platform',
          runs: [
            run({ name: 'earlier-ok', status: 'True', reason: 'Succeeded', start_time: '2026-10-01T00:00:00Z' }),
            run({ name: 'bad', status: 'False', reason: 'Failed', start_time: '2026-10-02T00:00:00Z' }),
          ],
        },
      ],
      [record({ run: 'prod-ok', pipeline: 'bifrost-deliver-platform-prod', lane: 'platform', env: 'prod', started_at: '2026-10-03T00:00:00Z' })],
    )
    expect(inProgress.map(row => row.name)).toEqual(['bad'])
  })
})

describe('releaseWindowView', () => {
  const now = Date.parse('2026-10-10T07:00:00Z')

  it('says Unknown without a viewer token', () => {
    const view = releaseWindowView({ hasToken: false, isLoading: false, error: null, data: undefined, now })
    expect(view.state).toBe('no-token')
    expect(view.text).toBe('Unknown')
  })

  it('shows the read error as Unknown with the error text', () => {
    const view = releaseWindowView({ hasToken: true, isLoading: false, error: 'release window: HTTP 502', data: undefined, now })
    expect(view.text).toBe('Unknown')
    expect(view.detail).toBe('release window: HTTP 502')
  })

  it('says free when no one holds the window', () => {
    const view = releaseWindowView({ hasToken: true, isLoading: false, error: null, data: { open: false }, now })
    expect(view.text).toBe(RELEASE_WINDOW_FREE)
  })

  it('shows the holder and the time left', () => {
    const view = releaseWindowView({
      hasToken: true,
      isLoading: false,
      error: null,
      data: {
        open: true,
        window: {
          who: 'ada@host',
          what: 'bifrost-platform',
          env: 'prod',
          pid: 1,
          host: 'host',
          started_at: '2026-10-10T06:48:00Z',
          expires_at: '2026-10-10T07:04:00Z',
          reason: 'release 0.9',
        },
      },
      now,
    })
    expect(view.state).toBe('held')
    expect(view.text).toBe('Held by ada@host')
    expect(view.detail).toBe('bifrost-platform · prod · 4m left · held 12m · release 0.9')
  })
})

describe('pending release approvals', () => {
  function approval(partial: Partial<ApprovalItem> & Pick<ApprovalItem, 'id' | 'action'>): ApprovalItem {
    return {
      tier: 'C',
      params: {},
      params_hash: 'h',
      status: 'pending',
      reason: 'r',
      requester: 'agent',
      created_at: '2026-10-10T06:00:00Z',
      expires_at: '2026-10-11T06:00:00Z',
      ...partial,
    }
  }

  it('keeps pending release actions, longest waiting first', () => {
    const rows = pendingReleaseApprovals([
      approval({ id: 'a', action: 'start_pipeline_run', created_at: '2026-10-10T05:00:00Z' }),
      approval({ id: 'b', action: 'drain_node' }),
      approval({ id: 'c', action: 'gitops_rollback_app', created_at: '2026-10-10T01:00:00Z' }),
      approval({ id: 'd', action: 'start_pipeline_run', status: 'approved' }),
    ])
    expect(rows.map(row => row.id)).toEqual(['c', 'a'])
  })

  it('describes what the request ships and links to its detail page', () => {
    expect(
      approvalSubject(
        approval({ id: 'x', action: 'start_pipeline_run', params: { name: 'bifrost-deliver-platform-prod', revision: SHA } }),
      ),
    ).toBe('bifrost-deliver-platform-prod · abcd123')
    expect(approvalSubject(approval({ id: 'y', action: 'gitops_sync_app' }))).toBe('—')
    expect(approvalHref('ap 1')).toBe('#approvals?id=ap%201')
  })

  it('formats waits compactly', () => {
    expect(formatDuration(45 * 60_000)).toBe('45m')
    expect(formatDuration(3 * 3_600_000)).toBe('3h')
    expect(formatDuration(52 * 3_600_000)).toBe('2d 4h')
    expect(formatDuration(Number.NaN)).toBe('—')
  })
})
