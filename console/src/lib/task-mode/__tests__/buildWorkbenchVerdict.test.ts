import { describe, expect, it } from 'vitest'
import type { QueueItem } from '@/lib/briefing/workLanes'
import {
  BUILD_DEV_LOOP_ELEMENT_ID,
  resolveBuildWorkbenchVerdict,
} from '@/lib/task-mode/buildWorkbenchVerdict'

function item(partial: Partial<QueueItem> & { id: string; status: QueueItem['status'] }): QueueItem {
  return { label: partial.id, ...partial }
}

describe('resolveBuildWorkbenchVerdict', () => {
  it('fails closed without Active Session', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: false,
      packReady: true,
    })
    expect(v.lamp).toBe('fail')
    expect(v.summary).toMatch(/No Active Session/)
    expect(v.nextLine).toMatch(/Copy session/)
    expect(v.cta).toEqual({ kind: 'navigate', tabId: 'briefing', label: 'Open Briefing →' })
  })

  it('asks to create program when session has no Delivery bind', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'market-data-expand',
      programId: null,
      packReady: true,
    })
    expect(v.lamp).toBe('degraded')
    expect(v.summary).toBe('Session · market-data-expand · no Delivery program')
    expect(v.nextLine).toMatch(/Create program/)
    expect(v.cta?.kind).toBe('scroll')
    if (v.cta?.kind === 'scroll') {
      expect(v.cta.elementId).toBe(BUILD_DEV_LOOP_ELEMENT_ID)
    }
  })

  it('uses Session Lane Focus for empty queue (Plan)', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'market-data-expand',
      programId: 'control-room-ui--build',
      packReady: true,
      laneQueue: [],
    })
    expect(v.summary).toBe('Lane market-data-expand · program control-room-ui--build')
    expect(v.nextLine).toBe('Next: Plan — break down work (no tasks yet)')
    expect(v.summary).not.toMatch(/playbook|0\/5/i)
    expect(v.nextLine).not.toMatch(/playbook/i)
    expect(v.cta).toEqual({
      kind: 'scroll',
      elementId: BUILD_DEV_LOOP_ELEMENT_ID,
      label: 'Copy pack →',
    })
  })

  it('starts the first pending queue item', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'trade-stack',
      programId: 'trade-stack--build',
      packReady: true,
      laneQueue: [item({ id: 'a', status: 'pending', label: 'P1 — schema' })],
    })
    expect(v.nextLine).toBe('Next: Start → P1 — schema')
    expect(v.lamp).toBe('degraded')
  })

  it('focuses Doing head from lane queue — not playbook implement', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'trade-stack',
      programId: 'trade-stack--build',
      packReady: true,
      laneQueue: [
        item({ id: 'a', status: 'done', label: 'P1' }),
        item({ id: 'b', status: 'in_progress', label: 'P6 — Subcontractors UI' }),
      ],
    })
    expect(v.nextLine).toBe('Focus: P6 — Subcontractors UI')
    expect(v.lamp).toBe('unknown')
    expect(v.cta?.kind).toBe('scroll')
  })

  it('points at Active Session sign-off when queue is Done but close is pending', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'market-data-expand',
      programId: 'control-room-ui--build',
      packReady: true,
      programsReleased: false,
      programSigned: 8,
      programPhaseCount: 8,
      laneQueue: [
        item({ id: 'a', status: 'closed', label: 'P0' }),
        item({ id: 'b', status: 'done', label: 'P1' }),
      ],
    })
    expect(v.lamp).toBe('degraded')
    expect(v.nextLine).toMatch(/In Flight/)
    expect(v.cta).toEqual({
      kind: 'navigate',
      tabId: 'active-session',
      label: 'In Flight →',
    })
  })

  it('does not flash sign-off or archive while close predicate is loading', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'console-api',
      programId: 'delivery-lifecycle-close',
      packReady: true,
      laneQueue: [
        item({ id: 'a', status: 'closed', label: 'P0' }),
        item({ id: 'b', status: 'done', label: 'P1' }),
      ],
    })
    expect(v.lamp).toBe('unknown')
    expect(v.nextLine).toMatch(/Wait for Delivery close/)
    expect(v.cta).toBeUndefined()
  })

  it('archives to Briefing after no_handoff (6/6 gates + 8 phases)', () => {
    const v = resolveBuildWorkbenchVerdict({
      hasActiveSession: true,
      activeLane: 'trade-stack',
      programId: 'trade-stack--build',
      packReady: true,
      programsReleased: true,
      programSigned: 6,
      programPhaseCount: 8,
      laneQueue: [item({ id: 'a', status: 'done', label: 'P1' })],
    })
    expect(v.lamp).toBe('ok')
    expect(v.nextLine).toMatch(/Briefing/)
    expect(v.cta).toEqual({
      kind: 'navigate',
      tabId: 'briefing',
      label: 'Open Briefing →',
    })
  })
})
