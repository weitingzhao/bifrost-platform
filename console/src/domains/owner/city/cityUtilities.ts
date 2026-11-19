/**
 * The utilities (the Owner, 2026-11-18): buildings are modules, the demand;
 * nodes are generator sets, the supply. Three utilities run from the sets to
 * the buildings: electricity is CPU (short of it a building dims and slows),
 * cooling water is memory (short of it a building trips: an OOM), nitrogen is
 * disk (the tank's level is capacity, the pipe's flow is IO). Every number is
 * read from the cluster's monitoring; one not read is unknown, never zero.
 */

export type PromPoint = { labels?: Record<string, string>; value: number }
export type PromRead = (query: string) => Promise<PromPoint[]>

/** Only pods that hold their reservation now: finished CI pods still carry requests in kube-state-metrics. */
const LIVE = `* on(namespace, pod) group_left() (max by (namespace, pod) (kube_pod_status_phase{phase=~"Running|Pending"}) == 1)`
const NS = 'cicd|bifrost-platform-prod|bifrost-platform-stg|bifrost-prod|bifrost-stg|research|data'

export const QUERIES = {
  capacity: 'sum by (node, resource) (kube_node_status_capacity{resource=~"cpu|memory|ephemeral_storage"})',
  ready: 'max by (node) (kube_node_status_condition{condition="Ready",status="true"})',
  info: 'max by (node, os_image) (kube_node_info)',
  cpu: 'sum by (node) (rate(container_cpu_usage_seconds_total{container!=""}[5m]))',
  memory: 'sum by (node) (container_memory_working_set_bytes{container!=""})',
  diskWrite: 'sum by (node) (rate(container_fs_writes_bytes_total{container!=""}[5m]))',
  requests: `sum by (node, resource) (kube_pod_container_resource_requests{resource=~"cpu|memory"} ${LIVE})`,
  podCpu: `sum by (namespace, pod, node) (rate(container_cpu_usage_seconds_total{container!="",namespace=~"${NS}"}[5m]))`,
  podMemory: `sum by (namespace, pod, node) (container_memory_working_set_bytes{container!="",namespace=~"${NS}"})`,
  podRequests: `sum by (namespace, pod, node, resource) (kube_pod_container_resource_requests{resource=~"cpu|memory",namespace=~"${NS}"} ${LIVE})`,
  podLimits: `sum by (namespace, pod, node, resource) (kube_pod_container_resource_limits{resource=~"cpu|memory",namespace=~"${NS}"} ${LIVE})`,
} as const
export type Reads = { [K in keyof typeof QUERIES]: PromPoint[] | null }

export async function readUtilities(read: PromRead): Promise<Reads> {
  const keys = Object.keys(QUERIES) as (keyof typeof QUERIES)[]
  const got = await Promise.all(keys.map(k => read(QUERIES[k]).then(v => v, () => null)))
  return Object.fromEntries(keys.map((k, i) => [k, got[i]])) as Reads
}

/** One utility's two lines and its limit: what is reserved, what is used, out of how much; null where not read. */
export type Meter = { used: number | null; reserved: number | null; of: number | null }
export type Tone = 'calm' | 'busy' | 'alert' | 'unknown'
export type Supply = { node: string; cores: number | null; ready: boolean | null; power: Meter; water: Meter; gas: Meter; diskWrite: number | null; word: string; tone: Tone; lines: string[] }
export type Demand = { power: Meter; water: Meter; gas: Meter; nodes: string[]; shared?: string; word: string; tone: Tone; lines: string[] }

const by = (pts: PromPoint[] | null, ...keys: string[]) => {
  if (!pts) return null
  const m = new Map<string, number>()
  for (const p of pts) { const k = keys.map(key => p.labels?.[key] ?? '').join('|'); m.set(k, (m.get(k) ?? 0) + p.value) }
  return m
}
const gb = (b: number | null) => b == null ? 'unknown' : `${(b / 2 ** 30).toFixed(b < 10 * 2 ** 30 ? 1 : 0)} GiB`
const cores = (c: number | null) => c == null ? 'unknown' : `${c.toFixed(c < 10 ? 1 : 0)} cores`
const share = (m: Meter, of = m.of) => m.used != null && of ? m.used / of : null

