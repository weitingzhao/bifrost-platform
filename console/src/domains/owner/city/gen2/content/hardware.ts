import QUERIES from './hardwareQueries.json'
import { facilityFor, HARDWARE_TOWN } from './binding'
import { cityRulers, unitRoomsOf, type UnitReads } from './unitRooms'
import { assign, DISTRICT, encodePlan, fitsIn, GRID, MAX_THINGS, planFor, plotsOf, shapeArea, splitPlot, type Plan, type Shape } from '@bifrost/ui/city/grid'
import type { Look, Meter, Place, State } from '@bifrost/ui/city/places'

/**
 * The hardware district (RULES.md §2, the Owner 2026-11-19): the city's utilities, every physical
 * device a building, its resources the rooms. Nothing here is hand-kept (RULES.md 11): the devices
 * are the platform's topology, their states and readings the cluster's and the node exporters'
 * monitoring. City runs in reverse (rule 0): this only shows what the system says.
 */

export type TopoNode = { id: string; label?: string; group?: string; host?: string; in_k3s_cluster?: boolean; compose_roles?: string[]; k3s_roles?: string[] }
export type PromPoint = { labels?: Record<string, string>; value: number }

/**
 * What is read, and how (the PromQL is the reading's source, shown with every number). The queries are
 * configuration, kept as data in hardwareQueries.json, not written into the source (code-health LAW-8).
 */
export const HARDWARE_QUERIES = QUERIES
export type HardwareReads = { topology: TopoNode[] | null; prom: { [K in keyof typeof HARDWARE_QUERIES]: PromPoint[] | null } }

/** Read everything the district needs, through the platform: a failed read is null, and what it fed shows unread. */
export async function readHardware(get: (path: string) => Promise<unknown>): Promise<HardwareReads> {
  const keys = Object.keys(HARDWARE_QUERIES) as (keyof typeof HARDWARE_QUERIES)[]
  const [topo, ...prom] = await Promise.all([
    get('/api/v1/topology?env=prod').then(v => (v as { nodes?: TopoNode[] }).nodes ?? null, () => null),
    ...keys.map(k => get(`/api/v1/telemetry/promql?q=${encodeURIComponent(HARDWARE_QUERIES[k])}`).then(v => (v as { points?: PromPoint[] }).points ?? null, () => null)),
  ])
  return { topology: topo as TopoNode[] | null, prom: Object.fromEntries(keys.map((k, i) => [k, prom[i]])) as HardwareReads['prom'] }
}

/** RULES.md §2.4: the look from what the topology says the device is. */
export function lookOf(n: TopoNode): Look {
  const name = `${n.id} ${n.label ?? ''}`
  if (/router|switch|gateway|firewall/i.test(name)) return 'network'
  if (/nas/i.test(name)) return 'storage'
  if (n.group === 'mac') return 'mac'
  if (/win/i.test(name)) return 'windows'
  return 'linux'
}

const hasGpu = (n: TopoNode) => /gpu|4090/i.test(`${n.id} ${n.label ?? ''}`) || (n.k3s_roles ?? []).some(r => /gpu/.test(r))

/**
 * RULES.md §2.3: a device's size by its weight, large enough that what it shows reads at a glance
 * (the Owner, 2026-11-19: use the ground): the GPU tower and the NAS (a logistics park, tens of
 * terabytes) medium (4×8, two units); every other device small (4×4, one unit): the three classes, 2026-11-20.
 */
export function shapeOf(look: Look, cores: number | null, gpu: boolean): Shape {
  // One unit each (4×4); the GPU tower and the NAS two (4×8): thirteen devices, fifteen units of a sheet's sixteen.
  if (gpu || (cores ?? 0) >= 32) return '4x8'
  if (look === 'storage') return '4x8'
  return '4x4'
}

/**
 * The district's zones (the Owner, 2026-11-19: lay it out by what things are for, not by chance):
 * the west half is the machine room (the GPU tower and the cluster's nodes); in the east, trading
 * (the TWS desks) and storage with the network at the top, operations and the office (the Mac minis,
 * the agent host, the laptop) at the bottom.
 */
export type Zone = 'compute' | 'trading' | 'storage' | 'office'
const ZONES: Record<Zone, string[]> = { compute: ['aa', 'ab'], trading: ['baa'], storage: ['bab'], office: ['bb'] }

