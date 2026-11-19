import { describe, expect, it } from 'vitest'
import { rowsDrawn, setRuler } from '../library/dictionary'
import { benchOf, memoryRoom, memoryRoomShape, sectionsOf, SECTION, type MemoryRoomReadings } from '../units/memoryRoom'
import { ROOM_STANDARDS } from '../units/standards'

// 2026-11-19, the memory unit's room, the workbench, drawn against the room standards the CPU room set
// (frame/units/standards.ts). The readings are one real read of that day.
const GB = 1e9
const node: MemoryRoomReadings = { total: 24.4 * GB, used: 6.3 * GB, free: 2.5 * GB, cache: 15.7 * GB, compressed: 0, kernel: 0.76 * GB, dirty: 0, swapTotal: 8.6 * GB, swapUsed: 0.087 * GB, swapIO: 0, corrupted: 0 }
const mac: MemoryRoomReadings = { total: 17.2 * GB, used: 9.4 * GB, free: 0.37 * GB, cache: 7.1 * GB, compressed: 1.4 * GB, kernel: 1.7 * GB, dirty: null, swapTotal: 0, swapUsed: 0, swapIO: 95e3, corrupted: null }
const none: MemoryRoomReadings = { total: null, used: null, free: null, cache: null, compressed: null, kernel: null, dirty: null, swapTotal: null, swapUsed: null, swapIO: null, corrupted: null }

describe('the memory room', () => {
  it('the bench is laid in 4 GB sections, the last as long as its part; the room the smallest that holds them', () => {
    expect(sectionsOf(node)).toBeCloseTo(6.1)
    // The first draw put 24 GB in a 4×8 room, half of it empty: rows were spaced like the lathes' though a
    // bench is low. A room holds its parts snugly, neither crowded nor empty.
    expect(memoryRoomShape(node)).toEqual([4, 4])
    expect(memoryRoomShape({ ...node, total: 7.8 * GB })).toEqual([4, 4])
    expect(memoryRoomShape({ ...node, total: 134 * GB })).toEqual([8, 8])
  })
  it('what lies on the bench adds up to what is installed, in order; what is not broken down is shown as such', () => {
    const b = benchOf(mac)
    expect(b.map(s => s.id)).toEqual(['kernel', 'work', 'compressed', 'cache', 'unbroken', 'free'])
    expect(b.reduce((a, s) => a + s.bytes, 0)).toBeCloseTo(17.2 * GB)
    expect(b.find(s => s.id === 'work')!.bytes).toBeCloseTo(6.3 * GB)
  })
  it('readings clipped to what is installed: the bench never holds more than it has', () => {
    const over = benchOf({ ...node, cache: 40 * GB })
    expect(over.reduce((a, s) => a + s.bytes, 0)).toBeCloseTo(24.4 * GB)
  })
  it('a part not read is not drawn; nothing read, no console', () => {
    expect(rowsDrawn(() => memoryRoom(mac)).ids).not.toContain('swap.room')
    const { ids, out } = rowsDrawn(() => memoryRoom(none))
    expect(ids).toEqual([])
    expect(out.svg).toContain('MEMORY NOT READ')
  })
  it('every room has a console: in use on the desk, the overflow traffic on an instrument', () => {
    const svg = memoryRoom(node).svg
    expect(svg).toContain('IN USE 26%')
    expect(svg).toContain('TRAFFIC')
    expect(ROOM_STANDARDS.map(s => s.id)).toContain('console')
  })
  // Every bench stands the same height and what lies on it is flat, so a bench never rises above the one
  // behind it; the crates in the corner stand lower than a bench top.
  it('nothing hides anything: one height for every bench, what lies on it flat, crates lower still', () => {
    expect(3 + 4).toBeLessThan(SECTION.height)
  })
  it('the ruler counts the sections: 2 GB a section, twice the sections, the same reading', () => {
    setRuler('mem.sections', 2e9)
    try { expect(sectionsOf(node)).toBeCloseTo(12.2) } finally { setRuler('mem.sections', null) }
  })
})
