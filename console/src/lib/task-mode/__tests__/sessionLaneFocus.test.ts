import { describe, expect, it } from 'vitest'
import type { QueueItem } from '@/lib/briefing/workLanes'
import { resolveSessionLaneFocus } from '@/lib/task-mode/sessionLaneFocus'

function item(partial: Partial<QueueItem> & { id: string; status: QueueItem['status'] }): QueueItem {
  return { label: partial.id, ...partial }
}

describe('resolveSessionLaneFocus', () => {
  it('sends operator to Briefing when there is no Active Session', () => {
    const f = resolveSessionLaneFocus({ queue: [], hasActiveSession: false })
    expect(f.kind).toBe('pick-session')
    expect(f.status).toBe('ready')
    expect(f.line).toMatch(/Open Briefing/)
  })

  it('treats empty queue as Ready → Plan', () => {
    const f = resolveSessionLaneFocus({ queue: [], hasActiveSession: true })
    expect(f.kind).toBe('plan')
    expect(f.status).toBe('ready')
    expect(f.line).toMatch(/Plan/)
  })

  it('starts the first pending item when Planned', () => {
    const f = resolveSessionLaneFocus({
      hasActiveSession: true,
      queue: [
        item({ id: 'a', status: 'pending', label: 'P1 — schema' }),
        item({ id: 'b', status: 'pending', label: 'P2 — ingest' }),
      ],
    })
    expect(f.kind).toBe('start')
    expect(f.status).toBe('planned')
    expect(f.line).toBe('Next: Start → P1 — schema')
    expect(f.focusItem?.id).toBe('a')
  })

  it('focuses Doing head and surfaces next pending', () => {
    const f = resolveSessionLaneFocus({
      hasActiveSession: true,
      queue: [
        item({ id: 'a', status: 'done', label: 'P1' }),
        item({ id: 'b', status: 'in_progress', label: 'P6 — Subcontractors UI' }),
        item({ id: 'c', status: 'pending', label: 'P7 — cleanup' }),
      ],
    })
    expect(f.kind).toBe('doing')
    expect(f.status).toBe('doing')
    expect(f.line).toBe('Focus: P6 — Subcontractors UI')
    expect(f.nextItem?.id).toBe('c')
  })

  it('keeps Doing / sign-off when queue is Done but program not sessionReleased', () => {
    const f = resolveSessionLaneFocus({
      hasActiveSession: true,
      hasProgram: true,
      programsReleased: false,
      queue: [
        item({ id: 'a', status: 'closed', label: 'P0' }),
        item({ id: 'b', status: 'done', label: 'P1' }),
      ],
    })
    expect(f.kind).toBe('signoff')
    expect(f.lifecycle).toBe('active')
    expect(f.status).toBe('doing')
    expect(f.line).toMatch(/In Flight/)
  })

  it('holds all-done queue until sessionReleased is known', () => {
    const f = resolveSessionLaneFocus({
      hasActiveSession: true,
      hasProgram: true,
      queue: [
        item({ id: 'a', status: 'closed', label: 'P0' }),
        item({ id: 'b', status: 'done', label: 'P1' }),
      ],
    })
    expect(f.kind).toBe('plan')
    expect(f.line).toMatch(/Wait for Delivery close/)
    expect(f.kind).not.toBe('signoff')
    expect(f.kind).not.toBe('archive')
  })

  it('archives when queue is Done and program is sessionReleased', () => {
    const f = resolveSessionLaneFocus({
      hasActiveSession: true,
      hasProgram: true,
      programsReleased: true,
      queue: [
        item({ id: 'a', status: 'closed', label: 'P0' }),
        item({ id: 'b', status: 'done', label: 'P1' }),
      ],
    })
    expect(f.kind).toBe('archive')
    expect(f.line).toMatch(/Briefing/)
    expect(f.line).not.toMatch(/Delivery Board →/)
  })
})
