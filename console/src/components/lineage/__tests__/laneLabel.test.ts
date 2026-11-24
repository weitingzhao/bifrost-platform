// 2026-11-23 箭10, the 390 sweep: the commit lineage header was a fixed 112px, so vertical lane names were cut to
// "ready/ci-cc…" and a reader could not tell which line a lane was. The header now grows with the longest name.
import { describe, expect, it } from 'vitest'
import { LABEL_CHAR_PX, laneHeaderHeight, laneLabel } from '@/components/lineage/laneLabel'

describe('lineage lane names', () => {
  it('names a branch lane without its kind prefix and a default lane by its repo', () => {
    expect(laneLabel({ repo: 'bifrost-platform' })).toBe('platform')
    expect(laneLabel({ repo: 'bifrost-platform', branch: 'claude/ready-x' })).toBe('↳ ready-x')
  })
  it('makes the header tall enough for the longest name, never shorter than the floor', () => {
    const long = '↳ ready/ci-codehealth-event-reader'
    expect(laneHeaderHeight([long, 'platform'], 112)).toBeGreaterThanOrEqual(long.length * LABEL_CHAR_PX)
    expect(laneHeaderHeight(['platform'], 112)).toBe(112)
  })
})
