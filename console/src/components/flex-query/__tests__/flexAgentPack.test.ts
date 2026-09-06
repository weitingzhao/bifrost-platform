import { describe, expect, it } from 'vitest'
import {
  buildFlexAgentPack,
  type FlexAgentPackSnapshot,
} from '@/components/flex-query/flexAgentPack'

function baseSnap(over: Partial<FlexAgentPackSnapshot> = {}): FlexAgentPackSnapshot {
  return {
    generatedAt: '2026-08-31T14:00:00.000Z',
    husbandry: {
      generated_at: '2026-08-31T14:00:00.000Z',
      overall: 'degraded',
      detail: 'market_batch:missed; research_olap:degraded',
      lanes: [
        {
          id: 'market_batch',
          label: 'Market batch',
          verdict: 'missed',
          detail: 'missed slots',
        },
        {
          id: 'flex_batch',
          label: 'IB Flex',
          verdict: 'healthy',
          detail: 'ok',
          source: 'secret',
        },
        {
          id: 'research_olap',
          label: 'Research',
          verdict: 'degraded',
          detail: 'batch caution',
        },
      ],
    },
    husbandryError: null,
    plugin: {
      reachability: 'degraded',
      summary: '2/2 deployments ready · flex 0 done 9 failed · freshness 0/2 ok',
      health_reachability: 'ok',
      freshness_reachability: 'degraded',
      workers: [{ pool: 'flex', jobs_done: 0, jobs_failed: 9, status: 'ok' }],
      deployments: [
        { name: 'flex-query-api', ready: '1/1', reachability: 'ok' },
        { name: 'flex-query-worker', ready: '1/1', reachability: 'ok' },
      ],
      freshness: [
        {
          dimension: 'flex-trades',
          last_run_at: '2026-08-29T12:00:00Z',
          rows_written: 56,
          age_hours: 50,
          verdict: 'stale',
        },
        {
          dimension: 'flex-transactions',
          last_run_at: '2026-08-28T22:00:00Z',
          rows_written: 11,
          age_hours: 64,
          verdict: 'stale',
        },
      ],
    },
    pluginError: null,
    analysis: {
      findings: [
        {
          id: 'fresh-stale-flex-trades',
          severity: 'warning',
          title: 'flex-trades ingest stale',
          detail: 'Last run 2d 2h ago · 56 rows (threshold 48h).',
          enqueueKind: 'flex-trades',
        },
        {
          id: 'worker-failed',
          severity: 'danger',
          title: 'Worker reported failures',
          detail: '9 failed job(s) on pool flex.',
        },
      ],
      staleKinds: ['flex-trades', 'flex-transactions'],
      needsAttention: true,
      primaryCause: 'Ingest freshness stale (flex-trades, flex-transactions)',
    },
    kpis: {
      generated_at: '2026-08-31T14:00:00.000Z',
      last_successful_sync: {
        at: '2026-08-29T12:00:00Z',
        age_seconds: 180000,
        age_label: '2d ago',
      },
      last_run: {
        at: '2026-08-29T12:00:00Z',
        age_seconds: 180000,
        age_label: '2d ago',
        status: 'done',
        kind: 'flex-trades',
      },
      latest_execution: {
        at: '2026-08-27T00:00:00Z',
        age_seconds: 400000,
        age_label: '4d ago',
        row_count: 430,
      },
      latest_transaction: {
        at: '2026-08-21T00:00:00Z',
        age_seconds: 900000,
        age_label: '10d ago',
        row_count: 105,
      },
      next_scheduled_run: {
        at: '2026-08-31T22:00:00Z',
        until_seconds: 30000,
        until_label: 'in 8h',
        slot: 'flex-trades',
      },
      last_planned: {
        at: '2026-08-28T22:00:00Z',
        age_seconds: 230000,
        age_label: '2d 15h ago',
      },
    },
    queue: {
      now: '2026-08-31T14:00:00Z',
      counts: { pending: 0, running: 0, done: 0, failed: 9 },
      slots: [],
    },
    config: {
      tokens: {
        host_token_set: true,
        host_token_last4: 'abcd',
        secondary_token_set: true,
        secondary_token_last4: 'efgh',
        host_source: 'secret',
        secondary_source: 'secret',
      },
      source: 'secret',
      range_days: { default: 7, init: 30 },
      query_rows: [
        {
          purpose: 'trades',
          query_label: 'Trades',
          query_host_id: '123',
          query_secondary_id: '456',
        },
      ],
    },
    coverageFreshness: {
      dimensions: [
        {
          dimension: 'flex-trades',
          latest_ts: '2026-08-29T12:00:00Z',
          row_count: 56,
        },
      ],
    },
    dbSummary: {
      tables: [{ name: 'executions_raw_flex', row_count: 430, latest_ts: '2026-08-27T00:00:00Z' }],
    },
    ...over,
  }
}

