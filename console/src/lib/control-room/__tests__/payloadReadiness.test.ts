import { describe, expect, it } from 'vitest'
import {
  projectPayloadReadinessRows,
  type PayloadReadinessRow,
} from '@/lib/control-room/payloadReadiness'
import type { FleetCell, FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import { cellKey } from '@/lib/control-room/fleetSnapshot'

function emptyFleet(cells: FleetCell[]): FleetSnapshot {
  return {
    viewerEnv: 'dev',
    columns: ['dev', 'stg', 'prod'],
    cells,
    generatedAt: new Date().toISOString(),
  } as FleetSnapshot
}

function vendorSpanOk(): FleetCell {
  return {
    key: cellKey('vendor', 'span'),
    role: 'vendor',
    env: null,
    span: true,
    signal: 'ok',
    value: '1/1',
    detail: 'IB Gateway ok',
    probePath: '',
    standards: [
      {
        id: 'ib-feed',
        label: 'IB Client / Gateway',
        signal: 'ok',
        reason: 'live · ib-gateway 1/1 · redis-ib ok',
        group: 'feed',
        required: true,
      },
    ],
    fixScope: null,
    agentFixEnabled: false,
    countsTowardVerdict: true,
  }
}

function ibRow(rows: PayloadReadinessRow[]): PayloadReadinessRow {
  const row = rows.find(r => r.id === 'ib')
  if (row == null) throw new Error('missing IB edge row')
  return row
}

describe('projectPayloadReadinessRows', () => {
  it('projects shared vendor:span IB Gateway onto IB edge for every Trade NS', () => {
    const rows = projectPayloadReadinessRows(emptyFleet([vendorSpanOk()]))
    const ib = ibRow(rows)
    expect(ib.dev.signal).toBe('ok')
    expect(ib.stg.signal).toBe('ok')
    expect(ib.prod.signal).toBe('ok')
    expect(ib.envDiverges).toBe(false)
    expect(ib.dev.detail).toMatch(/ib-gateway|IB/i)
  })

  it('stays PROBING when vendor span is absent', () => {
    const rows = projectPayloadReadinessRows(emptyFleet([]))
    const ib = ibRow(rows)
    expect(ib.dev.signal).toBe('unknown')
    expect(ib.dev.detail).toMatch(/No Fleet cell/)
  })
})
