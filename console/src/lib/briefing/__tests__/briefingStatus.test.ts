import { describe, expect, it } from 'vitest'
import type { ProgramSummary } from '@/api/programsTypes'
import type { QueueItem } from '@/lib/briefing/workLanes'
import {
  buildLaneProgramsCatalogCompleteMap,
  buildLaneProgramsSessionReleasedMap,
  computeScopeWorkSummary,
  isLaneLifecycleHold,
  isProgramCatalogComplete,
  isProgramDeliveryClosed,
  isProgramSessionReleased,
  laneLifecycleFromQueue,
  openDeliveryProgramsForLane,
  programsReleasedForLane,
  selectBriefingQueueInventory,
  type ScopeQueueLaneBreakdown,
} from '@/lib/briefing/briefingStatus'

function item(partial: Partial<QueueItem> & { id: string; status: QueueItem['status'] }): QueueItem {
  return { label: partial.id, ...partial }
}

function program(partial: Partial<ProgramSummary> & { id: string }): ProgramSummary {
  return {
    title: partial.id,
    description: '',
    status: 'active',
    phase_count: 4,
    phases_done: 4,
    all_phases_done: true,
    active: false,
    ...partial,
  }
}

describe('catalogComplete vs sessionReleased', () => {
  it('auto-graduates when post_completion is not required and gates are done', () => {
    const p = program({
      id: 'no-pc',
      complete: true,
      signed: 4,
      sign_off_required_count: 4,
    })
    expect(isProgramCatalogComplete(p)).toBe(true)
    expect(isProgramSessionReleased(p)).toBe(true)
    expect(isProgramDeliveryClosed(p)).toBe(true)
  })

  it('stays open when requires_post_completion and assessment is empty', () => {
    const p = program({
      id: 'req',
      complete: true,
      signed: 4,
      sign_off_required_count: 4,
      requires_post_completion: true,
    })
    expect(isProgramCatalogComplete(p)).toBe(false)
    expect(isProgramSessionReleased(p)).toBe(false)
    expect(isProgramDeliveryClosed(p)).toBe(false)
  })

  it('closes both after no_handoff', () => {
    const p = program({
      id: 'req',
      complete: true,
      signed: 4,
      sign_off_required_count: 4,
      requires_post_completion: true,
      assessment_status: 'no_handoff',
    })
    expect(isProgramCatalogComplete(p)).toBe(true)
    expect(isProgramSessionReleased(p)).toBe(true)
  })

  it('releases session but not catalog when in_operate', () => {
    const p = program({
      id: 'req',
      complete: true,
      signed: 4,
      sign_off_required_count: 4,
      requires_post_completion: true,
      assessment_status: 'in_operate',
    })
    expect(isProgramCatalogComplete(p)).toBe(false)
    expect(isProgramSessionReleased(p)).toBe(true)
  })

  it('closes catalog status completed without post-completion assessment', () => {
    const p = program({
      id: 'closed-yaml',
      status: 'completed',
      complete: false,
      signed: 0,
      sign_off_required_count: 4,
      requires_post_completion: true,
    })
    expect(isProgramCatalogComplete(p)).toBe(true)
    expect(isProgramSessionReleased(p)).toBe(true)
  })

  it('keeps pending_review in Active Session', () => {
    const p = program({
      id: 'req',
      complete: true,
      signed: 4,
      sign_off_required_count: 4,
      requires_post_completion: true,
      assessment_status: 'pending_review',
    })
    expect(isProgramCatalogComplete(p)).toBe(false)
    expect(isProgramSessionReleased(p)).toBe(false)
  })

  it('is open when gates are unsigned', () => {
    const p = program({
      id: 'a',
      complete: false,
      signed: 2,
      sign_off_required_count: 4,
      phases_done: 2,
      all_phases_done: false,
    })
    expect(isProgramCatalogComplete(p)).toBe(false)
    expect(isProgramSessionReleased(p)).toBe(false)
  })
})

describe('laneLifecycleFromQueue + programsReleased', () => {
  const doneQueue = [item({ id: 'a', status: 'closed' }), item({ id: 'b', status: 'done' })]

  it('archives when queue done and programs released', () => {
    expect(laneLifecycleFromQueue(doneQueue, { programsReleased: true })).toBe('complete')
    expect(laneLifecycleFromQueue(doneQueue, { programsClosed: true })).toBe('complete')
  })

  it('stays Doing when queue done but Delivery still open', () => {
    expect(laneLifecycleFromQueue(doneQueue, { programsReleased: false })).toBe('active')
  })

  it('leaves In Flight after sessionReleased even if catalog still says ready_for_signoff', () => {
    const staleSignoff = [
      item({ id: 'P6', status: 'ready_for_signoff' }),
      item({ id: 'P10', status: 'ready_for_signoff' }),
    ]
    expect(laneLifecycleFromQueue(staleSignoff, { programsReleased: true })).toBe('complete')
    expect(laneLifecycleFromQueue(staleSignoff, { programsReleased: false })).toBe('active')
  })

  it('keeps Doing when sessionReleased but queue still has in_progress/issue work', () => {
    expect(
      laneLifecycleFromQueue([item({ id: 'x', status: 'issue' })], { programsReleased: true }),
    ).toBe('active')
  })

  it('holds all-done lanes until the board map is ready (callers must skip Doing/Archive)', () => {
    expect(isLaneLifecycleHold(doneQueue, undefined)).toBe(true)
    expect(isLaneLifecycleHold(doneQueue, false)).toBe(false)
    expect(isLaneLifecycleHold(doneQueue, true)).toBe(false)
    expect(programsReleasedForLane('x', undefined)).toBeUndefined()
  })
})

