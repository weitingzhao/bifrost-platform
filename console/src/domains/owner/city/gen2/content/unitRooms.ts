import type { Shape } from '@bifrost/ui/city/grid'
import type { Place } from '@bifrost/ui/city/places'
import { cpuRoom, cpuRoomFits, cpuRoomKey, cpuRoomShape, machinesOf } from '@bifrost/ui/city/units/cpuRoom'
import { rulerOf, rulerToFit, withRuler } from '@bifrost/ui/city/library/dictionary'
import { memoryRoom, memoryRoomFits, memoryRoomKey, memoryRoomShape } from '@bifrost/ui/city/units/memoryRoom'
import { diskRoom, diskRoomFits, diskRoomKey, diskRoomShape } from '@bifrost/ui/city/units/diskRoom'
import { UNITS, cpuRoomReadings } from './units/cpu'
import { memoryRoomReadings } from './units/memory'
import { diskRoomReadings } from './units/disk'

/**
 * The units' rooms put into use in the city (ISSUE-9: put them in their rooms now; modular, iterated slowly).
 * Each room is its own module (frame/units/*Room.ts): readings in, a picture out. This
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

/** The parts a ruler can count, with how its scale is said. */
const PARTS = {
  cpu: { id: 'core.machines', each: 1, unit: 'cores', part: 'machine' },
  memory: { id: 'mem.sections', each: 2 ** 30, unit: 'GiB', part: 'freezer' },
  disk: { id: 'disk.bays', each: 1e9, unit: 'GB', part: 'pallet' },
} as const
export type CityRulers = { cpu: number; memory: number; disk: number }

/** The smallest ruler each room of this computer needs to hold its parts in its largest size. */
function neededRulers(reads: UnitReads, host: string): CityRulers {
  const cpu = cpuRoomReadings(reads.cpu as never, host)
  const mem = memoryRoomReadings(reads.memory as never, host)
  const disk = diskRoomReadings(reads.disk as never, host)
  return {
    cpu: cpu.cores.length ? rulerToFit(PARTS.cpu.id, () => cpuRoomFits(machinesOf(cpu))) : rulerOf(PARTS.cpu.id),
    memory: mem.total ? rulerToFit(PARTS.memory.id, () => memoryRoomFits(mem)) : rulerOf(PARTS.memory.id),
    disk: disk.drives.length || disk.volumes.length ? rulerToFit(PARTS.disk.id, () => diskRoomFits(disk)) : rulerOf(PARTS.disk.id),
  }
}

/**
 * One measurement for the whole city (ISSUE-9: the measurement is the same everywhere, like the
 * real world; if one machine stands for more cores, it does in every room): for each part, the coarsest ruler
 * any computer needs, so every room counts alike and two machines' rooms compare at a glance.
 */
export function cityRulers(hosts: { reads: UnitReads; host: string }[]): CityRulers {
  const out: CityRulers = { cpu: rulerOf(PARTS.cpu.id), memory: rulerOf(PARTS.memory.id), disk: rulerOf(PARTS.disk.id) }
  for (const h of hosts) {
    const n = neededRulers(h.reads, h.host)
    out.cpu = Math.max(out.cpu, n.cpu); out.memory = Math.max(out.memory, n.memory); out.disk = Math.max(out.disk, n.disk)
  }
  return out
}

/** A computer's three rooms, drawn by their modules from its own readings; null where its unit read nothing. */
export function unitRoomsOf(reads: UnitReads, host: string, common?: CityRulers): { cpu: UnitRoom | null; memory: UnitRoom | null; disk: UnitRoom | null } {
  const cpu = cpuRoomReadings(reads.cpu as never, host)
  const mem = memoryRoomReadings(reads.memory as never, host)
  const disk = diskRoomReadings(reads.disk as never, host)
  const room = (s: [number, number], key: string, make: () => ReturnType<typeof cpuRoom>, scale?: string): UnitRoom => ({ shape: shapeOf(s), picture: { key: `${host}:${key}`, w: s[0], d: s[1], make, ...(scale ? { scale } : {}) } })
  // The room grows to its largest first; where a room somewhere in the city still cannot hold its parts,
  // one part stands for more in every room alike (cityRulers), and each room's name says so.
  const fitted = (kind: keyof typeof PARTS, build: (scale?: string) => UnitRoom): UnitRoom => {
    const { id, each, unit, part } = PARTS[kind]
    const r = common?.[kind] ?? rulerOf(id)
    if (r === rulerOf(id)) return build()
    const scale = `1 ${part} = ${fmt(r / each)} ${unit}`
    return withRuler(id, r, () => { const u = build(scale); const make = u.picture.make; return { ...u, picture: { ...u.picture, key: `${u.picture.key}:r${r}`, make: () => withRuler(id, r, make) } } })
  }
  return {
    cpu: cpu.cores.length ? fitted('cpu', sc => room(cpuRoomShape(machinesOf(cpu)), cpuRoomKey(cpu), () => cpuRoom(cpu), sc)) : null,
    memory: mem.total ? fitted('memory', sc => room(memoryRoomShape(mem), memoryRoomKey(mem), () => memoryRoom(mem), sc)) : null,
    disk: disk.drives.length || disk.volumes.length ? fitted('disk', sc => room(diskRoomShape(disk), diskRoomKey(disk), () => diskRoom(disk), sc)) : null,
  }
}
