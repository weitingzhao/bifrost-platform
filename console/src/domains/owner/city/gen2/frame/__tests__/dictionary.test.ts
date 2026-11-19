import { describe, expect, it } from 'vitest'
import { DICTIONARY, encode, metaphor, rulerOf, rulersKey, setRuler, STANDARD_PARTS } from '../library/dictionary'
import { deviceSprite, type Form, type Levels } from '../devices'

// 2026-11-19, the Owner: the point is that metrics show naturally; which metaphor shows which metric
// is a standard, kept as data. And size must show capacity — the GPU box and a mini PC looked the
// same size, which shows nothing.
describe('the metaphor dictionary', () => {
  it('one row per metric, each with its part and encoding', () => {
    expect(new Set(DICTIONARY.map(m => m.id)).size).toBe(DICTIONARY.length)
    for (const m of DICTIONARY) expect(m.part && m.meaning).toBeTruthy()
  })
  it('a metric not in the table cannot be drawn', () => {
    expect(() => metaphor('temperature')).toThrow(/no row/)
  })
  it('counts and levels: nothing read is nothing lit, never a guess', () => {
    expect(encode('cpu.busy', null)).toBe(0)
    expect(encode('cpu.busy', 0.5)).toBe(4)
    expect(encode('mem.used', null)).toBeNull()
    expect(encode('mem.used', 1.4)).toBe(1)
  })
  it('sizes grow with what they show: more cores a taller hall, more memory a bigger tank', () => {
    expect(encode('cores', 32)!).toBeGreaterThan(encode('cores', 16)!)
    expect(encode('mem.total', 128e9)!).toBeGreaterThan(encode('mem.total', 24e9)!)
    expect(encode('disk.total', 2e12)!).toBeGreaterThan(encode('disk.total', 500e9)!)
  })
  // 2026-11-19, the Owner: capacity as standard parts, one size city-wide, so a glance compares
  // machines — 16 cores against 32, 16 GB against 24, a node's disk against the NAS.
  it('standard parts: counted in one fixed size each, whatever machine they are on', () => {
    expect(encode('core.machines', 16)).toBe(16)
    expect(encode('mem.sections', 24e9)).toBe(6)
    expect(encode('mem.sections', 16e9)).toBe(4)
    expect(encode('disk.bays', 512e9)).toBe(2)
    expect(encode('mem.sections', null)).toBeNull()
    expect(STANDARD_PARTS().map(m => m.id).sort()).toEqual(['core.machines', 'disk.bays', 'mem.sections'])
  })
  // The Owner, the same day: with a standard, the ruler can be adjusted — the picture changes, the
  // reading does not; that is what keeps the city maintainable and elastic.
  it('a ruler changes the count, never the reading; reset brings the default back', () => {
    setRuler('mem.sections', 2e9)
    expect(encode('mem.sections', 24e9)).toBe(12)
    expect(rulersKey()).toContain('mem.sections=2000000000')
    setRuler('mem.sections', null)
    expect(rulerOf('mem.sections')).toBe(4e9)
    expect(rulersKey()).toBe('')
    expect(() => setRuler('mem.sections', 0)).toThrow()
    expect(() => setRuler('cpu.busy', 2)).toThrow(/not a standard part/)
  })
  it('of two plants, the bigger machine is drawn bigger', () => {
    const lv = (cores: number, mem: number, disk: number): Levels => ({ state: 'online', cpu: 0.3, mem: 0.4, disks: [0.5], cap: { cores, mem, disk } })
    const small = deviceSprite('plant', 4, 4, '#f2d27a', 'day', lv(16, 24e9, 500e9))
    const big = deviceSprite('plant', 4, 4, '#f2d27a', 'day', lv(32, 128e9, 2e12))
    expect(big.svg).not.toBe(small.svg)
    expect(big.height).toBeGreaterThanOrEqual(small.height)
  })
  it('every facility draws from the table without a missing row', () => {
    const forms: Form[] = ['plant', 'station', 'warehouse', 'exchange', 'dispatch', 'office', 'tower']
    const lv: Levels = { state: 'online', cpu: 0.5, mem: 0.5, disks: [0.5], vols: [{ f: 0.5, units: ['online'] }], cap: { cores: 16, mem: 24e9, disk: 500e9 } }
    for (const f of forms) expect(() => deviceSprite(f, 4, 8, '#f2d27a', 'day', lv)).not.toThrow()
  })
})
