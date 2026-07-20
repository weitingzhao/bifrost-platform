/**
 * Verify Daily Ops Checklist catalog coverage contract.
 * Run: npx tsx scripts/daily-ops-checklist-test.ts
 */
import * as assert from 'node:assert/strict'
import {
  DAILY_OPS_CHECKLIST,
  DAILY_OPS_CHECKLIST_META,
  assertChecklistBoardProjectionContract,
  checklistBlockingSteps,
  checklistCoveredGroups,
  checklistTotalItems,
  matchStandardToChecklistItem,
} from '../src/lib/control-room/dailyOpsChecklistCatalog'
import {
  FLEET_STANDARD_GROUP_ORDER,
  type FleetStandardGroup,
} from '../src/lib/control-room/fleetSnapshot'

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

// ---------------------------------------------------------------------------
// Structural checks
// ---------------------------------------------------------------------------

check('catalog has 7 steps in dependency order', () => {
  assert.equal(DAILY_OPS_CHECKLIST.length, 7)
  for (let i = 0; i < DAILY_OPS_CHECKLIST.length; i++) {
    assert.equal(DAILY_OPS_CHECKLIST[i].order, i + 1)
  }
})

check('total items matches meta', () => {
  assert.equal(checklistTotalItems(), DAILY_OPS_CHECKLIST_META.coverage.totalItems)
})

check('all item ids are unique', () => {
  const ids = DAILY_OPS_CHECKLIST.flatMap(s => s.items.map(i => i.id))
  assert.equal(ids.length, new Set(ids).size)
})

check('all step ids are unique', () => {
  const ids = DAILY_OPS_CHECKLIST.map(s => s.id)
  assert.equal(ids.length, new Set(ids).size)
})

// ---------------------------------------------------------------------------
// Coverage checks
// ---------------------------------------------------------------------------

check('covers all Fleet groups except path', () => {
  const covered = checklistCoveredGroups()
  const expected = FLEET_STANDARD_GROUP_ORDER.filter(g => g !== 'path')
  for (const g of expected) {
    assert.ok(covered.includes(g), `Missing group: ${g}`)
  }
})

check('covers all 5 fleet roles', () => {
  const roles = new Set(DAILY_OPS_CHECKLIST.flatMap(s => s.fleetMapping.map(m => m.role)))
  assert.ok(roles.has('ground'))
  assert.ok(roles.has('rocket'))
  assert.ok(roles.has('engineer'))
  assert.ok(roles.has('satellite'))
  assert.ok(roles.has('vendor'))
})

check('satellite covers all 3 envs', () => {
  const satEnvs = new Set(
    DAILY_OPS_CHECKLIST.flatMap(s =>
      s.fleetMapping.filter(m => m.role === 'satellite').map(m => m.env),
    ),
  )
  assert.ok(satEnvs.has('dev'))
  assert.ok(satEnvs.has('stg'))
  assert.ok(satEnvs.has('prod'))
})

check('rocket covers all 3 envs', () => {
  const rocketEnvs = new Set(
    DAILY_OPS_CHECKLIST.flatMap(s =>
      s.fleetMapping.filter(m => m.role === 'rocket').map(m => m.env),
    ),
  )
  assert.ok(rocketEnvs.has('dev'))
  assert.ok(rocketEnvs.has('stg'))
  assert.ok(rocketEnvs.has('prod'))
})

// ---------------------------------------------------------------------------
// Fix capability checks
// ---------------------------------------------------------------------------

check('fixCapability summary matches actual items', () => {
  const summary = { full_auto: 0, semi_auto: 0, manual: 0, observe: 0 }
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      summary[item.fixCapability] += 1
    }
  }
  assert.deepEqual(summary, DAILY_OPS_CHECKLIST_META.coverage.fixCapabilitySummary)
})

check('every semi_auto or manual item has manualAction', () => {
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      if (item.fixCapability === 'semi_auto' || item.fixCapability === 'manual') {
        assert.ok(
          item.manualAction != null && item.manualAction.length > 0,
          `${step.id}/${item.id} is ${item.fixCapability} but missing manualAction`,
        )
      }
    }
  }
})