export function zoneOf(n: TopoNode, look: Look, gpu: boolean): Zone {
  if (look === 'storage' || look === 'network') return 'storage'
  if (look === 'windows') return 'trading'
  if (look === 'linux' && (gpu || n.in_k3s_cluster)) return 'compute'
  return 'office'
}

/** The ground cut into the zones, then each device given the smallest plot that fits it in its zone (or anywhere, if its zone is full). */
export function zonedArrangement(items: { id: string; shape: Shape; zone: Zone }[]): { plan: string; at: Record<string, string> } {
  let plan: Plan = null
  for (const id of ['', 'a', 'b', 'ba']) plan = splitPlot(plan, id)!
  const taken = new Set<string>(), at: Record<string, string> = {}
  for (const it of [...items].sort((a, b) => shapeArea(b.shape) - shapeArea(a.shape) || a.id.localeCompare(b.id))) {
    const put = (inside: (plot: string) => boolean): boolean => {
      for (;;) {
        const p = plotsOf(plan).filter(q => inside(q.id) && !taken.has(q.id) && fitsIn(it, q)).sort((a, b) => a.w * a.h - b.w * b.h || a.id.localeCompare(b.id))[0]
        if (!p) return false
        const next = splitPlot(plan, p.id)
        if (next && plotsOf(next).some(q => q.id.length > p.id.length && q.id.startsWith(p.id) && fitsIn(it, q))) { plan = next; continue }
        taken.add(p.id); at[it.id] = p.id
        return true
      }
    }
    if (!put(id => ZONES[it.zone].some(z => id.startsWith(z)))) put(() => true)
  }
  for (;;) {
    const big = plotsOf(plan).find(q => !taken.has(q.id) && Math.max(q.w, q.h) > 4)
    if (!big) break
    plan = splitPlot(plan, big.id)
  }
  return { plan: encodePlan(plan), at }
}

const G = 1e9
/** A room's size by how much it holds: cores, or bytes. */
const roomShape = (unit: 'cores' | 'bytes', of: number | null): Shape => {
  // The smallest thing is one unit, 4×4 (the Owner, 2026-11-20: sixteen units to a sheet).
  if (of == null) return '4x4'
  if (unit === 'cores') return of > 16 ? '4x8' : '4x4'
  return of > 4000 * G ? '8x8' : of > 96 * G ? '4x8' : '4x4'
}

const at = (pts: PromPoint[] | null, key: string, value: string) => pts?.find(p => p.labels?.[key] === value)?.value ?? null

