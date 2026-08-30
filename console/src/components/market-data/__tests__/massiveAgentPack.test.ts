import { describe, expect, it } from 'vitest'
import {
  buildMassiveAgentPack,
  type MassiveAgentPackSnapshot,
} from '@/components/market-data/massiveAgentPack'

function baseSnap(over: Partial<MassiveAgentPackSnapshot> = {}): MassiveAgentPackSnapshot {
  return {
    generatedAt: '2026-08-30T04:00:00.000Z',
    husbandry: {
      generated_at: '2026-08-30T04:00:00.000Z',
      overall: 'caution',
      detail: 'lanes draining or due',
      lanes: [
        {
          id: 'market_batch',
          label: 'Market batch',
          verdict: 'draining',
          detail: '100 pending',
        },
        {
          id: 'flex_batch',
          label: 'IB Flex',
          verdict: 'healthy',
          detail: 'source=secret',
          source: 'secret',
        },
      ],
    },
    husbandryError: null,
    plugin: {
      reachability: 'degraded',
      summary: 'probe degraded',
      health_reachability: 'ok',
      freshness_reachability: 'degraded',
      workers: [{ pool: 'stocks', jobs_done: 90, jobs_failed: 6, status: 'ok' }],
      freshness: [
        {
          dimension: 'ticker_sync',
          last_run_at: '2026-08-29T21:00:00Z',
          rows_written: 5000,
          age_hours: 7,
          verdict: 'stale',
        },
        {
          dimension: 'stock_daily',
          last_run_at: '2026-08-30T03:00:00Z',
          rows_written: 1,
          age_hours: 1,
          verdict: 'ok',
        },
      ],
    },
    pluginError: null,
    qualitySummary: 'PASS',
    queue: {
      ok: true,
      husbandry: { verdict: 'draining', detail: '100 pending' },
      schedule: { verdict: 'on_plan', on_plan: 10, due: 0, missed: 0, slots: [] },
      queue: { verdict: 'draining', pending: 100, running: 2 },
      throughput: { done_last_15m: 50, failed_last_15m: 1, eta_minutes_at_current_rate: 12 },
    },
    universe: { ok: true, total_tickers: 5367 },
    dbSummary: {
      ok: true,
      counts: { stock_daily: 3_454_337, option_contract: 155_096 },
      freshness: [
        { dimension: 'ticker_sync', last_run_at: '2026-08-29T21:00:00Z' },
        { dimension: 'stock_daily', last_run_at: '2026-08-30T03:00:00Z' },
        { dimension: 'option_contract', last_run_at: '2026-08-30T01:00:00Z' },
      ],
    },
    inventory: null,
    incomeStatementSymbols: 4463,
    ...over,
  }
}

describe('buildMassiveAgentPack', () => {
  it('includes husbandry lanes and agent goal constraints', () => {
    const text = buildMassiveAgentPack(baseSnap())
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('market_batch: draining')
    expect(text).toContain('flex_batch: healthy')
    expect(text).toContain('Universe:')
    expect(text).toContain('Suggested investigation order')
  })

  it('warns when coverage inventory is missing (analytics blocked root cause)', () => {
    const text = buildMassiveAgentPack(baseSnap({ inventory: null }))
    expect(text).toContain('coverage/inventory unavailable')
    expect(text).toContain('blocked')
    expect(text).toContain('SEPA Fundamental')
  })

  it('surfaces inventory feedstock counts when present', () => {
    const text = buildMassiveAgentPack(
      baseSnap({
        inventory: {
          ok: true,
          option: { snapshot_symbols: 28, oi_symbols: 28 },
          stock_daily: { symbols: 14828 },
        },
      }),
    )
    expect(text).toContain('snapshot=28')
    expect(text).toContain('oi=28')
    expect(text).not.toContain('coverage/inventory unavailable')
  })
})
