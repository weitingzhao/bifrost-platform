import { describe, expect, it } from 'vitest'
import { rulerOf, rulerToFit } from '@bifrost/ui/city/library/dictionary'
import { cpuRoomFits, cpuRoomShape, machinesOf } from '@bifrost/ui/city/units/cpuRoom'

// Origin (ISSUE-9): a room grows to its largest
// first; when its parts still do not fit, one part stands for more (one machine for 2, 3, 4 cores), and the
// viewer's own ruler is left as it was.
const cpu = (n: number) => ({ cores: Array.from({ length: n }, () => ({ busy: 0.1, freq: null, max: null })), temps: [] })

describe('a room grows first, then its ruler coarsens', () => {
  it('a CPU the largest room holds keeps one machine per core', () => {
    const r = cpu(16)
    expect(rulerToFit('core.machines', () => cpuRoomFits(machinesOf(r)))).toBe(1)
  })
  it('a CPU too big for the largest room gets the smallest whole ruler that fits, and the standard stays', () => {
    const r = cpu(256)
    const each = rulerToFit('core.machines', () => cpuRoomFits(machinesOf(r)))
    expect(each).toBeGreaterThan(1)
    expect(Number.isInteger(each)).toBe(true)
    expect(rulerOf('core.machines')).toBe(1)
    expect(cpuRoomShape(Math.ceil(256 / each))).toEqual([8, 8])
  })
})
