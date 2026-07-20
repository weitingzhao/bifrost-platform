#!/usr/bin/env node
/**
 * Daily Ops Execution queue helpers (Human vs Agent + noise).
 * Usage: npx tsx scripts/daily-ops-execution-queue-test.ts
 */
import assert from 'node:assert/strict'
import type { OperateQueueItem } from '../src/api/operateQueueTypes'
import type { RemediationJob } from '../src/api/types'
import {
  actionableOpenCount,
  isOperateQueueNoise,
  linkedRemediationJob,
  originFromOperateItem,
  partitionOpenQueue,
  queueLaneForOrigin,
  queueLinkedJobChip,
  queueLinkedJobStatusLabel,
} from '../src/lib/control-room/dailyOpsExecutionQueue'

function item(partial: Partial<OperateQueueItem> & Pick<OperateQueueItem, 'id' | 'title'>): OperateQueueItem {
  return {
    program_id: 'daily-ops',
    status: 'open',
    created_at: '2026-07-19T00:00:00Z',
    ...partial,
  }
}

function job(partial: Partial<RemediationJob> & Pick<RemediationJob, 'id' | 'status'>): RemediationJob {
  return {
    phase: 'remediating',
    created_at: '2026-07-19T00:00:00Z',
    updated_at: '2026-07-19T00:00:00Z',
    ...partial,
  }
}

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`  ✓ ${name}`)
  } catch (e) {
    console.error(`  ✗ ${name}`)
    throw e
  }
}

console.log('daily-ops-execution-queue-test')

check('Human lane: manual / D10 skipped', () => {
  assert.equal(queueLaneForOrigin(originFromOperateItem(item({ id: '1', title: 'Mac seat', source: 'manual' }))), 'human')
  assert.equal(
    queueLaneForOrigin(
      originFromOperateItem(item({ id: '2', title: 'IB feed', reason: 'D10 observe never auto-dispatch' })),
    ),
    'human',
  )
})

check('Agent lane: checklist_dispatch / ask-ai', () => {
  assert.equal(
    queueLaneForOrigin(
      originFromOperateItem(item({ id: '3', title: 'Git dirty', source: 'checklist_dispatch' })),
    ),
    'agent',
  )
  assert.equal(
    queueLaneForOrigin(
      originFromOperateItem(item({ id: '4', title: 'Ask for AI pack', reason: 'ask-ai failover' })),
    ),
    'agent',
  )
})

check('Noise: dedup / skip D10 excluded from actionable count', () => {
  const open = [
    item({ id: 'a', title: 'Real handoff', source: 'post_completion' }),
    item({ id: 'b', title: 'Skip dedup 24h', reason: 'dedup 24h' }),
    item({ id: 'c', title: 'Skip · D10', reason: 'skip D10 observe' }),
  ]
  assert.equal(isOperateQueueNoise(open[1]!), true)
  assert.equal(isOperateQueueNoise(open[2]!), true)
  assert.equal(actionableOpenCount(open), 1)
  const p = partitionOpenQueue(open)
  assert.equal(p.actionable, 1)
  assert.equal(p.noise.length, 2)
  assert.equal(p.agentActionable, 1)
})

check('Linked job status chips; terminal → null (History not Queue)', () => {
  const running = job({ id: 'j1', status: 'running', phase: 'remediating' })
  const awaiting = job({ id: 'j2', status: 'running', phase: 'awaiting_approval' })
  const done = job({ id: 'j3', status: 'done', phase: 'done' })
  assert.equal(queueLinkedJobStatusLabel(running), 'running')
  assert.equal(queueLinkedJobStatusLabel(awaiting), 'awaiting_approval')
  assert.equal(queueLinkedJobStatusLabel(done), null)
  assert.equal(queueLinkedJobChip(running)?.label, 'running')
  assert.equal(queueLinkedJobChip(done), null)

  const q = item({ id: 'q1', title: 'Fix', execution_job_id: 'j1' })
  assert.equal(linkedRemediationJob(q, [running, awaiting])?.id, 'j1')
  assert.equal(linkedRemediationJob(item({ id: 'q2', title: 'No link' }), [running]), null)
})

console.log(`\n${passed} checks passed`)
