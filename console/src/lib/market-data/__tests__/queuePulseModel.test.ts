import { describe, expect, it } from 'vitest'
import type { IngestQueueDashboardResponse } from '@/api/marketDataPlugin'
import {
  buildQueuePulseView,
  formatChipLine,
  formatCompactCount,
  formatEtaMinutes,
  formatRatePerMin,
  pendingDeltaView,
  queueVerdictTagVariant,
  queueVerdictToLamp,
} from '@/lib/market-data/queuePulseModel'
import { formatSignedDelta as fmtDelta } from '@/components/market-data/queueReadyCheck'

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
  })

  it('shows when pending > 0', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'healthy' },
        queue: { pending: 4000, running: 2, verdict: 'draining' },
        throughput: { jobs_per_min_15m: 16, eta_minutes_at_current_rate: 250 },
      }),
    )
    expect(view.active).toBe(true)
    expect(view.verdict).toBe('draining')
    expect(view.pending).toBe(4000)
    expect(view.ratePerMin).toBe(16)
    expect(view.etaMinutes).toBe(250)
    expect(view.lamp).toBe('degraded')
    expect(view.tagVariant).toBe('warning')
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

  it('builds chip line with Δ', () => {
    const view = buildQueuePulseView(
      dash({
        husbandry: { verdict: 'draining' },
        queue: { pending: 4000, running: 1 },
        throughput: { jobs_per_min_15m: 16, eta_minutes_at_current_rate: 240 },
      }),
    )
    expect(formatChipLine({ view, deltaLabel: 'Δ −3' })).toContain('DRAINING')
    expect(formatChipLine({ view, deltaLabel: 'Δ −3' })).toContain('Δ −3')
  })
})

describe('pending Δ', () => {
  it('labels signed delta like Ready Check', () => {
    expect(fmtDelta(-3)).toBe('Δ −3')
    expect(fmtDelta(5)).toBe('Δ +5')
    const d = pendingDeltaView({
      previous: { count: 100, atMs: 0 },
      current: { count: 97, atMs: 10_000 },
    }, 10_000)
    expect(d.delta).toBe(-3)
    expect(d.label).toBe('Δ −3')
    expect(d.caption).toContain('was 100')
  })
})