/**
 * A set's load, by fixed thresholds (dictionary v3): the worse of power and
 * water used against capacity; normal under 60 %, tight to 85 %, overloaded
 * above; a node not Ready is a blackout; one not read is unknown.
 */
export const LOAD = { tight: 0.6, overload: 0.85 } as const
function load(ready: boolean | null, worst: number | null): { word: string; tone: Tone } {
  if (ready === false) return { word: 'blackout', tone: 'alert' }
  if (worst == null || ready == null) return { word: 'unknown', tone: 'unknown' }
  return worst >= LOAD.overload ? { word: 'overloaded', tone: 'alert' } : worst >= LOAD.tight ? { word: 'tight', tone: 'busy' } : { word: 'normal', tone: 'calm' }
}

export function supplies(r: Reads): Supply[] {
  const cap = by(r.capacity, 'node', 'resource'), ready = by(r.ready, 'node'), cpu = by(r.cpu, 'node'), mem = by(r.memory, 'node')
  const req = by(r.requests, 'node', 'resource'), disk = by(r.diskWrite, 'node')
  const nodes = [...new Set((r.capacity ?? []).map(p => p.labels?.node ?? '').filter(Boolean))].sort()
  return nodes.map(node => {
    const get = (m: Map<string, number> | null, k: string) => m ? (m.get(k) ?? null) : null
    const power: Meter = { used: get(cpu, node), reserved: get(req, `${node}|cpu`), of: get(cap, `${node}|cpu`) }
    const water: Meter = { used: get(mem, node), reserved: get(req, `${node}|memory`), of: get(cap, `${node}|memory`) }
    const gas: Meter = { used: null, reserved: null, of: get(cap, `${node}|ephemeral_storage`) }
    const up = ready ? (ready.has(node) ? ready.get(node)! >= 1 : null) : null
    const shares = [share(power), share(water)]
    const worst = shares.some(s => s == null) ? null : Math.max(...(shares as number[]))
    const t = load(up, worst)
    const lines = [
      `power (CPU): ${cores(power.used)} used, ${cores(power.reserved)} reserved, of ${cores(power.of)}`,
      `water (memory): ${gb(water.used)} used, ${gb(water.reserved)} reserved, of ${gb(water.of)}`,
      `gas (disk): ${gb(gas.of)} capacity; writing ${disk?.has(node) ? `${(disk.get(node)! / 2 ** 20).toFixed(1)} MiB/s` : 'unknown'}; use and reservation not read`,
    ]
    if (power.reserved != null && power.of && power.used != null && power.reserved / power.of > 0.85 && power.used / power.of < 0.3) lines.push('reserved far above what is used')
    return { node, cores: power.of, ready: up, power, water, gas, diskWrite: disk?.get(node) ?? null, ...t, lines }
  })
}

/**
 * The four kinds of utility conflict (the coordinator, 2026-11-18), read
 * across the Ready sets, fixed thresholds (dictionary v3):
 * - short: the whole city uses ≥ 85 % of a utility
 * - uneven: one set ≥ 85 % while another is under 30 %
 * - inflated: ≥ 85 % of a utility reserved while under 30 % is used
 * - mismatched: on one set, one utility ≥ 85 % while the other is under 30 %
 * A conflict not readable (a meter unknown) is left out, never guessed.
 */
export type ConflictKind = 'short' | 'uneven' | 'inflated' | 'mismatched'
export type Conflict = { kind: ConflictKind; utility?: 'power' | 'water'; nodes: string[]; line: string }
export const CONFLICT = { high: 0.85, low: 0.3 } as const
export const CONFLICT_WORD: Record<ConflictKind, string> = { short: 'not enough in all', uneven: 'unevenly spread', inflated: 'reserved far above use', mismatched: 'utilities do not match' }

