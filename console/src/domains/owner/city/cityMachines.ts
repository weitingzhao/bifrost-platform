import type { MachineOs, Tier } from './landmarks'
import type { PromPoint } from './cityUtilities'

/**
 * The machines (the Owner, 2026-11-18): every computer that supplies work is
 * a generator set; Mac, Windows and Linux look different; memory and disk
 * show as how tall its bars stand (the Owner, same day: bars beside the
 * building read better than tanks inside it). The cluster's
 * nodes are read from kube-state-metrics (their OS image, their capacity);
 * the machines outside the cluster (the Macs, the Windows TWS hosts, the
 * NAS…) from the platform's topology, off the grid: their capacity is not
 * read, so their bars are hatched.
 */

export type Machine = { id: string; name: string; os: MachineOs; cores: number | null; memory: number | null; disk: number | null; offGrid: boolean; status?: string; roles: string[] }

const G = 2 ** 30
/** Memory tiers in GiB (dictionary v5): under 20, under 48, under 96, more. */
export const MEM_TIERS = [20, 48, 96] as const
/** Disk tiers in GB: under 750, under 1500, under 4000, more. */
export const DISK_TIERS = [750, 1500, 4000] as const

const tier = (v: number | null, edges: readonly number[]): Tier => v == null ? -1 : (edges.findIndex(e => v < e) === -1 ? 3 : edges.findIndex(e => v < e)) as Tier
/** Core tiers: 8 or fewer, 16 or fewer, 32 or fewer, more. */
export const CORE_TIERS = [9, 17, 33] as const
export const coreTier = (cores: number | null) => tier(cores, CORE_TIERS)
export const memTier = (bytes: number | null) => tier(bytes == null ? null : bytes / G, MEM_TIERS)
export const diskTier = (bytes: number | null) => tier(bytes == null ? null : bytes / 1e9, DISK_TIERS)

export function osOf(image: string | undefined): MachineOs {
  const s = (image ?? '').toLowerCase()
  if (!s) return 'unknown'
  if (/darwin|mac/.test(s)) return 'mac'
  if (/windows|win\d/.test(s)) return 'win'
  if (/ubuntu|debian|linux|fedora|centos|rocky|alpine/.test(s)) return 'linux'
  return 'unknown'
}

export type TopoNode = { id: string; label?: string; group?: string; host?: string; in_k3s_cluster?: boolean; status?: string; compose_roles?: string[]; k3s_roles?: string[] }

/** The topology says a machine's group (mac, linux, compute, external); a Windows host is told by its name. */
function topoOs(n: TopoNode): MachineOs {
  if (n.group === 'mac') return 'mac'
  if (n.group === 'linux' || n.group === 'compute') return 'linux'
  if (/win/i.test(`${n.id} ${n.label ?? ''}`)) return 'win'
  return 'unknown'
}

export function machines(info: PromPoint[] | null, capacity: PromPoint[] | null, topology: readonly TopoNode[] | null): Machine[] {
  const cap = (node: string, res: string) => capacity?.find(p => p.labels?.node === node && p.labels?.resource === res)?.value ?? null
  const nodes = [...new Set((capacity ?? []).map(p => p.labels?.node ?? '').filter(Boolean))].sort()
  const out: Machine[] = nodes.map(node => ({
    id: node, name: node.replace(/^ubt-k3s-/, 'node '),
    os: osOf(info?.find(p => p.labels?.node === node)?.labels?.os_image),
    cores: cap(node, 'cpu'), memory: cap(node, 'memory'), disk: cap(node, 'ephemeral_storage'), offGrid: false, roles: [],
  }))
  for (const t of topology ?? []) {
    if (t.in_k3s_cluster) continue
    out.push({ id: t.id, name: t.label && !out.some(m => m.name === t.label) ? t.label : t.id, os: topoOs(t), cores: null, memory: null, disk: null, offGrid: true, status: t.status, roles: [...(t.compose_roles ?? []), ...(t.k3s_roles ?? [])] })
  }
  return out
}

/** The variant a machine is drawn as: its OS. */
export const variantOf = (m: Machine) => m.os