/** The district from what was read. A device the topology does not list is not drawn (RULES.md §2.2). */
export function hardwareDistrict(r: HardwareReads, fn = 'system', when = '', units?: UnitReads, maxSheets: Sheets = 1): Place {
  const P = r.prom
  const nodeOfIp = new Map((P.nodeIp ?? []).map(p => [p.labels?.internal_ip ?? '', p.labels?.node ?? '']))
  const zones = new Map<string, Zone>()
  // One measurement for every computer's rooms (unitRooms.ts cityRulers).
  const hostOf = (n: TopoNode) => (n.host && n.host !== '127.0.0.1' ? n.host : null)
  const common = units ? cityRulers((r.topology ?? []).flatMap(n => { const h = hostOf(n); return h ? [{ reads: units, host: h }] : [] })) : undefined
  const devices = (r.topology ?? []).map(n => {
    const ip = n.host && n.host !== '127.0.0.1' ? n.host : null
    const node = ip ? nodeOfIp.get(ip) ?? null : null
    const inst = ip ? `${ip}:9100` : null
    const look = lookOf(n)
    const gpu = hasGpu(n)
    const exporterUp = inst ? at(P.up, 'instance', inst) : null
    const ready = node ? at(P.ready, 'node', node) : null
    // RULES.md §2.6: how each state is decided, from readings only.
    let state: State
    if (node != null && ready === 1) state = 'online'
    else if (node != null && ready === 0 && at(P.cordoned, 'node', node) === 1 && at(P.wakeable, 'node', node) === 1) state = 'standby'
    else if (node != null && ready === 0) state = 'down'
    else if (exporterUp === 1) state = 'online'
    else if (exporterUp === 0) state = 'down'
    else state = 'unread'
    const reading = state === 'online'
    const cap = (res: string) => node ? P.capacity?.find(p => p.labels?.node === node && p.labels?.resource === res)?.value ?? null : null
    const cores = (inst ? at(P.cores, 'instance', inst) : null) ?? cap('cpu')
    const memOf = (inst ? at(P.memTotal, 'instance', inst) : null) ?? cap('memory')
    const meter = (unit: Meter['unit'], used: number | null, of: number | null, source: string): Meter => ({ unit, used: reading ? used : null, of: state === 'unread' ? null : of, source })
    const rooms: Place[] = []
    const room = (id: string, label: string, m: Meter): Place => ({ id, kind: 'room', shape: roomShape(m.unit, m.of), label, question: `${label} of ${n.label ?? n.id}: how much is used.`, dashboard: null, state, meter: m, children: [] })
    if (look !== 'network') {
      rooms.push(room('cpu', 'CPU', meter('cores', inst ? at(P.busy, 'instance', inst) : null, cores, HARDWARE_QUERIES.busy)))
      rooms.push(room('memory', 'Memory', meter('bytes', inst ? at(P.memUsed, 'instance', inst) : null, memOf, HARDWARE_QUERIES.memUsed)))
      const mounts = (P.diskSize ?? []).filter(p => p.labels?.instance === inst).map(p => p.labels?.mountpoint ?? '/')
      const vols = look === 'storage' ? mounts.filter(m => m !== '/').sort() : ['/']
      for (const v of vols.length > 0 ? vols : ['/']) {
        const size = (P.diskSize ?? []).find(p => p.labels?.instance === inst && p.labels?.mountpoint === v)?.value ?? (v === '/' ? cap('ephemeral_storage') : null)
        const used = (P.diskUsed ?? []).find(p => p.labels?.instance === inst && p.labels?.mountpoint === v)?.value ?? null
        const id = v === '/' ? 'disk' : `volume-${v.replace(/\D/g, '')}`
        const r = room(id, v === '/' ? 'Disk' : `Volume ${v.replace(/\D/g, '')}`, meter('bytes', used, size, HARDWARE_QUERIES.diskUsed))
        // A NAS volume's disks: the RAID array whose size is the volume's (within 3%), its disks by state —
        // matched by what was read, never by the numbers in their names.
        if (look === 'storage' && size != null) {
          const arr = (P.arraySize ?? []).find(p => p.labels?.instance === inst && Math.abs(p.value * 1024 - size) / size < 0.03)?.labels?.device
          if (arr) {
            const count = (st: string) => (P.arrayDisks ?? []).find(p => p.labels?.instance === inst && p.labels?.device === arr && p.labels?.state === st)?.value ?? 0
            r.units = [...Array(count('active')).fill('online'), ...Array(count('failed')).fill('down'), ...Array(count('spare')).fill('standby')] as State[]
            r.label = `${r.label} · ${r.units.length} disks`
          }
        }
        rooms.push(r)
      }
      if (gpu) rooms.push(room('gpu', 'GPU', meter('cores', null, null, 'no GPU exporter: not read')))
      // The units' rooms in use (content/unitRooms.ts): each drawn by its module from the machine's own
      // readings. The disk is one warehouse holding every volume (its volumes are inside it).
      if (units && ip) {
        const u = unitRoomsOf(units, ip, common)
        const put = (r: Place | undefined, x: ReturnType<typeof unitRoomsOf>['cpu']) => { if (r && x) { r.shape = x.shape; r.picture = x.picture } }
        put(rooms.find(x => x.id === 'cpu'), u.cpu)
        put(rooms.find(x => x.id === 'memory'), u.memory)
        if (u.disk) {
          const vols = rooms.filter(x => x.id === 'disk' || x.id.startsWith('volume-'))
          const sum = (f: (m: Meter) => number | null) => vols.every(v => v.meter && f(v.meter) != null) ? vols.reduce((a, v) => a + f(v.meter!)!, 0) : null
          const disk = room('disk', 'Disk', meter('bytes', sum(m => m.used), sum(m => m.of), HARDWARE_QUERIES.diskUsed))
          put(disk, u.disk)
          rooms.splice(rooms.indexOf(vols[0]), vols.length, disk)
        }
      }
    }
    zones.set(n.id, zoneOf(n, look, gpu))
    // Its surfaces (all read): the roof its number from its name; the sign what it is for — its workload
    // pool, else its cluster role, else the role the topology gives it; a wall the pods running on it now.
    const digits = (node ?? n.id).match(/(\d+)$/)?.[1]
    const pool = node ? (P.pool ?? []).find(p => p.labels?.node === node)?.labels?.label_bifrost_io_workload_pool : undefined
    const role = node ? (P.role ?? []).find(p => p.labels?.node === node && p.labels?.role === 'control-plane')?.labels?.role : undefined
    const topoRole = (n.k3s_roles ?? []).find(r => !/^k3s_/.test(r))
    const sign = (pool ?? role ?? topoRole ?? '').replace(/[-_]/g, ' ').toUpperCase()
    const pods = node && state === 'online' ? at(P.pods, 'node', node) : null
    // Its number: a server keeps its node's (01–09); every other device gets one below (numberDevices).
    const face: Place['face'] = { roof: node && digits ? digits.padStart(2, '0') : undefined, sign: sign || undefined, ...(node ? { tally: { n: pods, what: 'pods', source: HARDWARE_QUERIES.pods } } : {}) }
    const building: Place = {
      face,
      // The topology names the box, the cluster its node: show both where they differ (mini-pc-a · ubt-k3s-02).
      id: n.id, kind: 'building', facility: facilityFor(look, gpu), shape: shapeOf(look, cores, gpu), label: node && !(n.label ?? n.id).includes(node) ? `${n.label ?? n.id} · ${node}` : n.label ?? n.id, look, state,
      question: `${n.label ?? n.id}${ip ? ` (${ip})` : ''}: is it up, and how full is it?`, dashboard: null, children: rooms,
    }
    return building
  })
  numberDevices(devices)
  // As many sheets as its devices need, up to the land the town has left (sheetDevices); Other only past that.
  const placed = sheetDevices(devices, zones, maxSheets)
  if (placed.other) zones.set(OTHER_DEVICES, 'office')
  const shown = placed.other ? [...placed.kept, placed.other] : placed.kept
  // On one sheet, the zoned plan, when it finds every device a plot; else none, and the engine lays them out itself.
  // On several, the engine lays each sheet out anew (places.ts onSheet), so no plan is given.
  const zoned = placed.sheets === 1 ? zonedArrangement(shown.map(b => ({ id: b.id, shape: b.shape, zone: zones.get(b.id)! }))) : undefined
  const arrangement = zoned && shown.every(b => zoned.at[b.id]) ? zoned : undefined
  // The hardware is the town's power on the map: a nuclear plant (the Owner, 2026-11-20).
  return { id: 'utilities', kind: 'district', shape: SHEET_SHAPE[placed.sheets], style: HARDWARE_TOWN, function: fn, ...(arrangement ? { arrangement } : {}), label: 'Utilities', question: `Is the hardware fine? Every device, and how full it is.${when ? ` ${when}` : ''}`, dashboard: null, children: shown }
}

