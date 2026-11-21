import type { TechTree, TreeTone } from '@bifrost/ui/city/techTree'
import type { CiDomainGraph } from '@/api/ciDomains'
import type { MapStatus } from './domainsPanel'

/**
 * The districts' tech tree (the Owner, 2026-11-20): who stands on whom among the domains, from the real
 * graph. The nodes are the districts (the domains the map serves), each with its overall light; the edges
 * are the imports between domains' Go packages at the platform's main, as GET /api/v1/ci/domains serves
 * them (the same graph the domain score and the map's boundary light count, so the tree cannot call an
 * edge a loop that they do not). The frame lays it out by itself.
 *
 * What it does not see is said under it: it is the platform repository's Go imports only, not the
 * Console's calls, the bus or shared storage; the packages several domains share are not a node.
 * Unread, the nodes stand alone with why; an edge is never guessed.
 */
export const GRAPH_REPO = 'bifrost-platform'

export type GraphRead = { data?: CiDomainGraph; error?: unknown }

/**
 * Each domain's glyph on its node, from the world's dictionary (charter 10, §4–§5): the court where the
 * Owner rules, the rocket that ships, the inspection that checks, the messenger's signal. A domain the map
 * adds and this list does not know shows its initial, never a guessed picture.
 */
const GLYPH: Record<string, string> = {
  foundation: 'layers', plan: 'blueprint', lineage: 'branch', verification: 'shield', delivery: 'rocket',
  owner: 'gavel', compute: 'chip', tenant: 'plug', coordination: 'signal',
}

const TONES: readonly string[] = ['good', 'problem', 'bad']
const toneOf = (v: string | undefined): TreeTone => (v && TONES.includes(v) ? v : 'unknown') as TreeTone
const whyOf = (e: unknown) => e instanceof Error ? e.message : e ? String(e) : 'reading'

export function domainTechTree(status: MapStatus | undefined, graph: GraphRead): TechTree | undefined {
  if (!status || status.domains.length === 0) return undefined
  const nodes = status.domains.map(d => {
    const lights = [['alive', d.alive], ['interfaces', d.interfaces], ['boundary', d.boundary], ['fresh', d.fresh]] as const
    return {
      id: d.id,
      label: d.name || d.id,
      icon: GLYPH[d.id],
      tone: toneOf(d.overall),
      why: [...lights.map(([k, l]) => `${k} ${l?.value || 'unknown'}`), ...(d.tier ? [`declared tier ${d.tier}`] : [])].join(' · '),
    }
  })
  const g = graph.data
  if (!g) return { nodes, note: `Who stands on whom is not known: ${whyOf(graph.error)}` }
  if (!g.measured) return { nodes, note: `Who stands on whom is not measured: ${g.note || 'no ownership table'}` }
  const ids = new Set(nodes.map(n => n.id))
  const edges = g.edges.filter(e => ids.has(e.from) && ids.has(e.to)).map(e => ({ from: e.from, to: e.to, weight: e.imports, wrong: e.wrong_way }))
  const shared = g.shared_packages.length
  return {
    nodes,
    edges,
    note: `Imports between the domains' Go packages in ${g.repo} at ${g.commit.slice(0, 8)}${g.refreshing ? ' (main moved on; reading it)' : ''}. `
      + `Not seen here: the Console's calls, the bus, shared storage, other repositories${shared ? `; ${shared} shared package${shared === 1 ? '' : 's'} not drawn` : ''}.`,
  }
}