describe('buildFlexAgentPack', () => {
  it('includes husbandry, findings, D10, and Copy for Agent source line', () => {
    const text = buildFlexAgentPack(baseSnap())
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('flex_batch: healthy')
    expect(text).toContain('flex-trades ingest stale')
    expect(text).toContain('token_source: secret')
    expect(text).toContain('stale_kinds_to_enqueue: flex-trades, flex-transactions')
    expect(text).toContain('Suggested investigation order')
    expect(text).not.toMatch(/host_token[^_\s]*=/i)
  })

  it('surfaces token_source=none as config fact', () => {
    const text = buildFlexAgentPack(
      baseSnap({
        config: {
          tokens: {
            host_token_set: false,
            host_token_last4: null,
            secondary_token_set: false,
            secondary_token_last4: null,
            host_source: 'none',
            secondary_source: 'none',
          },
          source: 'none',
          range_days: { default: 7, init: 30 },
          query_rows: [],
        },
      }),
    )
    expect(text).toContain('token_source: none')
    expect(text).toMatch(/sync Flex tokens/i)
  })
})

describe('self-check section', () => {
  it('leads with the verdict, the next step and the actions with their reasons', () => {
    const text = buildFlexAgentPack(
      baseSnap({
        check: {
          generated_at: '2026-09-08T11:05:00Z',
          timezone: 'America/New_York',
          verdict: 'waiting',
          next_step: 'The worker retries flex-trades at Tue 07:32 EDT; press Run now to skip the wait.',
          kinds: [
            {
              kind: 'flex-trades',
              verdict: 'waiting',
              headline: 'IB has not generated the statement yet; the worker retries at Tue 07:32 EDT (attempt 1/8).',
              detail: 'Flex query 1/2 (host): [1003] Statement is not available.',
              next_at: '2026-09-08T11:32:00Z',
              next_in_seconds: 1620,
              last_success_at: '2026-09-07T10:31:00Z',
              job: {
                id: 90, status: 'pending', attempts: 1, max_attempts: 8, error_category: 'not_ready',
                not_before: '2026-09-08T11:32:00Z', created_at: '2026-09-08T10:30:03Z', finished_at: null,
                error: '[1003] Statement is not available.', manual: false,
              },
              actions: [
                { id: 'run_now', label: 'Run now', method: 'POST', path: '/flex/ingest/jobs/90/run-now', body: null, enabled: true, reason: null },
              ],
            },
          ],
          checks: [
            { id: 'worker', ok: true, detail: 'worker alive, seen 20s ago' },
            { id: 'cooldown', ok: false, detail: 'IB throttle cooldown until Tue 07:40 EDT' },
          ],
        },
      }),
    )
    const selfCheck = text.indexOf('## Self-check')
    expect(selfCheck).toBeGreaterThan(-1)
    expect(selfCheck).toBeLessThan(text.indexOf('## Data husbandry'))
    expect(text).toContain('verdict: waiting (America/New_York, 2026-09-08T11:05:00Z)')
    expect(text).toContain('next_step: The worker retries flex-trades at Tue 07:32 EDT')
    expect(text).toContain('- flex-trades: waiting — IB has not generated the statement yet')
    expect(text).toContain('job: #90 pending attempts=1/8 category=not_ready not_before=2026-09-08T11:32:00Z')
    expect(text).toContain('action run_now: POST /flex/ingest/jobs/90/run-now — available')
    expect(text).toContain('- check cooldown: ATTENTION — IB throttle cooldown until Tue 07:40 EDT')
    expect(text).toContain('0. Start from the Self-check verdict above')
  })
  it('says so when the plugin predates the self-check', () => {
    expect(buildFlexAgentPack(baseSnap({ check: null }))).toContain('unavailable: plugin predates 0.6.1')
  })
})