describe('lane AND maps', () => {
  it('treats missing lanes as released once the map is ready', () => {
    const map = buildLaneProgramsSessionReleasedMap([
      program({
        id: 'briefing',
        lane_id: 'console-api',
        complete: true,
        signed: 4,
        sign_off_required_count: 4,
        requires_post_completion: true,
      }),
    ])
    expect(map.get('console-api')).toBe(false)
    expect(programsReleasedForLane('console-api', map)).toBe(false)
    expect(programsReleasedForLane('trade-stack', map)).toBe(true)
  })

  it('ANDs catalogComplete and sessionReleased independently on a multi-program lane', () => {
    const programs = [
      program({
        id: 'control-room-ui--build',
        lane_id: 'market-data-expand',
        complete: true,
        signed: 7,
        sign_off_required_count: 7,
        requires_post_completion: true,
        assessment_status: 'no_handoff',
      }),
      program({
        id: 'market-data-expand',
        lane_id: 'market-data-expand',
        complete: true,
        signed: 6,
        sign_off_required_count: 6,
        requires_post_completion: true,
        assessment_status: 'in_operate',
      }),
    ]
    const catalog = buildLaneProgramsCatalogCompleteMap(programs)
    const released = buildLaneProgramsSessionReleasedMap(programs)
    expect(catalog.get('market-data-expand')).toBe(false)
    expect(released.get('market-data-expand')).toBe(true)
    const open = openDeliveryProgramsForLane('market-data-expand', programs)
    expect(open.map(p => p.id)).toEqual([])
  })

  it('lists only not-sessionReleased programs as open', () => {
    const programs = [
      program({
        id: 'hist',
        lane_id: 'console-api',
        complete: true,
        signed: 4,
        sign_off_required_count: 4,
        requires_post_completion: true,
        assessment_status: 'no_handoff',
      }),
      program({
        id: 'live',
        lane_id: 'console-api',
        complete: true,
        signed: 4,
        sign_off_required_count: 4,
        requires_post_completion: true,
      }),
    ]
    expect(openDeliveryProgramsForLane('console-api', programs).map(p => p.id)).toEqual(['live'])
  })
})

describe('computeScopeWorkSummary queueByLane', () => {
  it('breaks progress into per-lane queue items that sum to the meter', () => {
    const summary = computeScopeWorkSummary(
      [
        {
          laneId: 'compose-k3s',
          label: 'Compose → K3s',
          queue: [
            item({ id: 'a', status: 'closed' }),
            item({ id: 'b', status: 'closed' }),
            item({ id: 'c', status: 'closed' }),
          ],
        },
        {
          laneId: 'trade-k8s-native',
          label: 'Trade K8s-native',
          queue: Array.from({ length: 5 }, (_, i) => item({ id: `w${i}`, status: 'done' })),
        },
        {
          laneId: 'stock-readiness-retire',
          label: 'Retire stock readiness',
          queue: [],
        },
      ],
      {
        programsReleasedByLane: new Map([
          ['compose-k3s', true],
          ['trade-k8s-native', true],
          ['stock-readiness-retire', true],
        ]),
      },
    )
    expect(summary.progress).toEqual({ done: 8, total: 8, percent: 100 })
    expect(summary.queueByLane).toEqual([
      {
        laneId: 'compose-k3s',
        label: 'Compose → K3s',
        lifecycle: 'complete',
        done: 3,
        total: 3,
      },
      {
        laneId: 'trade-k8s-native',
        label: 'Trade K8s-native',
        lifecycle: 'complete',
        done: 5,
        total: 5,
      },
    ])
    expect(summary.queueByLane.reduce((n, r) => n + r.total, 0)).toBe(summary.progress?.total)
  })
})

describe('selectBriefingQueueInventory', () => {
  const row = (
    partial: Pick<ScopeQueueLaneBreakdown, 'laneId' | 'lifecycle'> &
      Partial<ScopeQueueLaneBreakdown>,
  ): ScopeQueueLaneBreakdown => ({
    label: partial.label ?? partial.laneId,
    done: partial.done ?? 0,
    total: partial.total ?? 1,
    ...partial,
  })

  it('puts Doing first, then Planned, then Done, and caps at 3', () => {
    const { visible, hiddenCount } = selectBriefingQueueInventory([
      row({ laneId: 'done-a', lifecycle: 'complete', label: 'Z Done' }),
      row({ laneId: 'planned', lifecycle: 'planned', label: 'Plan' }),
      row({ laneId: 'doing', lifecycle: 'active', label: 'Active' }),
      row({ laneId: 'done-b', lifecycle: 'complete', label: 'A Done' }),
      row({ laneId: 'done-c', lifecycle: 'complete', label: 'M Done' }),
    ])
    expect(visible.map(r => r.laneId)).toEqual(['doing', 'planned', 'done-b'])
    expect(hiddenCount).toBe(2)
  })

  it('returns empty when there are no rows', () => {
    expect(selectBriefingQueueInventory([])).toEqual({ visible: [], hiddenCount: 0 })
  })
})
