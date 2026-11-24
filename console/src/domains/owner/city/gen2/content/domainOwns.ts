import type { Panel } from '@bifrost/ui/city/content'
import type { Block, Cell } from '@bifrost/ui/city/cards'

/**
 * What a domain owns, read when the city goes into its district (the map line's GET /api/v1/catalog/domain?id=,
 * 2026-11-21: the perceive-first brief, item 4): what runs of it in the cluster, its tables, its Go packages,
 * Console groups and other paths, and its contract as written, counted by kind. A row the table gives to more
 * than one domain says with whom it is shared (law 95 wants one).
 *
 * Handed to the engine as a read card (data only): the engine reads it for the district entered, never for
 * all nine at once, and says "not known" with why when it fails.
 */
export type OwnRow = { path: string; shared?: string[] }
export type DomainHoldings = {
  id: string
  repo?: string
  commit?: string
  packages: OwnRow[]
  groups: OwnRow[]
  paths: OwnRow[]
  workloads: OwnRow[]
  tables?: OwnRow[]
  /** Its Go packages grouped into buildings by the map (first directory under a Go root, f2c674e8), the biggest first. */
  buildings?: { id: string; packages: string[] }[]
  contract?: Record<string, unknown> | null
  note?: string
}
export type ReadDomain = (id: string) => Promise<DomainHoldings>

/** The contract's kinds, in the law's words (2026-11-16: what a domain opens to others, by form). */
const CONTRACT_KINDS: [string, string][] = [
  ['request_response', 'Routes'], ['events', 'Events'], ['event_streams', 'Event streams'],
  ['commands', 'Commands'], ['shared_storage', 'Shared storage'], ['go_surface', 'Go surface'],
]
const countOf = (v: unknown) => Array.isArray(v) ? v.length : v && typeof v === 'object' ? Object.keys(v).length : 0
const LIST_MAX = 12

function rows(rows: OwnRow[] | undefined, strip = ''): [Cell, Cell] {
  if (!rows) return [{ text: '—', tone: 'unknown' }, { text: 'not read', tone: 'unknown' }]
  return [rows.length, {
    lines: rows.slice(0, LIST_MAX).map(r => ({
      text: r.path.startsWith(strip) ? r.path.slice(strip.length) : r.path,
      title: r.shared?.length ? `shared with ${r.shared.join(', ')}` : undefined,
      mark: r.shared?.length ? { text: 'shared' } : undefined,
    })),
    more: Math.max(0, rows.length - LIST_MAX),
  }]
}

export function holdingsBlocks(d: DomainHoldings): Block[] {
  const c = d.contract ?? null
  const row = (label: string, rs: OwnRow[] | undefined, strip?: string) => { const [n, list] = rows(rs, strip); return { label, value: n, note: list } }
  return [
    {
      kind: 'facts',
      rows: [
        row('Runs in the cluster', d.workloads), row('Tables', d.tables), row('Go packages', d.packages, 'api/internal/'),
        row('Console groups', d.groups), row('Other paths', d.paths),
      ],
    },
    c ? {
      kind: 'table', head: [{ label: 'Contract · what it opens to others' }, { label: 'Items', num: true }],
      rows: CONTRACT_KINDS.map(([k, label]) => ({ key: k, cells: [label, k in c ? countOf(c[k]) : { text: 'not written', tone: 'unknown' }] })),
    } : { kind: 'note', text: 'No contract written.' },
    { kind: 'note', title: d.note || undefined, text: `From the map${d.repo ? ` · ${d.repo}` : ''}${d.commit ? ` at ${d.commit.slice(0, 8)}` : ''}${d.note ? ` · ${d.note}` : ''}.` },
  ]
}

/** A domain district's second card: what it owns, read from the map when the district is entered. */
export const ownsPanel = (read: ReadDomain, id: string): Panel => ({
  id: `owns-${id}`, title: 'Owns · from the map', dock: 'right',
  cards: { key: `owns-${id}`, read: () => read(id).then(holdingsBlocks) },
})
