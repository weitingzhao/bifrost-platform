/**
 * Daily Ops Execution queue helpers (Human vs Agent + noise).
 * Ported from scripts/daily-ops-execution-queue-test.ts.
 */
import { describe, expect, it } from 'vitest'
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import type { RemediationJob } from '@/api/remediationTypes'
import {
  actionableOpenCount,
  isOperateQueueNoise,
  linkedRemediationJob,
  originFromOperateItem,
  partitionOpenQueue,
  queueLaneForOrigin,
  queueLinkedJobChip,
  queueLinkedJobStatusLabel,
} from '@/lib/control-room/dailyOpsExecutionQueue'

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

describe('dailyOpsExecutionQueue', () => {
  it('Human lane: manual / D10 skipped', () => {
    expect(queueLaneForOrigin(originFromOperateItem(item({ id: '1', title: 'Mac seat', source: 'manual' })))).toBe(
      'human',
    )
    expect(
      queueLaneForOrigin(
        originFromOperateItem(item({ id: '2', title: 'IB feed', reason: 'D10 observe never auto-dispatch' })),
      ),
    ).toBe('human')
  })

  it('Agent lane: checklist_dispatch / ask-ai', () => {
    expect(
      queueLaneForOrigin(
        originFromOperateItem(item({ id: '3', title: 'Git dirty', source: 'checklist_dispatch' })),
      ),
    ).toBe('agent')
    expect(
      queueLaneForOrigin(
        originFromOperateItem(item({ id: '4', title: 'Ask for AI pack', reason: 'ask-ai failover' })),
      ),
    ).toBe('agent')
  })

  it('Noise: dedup / skip D10 excluded from actionable count', () => {
    const open = [
      item({ id: 'a', title: 'Real handoff', source: 'post_completion' }),
      item({ id: 'b', title: 'Skip dedup 24h', reason: 'dedup 24h' }),
      item({ id: 'c', title: 'Skip · D10', reason: 'skip D10 observe' }),
    ]
    expect(isOperateQueueNoise(open[1]!)).toBe(true)
    expect(isOperateQueueNoise(open[2]!)).toBe(true)
    expect(actionableOpenCount(open)).toBe(1)
    const p = partitionOpenQueue(open)
    expect(p.actionable).toBe(1)
    expect(p.noise).toHaveLength(2)
    expect(p.agentActionable).toBe(1)
  })

  it('Linked job status chips; terminal → null (History not Queue)', () => {
    const running = job({ id: 'j1', status: 'running', phase: 'remediating' })
    const awaiting = job({ id: 'j2', status: 'running', phase: 'awaiting_approval' })
    const done = job({ id: 'j3', status: 'done', phase: 'done' })
    expect(queueLinkedJobStatusLabel(running)).toBe('running')
    expect(queueLinkedJobStatusLabel(awaiting)).toBe('awaiting_approval')
    expect(queueLinkedJobStatusLabel(done)).toBeNull()
    expect(queueLinkedJobChip(running)?.label).toBe('running')
    expect(queueLinkedJobChip(done)).toBeNull()

    const q = item({ id: 'q1', title: 'Fix', execution_job_id: 'j1' })
    expect(linkedRemediationJob(q, [running, awaiting])?.id).toBe('j1')
    expect(linkedRemediationJob(item({ id: 'q2', title: 'No link' }), [running])).toBeNull()
  })
})
