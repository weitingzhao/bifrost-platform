import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchCatalogIndex,
  fetchDomainPage,
  fetchDomainStatus,
  LIGHTS,
  type DomainStatusResponse,
  type DomainStatusRow,
} from '@/domains/foundation/catalog/api/domainStatus'

/*
 * Status → Domains: each domain's four lights from GET /catalog/status (map line, segment 4, 2026-11-20). The page
 * shows what was kept; a light that was not measured says unknown, never good. Worst first; a row opens to the
 * numbers each light was read from.
 */

const RANK: Record<string, number> = { bad: 3, problem: 2, unknown: 1, good: 0 }
const DOT: Record<string, string> = {
  good: 'bg-emerald-500',
  problem: 'bg-amber-500',
  bad: 'bg-red-500',
}

function Dot({ value, label }: { value: string; label: string }) {
  const cls = DOT[value] ?? 'border border-muted-foreground bg-transparent'
  return <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${cls}`} aria-label={`${label}: ${value}`} title={`${label}: ${value}`} data-light={value} />
}

/** What the domain owns, read from GET /catalog/domain once the row is opened. */
function Owns({ id }: { id: string }) {
  const q = useQuery({ queryKey: ['status', 'domain-page', id], queryFn: () => fetchDomainPage(id), retry: false, staleTime: 60_000 })
  if (q.isLoading) return <p className="m-0 pb-2 text-xs text-muted-foreground">Reading what it owns…</p>
  if (q.isError || q.data == null) return <p className="m-0 pb-2 text-xs text-muted-foreground">What it owns could not be read: unknown.</p>
  const p = q.data
  const parts = [
    ['Go packages', p.packages.length],
    ['console groups', p.groups.length],
    ['paths', p.paths.length],
    ['workloads', p.workloads.length],
    ['tables', (p.tables ?? []).length],
  ] as const
  return <p className="m-0 pb-2 text-xs" data-owns={id}>Owns: {parts.map(([k, n]) => `${n} ${k}`).join(' · ')}</p>
}

function Row({ d }: { d: DomainStatusRow }) {
  const [open, setOpen] = useState(false)
  return (
    <li className="list-none border-t border-[var(--table-rule)] first:border-t-0" data-domain={d.id}>
      <details onToggle={e => setOpen((e.currentTarget as HTMLDetailsElement).open)}>
        <summary className="flex min-h-11 cursor-pointer flex-wrap items-center gap-2 py-2 text-sm">
          <Dot value={d.overall} label="Overall" />
          <span className="font-medium">{d.id}</span>
          <span className="text-muted-foreground">{d.overall}</span>
          <span className="ml-auto flex items-center gap-1.5">
            {LIGHTS.map(([k, label]) => <Dot key={k} value={d[k].value} label={label} />)}
          </span>
        </summary>
        <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 pb-2 text-xs">
          {LIGHTS.map(([k, label]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{label} · {d[k].value}</dt>
              <dd className="m-0 break-words">{d[k].why}</dd>
            </div>
          ))}
        </dl>
        {open ? <Owns id={d.id} /> : null}
      </details>
    </li>
  )
}

/** Total = on map + off map for every repository, the platform database and the cluster (perceive first, 2026-11-20). */
function Coverage({ s }: { s: DomainStatusResponse }) {
  const inv = s.inventory ?? []
  const ix = useQuery({ queryKey: ['status', 'catalog-index'], queryFn: fetchCatalogIndex, retry: false, staleTime: 300_000 })
  if (inv.length === 0) return null
  const by = Object.entries(s.off_map_by_domain ?? {}).sort((a, b) => b[1] - a[1])
  const notReconciled = (ix.data?.classes ?? []).filter(c => c.check !== 'scanned')
  return (
    <div className="flex flex-col gap-1 pt-2" aria-label="Map coverage">
      <h3 className="m-0 text-xs font-medium text-foreground">Map coverage</h3>
      <table className="w-full text-xs tabular-nums">
        <thead>
          <tr className="text-left text-muted-foreground"><th className="font-normal">Place</th><th className="text-right font-normal">Total</th><th className="text-right font-normal">On map</th><th className="text-right font-normal">Off map</th><th className="text-right font-normal">Gap</th></tr>
        </thead>
        <tbody>
          {inv.map(c => (
            <tr key={c.repo} data-repo={c.repo} className="border-t border-[var(--table-rule)]">
              <td className="break-all">{c.repo}{c.whole ? ' (tenant: inside not looked at)' : ''}{c.measured ? '' : ' (not measured)'}</td>
              <td className="text-right">{c.total}</td>
              <td className="text-right">{c.on_map}</td>
              <td className="text-right">{c.off_map}</td>
              <td className={`text-right ${c.gap !== 0 ? 'text-red-600' : ''}`} data-gap={c.gap}>{c.gap}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {by.length > 0 ? <p className="m-0 text-xs text-muted-foreground">Off-map entries by claimed domain: {by.map(([d, n]) => `${d} ${n}`).join(' · ')}</p> : null}
      {ix.isError ? <p className="m-0 text-xs text-muted-foreground">Which classes are reconciled could not be read: unknown.</p> : null}
      {notReconciled.length > 0 ? (
        <p className="m-0 text-xs text-muted-foreground" data-not-reconciled={notReconciled.length}>
          Not reconciled with the real system ({notReconciled.length} of {ix.data!.classes.length} classes; the map cannot see what is missing from them): {notReconciled.map(c => `${c.class} (${c.check})`).join(', ')}
        </p>
      ) : null}
    </div>
  )
}

export function DomainStatusSummary() {
  const q = useQuery({
    queryKey: ['status', 'domain-status'],
    queryFn: fetchDomainStatus,
    refetchInterval: 60_000,
    retry: false,
  })
  const head = <h2 className="m-0 text-sm font-medium text-foreground">Domains</h2>
  if (q.isLoading) {
    return <section aria-label="Domain status" className="flex flex-col gap-1">{head}<p className="m-0 text-sm text-muted-foreground">Loading domain status…</p></section>
  }
  if (q.isError || q.data == null) {
    const message = q.error instanceof Error ? q.error.message : 'domain status request failed'
    return <section aria-label="Domain status" className="flex flex-col gap-1">{head}<p role="alert" className="m-0 text-sm text-muted-foreground">{`Domain status could not be read: ${message}. Each domain's state is unknown.`}</p></section>
  }
  const s = q.data
  const rows = [...s.domains].sort((a, b) => (RANK[b.overall] ?? 1) - (RANK[a.overall] ?? 1) || a.id.localeCompare(b.id))
  const counts = ['bad', 'problem', 'unknown', 'good'].map(v => [v, rows.filter(r => r.overall === v).length] as const).filter(([, n]) => n > 0)
  return (
    <section aria-label="Domain status" className="flex flex-col gap-1">
      {head}
      {rows.length === 0 ? (
        <p className="m-0 text-sm text-muted-foreground">{s.note ?? 'No domain status kept yet.'}</p>
      ) : (
        <>
          <p className="m-0 text-sm text-muted-foreground">
            {rows.length} domains · {counts.map(([v, n]) => `${n} ${v}`).join(' · ')}
            {s.at ? ` · computed ${new Date(s.at).toLocaleString()}` : ''}
            {s.commit ? ` · table ${s.commit.slice(0, 8)}` : ''}
          </p>
          <ul className="m-0 p-0">{rows.map(d => <Row key={d.id} d={d} />)}</ul>
          <Coverage s={s} />
        </>
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="min-h-11 cursor-pointer py-3">?</summary>
        <p className="m-0">Four lights per domain, dots in this order: alive (ready replicas of its workloads on the map), interfaces (requests and 5xx to its contract routes in the last hour, or the last day when the hour has under 20; under 20 in a day is too few to judge), boundary (imports against the tiers in or out of it), fresh (the table and the cluster were scanned in the last three rounds). Overall is the worst light. Hollow is unknown: not measured, never counted as good. Map coverage: for every repository, the platform database and the cluster, total (counted from the tree) = on map (a row covers it) + off map (in the pool of what the map cannot perceive); a gap that is not 0 means the scan missed something and turns every fresh light bad. A tenant&apos;s repository is one entry: its inside is not looked at this round.</p>
      </details>
    </section>
  )
}
