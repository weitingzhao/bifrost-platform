/**
 * Checklist ↔ Fleet Board union (virtual inject + bidirectional empty gap).
 * Run: npx tsx scripts/daily-ops-checklist-union-test.ts
 */
import * as assert from 'node:assert/strict'
import {
  applyChecklistFleetUnion,
  auditChecklistFleetUnion,
  injectChecklistVirtualStandards,
} from '../src/lib/control-room/dailyOpsChecklistInject'
import { buildFleetSnapshot } from '../src/lib/control-room/buildFleetSnapshot'
import { buildChecklistCoverageIndex } from '../src/lib/control-room/dailyOpsChecklistCoverage'
import {
  assertChecklistBoardProjectionContract,
  matchStandardToChecklistItem,
} from '../src/lib/control-room/dailyOpsChecklistCatalog'
import type { FleetCell, FleetSnapshot } from '../src/lib/control-room/fleetSnapshot'
import { resolveCellGate } from '../src/lib/control-room/fleetSnapshot'

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok  ${name}`)
  } catch (e) {
    console.error(`FAIL  ${name}`)
    throw e
  }
}

function std(
  id: string,
  group: FleetCell['standards'][0]['group'],
  signal: FleetCell['standards'][0]['signal'] = 'ok',
  required = true,
): FleetCell['standards'][0] {
  return {
    id,
    label: id,
    signal,
    group,
    reason: 'test',
    required,
    source: 'probe',
  }
}

function cell(
  role: FleetCell['role'],
  env: FleetCell['env'],
  standards: FleetCell['standards'],
): FleetCell {
  const key = env == null ? `${role}:span` : `${role}:${env}`
  return {
    key,
    role,
    env,
    span: env == null,
    signal: 'ok',
    value: 'test',
    detail: '',
    probePath: '',
    standards,
    fixScope: null,
    agentFixEnabled: false,
    countsTowardVerdict: true,
  }
}

function snap(cells: FleetCell[]): FleetSnapshot {
  return {
    viewerEnv: 'dev',
    columns: ['dev', 'stg', 'prod'],
    roles: ['rocket', 'satellite', 'engineer', 'ground', 'vendor'],
    cells,
    verdict: {
      kind: 'GO',
      topReason: 'test',
      primaryCta: { label: 'None', kind: 'none' },
      worstCell: null,
    },
    fleetNominal: true,
    fleetClear: true,
  }
}

check('injects IB virtual when vendor has massive probe but no IB probe', () => {
  const vendor = cell('vendor', null, [
    std('api-massive', 'feed'),
    std('hermes', 'tooling'),
  ])
  const out = injectChecklistVirtualStandards([vendor])
  const ib = out[0].standards.find(s => s.id === 'ib-feed')
  assert.ok(ib != null, 'expected ib-feed virtual')
  assert.equal(ib!.source, 'checklist')
  assert.equal(ib!.required, true)
  assert.equal(ib!.signal, 'unknown')
})

check('does not inject IB when dedicated IB probe exists', () => {
  const vendor = cell('vendor', null, [
    std('ib-gateway', 'feed'),
    std('hermes', 'tooling'),
  ])
  const out = injectChecklistVirtualStandards([vendor])
  const ibVirtual = out[0].standards.find(s => s.id === 'ib-feed' && s.source === 'checklist')
  assert.equal(ibVirtual, undefined, 'IB must not be re-projected when ib-gateway probe exists')
  // Massive may still project when no massive/polygon probe is present
  const massiveVirtual = out[0].standards.find(
    s => s.id === 'massive-polygon' && s.source === 'checklist',
  )
  assert.ok(massiveVirtual != null, 'expected massive-polygon projection when Massive probe absent')
})

check('injects IB when only massive-polygon placeholder exists (claimed by massive item first)', () => {
  const vendor = cell('vendor', null, [
    std('massive-polygon', 'feed', 'unknown', false),
    std('hermes', 'tooling'),
  ])
  const out = injectChecklistVirtualStandards([vendor])
  const ib = out[0].standards.find(s => s.id === 'ib-feed')
  assert.ok(ib != null && ib.source === 'checklist')
})

check('paints db-backup-fresh virtual from checklist signals', () => {
  const sat = cell('satellite', 'prod', [
    std('nginx', 'edge'),
    std('postgres', 'datastore'),
    std('redis', 'datastore'),
  ])
  const out = injectChecklistVirtualStandards([sat], [
    {
      item_id: 'db-backup-fresh',
      signal: 'ok',
      detail: 'last completed 1h ago',
    },
  ])[0]
  const chip = out.standards.find(s => s.id === 'db-backup-fresh')
  assert.equal(chip?.source, 'checklist')
  assert.equal(chip?.signal, 'ok')
  assert.equal(resolveCellGate(out), 'GO')
})

check('IB virtual is required — Vendor cannot GO without IB Client', () => {
  const vendor = cell('vendor', null, [
    std('api-massive', 'feed'),
    std('hermes', 'tooling'),
  ])
  const out = injectChecklistVirtualStandards([vendor])[0]
  const ib = out.standards.find(s => s.id === 'ib-feed')
  assert.equal(ib?.required, true)
  assert.equal(ib?.signal, 'unknown')
  assert.equal(resolveCellGate(out), 'NO-GO')
})

check('IB probe ok allows Vendor GO when other required feeds ok', () => {
  const vendor = cell('vendor', null, [
    std('api-massive', 'feed'),
    std('ib-feed', 'feed'),
    std('hermes', 'tooling'),
  ])
  const out = injectChecklistVirtualStandards([vendor])[0]
  assert.equal(
    out.standards.some(s => s.id === 'ib-feed' && s.source === 'checklist'),
    false,
  )
  assert.equal(resolveCellGate(out), 'GO')
})

check('union audit: board gaps empty after vendor git-bridge removal + inject', () => {
  const fleet = applyChecklistFleetUnion(
    snap([
      cell('ground', null, [
        std('cluster-api', 'cluster'),
        std('nodes-ready', 'cluster'),
        std('failing-pods', 'cluster'),
      ]),
      cell('rocket', 'stg', [
        std('platform-api-stg', 'control'),
        std('platform-console-stg', 'control'),
        std('argo-stg', 'gitops'),
        std('deliver-stg', 'release'),
        std('stg-smoke', 'release'),
      ]),
      cell('rocket', 'dev', [
        std('platform-api-dev', 'control'),
        std('platform-console-dev', 'control'),
      ]),
      cell('rocket', 'prod', [
        std('platform-api-prod', 'control'),
        std('platform-console-prod', 'control'),
      ]),
      cell('satellite', 'dev', [
        std('nginx', 'edge'),
        std('api-monitor', 'api'),
        std('postgres', 'datastore'),
        std('redis', 'datastore'),
      ]),
      cell('satellite', 'stg', [
        std('nginx', 'edge'),
        std('api-monitor', 'api'),
        std('postgres', 'datastore'),
        std('redis', 'datastore'),
      ]),
      cell('satellite', 'prod', [
        std('nginx', 'edge'),
        std('api-monitor', 'api'),
        std('postgres', 'datastore'),
        std('redis', 'datastore'),
      ]),
      cell('engineer', null, [
        std('runners', 'automation'),
        std('git-bridge', 'automation'),
        std('mac-seat', 'seat'),
      ]),
      cell('vendor', null, [std('api-massive', 'feed'), std('hermes', 'tooling')]),
    ]),
  )

  const audit = auditChecklistFleetUnion(fleet)
  assert.equal(audit.boardGapCount, 0, `board gaps: ${JSON.stringify(audit.boardGaps)}`)
  assert.equal(
    audit.checklistNeedsProjection.length,
    0,
    `checklist missing: ${JSON.stringify(audit.checklistNeedsProjection)}`,
  )
  assert.ok(audit.virtualCount >= 1)

  const cov = buildChecklistCoverageIndex(fleet, { persistTouches: false })
  assert.equal(cov.uncoveredCount, 0)
  assert.ok(cov.virtualCount >= 1)
})

check('virtual ib-feed matches checklist ib-feed item', () => {
  const hit = matchStandardToChecklistItem('ib-feed', 'feed', {
    role: 'vendor',
    env: 'span',
  })
  assert.ok(hit != null)
  assert.equal(hit!.item.id, 'ib-feed')
})

check('catalog boardProjection contract', () => {
  assertChecklistBoardProjectionContract()
})

check('buildFleetSnapshot public exit includes IB Client probe (required)', () => {
  const fleet = buildFleetSnapshot({ viewerEnv: 'dev', matrices: [] })
  const ib = fleet.cells
    .find(c => c.role === 'vendor')
    ?.standards.find(s => s.id === 'ib-feed')
  assert.ok(ib != null, 'IB Client standard must exist')
  assert.equal(ib!.required, true)
  // Without ibGateway input → unknown probe (not checklist virtual)
  assert.equal(ib!.source ?? 'probe', 'probe')
  assert.equal(ib!.signal, 'unknown')
  const vendor = fleet.cells.find(c => c.role === 'vendor')!
  assert.equal(resolveCellGate(vendor), 'NO-GO')
  const audit = auditChecklistFleetUnion(fleet)
  assert.ok(
    !audit.checklistNeedsProjection.some(x => x.itemId === 'ib-feed'),
    'ib-feed covered by probe',
  )
})

check('buildFleetSnapshot Vendor GO when IB Gateway plugin ok', () => {
  const fleet = buildFleetSnapshot({
    viewerEnv: 'dev',
    matrices: [],
    ibGateway: { reachability: 'ok', reachable: true, summary: 'IB Gateway ready' },
    bridge: {
      generated_at: 't',
      remediation_runner: { url: '', status: 'ok' },
      runners: [],
      git_bridge: { status: 'ok' },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'unavailable' },
      nous_hermes: {
        status: 'ok',
        gateway_running: true,
        active_agents: 0,
        active_sessions: 0,
        mcp_tool_count: 0,
      },
      platform_mcp: {
        server_name: 'x',
        server_version: '0',
        tool_count: 0,
        implemented_count: 0,
        agent_tool_count: 0,
        transport: 'stdio',
        script_path: '',
      },
      nightly_report: { available: false },
    },
  })
  const vendor = fleet.cells.find(c => c.role === 'vendor')!
  const ib = vendor.standards.find(s => s.id === 'ib-feed')!
  assert.equal(ib.signal, 'ok')
  assert.equal(ib.required, true)
  const requiredOk = vendor.standards.filter(s => s.required !== false).every(s => s.signal === 'ok')
  assert.ok(requiredOk, 'required standards should be ok')
  assert.equal(resolveCellGate(vendor), 'GO')
})

console.log(`\n${passed} passed`)
