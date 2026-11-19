import { describe, expect, it } from 'vitest'
import { arrange, canStand, fitsBudget, GRID, move, pack, SIZES, type Item, type Placed } from '../grid'

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

describe('spread (the default when nobody arranged a place)', () => {
  it('leaves a cell between things and centres the group when the ground allows', () => {
    const a = arrange(items(4, 4, 4, 4, 4))
    expect(noOverlap(a)).toBe(true)
    for (const p of a) for (const q of a) if (p !== q) {
      const apart = p.x + p.size < q.x || q.x + q.size < p.x || p.y + p.size < q.y || q.y + q.size < p.y
      expect(apart, `${p.id} touches ${q.id}`).toBe(true)
    }
    const minX = Math.min(...a.map(p => p.x)), maxX = Math.max(...a.map(p => p.x + p.size))
    expect(Math.abs(minX - (GRID - maxX))).toBeLessThanOrEqual(1)
  })
  it('falls back to packing tight when there is no room for air', () => {
    const full = items(8, 8, 8, 8)
    expect(noOverlap(arrange(full))).toBe(true)
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
    expect(move(base, 'i2', { x: 15, y: 0 })).toBeNull()
    expect(move(base, 'i0', { x: -1, y: 0 })).toBeNull()
  })
  it('positions are kept, and only where they still fit', () => {
    const saved = { i0: { x: 12, y: 12 }, i1: { x: 12, y: 12 }, gone: { x: 0, y: 0 } }
    const a = arrange(items(4, 4, 2), saved)
    expect(a.find(p => p.id === 'i0')).toMatchObject({ x: 12, y: 12 })
    expect(a.find(p => p.id === 'i1')).not.toMatchObject({ x: 12, y: 12 })
    expect(noOverlap(a)).toBe(true)
  })
  it('a newcomer still gets a place when saved positions left the room in pieces', () => {
    // Fifteen 4×4 scattered off the aligned spots leave no 4×4 hole for a sixteenth.
    const set = items(...Array(16).fill(4))
    const saved = Object.fromEntries(set.slice(0, 15).map((it, i) => [it.id, { x: (i % 4) * 4 + (i < 4 ? 1 : 0), y: Math.floor(i / 4) * 4 }]))
    const a = arrange(set, saved)
    expect(a).toHaveLength(16)
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
