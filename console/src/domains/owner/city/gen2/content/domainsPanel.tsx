import type { Panel } from '@bifrost/ui/city/content'
import { LIGHTS, type DomainLight, type DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'

/**
 * The domains as the map line sees them (GET /api/v1/catalog/status, 2026-11-20): each domain's four
 * lights and its overall light, as the map's workers kept them after their last scan round. City is
 * the map's first reader (perceive first: the reading must be right even when it reads badly); the
 * districts themselves are not changed by it until the Owner says how districts and domains meet.
 *
 * Beside the lights: how many things off the map each domain has (and how many nobody claims), and per
 * repository the equation total = on map + off map, a gap not 0 marked (2026-11-20, the map's 389d1768).
 *
 * A failed read says the lights are not known; it never shows an empty table as if all were well.
 */
/** One repository's (or the cluster's) equation: total = on map + off map, gap 0 when the scan missed nothing. */
type RepoCount = { repo: string; measured: boolean; whole?: boolean; total: number; on_map: number; off_map: number; gap: number }
/** The read as the map serves it since 389d1768: the lights, the off-map pool by domain, the equation per repository. */
export type MapStatus = DomainStatusResponse & { off_map_by_domain?: Record<string, number>; inventory?: RepoCount[] }
export type DomainsRead = { data?: MapStatus; error?: unknown }

const UNCLAIMED = 'unclaimed'
/** The pool's count for a domain; unknown when the map does not serve the pool yet. */
const offMapOf = (d: MapStatus, id: string) => d.off_map_by_domain ? String(d.off_map_by_domain[id] ?? 0) : 'unknown'

function InventoryTable({ rows }: { rows: RepoCount[] }) {
  return <table className="g2-table">
    <thead><tr><th>Repository</th><th className="num">Total</th><th className="num">On map</th><th className="num">Off map</th><th className="num">Gap</th></tr></thead>
    <tbody>
      {rows.map(r => !r.measured || r.whole
        ? <tr key={r.repo}><td>{r.repo}</td><td colSpan={4} className="g2-unknown">{r.whole ? 'one line for the whole repository; inside not seen' : 'not measured'}</td></tr>
        : <tr key={r.repo}>
          <td>{r.repo}</td><td className="num">{r.total}</td><td className="num">{r.on_map}</td><td className="num">{r.off_map}</td>
          <td className={`num${r.gap !== 0 ? ' g2-warn' : ''}`}>{r.gap}</td>
        </tr>)}
    </tbody>
  </table>
}

const toneOf = (value: string) => value === 'bad' ? 'g2-warn' : value === 'good' ? '' : value === 'problem' ? 'g2-warn' : 'g2-unknown'

function Light({ light }: { light: DomainLight | undefined }) {
  const value = light?.value || 'unknown'
  return <td className={toneOf(value)} title={light?.why || undefined}>{value}</td>
}

function DomainsTable({ read }: { read: DomainsRead }) {
  const d = read.data
  if (!d) {
    const why = read.error instanceof Error ? read.error.message : read.error ? String(read.error) : 'reading'
    return <p className="g2-unknown">What the map counts is not known: {why}</p>
  }
  // The lights went onto the tech tree's nodes (2026-11-20, the tree opens from the levels); here, what the map counts off it.
  return <>
    <table className="g2-table">
      <thead><tr><th>Domain</th><th className="num">Off map</th></tr></thead>
      <tbody>
        {d.domains.map(row => <tr key={row.id}><td>{(row as typeof row & { name?: string }).name || row.id}</td><td className="num">{offMapOf(d, row.id)}</td></tr>)}
        {d.off_map_by_domain && <tr><td className="g2-unknown">{UNCLAIMED}</td><td className="num">{offMapOf(d, UNCLAIMED)}</td></tr>}
      </tbody>
    </table>
    {d.inventory && d.inventory.length > 0 && <InventoryTable rows={d.inventory} />}
    <p className="g2-unknown" title={d.note || undefined}>
      From the map's scan{d.at ? ` at ${new Date(d.at).toLocaleString()}` : ''}{d.commit ? ` (${d.commit.slice(0, 8)})` : ''}. Each domain's lights: on its node in the tech tree (the levels' ⑂), and inside its district.
    </p>
  </>
}

/** The city level's panel: what the map counts off it, by domain and by repository; it rides with the town's own panels. */
export const domainsPanel = (read: DomainsRead): Panel => ({
  id: 'domains', title: 'Off the map · by domain', dock: 'right', body: <DomainsTable read={read} />,
})

/** One domain's lights, each with what it was read from, and its count off the map. */
function DomainLights({ read, id }: { read: DomainsRead; id: string }) {
  const d = read.data
  const row = d?.domains.find(r => r.id === id)
  if (!d || !row) return <p className="g2-unknown">This domain's lights are not known.</p>
  return <table className="g2-table">
    <tbody>
      <tr><th>Overall</th><td className={toneOf(row.overall || 'unknown')}>{row.overall || 'unknown'}</td><td /></tr>
      {LIGHTS.map(([k, label]) => <tr key={k}><th>{label}</th><Light light={row[k]} /><td className="g2-unknown">{row[k]?.why}</td></tr>)}
      <tr><th>Off map</th><td className="num">{offMapOf(d, id)}</td><td /></tr>
    </tbody>
  </table>
}

/** A domain district's panel: its own row of the map. */
export const domainPanel = (read: DomainsRead, id: string): Panel => ({
  id: `domain-${id}`, title: 'Lights · from the map', dock: 'right', body: <DomainLights read={read} id={id} />,
})
