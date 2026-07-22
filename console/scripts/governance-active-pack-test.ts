#!/usr/bin/env node
/**
 * Governance P0 — default LLM packs stay "active rules first, history archived".
 * Deterministic assertions (no test framework).
 * Usage: npx tsx scripts/governance-active-pack-test.ts
 */
import assert from 'node:assert/strict'
import {
  ROADMAP_ARCHIVE_SUMMARY,
  buildRoadmapLlmPack,
} from '../src/lib/architecture/roadmapCatalog'
import {
  VISION_ARCHIVE_SUMMARY,
  VISION_STATEMENT,
  buildDualFlywheelVisionLlmPack,
} from '../src/lib/architecture/dualFlywheelVisionCatalog'
import {
  MISSION_SIGNAL_PROGRAM_REFERENCE,
  buildAgentProtocolLlmPack,
} from '../src/lib/architecture/agentProtocolCatalog'

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

// ---------------------------------------------------------------------------
// Roadmap pack
// ---------------------------------------------------------------------------

const roadmap = buildRoadmapLlmPack()

check('roadmap — active baseline sections present', () => {
  assert.ok(roadmap.includes('## §1 Hardware mapping'))
  assert.ok(roadmap.includes('## §2 Software baseline'))
  assert.ok(roadmap.includes('Phase C — AI-native ops + downstream (active)'))
  assert.ok(roadmap.includes('## Owner checklist'))
  assert.ok(roadmap.includes('## Related authorities'))
})

check('roadmap — archive summary present, exactly one line of A/B trace', () => {
  assert.ok(roadmap.includes('## Archive (history — not current work)'))
  assert.ok(roadmap.includes(ROADMAP_ARCHIVE_SUMMARY))
})

check('roadmap — Phase A/B runbook detail no longer in default pack', () => {
  assert.ok(!roadmap.includes('A1.1'), 'A1 2C-B cutover steps leaked')
  assert.ok(!roadmap.includes('### A2 Mac Mini roles'), 'Phase A Mac Mini table leaked')
  assert.ok(!roadmap.includes('4090 trial rules'), 'Phase A GPU rules leaked')
  assert.ok(!roadmap.includes('mini-pc-a: Ubuntu 24.04'), 'Phase B bootstrap steps leaked')
  assert.ok(!roadmap.includes('Repo layout:'), 'Phase B repo layout leaked')
  assert.ok(!roadmap.includes('App order:'), 'Phase B app migration order leaked')
})

check('roadmap — no "current priority" misdirection', () => {
  assert.ok(!roadmap.includes('current priority'))
})

// ---------------------------------------------------------------------------
// Vision pack
// ---------------------------------------------------------------------------

const vision = buildDualFlywheelVisionLlmPack()

check('vision — thesis and absolute boundaries retained', () => {
  assert.ok(vision.includes(VISION_STATEMENT))
  assert.ok(vision.includes('## Flywheel convergence thesis'))
  assert.ok(vision.includes('## Absolute boundaries'))
  assert.ok(vision.includes('R-DV3'))
  assert.ok(vision.includes('## Decoupling principle (platform ≠ business)'))
  assert.ok(vision.includes('## Three-layer Agent architecture'))
})

check('vision — V1–V5 collapsed to SIGNED archive summary', () => {
  assert.ok(vision.includes('## Convergence milestones (V1–V5 — archived, SIGNED)'))
  assert.ok(vision.includes(VISION_ARCHIVE_SUMMARY))
})

check('vision — V1–V5 deliverable detail no longer in default pack', () => {
  assert.ok(!vision.includes('### V1:'), 'V1 milestone block leaked')
  assert.ok(!vision.includes('### V5:'), 'V5 milestone block leaked')
  assert.ok(!vision.includes('bifrost-dev namespace with remaining APIs'), 'V1 deliverable leaked')
  assert.ok(!vision.includes('Cursor SDK CI hook'), 'V2 deliverable leaked')
  assert.ok(!vision.includes('→ Unlocks:'), 'milestone unlock lines leaked')
})

// ---------------------------------------------------------------------------
// Agent Protocol pack
// ---------------------------------------------------------------------------

const protocol = buildAgentProtocolLlmPack()

check('protocol — live protocol sections retained (modes/domains/forbidden/D10)', () => {
  assert.ok(protocol.includes('## Agent modes (per-session intent)'))
  assert.ok(protocol.includes('## Forbidden actions (all modes)'))
  assert.ok(protocol.includes('D10'))
  assert.ok(protocol.includes('ib:operator:cmd'))
})

check('protocol — P2/P3 diagnostics remain live protocol', () => {
  assert.ok(protocol.includes('## Mission diagnostic playbooks (verify_payload)'))
  assert.ok(protocol.includes('## Mission post-fix validation loop (Autonomous Loop)'))
  assert.ok(protocol.includes('## Network diagnostic playbooks'))
})

check('protocol — P4–P7 collapsed to a program reference', () => {
  assert.ok(protocol.includes('## Mission Signal program references (P4–P7'))
  assert.ok(protocol.includes(MISSION_SIGNAL_PROGRAM_REFERENCE))
  assert.ok(protocol.includes('Delivery Board'))
  assert.ok(protocol.includes('Agent System'))
})

check('protocol — P4–P7 step detail no longer in default pack', () => {
  assert.ok(!protocol.includes('get_hermes_readiness'), 'P4 Hermes readiness steps leaked')
  assert.ok(!protocol.includes('0. Readiness gate'), 'P4 step table leaked')
  assert.ok(!protocol.includes('1. Performance KPIs'), 'P5 Flight Director steps leaked')
  assert.ok(!protocol.includes('trust-overrides/{skill_id}'), 'P6 trust override steps leaked')
  assert.ok(!protocol.includes('3. Maintenance mode'), 'P7 closure steps leaked')
})

console.log(`\n${passed} checks passed — governance active packs are lean.`)
