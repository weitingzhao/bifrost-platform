/**
 * Daily Ops Checklist ↔ Fleet Board coverage / dry-run + run touch.
 * Run: npx tsx scripts/daily-ops-checklist-coverage-test.ts
 */
import * as assert from 'node:assert/strict'
import {
  buildChecklistCoverageIndex,
  coverageKey,
  formatChecklistTouchAge,
  formatChecklistTouchAgeCompact,
  lookupCoverage,
  matchCellStandard,
  recordChecklistRunTouch,
  touchKindShortLabel,
} from '../src/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetCell, FleetSnapshot } from '../src/lib/control-room/fleetSnapshot'

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
): FleetCell['standards'][0] {
  return { id, label: id, signal, group, reason: 'test' }
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
  }
}

const groundCell = cell('ground', null, [
  std('cluster-api', 'cluster'),
  std('nodes-ready', 'cluster'),
  std('failing-pods', 'cluster'),
  std('path-missing', 'path', 'unavailable'),
])

const mockFleet: FleetSnapshot = {
  viewerEnv: 'dev',
  columns: ['dev', 'stg', 'prod'],
  roles: ['rocket', 'satellite', 'engineer', 'ground', 'vendor'],
  cells: [
    groundCell,
    cell('rocket', 'stg', [
      std('platform-api-stg', 'control'),
      std('platform-console-stg', 'control'),
      std('argo-stg', 'gitops'),
      std('deliver-stg', 'release'),
      std('stg-smoke', 'release'),
      std('orphan-probe', 'control'),
    ]),
    cell('engineer', null, [
      std('runners', 'automation'),
      std('git-bridge', 'automation'),
      std('mac-seat', 'seat'),
    ]),
  ],
  verdict: {
    kind: 'GO',
    topReason: 'test',
    primaryCta: { label: 'None', kind: 'none' },
    worstCell: null,
  },
  fleetNominal: true,
  fleetClear: true,
}

check('formatChecklistTouchAge buckets', () => {
  const now = Date.parse('2026-07-18T12:00:00.000Z')
  assert.equal(formatChecklistTouchAge('2026-07-18T11:59:58.000Z', now), 'just now')
  assert.equal(formatChecklistTouchAge('2026-07-18T11:59:30.000Z', now), '30s ago')
  assert.equal(formatChecklistTouchAgeCompact('2026-07-18T11:59:30.000Z', now), '30s')
  assert.equal(formatChecklistTouchAge('2026-07-18T11:00:00.000Z', now), '1h ago')
  assert.equal(formatChecklistTouchAge(null, now), 'never')
})

check('touchKindShortLabel', () => {
  assert.equal(touchKindShortLabel('dry-run'), 'd')
  assert.equal(touchKindShortLabel('run'), 'r')
})

check('matchCellStandard respects cell mapping', () => {
  const hit = matchCellStandard(
    { key: 'ground:span', role: 'ground', env: null },
    { id: 'cluster-api', group: 'cluster' },
  )
  assert.ok(hit != null)
  assert.equal(hit!.item.id, 'cluster-api')

  const wrongRole = matchCellStandard(
    { key: 'rocket:stg', role: 'rocket', env: 'stg' },
    { id: 'cluster-api', group: 'cluster' },
  )
  assert.equal(wrongRole, null)
})

check('buildChecklistCoverageIndex dry-run by default', () => {
  const idx = buildChecklistCoverageIndex(mockFleet, {
    dryRunAt: '2026-07-18T12:00:00.000Z',
    persistTouches: false,
  })
  assert.ok(idx.coveredCount >= 10)
  assert.ok(idx.uncoveredCount >= 1)
  assert.equal(idx.excludedCount, 1)
  assert.equal(idx.runTouchedCount, 0)
  assert.equal(idx.virtualCount, 0)

  const cluster = lookupCoverage(idx, { key: 'ground:span' }, { id: 'cluster-api' })
  assert.ok(cluster?.hit != null)
  assert.equal(cluster!.hit!.touchKind, 'dry-run')
  assert.equal(cluster!.hit!.touchedAt, '2026-07-18T12:00:00.000Z')
})

check('recordChecklistRunTouch prefers run over dry-run', () => {
  // Use in-memory-ish path: persist true needs sessionStorage — polyfill for node
  const mem: Record<string, string> = {}
  // @ts-expect-error test polyfill
  globalThis.sessionStorage = {
    getItem: (k: string) => mem[k] ?? null,
    setItem: (k: string, v: string) => {
      mem[k] = v
    },
    removeItem: (k: string) => {
      delete mem[k]
    },
  }

  buildChecklistCoverageIndex(mockFleet, {
    dryRunAt: '2026-07-18T12:00:00.000Z',
    persistTouches: true,
  })
  recordChecklistRunTouch(groundCell, { at: '2026-07-18T12:05:00.000Z' })

  const idx = buildChecklistCoverageIndex(mockFleet, {
    dryRunAt: '2026-07-18T12:10:00.000Z',
    persistTouches: true,
  })
  const cluster = lookupCoverage(idx, { key: 'ground:span' }, { id: 'cluster-api' })
  assert.ok(cluster?.hit != null)
  assert.equal(cluster!.hit!.touchKind, 'run')
  assert.equal(cluster!.hit!.touchedAt, '2026-07-18T12:05:00.000Z')
  assert.equal(cluster!.hit!.dryRunAt, '2026-07-18T12:10:00.000Z')
  assert.ok(idx.runTouchedCount >= 3)
})

check('coverageKey is cell-scoped', () => {
  assert.equal(coverageKey('rocket:stg', 'platform-api-stg'), 'rocket:stg::platform-api-stg')
})

console.log(`\n${passed} passed`)
