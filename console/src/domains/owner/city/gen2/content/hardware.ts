import { cityRulers, unitRoomsOf, type UnitReads } from './unitRooms'
import { DISTRICT, encodePlan, fitsIn, plotsOf, shapeArea, splitPlot, type Plan, type Shape } from '../frame/grid'
import type { Look, Meter, Place, State } from '../frame/places'

/**
 * The hardware district (RULES.md §2, the Owner 2026-11-19): the city's utilities, every physical
 * device a building, its resources the rooms. Nothing here is hand-kept (RULES.md 11): the devices
 * are the platform's topology, their states and readings the cluster's and the node exporters'
 * monitoring. City runs in reverse (rule 0): this only shows what the system says.
 */

export type TopoNode = { id: string; label?: string; group?: string; host?: string; in_k3s_cluster?: boolean; compose_roles?: string[]; k3s_roles?: string[] }
export type PromPoint = { labels?: Record<string, string>; value: number }

/** What is read, and how (the PromQL is the reading's source, shown with every number). */
export const HARDWARE_QUERIES = {
  nodeIp: 'max by (node, internal_ip) (kube_node_info)',
  ready: 'max by (node) (kube_node_status_condition{condition="Ready",status="true"})',
  cordoned: 'max by (node) (kube_node_spec_unschedulable)',
  wakeable: 'max by (node) (kube_node_labels{label_bifrost_io_wol="enabled"})',
  capacity: 'sum by (node, resource) (kube_node_status_capacity{resource=~"cpu|memory|ephemeral_storage"})',
  up: 'max by (instance) (up{job="node-exporter"})',
  cores: 'count by (instance) (node_cpu_seconds_total{mode="idle"})',
  busy: 'sum by (instance) (rate(node_cpu_seconds_total{mode!="idle"}[5m]))',
  memTotal: 'max by (instance) (node_memory_MemTotal_bytes or node_memory_total_bytes)',
  memUsed: 'max by (instance) ((node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) or (node_memory_active_bytes + node_memory_wired_bytes + node_memory_compressed_bytes))',
  diskSize: 'max by (instance, mountpoint) (node_filesystem_size_bytes{mountpoint=~"/|/volume[0-9]+",fstype!~"tmpfs|overlay"})',
  diskUsed: 'max by (instance, mountpoint) (node_filesystem_size_bytes{mountpoint=~"/|/volume[0-9]+",fstype!~"tmpfs|overlay"} - node_filesystem_avail_bytes{mountpoint=~"/|/volume[0-9]+",fstype!~"tmpfs|overlay"})',
  arraySize: 'max by (instance, device) (node_md_blocks)',
  pool: 'max by (node, label_bifrost_io_workload_pool) (kube_node_labels)',
  role: 'max by (node, role) (kube_node_role)',
  pods: 'count by (node) (kube_pod_info{node!=""} * on (namespace, pod) group_left () (max by (namespace, pod) (kube_pod_status_phase{phase="Running"}) == 1))',
  arrayDisks: 'max by (instance, device, state) (node_md_disks)',
} as const
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
export function hardwareDistrict(r: HardwareReads, fn = 'system', when = '', units?: UnitReads): Place {
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
    const face: Place['face'] = { roof: digits ? digits.padStart(2, '0') : gpu ? 'GPU' : undefined, sign: sign || undefined, ...(node ? { tally: { n: pods, what: 'pods', source: HARDWARE_QUERIES.pods } } : {}) }
    const building: Place = {
      face,
      // The topology names the box, the cluster its node: show both where they differ (mini-pc-a · ubt-k3s-02).
      id: n.id, kind: 'building', shape: shapeOf(look, cores, gpu), label: node && !(n.label ?? n.id).includes(node) ? `${n.label ?? n.id} · ${node}` : n.label ?? n.id, look, state,
      question: `${n.label ?? n.id}${ip ? ` (${ip})` : ''}: is it up, and how full is it?`, dashboard: null, children: rooms,
    }
    return building
  })
  const arrangement = zonedArrangement(devices.map(b => ({ id: b.id, shape: b.shape, zone: zones.get(b.id)! })))
  // The hardware is the town's power on the map: a fusion works (the Owner, 2026-11-20: sci-fi, not smoking chimneys).
  return { id: 'utilities', kind: 'district', shape: DISTRICT, style: 'power', function: fn, arrangement, label: 'Utilities', question: `Is the hardware fine? Every device, and how full it is.${when ? ` ${when}` : ''}`, dashboard: null, children: devices }
}

/**
 * The hardware district's own threshold (RULES.md §2), on top of the frame's: every device has a
 * look and a state; a computer has CPU, Memory and a disk, in that order, before anything else; a
 * GPU room only where there is a GPU; every room says where its reading comes from.
 */
export function hardwareRulesBroken(d: Place): string[] {
  const out: string[] = []
  for (const b of d.children) {
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
