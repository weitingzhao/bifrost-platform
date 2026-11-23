import { describe, expect, it } from 'vitest'
import type { TelemetryAlertsResponse } from '@/api/clusterTypes'
import { consoleCrumbs, consoleFramePlaces, frameAlertsOf } from '@/lib/shell/consoleFrame'

// Origin (2026-11-23, UI lead): every view wears one world frame; the Console's own header was
// another website's bar (World baseline: places 1, alerts 5 warning while the City said 6 firing).

function alerts(rows: Array<[string, string]>): TelemetryAlertsResponse {
  return {
    prometheus_url: '', generated_at: '2026-11-23T16:10:00Z',
    alerts: rows.map(([severity, state]) => ({ labels: { alertname: 'A', severity }, annotations: {}, state })),
  }
}

describe('console frame data', () => {
  it('counts the alerts corner with the City rule, worst in the Ops words; not read is not 0', () => {
    expect(frameAlertsOf(alerts([['warning', 'firing'], ['info', 'firing'], ['critical', 'pending']]), null)).toMatchObject({ count: 2, worst: 'warn' })
    expect(frameAlertsOf(alerts([['critical', 'firing']]), null)).toMatchObject({ count: 1, worst: 'fail' })
    expect(frameAlertsOf(undefined, 'HTTP 502')).toEqual({ count: null, why: 'HTTP 502' })
  })

  it('crumbs are Console › entry › page, the entry a link, no repeat on an entry page', () => {
    expect(consoleCrumbs({ label: 'System', href: '#system' }, 'Status')).toEqual([{ label: 'Console', href: '#command' }, { label: 'System', href: '#system' }, { label: 'Status' }])
    expect(consoleCrumbs(null, 'Work')).toEqual([{ label: 'Console', href: '#command' }, { label: 'Work' }])
  })
})

describe('the places and looks the Console hands the frame', () => {
  it('Console is here; City keeps the place; Table is the ledger look', () => {
    const p = consoleFramePlaces({ on: 'page', city: '#city?at=system%2Fstatus', ledger: '#city?look=ledger&at=system', page: '#status' }, '#collaboration?look=agents', '#status')
    expect(p.hrefs).toEqual({ city: '#city?at=system%2Fstatus', signals: '#observation', agents: '#collaboration?look=agents', console: '#status' })
    expect(p.looks?.map(l => [l.label, l.href, l.on])).toEqual([['Page', '#status', true], ['Table', '#city?look=ledger&at=system', false]])
  })

  it('a page with no looks (More) has no Page | Table and stays where it is', () => {
    const p = consoleFramePlaces(undefined, '#collaboration?look=agents', '#more')
    expect(p.looks).toBeUndefined()
    expect(p.hrefs.console).toBe('#more')
    expect(p.hrefs.city).toBeUndefined()
  })
})
