import type { Shape } from '../frame/grid'
import type { Place } from '../frame/places'
import { cpuRoom, cpuRoomFits, cpuRoomKey, cpuRoomShape, machinesOf } from '../frame/units/cpuRoom'
import { rulerOf, rulerToFit, withRuler } from '../frame/library/dictionary'
import { memoryRoom, memoryRoomFits, memoryRoomKey, memoryRoomShape } from '../frame/units/memoryRoom'
import { diskRoom, diskRoomFits, diskRoomKey, diskRoomShape } from '../frame/units/diskRoom'
import { UNITS, cpuRoomReadings } from './units/cpu'
import { memoryRoomReadings } from './units/memory'
import { diskRoomReadings } from './units/disk'

/**
 * The units' rooms put into use in the city (the Owner, 2026-11-19: "可以把这些东西放到这个房间里面去了…
 * 模块化，慢慢迭代"). Each room is its own module (frame/units/*Room.ts): readings in, a picture out. This
 * file only reads every unit's details through the platform and hands each computer's rooms their pictures;
 * a room's drawing changes in its module alone.
 */

type Point = { labels: Record<string, string>; value: number }
/** Every unit's details as read: unit → detail → points (null where the platform did not answer). */
export type UnitReads = Record<string, Record<string, Point[] | null>>

export async function readUnits(get: (path: string) => Promise<unknown>): Promise<UnitReads> {
  const out: UnitReads = {}
  await Promise.all(UNITS.flatMap(u => {
    out[u.id] = {}
    return u.details.map(d => get(`/api/v1/telemetry/promql?q=${encodeURIComponent(d.query)}`)
      .then(v => { out[u.id][d.id] = (v as { points?: Point[] }).points ?? null }, () => { out[u.id][d.id] = null }))
  }))
  return out
}

/** A module's [w, d] as the frame's shape (short side first). */
const fmt = (v: number) => (Number.isInteger(v) ? String(v) : v.toFixed(1))
const shapeOf = ([w, d]: [number, number]) => (w <= d ? `${w}x${d}` : `${d}x${w}`) as Shape

export type UnitRoom = { shape: Shape; picture: NonNullable<Place['picture']> }

/** A computer's three rooms, drawn by their modules from its own readings; null where its unit read nothing. */
export function unitRoomsOf(reads: UnitReads, host: string): { cpu: UnitRoom | null; memory: UnitRoom | null; disk: UnitRoom | null } {
  const cpu = cpuRoomReadings(reads.cpu as never, host)
  const mem = memoryRoomReadings(reads.memory as never, host)
  const disk = diskRoomReadings(reads.disk as never, host)
  const room = (s: [number, number], key: string, make: () => ReturnType<typeof cpuRoom>, scale?: string): UnitRoom => ({ shape: shapeOf(s), picture: { key: `${host}:${key}`, w: s[0], d: s[1], make, ...(scale ? { scale } : {}) } })
  // The room grows to its largest first; if its parts still do not fit, one part stands for more (the
  // Owner, 2026-11-20: detail needs room, or it blurs), and the room's name says so.
  const fitted = (id: string, fits: () => boolean, each: number, unit: string, part: string, build: (scale?: string) => UnitRoom): UnitRoom => {
    const r = rulerToFit(id, fits)
    if (r === rulerOf(id)) return build()
    const scale = `1 ${part} = ${fmt(r / each)} ${unit}`
    return withRuler(id, r, () => { const u = build(scale); const make = u.picture.make; return { ...u, picture: { ...u.picture, key: `${u.picture.key}:r${r}`, make: () => withRuler(id, r, make) } } })
  }
  return {
    cpu: cpu.cores.length ? fitted('core.machines', () => cpuRoomFits(machinesOf(cpu)), 1, 'cores', 'machine', sc => room(cpuRoomShape(machinesOf(cpu)), cpuRoomKey(cpu), () => cpuRoom(cpu), sc)) : null,
    memory: mem.total ? fitted('mem.sections', () => memoryRoomFits(mem), 2 ** 30, 'GiB', 'freezer', sc => room(memoryRoomShape(mem), memoryRoomKey(mem), () => memoryRoom(mem), sc)) : null,
    disk: disk.drives.length || disk.volumes.length ? fitted('disk.bays', () => diskRoomFits(disk), 1e9, 'GB', 'pallet', sc => room(diskRoomShape(disk), diskRoomKey(disk), () => diskRoom(disk), sc)) : null,
  }
}
