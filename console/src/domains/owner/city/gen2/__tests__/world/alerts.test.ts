import { describe, expect, it } from 'vitest'
import { alertsUnder, placedIn, worst, type CityAlert } from '@bifrost/ui/city/alerts'
import type { Place } from '@bifrost/ui/city/places'

// 2026-11-21, the Owner: the picture never judges; the line that measures does. The engine lights each place
// with the alerts under it (how many, the worst) and lists the ones it cannot place: these pin that counting.
const a = (id: string, severity: CityAlert['severity'], at: string[] | null): CityAlert => ({ id, name: id, severity, summary: '', at })
const alerts = [a('down', 'critical', ['compute', 'ubt-k3s-05']), a('disk', 'warning', ['compute', 'nas']), a('pod', 'warning', null), a('stale', 'info', ['compute', 'gone'])]
const tree: Place = { id: 'city', kind: 'town', shape: '16x16', label: 'City', question: '', dashboard: null, children: [
  { id: 'compute', kind: 'district', shape: '4x4', label: 'Compute', question: '', dashboard: null, children: ['ubt-k3s-05', 'nas'].map(id => ({ id, kind: 'building' as const, shape: '4x4' as const, label: id, question: '', dashboard: null, children: [] })) },
] }

describe('alerts under the places on screen', () => {
  it('on the town, the district carries all under it, with the worst', () => {
    expect(alertsUnder(alerts, []).get('compute')).toEqual({ n: 3, worst: 'critical' })
  })
  it('inside the district, each building its own', () => {
    const m = alertsUnder(alerts, ['compute'])
    expect(m.get('ubt-k3s-05')).toEqual({ n: 1, worst: 'critical' })
    expect(m.get('nas')).toEqual({ n: 1, worst: 'warning' })
  })
  it('a path that is not a place, or none, is not placed', () => {
    expect(alerts.filter(x => placedIn(tree, x)).map(x => x.id)).toEqual(['down', 'disk'])
  })
  it('the worst of two', () => {
    expect(worst('warning', 'critical')).toBe('critical')
    expect(worst(null, 'info')).toBe('info')
  })
})
