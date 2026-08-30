import { afterEach, describe, expect, it } from 'vitest'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  computeBuildDeskWorkloadCounts,
  countDoingLanes,
  countReadyLanes,
} from '@/lib/briefing/buildDeskWorkload'
import { setLaneCatalog, type WorkLane } from '@/lib/briefing/workLanes'

function program(partial: Partial<ProgramSummary> & { id: string }): ProgramSummary {
  return {
    title: partial.id,
    description: '',
    status: 'active',
    phase_count: 1,
    phases_done: 0,
    all_phases_done: false,
    active: true,
    ...partial,
  }
}

const readyLane: WorkLane = {
  id: 'test-ready-lane',
  track: 'automate',
  componentLine: 'engineer',
  trackType: 'build',
  label: 'Test Ready',
  shortLabel: 'Ready',
  description: 'Empty queue fixture',
  agentMode: 'Ops',
  workIntent: 'feature',
}

const doingLane: WorkLane = {
  id: 'market-data-gs-closeout',
  track: 'automate',
  componentLine: 'subcontractor',
  trackType: 'build',
  label: 'Market Data Golden Source Closeout',
  shortLabel: 'MD Closeout',
  description: 'Hardcoded done queue → Doing until sessionReleased',
  agentMode: 'Ops',
  workIntent: 'feature',
}

afterEach(() => {
  setLaneCatalog([])
})

describe('countReadyLanes / countDoingLanes', () => {
  it('Briefing counts Ready (empty); In Flight counts Doing when programsReady', () => {
    setLaneCatalog([readyLane, doingLane])
    const releasedByLane = new Map<string, boolean>([
      ['test-ready-lane', true],
      ['market-data-gs-closeout', false],
    ])
    const base = {
      context: undefined,
      matrices: [],
      clusterSummary: undefined,
      programs: [] as ProgramSummary[],
      releasedByLane,
    }
    expect(countReadyLanes(base)).toBe(1)
    expect(
      countDoingLanes({ ...base, programsReady: true }),
    ).toBe(1)
    expect(
      countDoingLanes({ ...base, programsReady: false }),
    ).toBe(0)
  })
})

describe('computeBuildDeskWorkloadCounts', () => {
  it('maps Ready → briefing and Doing → activeSession', () => {
    setLaneCatalog([readyLane, doingLane])
    const counts = computeBuildDeskWorkloadCounts({
      programs: [program({ id: 'x', lane_id: 'market-data-gs-closeout' })],
      context: undefined,
      matrices: [],
      clusterSummary: undefined,
      releasedByLane: new Map([
        ['test-ready-lane', true],
        ['market-data-gs-closeout', false],
      ]),
      programsReady: true,
    })
    expect(counts.briefing).toBe(1)
    expect(counts.activeSession).toBe(1)
  })
})
