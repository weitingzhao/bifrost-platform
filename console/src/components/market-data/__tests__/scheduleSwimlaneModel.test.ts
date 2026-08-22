import { describe, expect, it } from 'vitest'
import {
  clipBar,
  resolveDrain,
  resolveFires,
  resolveHorizon,
  swimlaneSlots,
  toggleSlotSelection,
} from '@/components/market-data/scheduleSwimlaneModel'
import type { IngestScheduleSlot } from '@/api/marketDataPlugin'

const now = Date.parse('2026-08-22T13:20:00Z')

function slot(partial: Partial<IngestScheduleSlot> & Pick<IngestScheduleSlot, 'slot'>): IngestScheduleSlot {
  return { cron: '0 3 * * *', ...partial }
}

describe('scheduleSwimlaneModel', () => {
  it('uses API horizon when present', () => {
    const h = resolveHorizon(now, {
      start: '2026-08-21T13:20:00Z',
      end: '2026-08-22T19:20:00Z',
    })
    expect(h.startMs).toBe(Date.parse('2026-08-21T13:20:00Z'))
    expect(h.endMs).toBe(Date.parse('2026-08-22T19:20:00Z'))
  })

  it('prefers API fires_in_window', () => {
    const { startMs, endMs } = resolveHorizon(now, {
      start: '2026-08-21T13:20:00Z',
      end: '2026-08-22T19:20:00Z',
    })
    const fires = resolveFires(
      slot({
        slot: 'fundamentals-rotate',
        fires_in_window: ['2026-08-22T03:00:00Z', '2026-08-23T03:00:00Z'],
      }),
      startMs,
      endMs,
    )
    expect(fires).toEqual([Date.parse('2026-08-22T03:00:00Z')])
  })

  it('falls back to cron iterator', () => {
    const startMs = Date.parse('2026-08-21T20:00:00Z')
    const endMs = Date.parse('2026-08-22T00:00:00Z')
    const fires = resolveFires(slot({ slot: 'stock-eod', cron: '30 21 * * *' }), startMs, endMs)
    expect(fires).toEqual([Date.parse('2026-08-21T21:30:00Z')])
  })

  it('uses API drain when present and keeps active bars open to now', () => {
    const d = resolveDrain(
      slot({
        slot: 'financials',
        drain: {
          started_at: '2026-08-22T12:54:00Z',
          ended_at: null,
          active: true,
        },
      }),
      [],
      now,
    )
    expect(d).toEqual({
      startMs: Date.parse('2026-08-22T12:54:00Z'),
      endMs: now,
      active: true,
    })
  })

  it('falls back to last_fire → now when kinds are still queued', () => {
    const d = resolveDrain(
      slot({
        slot: 'fundamentals-rotate',
        last_fire: '2026-08-22T03:00:00Z',
        evidence_kinds: ['financials'],
      }),
      [{ kind: 'financials', pending: 5000, running: 11, active: 5011 }],
      now,
    )
    expect(d?.active).toBe(true)
    expect(d?.startMs).toBe(Date.parse('2026-08-22T03:00:00Z'))
    expect(d?.endMs).toBe(now)
  })

  it('clips bars to the view and hides migrated slots', () => {
    const bar = clipBar(
      Date.parse('2026-08-21T20:00:00Z'),
      Date.parse('2026-08-22T16:00:00Z'),
      Date.parse('2026-08-22T00:00:00Z'),
      Date.parse('2026-08-22T12:00:00Z'),
    )
    expect(bar?.leftPct).toBe(0)
    expect(bar?.widthPct).toBe(100)
    expect(
      swimlaneSlots([
        slot({ slot: 'keep' }),
        slot({ slot: 'gone', adherence: 'migrated' }),
      ]).map(s => s.slot),
    ).toEqual(['keep'])
  })

  it('toggles slot selection', () => {
    expect(toggleSlotSelection(null, 'stock-eod')).toBe('stock-eod')
    expect(toggleSlotSelection('stock-eod', 'stock-eod')).toBeNull()
    expect(toggleSlotSelection('stock-eod', 'calendar')).toBe('calendar')
  })
})
