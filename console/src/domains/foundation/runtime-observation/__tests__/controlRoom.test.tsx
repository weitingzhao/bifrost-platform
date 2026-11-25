import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { RuntimeWorld, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { controlRoomOf, type ControlRoomInput } from '../controlRoom'
import { NO_ADDRESS } from '../read'

// 2026-11-24: the Owner saw PROD Signals on a phone ("a wall of cards; no signal shows; I want a power plant's big
// screen"). 箭10 approved the control room prototype with corrections: count only what needs attention (fail, warn),
// "can't be read" apart, devices with no address and lights not measured by plan are boundary and quiet, and nothing
// read is dropped (the card wall stays behind "All signals").
const at = '2026-11-24T06:30:00.000Z'
const ev = (source: string) => [{ source, receivedAt: at, outcome: 'read' as const }]
const building = (id: string, state: string, cpu = 0.5) => ({ id, kind: 'building', label: id, question: '', dashboard: null, state,
  children: [{ id: 'cpu', kind: 'room', label: 'CPU', question: '', dashboard: null, state, meter: { unit: 'cores', used: 8 * cpu, of: 8, source: 'q' }, children: [] }] })
const hardware = {
  version: 1, id: 'h', capturedAt: at, provenance: 'live',
  evidence: { '@alerts': ev('/api/v1/telemetry/alerts'), 'utilities/win11': [...ev('/api/v1/topology?env=prod'), { source: '/api/v1/topology?env=prod', receivedAt: at, outcome: 'missing', detail: NO_ADDRESS }], 'utilities/node-a': ev('q'), 'utilities/node-b': ev('q') },
  content: { version: 2, tree: { id: 'town', kind: 'town', shape: '8x8', label: 'Runtime', question: '', dashboard: null, children: [{ id: 'utilities', kind: 'district', label: 'Utilities', question: '', dashboard: null,
    children: [building('win11', 'unread'), building('node-a', 'online'), building('node-b', 'down')] }] },
    alerts: { source: '/api/v1/telemetry/alerts', alerts: [
      { id: 'SmartDown|instance=10.0.0.1:9633', name: 'SmartDown', severity: 'warning', summary: 'No SMART data', at: ['utilities', 'node-a'] },
      { id: 'TargetDown|job=smartctl', name: 'TargetDown', severity: 'warning', summary: 'Some monitored targets are unreachable', at: null },
      { id: 'Watchdog|', name: 'Watchdog', severity: 'info', summary: '', at: null }] } },
} as unknown as ObservationSnapshot
const light = (value: string, why: string) => ({ value, why })
const good = light('good', 'fine')
const input: ControlRoomInput = {
  hardware,
  operations: { evidence: { '@domains': ev('/api/v1/catalog/status'), 'domain:plan': ev('/api/v1/catalog/status') }, raw: {
    domains: { domains: [
      { id: 'plan', alive: good, interfaces: good, boundary: good, fresh: good },
      { id: 'assembly', alive: good, interfaces: light('unknown', 'its contract names no interface'), boundary: good, fresh: good },
      { id: 'station', alive: light('unknown', 'no running workload of it on the map'), interfaces: light('unknown', 'its contract names no interface'), boundary: good, fresh: good },
    ] } as never,
    ciCatalog: true,
    ci: [
      { name: 'bifrost-ci-infra', data: { reachability: 'ok', generated_at: at, runs: [{ name: 'r2', status: 'False', reason: 'Failed', start_time: '2026-11-24T06:00:00Z' }, { name: 'r1', status: 'True', start_time: '2026-11-24T05:00:00Z' }] } as never },
      { name: 'bifrost-ci-platform', data: { reachability: 'ok', generated_at: at, runs: [{ name: 'p1', status: 'Unknown', reason: 'Running', start_time: '2026-11-24T06:20:00Z' }] } as never },
      { name: 'bifrost-ci-python', data: undefined },
    ],
    husbandry: { lanes: [{ id: 'market', label: 'Market batch', verdict: 'healthy', detail: 'queue idle' }, { id: 'olap', label: 'Research OLAP', verdict: 'degraded', detail: 'stale tables' }] } as never,
  } },
  databases: { states: [{ subject: 'data/bifrost-postgres', state: { ready: 'ready', backup: 'failed', archiving: 'ok', primary: 'pg-1', last_backup: at } }] },
  workloads: { states: [{ subject: 'ns/a', state: { ready: 'ready' } }, { subject: 'ns/b', state: { ready: 'not ready' } }] },
  edges: [{ from: 'assembly', to: 'plan', imports: 6 }],
  trends: [{ id: 'cpu', label: 'CPU busy cores', values: [1, null, 3], stepMinutes: 30, source: 'q' }, { id: 'none', label: 'Nothing', values: [null, null], stepMinutes: 30, source: 'q' }],
}
afterEach(() => cleanup())

it('words every signal it read: boundary by design is quiet, a failed read is unknown, nothing is dropped', () => {
  const m = controlRoomOf(input)
  const word = (id: string) => m.items.find(i => i.id === id)?.word
  expect(word('device:win11')).toBe('boundary')            // no address in the topology
  expect(word('domain:assembly')).toBe('boundary')         // its contract names no interface
  expect(word('domain:station')).toBe('unknown')           // no running workload seen: can't be read
  expect(word('domain:plan')).toBe('ok')
  expect(word('device:node-a')).toBe('warn')               // its placed alert
  expect(word('device:node-b')).toBe('fail')               // down
  expect(word('ci:bifrost-ci-infra')).toBe('fail')         // latest run failed
  expect(word('ci:bifrost-ci-platform')).toBe('pending')   // running is never a finding
  expect(word('ci:bifrost-ci-python')).toBe('unknown')     // its runs not read
  expect(word('data:olap')).toBe('warn')
  expect(word('db:data/bifrost-postgres')).toBe('fail')    // backup failed
  expect(word('workload:ns/b')).toBe('unknown')
  expect(m.items.find(i => i.id.startsWith('alert:TargetDown'))?.plain).toBe('Some monitored targets are unreachable')
  expect(m.items.some(i => i.name.startsWith('Watchdog'))).toBe(false) // info alerts are not findings
  expect(m.items.find(i => i.id === 'domain:plan')?.usedBy).toEqual(['domain:assembly'])
  expect(m.stages.map(s => s.id)).toEqual(['domains', 'ci', 'data', 'devices', 'alerts'])
})

it('reads as pending, not unknown, before the operations answer', () => {
  const m = controlRoomOf({ hardware })
  expect(m.items.find(i => i.id === '@domains')?.word).toBe('pending')
})

it('shows the control room first: what needs attention counted apart from what cannot be read, the card wall one tap away', () => {
  const snapshot = { ...hardware, layerPanels: { domains: [], ci: [], data: [] }, controlRoom: controlRoomOf(input) }
  render(<RuntimeWorld snapshot={snapshot} frame={{ hrefs: { city: '#city', agents: '#collaboration', console: '#system' }, env: 'PROD' }}/>)
  // fail: node-b, infra CI, postgres; warn: node-a, olap, TargetDown → 6; unknown: station, python CI, ns/b → 3
  expect(screen.getByRole('heading', { name: /need attention/ }).textContent).toBe('6 need attention')
  expect(screen.getByText("3 can't be read")).toBeTruthy()
  const lamps = screen.getAllByRole('listitem').map(l => l.textContent ?? '')
  expect(lamps[0]).toMatch(/^fail/)
  expect(lamps.some(l => /win11|assembly/.test(l))).toBe(false) // boundary stays off the annunciator
  expect(screen.getByText('no history read')).toBeTruthy() // the all-unread trend says unknown, draws no line
  // 2026-11-24 Owner: one lamp group per module, one lamp per state; the red one is seen at once.
  const node = screen.getByRole('button', { name: 'node-a: warn' })
  expect([...node.querySelectorAll('.cr-l')].map(l => l.className)).toEqual(['cr-l cr-ok', 'cr-l cr-warn'])
  expect(screen.getByRole('button', { name: 'win11: boundary' }).querySelector('.cr-l.cr-boundary')).toBeTruthy()
  fireEvent.click(screen.getAllByRole('listitem')[0])
  expect(screen.getByLabelText('Signal evidence').textContent).toContain('SOURCE SAYS')
  fireEvent.click(screen.getByRole('button', { name: 'All signals' }))
  expect(screen.getByRole('button', { name: 'Overview' })).toBeTruthy()
})
