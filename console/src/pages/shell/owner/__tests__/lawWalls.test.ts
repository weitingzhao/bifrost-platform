import { describe, expect, it } from 'vitest'
import { domainRows, wallsLine, type WallsReading } from '../LawWalls'

// 2026-11-26, wall-tag convention (LAW-1 九): the first PROD reading was 108 walls, all orphan; the Laws page shows
// that number as served, and says "not measured" rather than 0 when the map line has no reading.
const zero = { total: 0, anchored: 0, orphan: 0, stale: 0, dup: 0, broken: 0 }
const reading = (over: Partial<WallsReading> = {}): WallsReading => ({ ...zero, measured: true, laws_read: true, ...over })

describe('wallsLine', () => {
  it('says not measured when there is no reading, never 0', () => {
    expect(wallsLine('Walls', undefined)).toBe('Walls: not measured.')
    expect(wallsLine('Walls', reading({ measured: false }))).toBe('Walls: not measured.')
  })
  it('gives the counts as served', () => {
    expect(wallsLine('Walls', reading({ total: 108, orphan: 108 })))
      .toBe('Walls 108 · on a law 0 · orphan 108 · stale 0 · broken 0 · duplicate 0')
  })
  it('says so when the laws in force were not read', () => {
    expect(wallsLine('Walls', reading({ laws_read: false }))).toContain('laws in force not read')
  })
})

describe('domainRows', () => {
  it('puts the domains with most to fix first', () => {
    const w = reading({ by_domain: { a: { ...zero, total: 3, anchored: 3 }, b: { ...zero, total: 5, orphan: 4, stale: 1 }, c: { ...zero, total: 2, orphan: 2 } } })
    expect(domainRows(w).map(([d]) => d)).toEqual(['b', 'c', 'a'])
  })
  it('is empty without a reading', () => {
    expect(domainRows(undefined)).toEqual([])
  })
})
