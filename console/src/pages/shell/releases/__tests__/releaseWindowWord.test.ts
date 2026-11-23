import { describe, expect, it } from 'vitest'
import { releaseWindowWord } from '../releaseView'

// Origin (2026-11-23, 箭10's Releases skin slice): the window's tag said held / free in warning / success; tags are the
// engine's seven words. A held window is a release under way (activity, info), not a fault; free is ok.
describe('releaseWindowWord', () => {
  it('held is info, free ok, anything else unknown', () => {
    expect(releaseWindowWord('held')).toBe('info')
    expect(releaseWindowWord('free')).toBe('ok')
    expect(releaseWindowWord('error')).toBe('unknown')
  })
})
