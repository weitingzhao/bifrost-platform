import { describe, expect, it } from 'vitest'
import { isDoctorComputing, type DoctorReport } from '@/api/marketDataDoctor'
import { buildDoctorAgentReport, doctorPanelState } from '@/components/market-data/doctorModel'

// What GET /market/doctor answers while a recompute runs on a cold cache (seen on PROD
// 2026-10-06 18:15 UTC right after market-data 0.79.0 restarted). TD-165: rendering it as a
// report threw on `universe.optionable` and blanked the whole Ingest tab.
const COMPUTING_STUB = { ok: true, computing: true as const, generated_at: null, age_sec: null, findings: [] }

const REPORT: DoctorReport = {
  ok: true,
  generated_at: '2026-10-06T18:16:31Z',
  session: '2026-10-05',
  session_is_today: false,
  universe: { watchlist: 18, underlyings: 713, optionable: 693 },
  verdict: 'degraded',
  summary: '0 critical · 2 warning',
  findings: [],
  prescriptions: [],
  retired_slots: [],
}

describe('doctor computing stub (TD-165)', () => {
  it('is recognised as computing; a real report is not', () => {
    expect(isDoctorComputing(COMPUTING_STUB)).toBe(true)
    expect(isDoctorComputing(REPORT)).toBe(false)
    expect(isDoctorComputing(null)).toBe(false)
  })

  it('the panel state never hands the stub over as a report', () => {
    expect(doctorPanelState(COMPUTING_STUB)).toEqual({ report: null, computing: true })
    expect(doctorPanelState(REPORT)).toEqual({ report: REPORT, computing: false })
    expect(doctorPanelState(undefined)).toEqual({ report: null, computing: false })
  })

  it('the agent report survives a report without a universe', () => {
    const { universe: _u, ...noUniverse } = REPORT
    const text = buildDoctorAgentReport(noUniverse as unknown as DoctorReport)
    expect(text).toContain('Universe: watchlist —')
  })
})