export function conflicts(sets: readonly Supply[]): Conflict[] {
  const up = sets.filter(s => s.ready === true)
  const out: Conflict[] = []
  const pct = (v: number) => `${Math.round(v * 100)} %`
  for (const u of ['power', 'water'] as const) {
    const read = up.filter(s => s[u].used != null && s[u].of)
    if (!read.length) continue
    const used = read.reduce((a, s) => a + s[u].used!, 0), of = read.reduce((a, s) => a + s[u].of!, 0)
    if (used / of >= CONFLICT.high) out.push({ kind: 'short', utility: u, nodes: read.map(s => s.node), line: `${u}: the city uses ${pct(used / of)} of what its sets supply` })
    const hot = read.filter(s => s[u].used! / s[u].of! >= CONFLICT.high), cold = read.filter(s => s[u].used! / s[u].of! < CONFLICT.low)
    if (hot.length && cold.length) out.push({ kind: 'uneven', utility: u, nodes: [...hot, ...cold].map(s => s.node), line: `${u}: ${hot.map(s => s.node).join(', ')} at or over ${pct(CONFLICT.high)} while ${cold.map(s => s.node).join(', ')} under ${pct(CONFLICT.low)}` })
    const res = read.filter(s => s[u].reserved != null)
    if (res.length) {
      const reserved = res.reduce((a, s) => a + s[u].reserved!, 0), rof = res.reduce((a, s) => a + s[u].of!, 0), rused = res.reduce((a, s) => a + s[u].used!, 0)
      if (reserved / rof >= CONFLICT.high && rused / rof < CONFLICT.low) out.push({ kind: 'inflated', utility: u, nodes: res.map(s => s.node), line: `${u}: ${pct(reserved / rof)} reserved, ${pct(rused / rof)} used` })
    }
  }
  for (const s of up) {
    const p = share(s.power), w = share(s.water)
    if (p == null || w == null) continue
    if ((p >= CONFLICT.high && w < CONFLICT.low) || (w >= CONFLICT.high && p < CONFLICT.low)) out.push({ kind: 'mismatched', nodes: [s.node], line: `${s.node}: power ${pct(p)}, water ${pct(w)}` })
  }
  return out
}

/** Which pods a building draws through: by namespace, or by the names of the runs it stands for. */
export type Draw = { namespaces?: readonly string[]; runs?: readonly string[]; shared?: string }

export function demand(r: Reads, draw: Draw): Demand {
  const match = (ns: string, pod: string) => (draw.namespaces?.includes(ns) ?? false) || (ns === 'cicd' && (draw.runs ?? []).some(run => pod.startsWith(`${run}-`)))
  const sum = (pts: PromPoint[] | null, res?: string) => {
    if (!pts) return null
    let s = 0
    for (const p of pts) if (match(p.labels?.namespace ?? '', p.labels?.pod ?? '') && (!res || p.labels?.resource === res)) s += p.value
    return s
  }
  const nodes = new Set<string>()
  for (const p of r.podCpu ?? []) if (match(p.labels?.namespace ?? '', p.labels?.pod ?? '') && p.labels?.node) nodes.add(p.labels.node)
  // A limit below the reservation means some pods set none: then there is no limit to read against.
  const limit = (lim: number | null, res: number | null) => lim != null && res != null && lim > 0 && lim >= res ? lim : null
  const power: Meter = { used: sum(r.podCpu), reserved: sum(r.podRequests, 'cpu'), of: null }
  power.of = limit(sum(r.podLimits, 'cpu'), power.reserved)
  const water: Meter = { used: sum(r.podMemory), reserved: sum(r.podRequests, 'memory'), of: null }
  water.of = limit(sum(r.podLimits, 'memory'), water.reserved)
  const gas: Meter = { used: null, reserved: null, of: null }
  const lines = [
    `power (CPU): ${cores(power.used)} used, ${cores(power.reserved)} reserved, limit ${power.of ? cores(power.of) : 'none set'}`,
    `water (memory): ${gb(water.used)} used, ${gb(water.reserved)} reserved, limit ${water.of ? gb(water.of) : 'none set'}`,
    'gas (disk): no pod declares a disk need: unknown',
    nodes.size ? `drawing from ${[...nodes].sort().join(', ')}` : 'no running pod',
    ...(draw.shared ? [`a shared meter: ${draw.shared}`] : []),
  ]
  const near = (m: Meter) => m.used != null && m.of ? m.used / m.of : null
  const worst = Math.max(near(power) ?? 0, near(water) ?? 0)
  const read = power.used != null && water.used != null
  const t: { word: string; tone: Tone } = !read ? { word: 'unknown', tone: 'unknown' } : worst >= LOAD.overload ? { word: 'near its limit', tone: 'alert' } : nodes.size ? { word: 'drawing', tone: 'calm' } : { word: 'idle', tone: 'calm' }
  return { power, water, gas, nodes: [...nodes].sort(), shared: draw.shared, ...t, lines }
}
