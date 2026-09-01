import { describe, expect, it } from 'vitest'
import {
  analyzeResearchEngine,
  buildResearchEngineAgentPack,
  buildResearchEngineDiagnosePrefill,
  type ResearchEngineAgentPackSnapshot,
} from '@/components/research/researchEngineAgentPack'

function baseSnap(over: Partial<ResearchEngineAgentPackSnapshot> = {}): ResearchEngineAgentPackSnapshot {
  return {
    generatedAt: '2026-08-31T17:16:00.000Z',
    husbandry: {
      generated_at: '2026-08-31T17:16:00.000Z',
      overall: 'degraded',
      detail: 'research_olap:degraded',
      lanes: [
        { id: 'market_batch', label: 'Market batch', verdict: 'healthy', detail: '16 slots on plan' },
        {
          id: 'flex_batch',
          label: 'IB Flex',
          verdict: 'healthy',
          detail: 'source=secret',
          source: 'secret',
        },
        {
          id: 'research_olap',
          label: 'Research OLAP',
          verdict: 'degraded',
          detail: 'stale/missing feature tables',
        },
      ],
    },
    husbandryError: null,
    status: { reachable: true, generated_at: '2026-08-31T17:16:00.000Z' },
    statusError: null,
    health: { version: '0.50.2', startup_ok: true },
    signalHealth: {
      overall: 'degraded',
      as_of: '2026-08-31T17:16:46.000Z',
      freshness: [
        { label: 'vrp', status: 'fresh', age_hours: 2.2, max_computed_at: '2026-08-31T15:00:00Z' },
        {
          label: 'canonical_pnl',
          status: 'stale',
          age_hours: 36.4,
          max_computed_at: '2026-08-30T04:50:00Z',
          table: 'features.stock_signal_canonical_pnl_daily',
        },
        { label: 'iv_reconstructed', status: 'fresh', age_hours: 2.1 },
        { label: 'playbook_trigger', status: 'fresh', age_hours: 1.8 },
        {
          label: 'scan',
          status: 'stale',
          age_hours: 36.4,
          max_computed_at: '2026-08-30T04:50:00Z',
          table: 'features.stock_signal_scan_daily',
        },
        { label: 'forecast_settlement', status: 'fresh', age_hours: 2.2 },
      ],
    },
    signalHealthError: null,
    orchestration: {
      verdict: 'healthy',
      job_name: 'research_trading_day',
      last_run_status: 'SUCCESS',
      last_run_ended_at: '2026-08-29T04:00:00Z',
      overdue: false,
      detail: 'last research_trading_day success within SLA',
      schedules_total: 26,
      schedules_running: 26,
      schedules_stopped: 0,
      schedules: [
        {
          name: 'research_trading_day_schedule',
          job_name: 'research_trading_day',
          status: 'RUNNING',
          last_run_status: 'SUCCESS',
          last_run_ended_at: '2026-08-29T04:00:00Z',
        },
        {
          name: 'research_canonical_pnl_schedule',
          job_name: 'research_canonical_pnl_job',
          status: 'RUNNING',
          last_run_status: 'SUCCESS',
          last_run_ended_at: '2026-08-30T00:10:00Z',
        },
      ],
      recent_failures: [],
    },
    orchestrationError: null,
    elementary: {
      ok: false,
      present: false,
      path: '/report/elementary_report.html',
      mtime: null,
    },
    elementaryError: null,
    ...over,
  }
}

describe('analyzeResearchEngine', () => {
  it('flags scan + canonical_pnl owners and weekend 36h SLA when batch is healthy', () => {
    const a = analyzeResearchEngine(baseSnap())
    expect(a.staleLabels).toEqual(['canonical_pnl', 'scan'])
    expect(a.primaryCause).toMatch(/scan \+ canonical_pnl/)
    expect(a.findings.some(f => f.id === 'weekend-36h-sla')).toBe(true)
    expect(a.findings.find(f => f.id === 'stale-canonical_pnl')?.detail).toContain(
      'research_canonical_pnl_schedule',
    )
    expect(a.findings.find(f => f.id === 'stale-canonical_pnl')?.detail).toContain(
      'EXCLUDED from research_trading_day',
    )
    expect(a.findings.find(f => f.id === 'stale-scan')?.detail).toContain(
      'research_trading_day_schedule',
    )
  })
})

describe('buildResearchEngineAgentPack', () => {
  it('includes Copy for Agent, D10, and forbids leftover analytics-docs', () => {
    const text = buildResearchEngineAgentPack(baseSnap())
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('research_olap: degraded')
    expect(text).toContain('Product DEGRADED')
    expect(text).toContain('analytics-docs')
    expect(text).not.toContain('Apply bifrost-analytics CronJob')
    expect(text).toContain('Do not re-apply bifrost-analytics CronJob')
    expect(text).toContain('research_canonical_pnl_schedule')
    expect(text).toContain('Suggested investigation order')
    expect(text).toContain('36h SLA vs Mon–Fri batch')
    expect(text).toContain('version: 0.50.2')
  })

  it('surfaces Elementary pending without treating it as the primary cause', () => {
    const a = analyzeResearchEngine(baseSnap())
    expect(a.findings.some(f => f.id === 'elementary-pending')).toBe(true)
    expect(a.primaryCause).not.toMatch(/Elementary/i)
  })
})

describe('buildResearchEngineDiagnosePrefill', () => {
  it('stays short and names the primary cause', () => {
    const text = buildResearchEngineDiagnosePrefill(baseSnap())
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('Primary cause:')
    expect(text).toContain('canonical_pnl')
    expect(text).toContain('analytics-docs')
  })
})
