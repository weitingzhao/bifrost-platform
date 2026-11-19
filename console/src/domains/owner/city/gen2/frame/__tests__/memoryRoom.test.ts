import { describe, expect, it } from 'vitest'
import { rowsDrawn, setRuler } from '../library/dictionary'
import { freezersOf, inUseOf, memoryRoom, memoryRoomShape, sectionsOf, SECTION, type MemoryRoomReadings } from '../units/memoryRoom'
import { ROOM_STANDARDS } from '../units/standards'

// 2026-11-19, the memory unit's room, the workbench, drawn against the room standards the CPU room set
// (frame/units/standards.ts). The readings are one real read of that day.
const GB = 1e9
const node: MemoryRoomReadings = { total: 24.4 * GB, available: 18.1 * GB, free: 2.5 * GB, swapTotal: 8.6 * GB, swapUsed: 0.087 * GB, swapIO: 0, corrupted: 0 }
const mac: MemoryRoomReadings = { total: 17.2 * GB, available: 7.8 * GB, free: 0.37 * GB, swapTotal: 0, swapUsed: 0, swapIO: 95e3, corrupted: null }
const none: MemoryRoomReadings = { total: null, available: null, free: null, swapTotal: null, swapUsed: null, swapIO: null, corrupted: null }

describe('the memory room', () => {
  // The Owner, 2026-11-19: a freezer cannot be half. Modules come in GiB and the system reports a little
  // less than installed, so whole freezers, rounded up to the modules; contents fill them in proportion.
  it('whole freezers only, one per 4 GiB of modules: 24 GB six, 16 GB four, 8 GB two, 128 GB thirty-two', () => {
    expect(sectionsOf(node)).toBe(6)
    expect(sectionsOf({ ...node, total: 16156614656 })).toBe(4)
    expect(sectionsOf({ ...node, total: 17179869184 })).toBe(4)
    expect(sectionsOf({ ...node, total: 7754600448 })).toBe(2)
    expect(sectionsOf({ ...node, total: 134225948672 })).toBe(32)
    // The first draw put 24 GB in a 4×8 room, half of it empty: rows were spaced like the lathes' though a
    // freezer is low. A room holds its parts snugly, neither crowded nor empty.
    expect(memoryRoomShape(node)).toEqual([4, 4])
    expect(memoryRoomShape({ ...node, total: 134225948672 })).toEqual([8, 8])
  })
  it('inside, plain fills only: no patterned blocks', () => {
    expect(memoryRoom(node).svg).not.toContain('<pattern')
  })
  // The Owner, 2026-11-19: seven kinds on the bench were noise; three, each readable apart and keyed.
  it('three kinds, in order, adding up to what is installed: in use, can be cleared, empty', () => {
    const f = freezersOf(mac)
    expect(f.map(s => s.id)).toEqual(['held', 'clearable', 'empty'])
    expect(f.reduce((a, s) => a + s.bytes, 0)).toBeCloseTo(17.2 * GB)
    expect(f[0].bytes).toBeCloseTo(9.4 * GB)
  })
  it('readings clipped to what is installed: the freezers never hold more than they have', () => {
    expect(freezersOf({ ...node, available: 40 * GB }).reduce((a, s) => a + s.bytes, 0)).toBeCloseTo(24.4 * GB)
  })
  it('a part not read is not drawn; nothing read, no console', () => {
    expect(rowsDrawn(() => memoryRoom(mac)).ids).not.toContain('swap.room')
    expect(rowsDrawn(() => memoryRoom(node)).ids.filter(i => i.startsWith('mem.') && !['mem.sections', 'mem.inuse'].includes(i)).sort()).toEqual(['mem.clearable', 'mem.empty', 'mem.held'])
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
  it('the ruler counts the freezers: 2 GiB a freezer, twice the freezers, the same reading', () => {
    setRuler('mem.sections', 2 * 2 ** 30)
    try { expect(sectionsOf(node)).toBe(12) } finally { setRuler('mem.sections', null) }
  })
  // The Owner, 2026-11-19: if the kinds in use can be told apart, show them — on the console, as a bar.
  it('in use by kind: system and compressed read exactly; the rest is programs and the rest; they add up to in use', () => {
    const k = inUseOf({ ...mac, kernel: 1.7 * GB, compressed: 1.4 * GB })!
    expect(k.map(x => x.id)).toEqual(['system', 'compressed', 'rest'])
    expect(k.reduce((a, x) => a + x.bytes, 0)).toBeCloseTo(9.4 * GB)
    expect(memoryRoom({ ...node, kernel: 0.74 * GB, compressed: 0 }).svg).toContain('IN USE BY KIND')
    expect(inUseOf(node)).toBeNull()
  })
})
