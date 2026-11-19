import { tenantOf, type CityLane } from './catalog'
import type { Contexts } from './cityReadings'
import type { PromPoint } from './cityUtilities'

/**
 * A tenant's campus (the Owner, 2026-11-18: a satellite like Trade is a
 * complex system; many factories feed it, joined to CI/CD, and the
 * multi-agent system exists to build such payloads). What stands inside is
 * read, never placed: one code factory per repository its lanes build (smoke
 * while a run of it is in flight), the payload bay where they come together,
 * and one module per service its PROD runs. Where the campus stands and how
 * big it is are the Owner's; what is inside follows the system.
 *
 * Which namespaces a tenant runs in comes from the tenant domain's contract;
 * until it has one, `namespacesOf` is not given and the modules are unknown.
 */

export type Factory = { repo: string; name: string; busy: boolean }
export type Module = { name: string; replicas: number }
export type Campus = { name: string; color: string; factories: Factory[]; modules: Module[] | null; lines: string[] }

/** Repositories every line clones (infrastructure, the shared UI): no tenant's own factory. */
const SHARED = new Set(['bifrost-trade-infra', 'bifrost-ui'])

export const DEPLOYMENTS_QUERY = 'sum by (namespace, deployment) (kube_deployment_spec_replicas)'

const short = (repo: string) => repo.replace(/^bifrost-(trade-)?/, '')

export function campuses(lanes: readonly CityLane[], contexts: Contexts, deployments: PromPoint[] | null, namespacesOf?: (tenant: string) => readonly string[] | undefined): Map<string, Campus> {
  const out = new Map<string, Campus>()
  const running = new Set<string>()
  for (const ctx of contexts.values()) for (const r of ctx?.facts.runs_in_flight?.value ?? []) running.add(r.pipeline)
  for (const l of lanes) {
    const t = tenantOf(l)
    if (!t) continue
    const c = out.get(t.name) ?? { name: t.name, color: t.color, factories: [], modules: null, lines: [] }
    for (const repo of l.repos) {
      if (SHARED.has(repo)) continue
      const f = c.factories.find(x => x.repo === repo) ?? (c.factories.push({ repo, name: short(repo), busy: false }), c.factories.at(-1)!)
      if (running.has(l.pipeline)) f.busy = true
    }
    out.set(t.name, c)
  }
  for (const c of out.values()) {
    c.factories.sort((a, b) => a.repo.localeCompare(b.repo))
    const ns = namespacesOf?.(c.name)
    if (ns && deployments) {
      c.modules = deployments.filter(p => ns.includes(p.labels?.namespace ?? '') && p.labels?.deployment)
        .map(p => ({ name: p.labels!.deployment, replicas: p.value })).sort((a, b) => a.name.localeCompare(b.name))
    }
    c.lines = [
      `factories (a repository each): ${c.factories.map(f => `${f.name}${f.busy ? ' (building)' : ''}`).join(', ') || 'none read'}`,
      c.modules ? `modules in PROD: ${c.modules.map(m => `${m.name}${m.replicas !== 1 ? ` ×${m.replicas}` : ''}`).join(', ') || 'none'}` : 'modules: unknown (the tenant domain has no contract naming its namespaces yet)',
    ]
  }
  return out
}

/** The campus's size: big (8) with more than four modules or three factories, else 4. */
export function campusSize(c: Campus | undefined): 4 | 8 {
  return c && ((c.modules?.length ?? 0) > 4 || c.factories.length >= 3) ? 8 : 4
}

/** Where things stand inside a campus of size n, in cells from its corner. */
export function campusPlan(c: Campus, n: 4 | 8): { factories: { f: Factory; x: number; y: number }[]; bay: { x: number; y: number }; modules: { m: Module; x: number; y: number }[]; more: number } {
  const slotsF = n === 8 ? 4 : 2
  const factories = c.factories.slice(0, slotsF).map((f, i) => ({ f, x: i * 2, y: 0 }))
  const bay = n === 8 ? { x: 3, y: 3 } : { x: 0, y: 2 }
  const cells: { x: number; y: number }[] = []
  if (n === 8) { for (let y = 5; y < 8; y++) for (let x = 0; x < 7; x++) cells.push({ x, y }); for (const x of [0, 1, 6]) cells.push({ x, y: 3 }) }
  else for (const [x, y] of [[2, 2], [3, 2], [2, 3], [3, 3]]) cells.push({ x, y })
  const mods = c.modules ?? []
  const modules = mods.slice(0, cells.length).map((m, i) => ({ m, ...cells[i] }))
  return { factories, bay, modules, more: Math.max(0, mods.length - cells.length) + Math.max(0, c.factories.length - slotsF) }
}
