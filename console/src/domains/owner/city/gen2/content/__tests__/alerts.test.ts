import { describe, expect, it } from 'vitest'
import type { Place } from '@bifrost/ui/city/places'
import { alertsRead, cityAlerts, deviceOf, type DeviceKey, type PromAlert } from '../alerts'

// 2026-11-21 (ISSUE-9): the utilities read the metrics line's judgment, not the picture's own. The alerts below
// are PROD's firing alerts of that evening (GET /api/v1/telemetry/alerts, read through the bench proxy) and its
// devices (topology): ubt-k3s-05 down, gpu-server in standby, CPUThrottlingHigh on cicd pods scraped from a
// kubelet on 192.168.10.80. Placement is exact: node label, host label, or a host's own address; a workload's
// scrape target never places it on the machine.
const devices: DeviceKey[] = [
  { id: 'ubt-k3s-05', ip: '192.168.10.77', node: 'ubt-k3s-05' },
  { id: 'gpu-server', ip: '192.168.10.60', node: 'gpu-server' },
  { id: 'ubt-k3s-07', ip: '192.168.10.80', node: 'ubt-k3s-07' },
  { id: 'UGREEN-NAS', ip: '192.168.10.20', node: null },
]
const tree: Place = {
  id: 'city', kind: 'town', shape: '16x16', label: 'City', question: '', dashboard: null,
  children: [{ id: 'compute', kind: 'district', shape: '4x4', label: 'Compute', question: '', dashboard: null,
    children: devices.map(d => ({ id: d.id, kind: 'building' as const, shape: '4x4' as const, label: d.id, question: '', dashboard: null, children: [] })) }],
}
const alert = (labels: Record<string, string>, state = 'firing'): PromAlert => ({ labels, state, annotations: { summary: 's' }, active_at: '2026-11-21T20:00:00Z' })

describe('alerts placed on the machines, exactly', () => {
  it('by its node label', () => {
    expect(deviceOf({ alertname: 'BifrostClusterNodeDown', node: 'ubt-k3s-05' }, devices)).toBe('ubt-k3s-05')
  })
  it('by its host label, the device’s id', () => {
    expect(deviceOf({ alertname: 'BifrostHostDiskSpaceLow', host: 'ugreen-nas', instance: '192.168.10.20:9100' }, devices)).toBe('UGREEN-NAS')
  })
  it('a host’s own alert by its address; a workload’s scrape target never places it', () => {
    expect(deviceOf({ alertname: 'BifrostHostDown', instance: '192.168.10.20:9100' }, devices)).toBe('UGREEN-NAS')
    expect(deviceOf({ alertname: 'CPUThrottlingHigh', instance: '192.168.10.80:10250', namespace: 'cicd' }, devices)).toBeNull()
  })
})

describe('the alerts the City shows', () => {
  const raw = [
    alert({ alertname: 'BifrostClusterNodeDown', severity: 'critical', node: 'ubt-k3s-05' }),
    alert({ alertname: 'KubePodNotReady', severity: 'warning', namespace: 'monitoring' }),
    alert({ alertname: 'Watchdog', severity: 'none' }),
    alert({ alertname: 'KubePodNotReady', severity: 'warning', namespace: 'cicd' }, 'pending'),
  ]
  it('firing only, the deadman left out, each at its device’s path or not placed', () => {
    const out = cityAlerts(raw, devices, tree)
    expect(out.map(a => [a.name, a.severity, a.at])).toEqual([
      ['BifrostClusterNodeDown', 'critical', ['compute', 'ubt-k3s-05']],
      ['KubePodNotReady', 'warning', null],
    ])
  })
  it('a failed read says why, never "no alerts"', () => {
    const r = alertsRead(undefined, new Error('telemetry alerts: HTTP 401'), devices, tree)
    expect(r.alerts).toBeUndefined()
    expect('why' in r && r.why).toContain('401')
  })
})