check('every full_auto or semi_auto item has fixScope', () => {
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      if (item.fixCapability === 'full_auto' || item.fixCapability === 'semi_auto') {
        assert.ok(
          item.fixScope != null && item.fixScope.length > 0,
          `${step.id}/${item.id} is ${item.fixCapability} but missing fixScope`,
        )
      }
    }
  }
})

// ---------------------------------------------------------------------------
// Blocking logic
// ---------------------------------------------------------------------------

check('blocking steps are infra-cluster and data-layer only', () => {
  const blocking = checklistBlockingSteps()
  assert.equal(blocking.length, 2)
  assert.equal(blocking[0].id, 'infra-cluster')
  assert.equal(blocking[1].id, 'data-layer')
})

check('blocking steps have at least 1 critical item', () => {
  for (const step of checklistBlockingSteps()) {
    const hasCritical = step.items.some(i => i.critical)
    assert.ok(hasCritical, `${step.id} blocks downstream but has no critical item`)
  }
})

// ---------------------------------------------------------------------------
// Standard matching
// ---------------------------------------------------------------------------

check('matchStandardToChecklistItem: runners → engineer-seat step', () => {
  const result = matchStandardToChecklistItem('runners', 'automation')
  assert.ok(result != null)
  assert.equal(result.step.id, 'engineer-seat')
  assert.equal(result.item.id, 'runners-ha')
})

check('matchStandardToChecklistItem: cluster-api → infra-cluster step', () => {
  const result = matchStandardToChecklistItem('cluster-api', 'cluster')
  assert.ok(result != null)
  assert.equal(result.step.id, 'infra-cluster')
  assert.equal(result.item.id, 'cluster-api')
})

check('matchStandardToChecklistItem: stg-smoke → release-readiness step', () => {
  const result = matchStandardToChecklistItem('stg-smoke', 'release')
  assert.ok(result != null)
  assert.equal(result.step.id, 'release-readiness')
  assert.equal(result.item.id, 'stg-smoke')
})

check('matchStandardToChecklistItem: dynamic matrix target (api group) → business-services', () => {
  const result = matchStandardToChecklistItem('api-monitor', 'api')
  assert.ok(result != null)
  assert.equal(result.step.id, 'business-services')
  assert.equal(result.item.id, 'trade-apis')
})

check('matchStandardToChecklistItem: redis target → data-layer', () => {
  const result = matchStandardToChecklistItem('redis-live', 'datastore')
  assert.ok(result != null)
  assert.equal(result.step.id, 'data-layer')
  assert.equal(result.item.id, 'redis')
})

check('matchStandardToChecklistItem: hermes → external-vendors', () => {
  const result = matchStandardToChecklistItem('hermes', 'tooling')
  assert.ok(result != null)
  assert.equal(result.step.id, 'external-vendors')
  assert.equal(result.item.id, 'hermes-tooling')
})

check('matchStandardToChecklistItem: mac-seat → engineer-seat', () => {
  const result = matchStandardToChecklistItem('mac-seat', 'seat')
  assert.ok(result != null)
  assert.equal(result.step.id, 'engineer-seat')
  assert.equal(result.item.id, 'mac-probe-bridge')
})

check('matchStandardToChecklistItem: unknown group returns null', () => {
  const result = matchStandardToChecklistItem('something', 'path')
  assert.equal(result, null)
})

// ---------------------------------------------------------------------------
// D10 constraint
// ---------------------------------------------------------------------------

check('IB feed item is observe-only with no fixScope', () => {
  const vendorStep = DAILY_OPS_CHECKLIST.find(s => s.id === 'external-vendors')!
  const ib = vendorStep.items.find(i => i.id === 'ib-feed')!
  assert.equal(ib.fixCapability, 'observe')
  assert.equal(ib.fixScope, null)
})

check('boardProjection contract (observe / null-pattern shadow)', () => {
  assertChecklistBoardProjectionContract()
})

console.log(`\n${passed} checks passed`)
