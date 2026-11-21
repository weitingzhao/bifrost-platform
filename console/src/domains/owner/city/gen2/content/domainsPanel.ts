import type { Panel } from '@bifrost/ui/city/content'
import type { Block, Tone } from '@bifrost/ui/city/cards'
import { LIGHTS, type DomainLight, type DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'

/**
 * The domains as the map line sees them (GET /api/v1/catalog/status, 2026-11-20), handed to the engine as
 * cards (data only; the engine draws them, 2026-11-21). The town's card: what each domain has off the map
 * (and how many nobody claims), per repository the equation total = on map + off map (a gap not 0 marked),
 * and who uses whom across domains as the map serves it. A district's card: its own lights, each with what
 * it was read from. The lights of all nine are on the tech tree's nodes.
 *
 * A failed read says not known, never an empty table as if all were well.
 */
/** One repository's (or the cluster's) equation: total = on map + off map, gap 0 when the scan missed nothing. */
type RepoCount = { repo: string; measured: boolean; whole?: boolean; total: number; on_map: number; off_map: number; gap: number }
/**
 * One cross-domain reference as the map serves it (2026-11-21, the map line leads it): pairs (user's package, used
 * package) from the platform's domain graph; registered when the used domain's go_surface names the package and
 * lists the user, else unregistered (reaching past the contract). Counted by package, not by name: the map says so.
 */
export type MapReference = { from: string; to: string; imports: number; registered: number; unregistered: number; wrong_way: boolean }
/** A domain row as the map serves it; name and tier come from the ownership table (empty / 0 when not written). */
export type MapDomain = DomainStatusResponse['domains'][number] & { name?: string; tier?: number }
export type MapStatus = Omit<DomainStatusResponse, 'domains'> & { domains: MapDomain[]; off_map_by_domain?: Record<string, number>; inventory?: RepoCount[]; references?: MapReference[] }
export type DomainsRead = { data?: MapStatus; error?: unknown }

const UNCLAIMED = 'unclaimed'
const whyOf = (e: unknown) => e instanceof Error ? e.message : e ? String(e) : 'reading'
/** The pool's count for a domain; unknown when the map does not serve the pool yet. */
const offMapOf = (d: MapStatus, id: string) => d.off_map_by_domain ? String(d.off_map_by_domain[id] ?? 0) : { text: 'unknown', tone: 'unknown' as Tone }
const nameOf = (d: MapStatus, id: string) => d.domains.find(r => r.id === id)?.name || id
export const toneOf = (value: string): Tone => value === 'good' ? 'good' : value === 'bad' || value === 'problem' ? 'warn' : 'unknown'

function inventory(rows: RepoCount[]): Block {
  return {
    kind: 'table',
    head: [{ label: 'Repository' }, { label: 'Total', num: true }, { label: 'On map', num: true }, { label: 'Off map', num: true }, { label: 'Gap', num: true }],
    rows: rows.map(r => !r.measured || r.whole
      ? { key: r.repo, cells: [r.repo, { text: r.whole ? 'one line for the whole repository; inside not seen' : 'not measured', tone: 'unknown' }, '', '', ''] }
      : { key: r.repo, cells: [r.repo, r.total, r.on_map, r.off_map, { text: r.gap, tone: r.gap !== 0 ? 'warn' : undefined }] }),
  }
}

/** Who uses whom across domains, worst first: a loop, then most reaching past the contract. */
function references(d: MapStatus): Block {
  if (!d.references) return { kind: 'note', text: 'Who uses whom across domains: not served by the map yet.' }
  const rows = [...d.references].sort((a, b) => Number(b.wrong_way) - Number(a.wrong_way) || b.unregistered - a.unregistered || b.imports - a.imports)
  return {
    kind: 'table',
    head: [{ label: 'Uses → used' }, { label: 'Imports', num: true }, { label: 'Past the contract', num: true }],
    empty: 'No reference between domains',
    rows: rows.map(r => ({
      key: `${r.from}>${r.to}`,
      cells: [
        { text: `${nameOf(d, r.from)} → ${nameOf(d, r.to)}`, tone: r.wrong_way ? 'warn' : undefined, title: r.wrong_way ? 'a loop: the two use each other' : undefined, mark: r.wrong_way ? { text: 'loop' } : undefined },
        r.imports,
        { text: r.unregistered, tone: r.unregistered > 0 ? 'warn' : undefined, title: `${r.registered} through its contract, ${r.unregistered} not (by package)` },
      ],
    })),
  }
}

export function townBlocks(read: DomainsRead): Block[] {
  const d = read.data
  if (!d) return [{ kind: 'note', text: `What the map counts is not known: ${whyOf(read.error)}` }]
  return [
    {
      kind: 'table',
      head: [{ label: 'Domain' }, { label: 'Off map', num: true }],
      rows: [
        ...d.domains.map(row => ({ key: row.id, cells: [nameOf(d, row.id), offMapOf(d, row.id)] })),
        ...(d.off_map_by_domain ? [{ key: UNCLAIMED, cells: [{ text: UNCLAIMED, tone: 'unknown' as Tone }, offMapOf(d, UNCLAIMED)] }] : []),
      ],
    },
    ...(d.inventory && d.inventory.length > 0 ? [inventory(d.inventory)] : []),
    references(d),
    {
      kind: 'note', title: d.note || undefined,
      text: `From the map's scan${d.at ? ` at ${new Date(d.at).toLocaleString()}` : ''}${d.commit ? ` (${d.commit.slice(0, 8)})` : ''}. Each domain's lights: on its node in the tech tree (the levels' ⑂), and inside its district.`,
    },
  ]
}

/** The city level's card: what the map counts off it, by domain and by repository, and who uses whom across domains. */
export const domainsPanel = (read: DomainsRead): Panel => ({ id: 'domains', title: 'Off the map · by domain', dock: 'right', cards: townBlocks(read) })

export function lightBlocks(read: DomainsRead, id: string): Block[] {
  const d = read.data
  const row = d?.domains.find(r => r.id === id)
  if (!d || !row) return [{ kind: 'note', text: "This domain's lights are not known." }]
  const light = (l: DomainLight | undefined) => ({ text: l?.value || 'unknown', tone: toneOf(l?.value || 'unknown'), title: l?.why || undefined })
  return [{
    kind: 'facts',
    rows: [
      { label: 'Overall', value: { text: row.overall || 'unknown', tone: toneOf(row.overall || 'unknown') }, note: '' },
      ...LIGHTS.map(([k, label]) => ({ label, value: light(row[k]), note: row[k]?.why ?? '' })),
      { label: 'Off map', value: offMapOf(d, id), note: '' },
      { label: 'Tier', value: row.tier ? `L${row.tier}` : { text: 'not written', tone: 'unknown' as Tone }, note: 'as the ownership table declares it' },
    ],
  }]
}

/** A domain district's card: its own row of the map. */
export const domainPanel = (read: DomainsRead, id: string): Panel => ({ id: `domain-${id}`, title: 'Lights · from the map', dock: 'right', cards: lightBlocks(read, id) })
