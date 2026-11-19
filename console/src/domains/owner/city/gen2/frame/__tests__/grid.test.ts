import { describe, expect, it } from 'vitest'
import { arrange, canStand, fitsBudget, GRID, move, pack, SIZES, snap, streets, type Item, type Placed } from '../grid'

// 2026-11-19, the Owner: every place is a 16×16 ground; things on it are 1, 2, 4 or 8 cells square;
// what does not fit goes a level down. He may drag positions; sizes are the design's.
const items = (...sizes: number[]): Item[] => sizes.map((s, i) => ({ id: `i${i}`, size: s as Item['size'] }))
const noOverlap = (ps: Placed[]) => ps.every(a => canStand(a, ps))

describe('pack', () => {
  it('fills the whole ground exactly when the areas add up to 256', () => {
    const full = items(8, 8, 4, 4, 4, 4, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 4)
    expect(full.reduce((a, i) => a + i.size ** 2, 0)).toBe(256)
    const p = pack(full)
    expect(p).toHaveLength(full.length)
    expect(noOverlap(p)).toBe(true)
  })

  it('packs any random set within the budget (the power-of-two guarantee)', () => {
    let seed = 7
    const rnd = () => (seed = (seed * 1103515245 + 12345) % 2 ** 31) / 2 ** 31
    for (let n = 0; n < 200; n++) {
      const set: Item[] = []
      while (true) {
        const s = SIZES[Math.floor(rnd() * SIZES.length)]
        const next = [...set, { id: `r${set.length}`, size: s }]
        if (!fitsBudget(next)) break
        set.push(next.at(-1)!)
      }
      const p = pack(set)
      expect(noOverlap(p)).toBe(true)
      expect(p.every(q => q.x + q.size <= GRID && q.y + q.size <= GRID)).toBe(true)
    }
  })

  it('refuses a set over the budget', () => {
    expect(fitsBudget(items(8, 8, 8, 8, 1))).toBe(false)
    expect(() => pack(items(8, 8, 8, 8, 1))).toThrow(/no room/)
  })
})

describe('lattices (the Owner, 2026-11-19: each size snaps to its own lines)', () => {
  it('everything packed stands on a multiple of its size', () => {
    expect(pack(items(8, 4, 4, 2, 2, 1, 1)).every(p => p.x % p.size === 0 && p.y % p.size === 0)).toBe(true)
  })
  it('the default gathers things around the centre', () => {
    const a = arrange(items(4, 4, 4, 4))
    expect(a.map(p => `${p.x},${p.y}`).sort()).toEqual(['4,4', '4,8', '8,4', '8,8'])
  })
  it('a drop snaps to the nearest spot of its lattice', () => {
    expect(snap(4, { x: 5.6, y: 1.2 })).toEqual({ x: 4, y: 0 })
    expect(snap(4, { x: 14, y: 14 })).toEqual({ x: 12, y: 12 })
    expect(snap(2, { x: 3.1, y: 6.9 })).toEqual({ x: 4, y: 6 })
  })
  it('a position off its lattice is refused', () => {
    expect(canStand({ id: 'a', size: 4, x: 2, y: 0 }, [])).toBe(false)
    expect(canStand({ id: 'a', size: 4, x: 4, y: 0 }, [])).toBe(true)
  })
})

describe('the Owner moves things', () => {
  const base = arrange(items(4, 4, 2))
  it('to a free cell', () => {
    const after = move(base, 'i2', { x: 14, y: 14 })!
    expect(after.find(p => p.id === 'i2')).toMatchObject({ x: 14, y: 14 })
  })
  it('never onto another thing or off the ground', () => {
    expect(move(base, 'i2', { x: base[0].x, y: base[0].y })).toBeNull()
    // Off the ground snaps back onto it; onto another thing is refused.
    expect(move(base, 'i2', { x: 15, y: 0 })!.find(p => p.id === 'i2')).toMatchObject({ x: 14, y: 0 })
  })
  it('positions are kept, and only where they still fit', () => {
    const saved = { i0: { x: 12, y: 12 }, i1: { x: 12, y: 12 }, i2: { x: 1, y: 1 }, gone: { x: 0, y: 0 } }
    const a = arrange(items(4, 4, 2), saved)
    expect(a.find(p => p.id === 'i0')).toMatchObject({ x: 12, y: 12 })
    expect(a.find(p => p.id === 'i1')).not.toMatchObject({ x: 12, y: 12 })
    // Off its lattice: not kept.
    expect(a.find(p => p.id === 'i2')).not.toMatchObject({ x: 1, y: 1 })
    expect(noOverlap(a)).toBe(true)
  })
  it('a newcomer still gets a place when saved positions left the room in pieces', () => {
    // A 1×1 saved in the middle of each quadrant leaves no 8×8 hole for the four 8×8 that come after.
    const set = [...items(1, 1, 1, 1), ...items(8, 8, 8).map((it, i) => ({ ...it, id: `b${i}` }))]
    const saved = { i0: { x: 3, y: 3 }, i1: { x: 11, y: 3 }, i2: { x: 3, y: 11 }, i3: { x: 11, y: 11 } }
    const a = arrange(set, saved)
    expect(a).toHaveLength(set.length)
    expect(noOverlap(a)).toBe(true)
  })
})

describe('the browser keeps the Owner positions per place', () => {
  const kv = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => { m.set(k, v) } } }
  it('round-trips, forgets on reset, and survives damaged storage', async () => {
    const { browserArrangements } = await import('../arrangementStore')
    const store = kv()
    const s = browserArrangements('k', () => store)
    s.set('system', { status: { x: 4, y: 0 } })
    expect(s.get('system')).toEqual({ status: { x: 4, y: 0 } })
    expect(s.get('work')).toEqual({})
    s.set('system', null)
    expect(s.get('system')).toEqual({})
    store.setItem('k', '{not json')
    expect(s.get('system')).toEqual({})
  })
})

// 2026-11-19, the Owner: roads between plots and green belts, set by the frame, not left to each place.
describe('streets', () => {
  it('run along every 4-line and round the edge when nothing big stands', () => {
    const s = streets([])
    expect(s).toHaveLength(2 * 5 * 4)
    expect(s.filter(x => x.avenue)).toHaveLength(8)
    expect(s.filter(x => x.edge)).toHaveLength(16)
  })
  it('never run through an 8×8', () => {
    const big: Placed = { id: 'b', size: 8, x: 0, y: 0 }
    const s = streets([big])
    const through = s.filter(x => x.at === 4 && x.from < 8)
    expect(through).toHaveLength(0)
    // The avenue and the edge round it stay.
    expect(s.some(x => x.along === 'y' && x.at === 8 && x.from === 0)).toBe(true)
    expect(s.some(x => x.along === 'y' && x.at === 0 && x.from === 0)).toBe(true)
  })
})
