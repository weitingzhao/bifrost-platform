#!/usr/bin/env node
/**
 * Lane detail context contract — deterministic unit tests (no test framework).
 * Usage: npx tsx scripts/lane-detail-context-test.ts
 */
import assert from 'node:assert/strict'
import { resolveInitialLaneStep } from '../src/lib/delivery/initialLaneStep'
import {
  LANE_DETAIL_REASON_COPY,
  LANE_DETAIL_REASONS,
  LANE_DETAIL_SUBTITLE,
  parseLaneDetailReason,
} from '../src/lib/delivery/laneDetailContext'
import type { StepStatus } from '../src/lib/delivery/releaseStepTypes'

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok — ${name}`)
  } catch (e) {
    console.error(`FAIL — ${name}`)
    throw e
  }
}

check('every reason has copy', () => {
  for (const reason of LANE_DETAIL_REASONS) {
    const copy = LANE_DETAIL_REASON_COPY[reason]
    assert.ok(copy.label.length > 0, `${reason} label`)
    assert.ok(copy.description.length > 0, `${reason} description`)
  }
})

check('subtitle names Mission Launch TCC as the primary workflow', () => {
  assert.match(LANE_DETAIL_SUBTITLE, /Mission Launch TCC/)
  assert.match(LANE_DETAIL_SUBTITLE, /operate & evidence/i)
})

check('plain hash defaults to direct', () => {
  assert.equal(parseLaneDetailReason('#platform-release'), 'direct')
  assert.equal(parseLaneDetailReason('#trade-release'), 'direct')
  assert.equal(parseLaneDetailReason(''), 'direct')
})

check('hash query resolves each known reason', () => {
  for (const reason of LANE_DETAIL_REASONS) {
    assert.equal(parseLaneDetailReason(`#trade-release?detail=${reason}`), reason)
  }
})

check('unknown or malformed reasons fall back to direct', () => {
  assert.equal(parseLaneDetailReason('#trade-release?detail=bogus'), 'direct')
  assert.equal(parseLaneDetailReason('#trade-release?detail='), 'direct')
  assert.equal(parseLaneDetailReason('#trade-release?other=1'), 'direct')
})

check('reason coexists with existing hash params', () => {
  assert.equal(
    parseLaneDetailReason('#platform-release?foo=bar&detail=failed-run'),
    'failed-run',
  )
})

// --- resolveInitialLaneStep — smart initial step focus -----------------------
// Step layout: [Staging Deploy, Staging Gate, Production Deploy, Production Gate]

const s = (...statuses: StepStatus[]) => statuses

check('all planned → focus first step', () => {
  assert.equal(resolveInitialLaneStep(s('pending', 'pending', 'pending', 'pending')), 0)
})

check('first error wins (stg deploy failed)', () => {
  assert.equal(resolveInitialLaneStep(s('error', 'pending', 'pending', 'pending')), 0)
  assert.equal(resolveInitialLaneStep(s('done', 'pending', 'error', 'pending')), 2)
})

check('error beats active', () => {
  assert.equal(resolveInitialLaneStep(s('active', 'pending', 'error', 'pending')), 2)
})

check('active step wins over next-pending (stg gate running)', () => {
  assert.equal(resolveInitialLaneStep(s('done', 'active', 'pending', 'pending')), 1)
  assert.equal(resolveInitialLaneStep(s('done', 'done', 'active', 'pending')), 2)
})

check('otherwise focus the first not-done step', () => {
  assert.equal(resolveInitialLaneStep(s('done', 'pending', 'pending', 'pending')), 1)
  assert.equal(resolveInitialLaneStep(s('done', 'done', 'done', 'pending')), 3)
})

check('all done → focus last step (final gate result)', () => {
  assert.equal(resolveInitialLaneStep(s('done', 'done', 'done', 'done')), 3)
})

check('reason acceptance-detail → Staging Gate regardless of statuses', () => {
  assert.equal(
    resolveInitialLaneStep(s('done', 'done', 'done', 'done'), 'acceptance-detail'),
    1,
  )
  assert.equal(
    resolveInitialLaneStep(s('error', 'pending', 'pending', 'pending'), 'acceptance-detail'),
    1,
  )
})

check('reason manual-gate → first gate not passed', () => {
  assert.equal(
    resolveInitialLaneStep(s('done', 'pending', 'pending', 'pending'), 'manual-gate'),
    1,
  )
  assert.equal(
    resolveInitialLaneStep(s('done', 'done', 'done', 'pending'), 'manual-gate'),
    3,
  )
  // Both gates passed → fall back to generic rules (all done → last step).
  assert.equal(
    resolveInitialLaneStep(s('done', 'done', 'done', 'done'), 'manual-gate'),
    3,
  )
})

check('reason failed-run → first error step, else generic rules', () => {
  assert.equal(
    resolveInitialLaneStep(s('done', 'done', 'error', 'pending'), 'failed-run'),
    2,
  )
  // No error present → generic rules (first active).
  assert.equal(
    resolveInitialLaneStep(s('done', 'active', 'pending', 'pending'), 'failed-run'),
    1,
  )
})

check('reason direct behaves like generic rules', () => {
  assert.equal(resolveInitialLaneStep(s('done', 'active', 'pending', 'pending'), 'direct'), 1)
})

check('empty statuses → safe default 0', () => {
  assert.equal(resolveInitialLaneStep([]), 0)
})

console.log(`\n${passed} checks passed`)
