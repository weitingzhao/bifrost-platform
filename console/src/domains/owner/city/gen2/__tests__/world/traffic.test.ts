import { describe, expect, it } from 'vitest'
import { along, lengthOf, tripAt, VEHICLES, type Trip } from '@bifrost/ui/city/traffic'

// 2026-11-21, the Owner: the communication line first, the first kind of vehicle on the roads (no railway;
// the kind of vehicle says the kind of talk). These pin where a trip is driven at a level's grain (between two
// children; one child's business is not driven here) and the point a vehicle stands at along its road.
const trip = (from: string[], to: string[]): Trip => ({ id: 't', kind: 'event', from, to, what: 'git.main.advanced → headrelease' })

describe('a trip is driven at the level on screen', () => {
  it('on the town, between two districts', () => {
    expect(tripAt(trip(['lineage'], ['delivery']), [])).toEqual({ from: 'lineage', to: 'delivery' })
  })
  it('inside one district it is that district’s business: not driven on the town', () => {
    expect(tripAt(trip(['delivery'], ['delivery']), [])).toBeNull()
    expect(tripAt(trip(['delivery', 'a'], ['delivery', 'b']), [])).toBeNull()
  })
  it('inside the district, between its two buildings; a trip that leaves it is not driven inside (yet)', () => {
    expect(tripAt(trip(['delivery', 'a'], ['delivery', 'b']), ['delivery'])).toEqual({ from: 'a', to: 'b' })
    expect(tripAt(trip(['delivery', 'a'], ['owner', 'b']), ['delivery'])).toBeNull()
  })
  it('every kind has its vehicle', () => {
    expect(VEHICLES.event.vehicle).toBe('bus')
  })
})

describe('a vehicle along its road', () => {
  const road = [{ x: 0, y: 0 }, { x: 4, y: 0 }, { x: 4, y: 2 }]
  it('starts at the start, ends at the end, and is halfway at half the length', () => {
    expect(lengthOf(road)).toBe(6)
    expect(along(road, 0)).toMatchObject({ x: 0, y: 0, dx: 1, dy: 0 })
    expect(along(road, 1)).toMatchObject({ x: 4, y: 2, dx: 0, dy: 1 })
    expect(along(road, 0.5)).toMatchObject({ x: 3, y: 0 })
  })
  it('turns the corner facing the new way', () => {
    expect(along(road, 5 / 6)).toMatchObject({ x: 4, y: 1, dx: 0, dy: 1 })
  })
})
