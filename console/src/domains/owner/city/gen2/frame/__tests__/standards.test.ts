import { describe, expect, it } from 'vitest'
import { ROOM_STANDARDS } from '../units/standards'

// 2026-11-19, the Owner: the standards formed in making the CPU room are the standards every room is
// designed against. Each says how a room is held to it and where it came from.
describe('the room standards', () => {
  it('each has a rule, its practice, its origin and how it is checked; ids unique', () => {
    expect(new Set(ROOM_STANDARDS.map(s => s.id)).size).toBe(ROOM_STANDARDS.length)
    for (const s of ROOM_STANDARDS) expect(s.rule && s.how && s.from && ['test', 'eye'].includes(s.check)).toBeTruthy()
  })
  it('every room has a console (the Owner, after the CPU console)', () => {
    expect(ROOM_STANDARDS.map(s => s.id)).toContain('console')
  })
})
