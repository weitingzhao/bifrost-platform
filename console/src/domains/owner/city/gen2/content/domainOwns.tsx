import { useEffect, useState } from 'react'

/**
 * What a domain owns, read when the city goes into its district (the map line's GET /api/v1/catalog/domain?id=,
 * 2026-11-21: the perceive-first brief, item 4, the domain read): what runs of it in the cluster, its tables,
 * its Go packages, Console groups and other paths, and its contract as written, counted by kind. A row the
 * table gives to more than one domain says with whom it is shared (law 50 wants one).
 *
 * Read only for the district entered, never for all nine at once; a failed read says the holdings are not
 * known and why, never an empty list as if it owned nothing.
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

function Rows({ title, rows, strip = '' }: { title: string; rows: OwnRow[] | undefined; strip?: string }) {
  if (!rows) return <tr><th>{title}</th><td className="g2-unknown" colSpan={2}>not read</td></tr>
  const shown = rows.slice(0, LIST_MAX)
  return <tr>
    <th>{title}</th>
    <td className="num">{rows.length}</td>
    <td>{rows.length === 0 ? <span className="g2-unknown">none</span> : <>
      {shown.map(r => <div key={r.path} title={r.shared?.length ? `shared with ${r.shared.join(', ')}` : undefined}>
        {r.path.startsWith(strip) ? r.path.slice(strip.length) : r.path}{r.shared?.length ? <span className="g2-warn"> · shared</span> : null}
      </div>)}
      {rows.length > shown.length && <div className="g2-unknown">+{rows.length - shown.length} more</div>}
    </>}</td>
  </tr>
}

export function DomainOwns({ id, read }: { id: string; read: ReadDomain }) {
  const [got, setGot] = useState<{ data?: DomainHoldings; error?: string } | null>(null)
  useEffect(() => {
    let on = true
    setGot(null)
    read(id).then(data => { if (on) setGot({ data }) }, e => { if (on) setGot({ error: e instanceof Error ? e.message : String(e) }) })
    return () => { on = false }
  }, [id, read])
  if (!got) return <p className="g2-unknown">Reading what it owns…</p>
  if (got.error || !got.data) return <p className="g2-unknown">What it owns is not known: {got.error ?? 'no answer'}</p>
  const d = got.data
  const c = d.contract ?? null
  return <>
    <table className="g2-table">
      <tbody>
        <Rows title="Runs in the cluster" rows={d.workloads} />
        <Rows title="Tables" rows={d.tables} />
        <Rows title="Go packages" rows={d.packages} strip="api/internal/" />
        <Rows title="Console groups" rows={d.groups} />
        <Rows title="Other paths" rows={d.paths} />
      </tbody>
    </table>
    <table className="g2-table">
      <thead><tr><th>Contract · what it opens to others</th><th className="num">Items</th></tr></thead>
      <tbody>
        {c ? CONTRACT_KINDS.map(([k, label]) => <tr key={k}><td>{label}</td><td className="num">{k in c ? countOf(c[k]) : <span className="g2-unknown">not written</span>}</td></tr>)
          : <tr><td className="g2-unknown" colSpan={2}>No contract written</td></tr>}
      </tbody>
    </table>
    <p className="g2-unknown" title={d.note || undefined}>From the map{d.repo ? ` · ${d.repo}` : ''}{d.commit ? ` at ${d.commit.slice(0, 8)}` : ''}{d.note ? ` · ${d.note}` : ''}.</p>
  </>
}
