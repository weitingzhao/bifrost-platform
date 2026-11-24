// 2026-11-23 箭10, the 390 sweep: the commit lineage header was a fixed 112px, so vertical lane names were cut to
// "ready/ci-cc…" and a reader could not tell which line a lane was. The header grows with the longest name; then
// names lean at 45° (a 337px header took half a phone screen), so the height is the longest name times sin 45°.
import { describe, expect, it } from 'vitest'
import { LABEL_CHAR_PX, laneHeaderHeight, laneLabel } from '@/components/lineage/laneLabel'

describe('lineage lane names', () => {
  it('names a branch lane without its kind prefix and a default lane by its repo', () => {
    expect(laneLabel({ repo: 'bifrost-platform' })).toBe('platform')
    expect(laneLabel({ repo: 'bifrost-platform', branch: 'claude/ready-x' })).toBe('↳ ready-x')
  })
  it('makes the header tall enough for the longest name, never shorter than the floor', () => {
    const long = '↳ ready/ci-codehealth-event-reader'
    const h = laneHeaderHeight([long, 'platform'], 40)
    expect(h).toBeGreaterThanOrEqual(long.length * LABEL_CHAR_PX * Math.SQRT1_2) // the slanted name fits
    expect(h).toBeLessThan(long.length * LABEL_CHAR_PX) // lower than a vertical name would need
    expect(laneHeaderHeight(['ui'], 40)).toBe(40)
  })
})
