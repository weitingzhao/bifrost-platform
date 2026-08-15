import { describe, expect, it } from 'vitest'
import { laneLifecycleFromQueue } from '@/lib/briefing/briefingStatus'
import {
  buildQueueForLane,
  setLaneCatalog,
  type WorkLane,
} from '@/lib/briefing/workLanes'

const closeoutLane: WorkLane = {
  id: 'market-data-gs-closeout',
  track: 'automate',
  componentLine: 'subcontractor',
  trackType: 'build',
  label: 'Market Data Golden Source Closeout',
  shortLabel: 'MD Closeout',
  description: 'Closeout residual',
  agentMode: 'Ops',
  workIntent: 'feature',
}

describe('market-data-gs-closeout In Flight queue', () => {
  it('keeps P6 and P10 ready_for_signoff until Owner signs', () => {
    setLaneCatalog([closeoutLane])
    const queue = buildQueueForLane('market-data-gs-closeout', undefined, [], undefined)
    expect(queue.find(q => q.id === 'P6')?.status).toBe('ready_for_signoff')
    expect(queue.find(q => q.id === 'P10')?.status).toBe('ready_for_signoff')
    expect(laneLifecycleFromQueue(queue, { programsReleased: false })).toBe('active')
    expect(laneLifecycleFromQueue(queue, { programsReleased: true })).toBe('complete')
  })
})