/** A district's sheets: 1, 2 or 4 (places.ts sheetCount), and the land each takes on the city. */
export type Sheets = 1 | 2 | 4
export const SHEET_SHAPE: Record<Sheets, Shape> = { 1: DISTRICT, 2: '4x8', 4: '8x8' }

/**
 * How many sheets the town can give the hardware: its ground less the land of the other districts, one each
 * (2026-11-24: with 13 other domains the town has 64 cells left, room for a 4×8, two sheets).
 */
export function sheetsTheTownGives(otherDistricts: number): Sheets {
  const free = GRID * GRID - otherDistricts * shapeArea(DISTRICT)
  return free >= shapeArea('8x8') ? 4 : free >= shapeArea('4x8') ? 2 : 1
}

export const OTHER_DEVICES = 'other'

/**
 * The devices on the district's sheets (2026-11-24, PROD: a new machine, win11-secondary, took the devices to 272
 * cells, more than one sheet's 256, and the engine refused the whole City; then, contained, Utilities stood empty
 * and the Owner could not find the servers). A district has 1, 2 or 4 sheets, each a 16×16 ground of at most 16
 * things (RULES.md: a district's sheets); the devices take the fewest sheets that hold them all, computing first,
 * then trading, storage, the office, each on the first sheet with room, up to `maxSheets` (the land the town has).
 * Only what still does not fit stands in one Other site that names it. Nothing is dropped.
 */
