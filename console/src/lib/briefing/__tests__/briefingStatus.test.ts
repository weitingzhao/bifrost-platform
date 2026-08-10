import { describe, expect, it } from 'vitest'
import type { ProgramSummary } from '@/api/programsTypes'
import type { QueueItem } from '@/lib/briefing/workLanes'
import {
  buildLaneProgramsCatalogCompleteMap,
  buildLaneProgramsSessionReleasedMap,
  isLaneLifecycleHold,
  isProgramCatalogComplete,
  isProgramDeliveryClosed,
  isProgramSessionReleased,
  laneLifecycleFromQueue,
  openDeliveryProgramsForLane,
  programsReleasedForLane,
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
