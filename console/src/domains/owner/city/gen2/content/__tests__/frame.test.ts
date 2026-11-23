// 2026-11-23, 箭10 «one world» (cockpit/ui-world/frame): the City wears the world frame. These cases pin what the City
// hands it: the breadcrumb is the places' own names as far as the address resolves, and the alerts corner is the
// count the City shows, its worst in the Ops vocabulary, and "not read" when it was not (never 0).
import { describe, expect, it } from 'vitest'
import type { Place } from '@bifrost/ui/city/places'
import { crumbsOf, frameAlerts } from '../frame'

const place = (id: string, label: string, children: Place[] = []): Place => ({ id, label, kind: 'district', shape: '4x4', question: '', dashboard: null, children } as Place)
const tree = place('town', 'City', [place('delivery', 'Delivery', [place('releases', 'releases')])])

describe('the City in the world frame', () => {
  it('names each step by the place, as far as the address resolves', () => {
    expect(crumbsOf(tree, ['delivery', 'releases']).map(c => c.label)).toEqual(['City', 'Delivery', 'releases'])
    expect(crumbsOf(tree, ['delivery', 'gone']).map(c => c.label)).toEqual(['City', 'Delivery'])
    expect(crumbsOf(tree, ['delivery'])[1].href).toBe('#city?at=delivery')
  })

  it('counts the alerts the City shows, worst first in the Ops words; not read is not 0', () => {
    const a = (severity: 'critical' | 'warning' | 'info') => ({ id: severity, name: 'X', severity, summary: '', at: null })
    expect(frameAlerts({ alerts: [a('info'), a('warning')], source: '/api/v1/telemetry/alerts' })).toMatchObject({ count: 2, worst: 'warn' })
    expect(frameAlerts({ alerts: [a('critical')], source: 's' }).worst).toBe('fail')
    expect(frameAlerts({ alerts: [], source: 's' })).toMatchObject({ count: 0 })
    expect(frameAlerts({ source: 's', why: 'HTTP 502' })).toEqual({ count: null, why: 'HTTP 502' })
    expect(frameAlerts(undefined).count).toBeNull()
  })
})