export function sheetDevices(devices: Place[], zones: Map<string, Zone>, maxSheets: Sheets = 1): { sheets: Sheets; kept: Place[]; other?: Place } {
  const rank: Record<Zone, number> = { compute: 0, trading: 1, storage: 2, office: 3 }
  const order = [...devices].sort((a, b) => rank[zones.get(a.id) ?? 'office'] - rank[zones.get(b.id) ?? 'office'] || a.id.localeCompare(b.id))
  const counts = ([1, 2, 4] as const).filter(n => n <= maxSheets)
  for (const n of counts) {
    const sheets: Place[][] = Array.from({ length: n }, () => [])
    const rest: Place[] = []
    for (const d of order) { const s = sheets.find(s => holds([...s, d])); if (s) s.push(d); else rest.push(d) }
    if (rest.length && n !== counts[counts.length - 1]) continue
    let other: Place | undefined
    if (rest.length) {
      // Room for the site itself: on the first sheet that takes one more 4×4, else the last device of the last sheet makes way.
      const site = (k: number): Place => ({
        id: OTHER_DEVICES, kind: 'building', shape: '4x4', landmark: 'site', label: `Other · ${k}`, dashboard: null, children: [], question: '',
      })
      while (!sheets.some(s => holds([...s, site(0)]))) rest.unshift([...sheets].reverse().find(s => s.length)!.pop()!)
      other = site(rest.length)
      other.question = `${rest.length} more device${rest.length === 1 ? '' : 's'} do not fit this district's ${n} sheet${n === 1 ? '' : 's'}: ${rest.map(d => d.label).join(', ')}. Give the district more land on the city level to see them.`
      sheets.find(s => holds([...s, other!]))!.push(other)
    }
    const sheetOf = new Map(sheets.flatMap((s, i) => s.map(d => [d.id, i] as const)))
    const kept = devices.filter(d => sheetOf.has(d.id))
    for (const d of [...kept, ...(other ? [other] : [])]) { if (n > 1) d.sheet = sheetOf.get(d.id); else delete d.sheet }
    return { sheets: n, kept, ...(other ? { other } : {}) }
  }
  return { sheets: 1, kept: devices }
}

/** One sheet holds these: at most 16 things, 256 cells, and a cut of its ground that gives each a plot. */
function holds(items: Place[]): boolean {
  if (items.length > MAX_THINGS) return false
  if (items.reduce((a, b) => a + shapeArea(b.shape as Shape), 0) > GRID * GRID) return false
  return assign(items, plotsOf(planFor(items))).homeless.length === 0
}

/**
 * The hardware district's own threshold (RULES.md §2), on top of the frame's: every device has a
 * look and a state; a computer has CPU, Memory and a disk, in that order, before anything else; a
 * GPU room only where there is a GPU; every room says where its reading comes from.
 */
export function hardwareRulesBroken(d: Place): string[] {
  const out: string[] = []
  for (const b of d.children) {
    if (b.id === OTHER_DEVICES) continue // the site for what did not fit: not a device
    const w = `utilities/${b.id}`
    if (!b.look) out.push(`${w}: a device has a look`)
    if (!b.state) out.push(`${w}: a device has a state`)
    if (b.look && b.look !== 'network') {
      const ids = b.children.map(c => c.id)
      if (ids[0] !== 'cpu' || ids[1] !== 'memory' || !(ids[2] === 'disk' || ids[2]?.startsWith('volume-'))) out.push(`${w}: a computer's rooms are CPU, Memory, then its disk or volumes (it has ${ids.join(', ')})`)
    }
    for (const r of b.children) if (!r.meter?.source) out.push(`${w}/${r.id}: a room says where its reading comes from`)
  }
  return out
}

/**
 * Every device carries a two-digit number on itself (the Owner, 2026-11-20: the building's look says what kind
 * it is, its number which one; two digits are enough). A server keeps its node's number (ubt-k3s-04 is 04,
 * 01–09); every other device takes the next free number from 10, in the order of its id, so it keeps the same
 * number from one read to the next. The numbering is the city's own (a house number), not a reading.
 */
export function numberDevices(devices: Place[]) {
  const taken = new Set(devices.map(d => d.face?.roof).filter((v): v is string => !!v && /^\d\d$/.test(v)))
  let next = 10
  for (const d of [...devices].filter(d => !d.face?.roof || !/^\d\d$/.test(d.face.roof)).sort((a, b) => a.id.localeCompare(b.id))) {
    while (taken.has(String(next).padStart(2, '0')) && next < 99) next++
    const n = String(next).padStart(2, '0')
    taken.add(n)
    d.face = { ...d.face, roof: n }
  }
}
