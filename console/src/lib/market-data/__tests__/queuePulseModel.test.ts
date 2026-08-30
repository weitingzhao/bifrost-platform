import { describe, expect, it } from 'vitest'
import type { IngestQueueDashboardResponse } from '@/api/marketDataPlugin'
import {
  buildQueuePulseView,
  classifyDrainMode,
  formatChipLine,
  formatCompactCount,
  formatEtaMinutes,
  formatRatePerMin,
  ignitionScheduleForKind,
  pendingDeltaView,
  queueVerdictTagVariant,
  queueVerdictToLamp,
  shortKindLabel,
} from '@/lib/market-data/queuePulseModel'
import { formatSignedDelta as fmtDelta } from '@/components/market-data/queueReadyCheck'
import { dagsterRunsUrl } from '@/lib/architecture/opsToolRackCatalog'

function dash(
  partial: Partial<IngestQueueDashboardResponse> & {
    queue?: IngestQueueDashboardResponse['queue']
    husbandry?: IngestQueueDashboardResponse['husbandry']
    schedule?: IngestQueueDashboardResponse['schedule']
    throughput?: IngestQueueDashboardResponse['throughput']
  },
): IngestQueueDashboardResponse {
  return { ok: true, ...partial }
}

describe('buildQueuePulseView active / hide', () => {
  it('hides when idle + healthy + pending=0', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'healthy', detail: 'on plan' },
        schedule: { verdict: 'healthy' },
        queue: { pending: 0, running: 0, verdict: 'idle', kinds: [] },
      }),
    )
    expect(view.active).toBe(false)
    expect(view.verdict).toBe('idle')
    expect(view.lamp).toBe('ok')
    expect(view.ignitionHint).toBeNull()
  })

  it('shows when pending > 0 with top kind + ignition', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'healthy' },
        queue: {
          pending: 4000,
          running: 2,
          verdict: 'draining',
          kinds: [
            { kind: 'financials', pending: 3990, running: 2 },
            { kind: 'option_snapshot', pending: 10, running: 0 },
          ],
        },
        throughput: { jobs_per_min_15m: 16, eta_minutes_at_current_rate: 250 },
      }),
    )
    expect(view.active).toBe(true)
    expect(view.verdict).toBe('draining')
    expect(view.pending).toBe(4000)
    expect(view.topKind).toBe('financials')
    expect(view.topKindLabel).toBe('financials')
    expect(view.ignitionHint).toBe('market_fundamentals_rotate_schedule')
    expect(view.drainMode).toBe('expected')
    expect(view.detail).toMatch(/expected drain/)
  })

  it('marks stalled drain when no rate', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'draining' },
        queue: {
          pending: 100,
          running: 0,
          verdict: 'draining',
          kinds: [{ kind: 'financials', pending: 100, running: 0 }],
        },
      }),
    )
    expect(view.drainMode).toBe('stalled')
    expect(view.detail).toMatch(/stalled/)
  })

  it('shows on husbandry draining even if pending=0 briefly', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'draining', detail: 'drain window' },
        queue: { pending: 0, running: 1, verdict: 'idle' },
      }),
    )
    expect(view.active).toBe(true)
    expect(view.verdict).toBe('draining')
  })

  it('shows on missed / degraded', () => {
    expect(
      buildQueuePulseView(
        dash({
          husbandry: { verdict: 'missed' },
          queue: { pending: 0, running: 0 },
        }),
      ).active,
    ).toBe(true)
    expect(
      buildQueuePulseView(
        dash({
          husbandry: { verdict: 'degraded' },
          queue: { pending: 0, running: 0 },
        }),
      ).lamp,
    ).toBe('fail')
  })

  it('fail-soft when dash is null', () => {
    const view = buildQueuePulseView(null)
    expect(view.active).toBe(false)
    expect(view.verdict).toBe('unknown')
  })
})

describe('kind ignition map', () => {
  it('maps financials → fundamentals schedule', () => {
    expect(ignitionScheduleForKind('financials')).toBe(
      'market_fundamentals_rotate_schedule',
    )
    expect(shortKindLabel('option_open_interest')).toBe('opt-oi')
  })

  it('classifies drain modes', () => {
    expect(
      classifyDrainMode({
        verdict: 'draining',
        ratePerMin: 10,
        etaMinutes: 60,
        pending: 100,
      }),
    ).toBe('expected')
    expect(
      classifyDrainMode({
        verdict: 'draining',
        ratePerMin: null,
        etaMinutes: null,
        pending: 100,
      }),
    ).toBe('stalled')
  })
})

describe('queue pulse formatting', () => {
  it('formats compact counts / rate / ETA', () => {
    expect(formatCompactCount(4000)).toBe('4.0k')
    expect(formatCompactCount(12000)).toBe('12k')
    expect(formatRatePerMin(16)).toBe('16.0/min')
    expect(formatRatePerMin(null)).toBe('—')
    expect(formatEtaMinutes(250)).toBe('4h 10m')
    expect(formatEtaMinutes(0.5)).toBe('<1m')
    expect(formatEtaMinutes(null)).toBe('—')
  })

  it('maps verdict → lamp / tag', () => {
    expect(queueVerdictToLamp('missed')).toBe('fail')
    expect(queueVerdictToLamp('draining')).toBe('degraded')
    expect(queueVerdictTagVariant('healthy')).toBe('success')
    expect(queueVerdictTagVariant('due')).toBe('warning')
  })

  it('builds chip line with kind on face', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'draining' },
        queue: {
          pending: 4000,
          running: 1,
          kinds: [{ kind: 'financials', pending: 4000, running: 1 }],
        },
        throughput: { jobs_per_min_15m: 16, eta_minutes_at_current_rate: 240 },
      }),
    )
    const line = formatChipLine({ view, deltaLabel: 'Δ −3' })
    expect(line).toContain('DRAINING')
    expect(line).toContain('financials')
    expect(line).toContain('Δ −3')
  })

  it('builds Dagster runs deep link', () => {
    expect(dagsterRunsUrl()).toMatch(/\/runs$/)
    expect(dagsterRunsUrl({ runId: 'abc' })).toMatch(/\/runs\/abc$/)
  })
})

describe('pending Δ', () => {
  it('labels signed delta like Ready Check', () => {
    expect(fmtDelta(-3)).toBe('Δ −3')
    expect(fmtDelta(5)).toBe('Δ +5')
    const d = pendingDeltaView(
      {
        previous: { count: 100, atMs: 0 },
        current: { count: 97, atMs: 10_000 },
      },
      10_000,
    )
    expect(d.delta).toBe(-3)
    expect(d.label).toBe('Δ −3')
    expect(d.caption).toContain('was 100')
  })
})
