// 2026-11-23: in one minute the City's alerts corner said "6 firing" and Console Status "5 warning alerts" — two
// ways of counting. The metrics line fixed the rule (m_01M84K76…); these cases pin it, each line of it once.
import { describe, expect, it } from 'vitest'
import { alertCount } from '../alertCount'

const a = (severity: string, state = 'firing', extra: Record<string, string> = {}) => ({ state, labels: { alertname: 'X', severity, ...extra } })

describe('alertCount', () => {
  it('counts firing, info as its own grade; the 16:10Z reading was 6 (info 1, warning 5), worst warning', () => {
    const c = alertCount([...Array(5)].map(() => a('warning')).concat([a('info')]))
    expect(c).toEqual({ count: 6, worst: 'warning', by_severity: { critical: 0, warning: 5, info: 1 } })
  })

  it('leaves out pending, severity none (Watchdog) and drills; keeps silenced ones', () => {
    const c = alertCount([a('critical', 'pending'), a('none'), a('critical', 'firing', { drill: 'true' }), a('warning', 'firing', { silenced: 'true' })])
    expect(c).toEqual({ count: 1, worst: 'warning', by_severity: { critical: 0, warning: 1, info: 0 } })
  })

  it('reads an unnamed severity as info, and has no worst when nothing fires', () => {
    expect(alertCount([a('page')]).by_severity.info).toBe(1)
    expect(alertCount([])).toEqual({ count: 0, worst: null, by_severity: { critical: 0, warning: 0, info: 0 } })
  })
})
