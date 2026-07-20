/**
 * Trade readiness Fleet projection — unit checks (no network).
 * Run: npx tsx scripts/payload-readiness-fleet-test.ts
 */
import assert from 'node:assert/strict'
import {
  projectPayloadReadinessRows,
  type PayloadReadinessRow,
} from '../src/lib/control-room/payloadReadiness.ts'
import type {
  FleetCell,
  FleetSnapshot,
  FleetStandard,
} from '../src/lib/control-room/fleetSnapshot.ts'
import { cellKey, std } from '../src/lib/control-room/fleetSnapshot.ts'

function cell(
  role: 'satellite' | 'vendor',
  env: 'dev' | 'stg' | 'prod',
  standards: FleetStandard[],
): FleetCell {
  return {
    key: cellKey(role, env),
    role,
    env,
    span: false,
    signal: 'ok',
    value: 'ok',
    detail: '',
    probePath: '',
    standards,
    fixScope: null,
    agentFixEnabled: false,
    countsTowardVerdict: true,
  }
}

function mockFleet(cells: FleetCell[]): FleetSnapshot {
  return {
    viewerEnv: 'dev',
    columns: ['dev', 'stg', 'prod'],
    roles: ['rocket', 'satellite', 'engineer', 'ground', 'vendor'],
    cells,
    verdict: {
      kind: 'GO',
      topReason: 'ok',
      primaryCta: { label: 'Fleet clear', kind: 'none' },
      worstCell: null,
    },
    fleetNominal: true,
    fleetClear: true,
  }
}

function rowById(rows: PayloadReadinessRow[], id: string) {
  const r = rows.find(x => x.id === id)
  assert.ok(r, `missing row ${id}`)
  return r
}

{
  const fleet = mockFleet([
    cell('satellite', 'dev', [
      std('api-monitor', 'api-monitor', 'ok', 'monitor healthy', 'api'),
      std('api-ops', 'api-ops', 'degraded', 'ops slow', 'api'),
      std('postgres', 'postgres', 'ok', 'pg ok', 'datastore'),
      std('redis', 'redis', 'fail', 'redis down', 'datastore'),
      std('nginx-spa', 'nginx-spa', 'ok', 'spa ok', 'edge'),
    ]),
    cell('satellite', 'stg', [
      std('stg-smoke', 'STG smoke 8/10', 'degraded', '2 targets fail', 'release'),
    ]),
    cell('satellite', 'prod', [
      std('api-monitor', 'api-monitor', 'ok', 'monitor healthy', 'api'),
      std('api-ops', 'api-ops', 'ok', 'ops ok', 'api'),
      std('postgres', 'postgres', 'ok', 'pg ok', 'datastore'),
      std('redis', 'redis', 'ok', 'redis ok', 'datastore'),
      std('nginx-spa', 'nginx-spa', 'ok', 'spa ok', 'edge'),
    ]),
    cell('vendor', 'dev', [
      std('ib-feed', 'IB Client / Gateway', 'fail', 'ghost TWS API client', 'feed'),
    ]),
    cell('vendor', 'stg', [
      std('ib-feed', 'IB Client / Gateway', 'ok', 'ib ok', 'feed'),
    ]),
    cell('vendor', 'prod', [
      std('ib-feed', 'IB Client / Gateway', 'ok', 'ib ok', 'feed'),
    ]),
  ])

  const rows = projectPayloadReadinessRows(fleet)
  assert.equal(rows.length, 5)

  const daemon = rowById(rows, 'daemon')
  assert.equal(daemon.dev.signal, 'ok')
  assert.equal(daemon.dev.mapTargetId, 'api-monitor')
  assert.equal(daemon.stg.signal, 'degraded')
  assert.match(daemon.stg.detail, /STG smoke rollup/)
  assert.equal(daemon.prod.signal, 'ok')

  const celery = rowById(rows, 'celery')
  assert.equal(celery.dev.signal, 'degraded')
  assert.equal(celery.dev.mapTargetId, 'api-ops')

  const ib = rowById(rows, 'ib')
  assert.equal(ib.mapMode, 'fleet-vendor')
  assert.equal(ib.dev.signal, 'fail')
  assert.match(ib.dev.detail, /ghost/)
  assert.equal(ib.dev.mapTargetId, null)
  // Must not be L0 / policyBlocked fiction
  assert.ok(!ib.dev.detail.toLowerCase().includes('l0 blocked'))

  const ds = rowById(rows, 'datastore')
  assert.equal(ds.dev.signal, 'fail')
  assert.equal(ds.dev.mapTargetId, 'postgres')

  const ui = rowById(rows, 'frontend')
  assert.equal(ui.dev.signal, 'ok')
  assert.equal(ui.dev.mapTargetId, 'nginx-spa')

  assert.equal(daemon.envDiverges || celery.envDiverges || ib.envDiverges, true)
  console.log('ok  projectPayloadReadinessRows: daemon/celery/ib/datastore/frontend')
}

{
  const fleet = mockFleet([])
  const rows = projectPayloadReadinessRows(fleet)
  const ib = rowById(rows, 'ib')
  assert.equal(ib.dev.signal, 'unknown')
  assert.match(ib.dev.detail, /No Fleet cell/)
  console.log('ok  empty fleet → unknown cells')
}

console.log('\nAll payload-readiness-fleet checks passed')
