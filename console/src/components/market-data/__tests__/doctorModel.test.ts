import { describe, expect, it } from 'vitest'
import type { DoctorReport } from '@/api/marketDataDoctor'
import {
  autoFixableIds,
  buildDoctorAgentReport,
  describeFix,
  sortFindings,
  verdictVariant,
} from '@/components/market-data/doctorModel'

const report: DoctorReport = {
  ok: true,
  generated_at: '2026-09-06T23:50:00Z',
  session: '2026-09-04',
  session_is_today: false,
  universe: { watchlist: 18, underlyings: 27, optionable: 25 },
  verdict: 'critical',
  summary: '1 critical · 1 warning · 3 ok',
  findings: [
    { id: 'stale:calendar', slot: 'calendar', severity: 'warn', title: 'calendar freshness', expected: '< 48h', actual: 80, detail: 'freshness.calendar is 80.0h old (limit 48h).', fix: { action: 'enqueue-slot', slot: 'calendar', force: true }, auto_fixable: true },
    { id: 'stock_daily:2026-09-04', slot: 'universe-daily', severity: 'ok', title: 'Stock daily bars', expected: '>= 4000', actual: 9000, detail: '9000 rows.', fix: null, auto_fixable: false },
    { id: 'option_snapshot:2026-09-04', slot: 'eod-pipeline', severity: 'crit', title: 'Option chain snapshot', expected: 25, actual: 3, detail: '3/25 underlyings have rows.', session: '2026-09-04', fix: { action: 'enqueue-slot', slot: 'eod-pipeline', force: true, date: '2026-09-04' }, auto_fixable: true, missing_sample: ['AAPL', 'MSFT'] },
    { id: 'worker:options', slot: 'workers', severity: 'crit', title: 'options workers', expected: 'reachable', actual: 'unreachable', detail: '/health did not answer.', fix: { action: 'rollout-restart', deployment: 'polygon-worker-options' }, auto_fixable: false },
  ],
  prescriptions: [
    { finding_ids: ['option_snapshot:2026-09-04'], action: 'enqueue-slot', slot: 'eod-pipeline', force: true, date: '2026-09-04' },
    { finding_ids: ['stale:calendar'], action: 'enqueue-slot', slot: 'calendar', force: true },
  ],
  retired_slots: ['option-trades'],
}

describe('doctorModel', () => {
  it('sorts critical first and keeps ok last', () => {
    expect(sortFindings(report.findings).map(f => f.severity)).toEqual(['crit', 'crit', 'warn', 'ok'])
  })

  it('describes a prescription as the action the button will take', () => {
    expect(describeFix({ action: 'enqueue-slot', slot: 'eod-pipeline', date: '2026-09-04', force: true })).toBe(
      'Enqueue eod-pipeline for 2026-09-04 (force)',
    )
    expect(describeFix({ action: 'retry-jobs', kind: 'option_bars', job_ids: [1, 2] })).toBe('Retry 2 option_bars job(s)')
    expect(describeFix({ action: 'rollout-restart', deployment: 'polygon-worker-options' })).toContain('Restart polygon-worker-options')
    expect(describeFix(null)).toBe('—')
  })

  it('lists only auto-fixable findings for Fix all', () => {
    expect(autoFixableIds(report)).toEqual(['stale:calendar', 'option_snapshot:2026-09-04'])
    expect(autoFixableIds(null)).toEqual([])
  })

  it('maps verdicts to tag tones', () => {
    expect(verdictVariant('critical')).toBe('danger')
    expect(verdictVariant('degraded')).toBe('warning')
    expect(verdictVariant('healthy')).toBe('success')
    expect(verdictVariant(undefined)).toBe('neutral')
  })

  it('builds an agent report with findings, prescriptions and the MCP calls', () => {
    const text = buildDoctorAgentReport(report)
    expect(text).toContain('Session: 2026-09-04 (last completed)')
    expect(text).toContain('[crit] Option chain snapshot (option_snapshot:2026-09-04)')
    expect(text).toContain('missing: AAPL, MSFT')
    expect(text).toContain('fix: Enqueue eod-pipeline for 2026-09-04 (force) [auto]')
    expect(text).toContain('fix: Restart polygon-worker-options (kubectl / agent) [manual]')
    expect(text).not.toContain('Stock daily bars')
    expect(text).toContain('- Enqueue calendar (force) ← stale:calendar')
    expect(text).toContain('`market_data_heal` with `{"dry_run": true}`')
    expect(text).toContain('D10 BLOCKED')
  })
})
