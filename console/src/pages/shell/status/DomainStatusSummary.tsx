import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { readQuality } from '@bifrost/ui/city/observation'
import { useDomainStatusObservation } from '@/domains/foundation/observation-access'
import { useNowMs } from '@/hooks/useNowMs'
import {
  fetchCatalogIndex,
  fetchDomainPage,
  fetchPackageUsers,
  fetchPool,
  LIGHTS,
  type DomainStatusResponse,
  type DomainReference,
  type DomainStatusRow,
  type RepoCount,
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

/** One place's equation; opened, its off-map entries and, for the cluster and the database, what a whole namespace row covers. */
function PlaceRow({ c }: { c: RepoCount }) {
  const [open, setOpen] = useState(false)
  const canOpen = c.entries > 0 || (c.stale_rows ?? 0) > 0 || (c.root_group_only ?? 0) > 0 || c.repo === 'cluster' || c.repo === 'platform-db'
  return (
    <>
      <tr data-repo={c.repo} className="border-t border-[var(--table-rule)]">
        <td className="break-all">
          {canOpen ? (
            <button type="button" className="min-h-11 cursor-pointer text-left underline decoration-dotted" aria-expanded={open} onClick={() => setOpen(!open)}>{c.repo}</button>
          ) : c.repo}
          {c.whole ? ' (tenant: inside not looked at)' : ''}{c.measured ? '' : ' (not measured)'}
        </td>
        <td className="text-right">{c.total}</td>
        <td className="text-right">{c.on_map}</td>
        <td className="text-right">{c.off_map}</td>
        <td className={`text-right ${c.gap !== 0 ? 'text-red-600' : ''}`} data-gap={c.gap}>{c.gap}</td>
        <td className="text-right" data-stale={c.stale_rows ?? ''}>{c.stale_rows ?? ''}</td>
      </tr>
      {open ? <tr><td colSpan={6}><PoolEntries repo={c.repo} /></td></tr> : null}
    </>
  )
}

function PoolEntries({ repo }: { repo: string }) {
  const q = useQuery({ queryKey: ['status', 'pool', repo], queryFn: () => fetchPool(repo), retry: false, staleTime: 60_000 })
  if (q.isLoading) return <p className="m-0 py-1 text-muted-foreground">Reading the off-map entries…</p>
  if (q.isError || q.data == null) return <p className="m-0 py-1 text-muted-foreground">The off-map entries could not be read: unknown.</p>
  const p = q.data
  const byNs = (p.covered ?? []).filter(x => x.by_namespace).length
  return (
    <div className="py-1" data-pool={repo}>
      {(p.root_group_only ?? []).length > 0 ? <p className="m-0 break-all" data-root-group-only={p.root_group_only!.length}>Covered only by the front end&apos;s root group: {p.root_group_only!.join(', ')}</p> : null}
      {(p.stale_rows ?? []).length > 0 ? <p className="m-0 break-all" data-stale-rows={p.stale_rows!.length}>Rows covering nothing: {p.stale_rows!.join(', ')}</p> : null}
      {p.covered ? <p className="m-0" data-by-namespace={byNs}>On the map only by a whole namespace&apos;s row: {byNs} of {p.covered.length}</p> : null}
      {p.entries.length === 0 ? <p className="m-0 text-muted-foreground">No off-map entry.</p> : (
        <table className="w-full tabular-nums">
          <thead><tr className="text-left text-muted-foreground"><th className="font-normal">Entry</th><th className="font-normal">Kind</th><th className="text-right font-normal">Files</th><th className="font-normal">First seen</th><th className="font-normal">Review</th></tr></thead>
          <tbody>
            {p.entries.map(e => (
              <tr key={e.path} data-entry={e.path} className="border-t border-[var(--table-rule)]">
                <td className="break-all">{e.path}</td>
                <td>{e.kind}</td>
                <td className="text-right">{e.files}</td>
                <td>{e.first_seen ? new Date(e.first_seen).toLocaleDateString() : ''}</td>
                <td>{e.review ? `${e.review.verdict}${e.review.domain ? ` ${e.review.domain}` : ''}` : 'not reviewed'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

/** Who uses whom across domains: Go imports (production, test) and events, from the status the workers kept. */
function References({ s }: { s: DomainStatusResponse }) {
  const raw = s.references as DomainReference[] | null | undefined
  if (raw === undefined) return null
  const refs = raw ?? []
  return (
    <div className="flex flex-col gap-1 pt-2" aria-label="Cross-domain uses">
      <h3 className="m-0 text-xs font-medium text-foreground">Cross-domain uses</h3>
      {raw === null ? <p className="m-0 text-xs text-muted-foreground">The domain graph was not measured: unknown.</p> : (
        <table className="w-full text-xs tabular-nums">
          <thead>
            <tr className="text-left text-muted-foreground"><th className="font-normal">Kind</th><th className="font-normal">From → to</th><th className="text-right font-normal">Prod imports</th><th className="text-right font-normal">Test imports</th><th className="text-right font-normal">Events</th><th className="text-right font-normal">Unregistered</th><th className="font-normal">Wrong way</th></tr>
          </thead>
          <tbody>
            {refs.map(r => (
              <tr key={`${r.kind ?? 'import'} ${r.from} ${r.to}`} data-reference={`${r.kind ?? 'import'}:${r.from}>${r.to}`} className="border-t border-[var(--table-rule)]">
                <td>{r.kind ?? 'import'}</td>
                <td>{r.from} → {r.to}</td>
                <td className="text-right">{r.prod_imports ?? r.imports}</td>
                <td className="text-right">{r.test_imports ?? ''}</td>
                <td className="text-right">{r.events ?? ''}</td>
                <td className="text-right">{(r.kind ?? 'import') === 'import' ? r.unregistered : ''}</td>
                <td>{r.wrong_way ? 'yes' : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {s.references_to_base != null ? <p className="m-0 text-xs text-muted-foreground">Production imports of the base: {s.references_to_base}</p> : null}
    </div>
  )
}

/** Who uses each Go package (GET /catalog/users), read when opened; packages used by most other domains first. */
function PackageUsersBlock() {
  const [open, setOpen] = useState(false)
  const q = useQuery({ queryKey: ['status', 'package-users'], queryFn: fetchPackageUsers, retry: false, staleTime: 300_000, enabled: open })
  const rows = (q.data?.packages ?? []).map(p => {
    const others = new Set([...p.user_domains.prod, ...p.user_domains.test, ...p.user_domains.event].filter(d => d !== p.domain))
    return { p, others: [...others].sort() }
  }).sort((a, b) => b.others.length - a.others.length || a.p.package.localeCompare(b.p.package))
  return (
    <details className="pt-2 text-xs" onToggle={e => setOpen((e.currentTarget as HTMLDetailsElement).open)} aria-label="Package users">
      <summary className="min-h-11 cursor-pointer py-2 font-medium text-foreground">Package users</summary>
      {!open ? null : q.isLoading ? <p className="m-0 text-muted-foreground">Reading who uses each package…</p>
        : q.isError || q.data == null ? <p className="m-0 text-muted-foreground">Package users could not be read: unknown.</p>
        : !q.data.measured ? <p className="m-0 text-muted-foreground">{q.data.note ?? 'Not measured: unknown.'}</p>
        : (
          <table className="w-full tabular-nums">
            <thead><tr className="text-left text-muted-foreground"><th className="font-normal">Package</th><th className="font-normal">Domain</th><th className="text-right font-normal">Prod</th><th className="text-right font-normal">Test</th><th className="text-right font-normal">Event</th><th className="font-normal">Other domains using it</th></tr></thead>
            <tbody>
              {rows.map(({ p, others }) => (
                <tr key={p.package} data-package={p.package} className="border-t border-[var(--table-rule)]">
                  <td className="break-all">{p.package}</td>
                  <td>{p.domain}</td>
                  <td className="text-right">{p.users.prod.length}</td>
                  <td className="text-right">{p.users.test.length}</td>
                  <td className="text-right">{p.users.event.length}</td>
                  <td>{others.join(', ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
    </details>
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
      <p className="m-0 text-xs text-muted-foreground sm:hidden">Scroll the coverage table horizontally to read every column.</p>
      <div className="overflow-x-auto" role="region" aria-label="Coverage table" tabIndex={0}>
      <table className="w-full min-w-[36rem] text-xs tabular-nums [&_th]:px-2 [&_td]:px-2 [&_th]:py-2 [&_td]:py-2">
        <thead>
          <tr className="text-left text-muted-foreground"><th className="font-normal">Place</th><th className="text-right font-normal">Total</th><th className="text-right font-normal">On map</th><th className="text-right font-normal">Off map</th><th className="text-right font-normal">Gap</th><th className="text-right font-normal">Rows covering nothing</th></tr>
        </thead>
        <tbody>
          {inv.map(c => <PlaceRow key={c.repo} c={c} />)}
        </tbody>
      </table>
      </div>
      {by.length > 0 ? <p className="m-0 text-xs text-muted-foreground">Off-map entries by claimed domain: {by.map(([d, n]) => `${d} ${n}`).join(' · ')}</p> : null}
      {ix.isError ? <p className="m-0 text-xs text-muted-foreground">Which classes are reconciled could not be read: unknown.</p> : null}
      {(s.checks ?? []).map(c => (
        <p key={c.class} className="m-0 break-words text-xs" data-check={c.class}>
          {c.measured
            ? `${c.class}: ${c.registered} registered, ${c.live} live · not registered: ${c.not_registered.length ? c.not_registered.join(', ') : 'none'} · registered, not live: ${c.not_live.length ? c.not_live.join(', ') : 'none'}${(c.problems ?? []).length ? ` · breaks its own rules: ${c.problems!.join('; ')}` : ''}`
            : `${c.class}: not compared (${c.note ?? 'unknown'})`}
        </p>
      ))}
      {notReconciled.length > 0 ? (
        <p className="m-0 text-xs text-muted-foreground" data-not-reconciled={notReconciled.length}>
          Not reconciled with the real system ({notReconciled.length} of {ix.data!.classes.length} classes; the map cannot see what is missing from them): {notReconciled.map(c => `${c.class} (${c.check})`).join(', ')}
        </p>
      ) : null}
    </div>
  )
}

export function DomainStatusSummary() {
  const q = useDomainStatusObservation()
  const at = new Date(useNowMs()).toISOString()
  return <>
    {q.evidence && <p className="text-xs text-muted-foreground" aria-label="Domain read evidence">
      Read quality: {readQuality([q.evidence], at)} · Source: {q.evidence.source}
      {' · Source time: '}{q.evidence.evaluatedAt ?? 'Unknown'}
      {' · Received: '}{q.evidence.receivedAt}
      {' · Refresh deadline: '}{q.evidence.expiresAt ?? 'Unknown'}
    </p>}
    <DomainStatusContent query={q} />
  </>
}

/** Preserve the backend response and state; shared reads can replace the caller independently. */
export function DomainStatusContent({ query: q }: { query: {
  data?: DomainStatusResponse
  isLoading: boolean
  isError: boolean
  error: unknown
} }) {
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
          <References s={s} />
          <PackageUsersBlock />
        </>
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="min-h-11 cursor-pointer py-3">?</summary>
        <p className="m-0">Four lights per domain, dots in this order: alive (ready replicas of its workloads on the map), interfaces (requests and 5xx to its contract routes in the last hour, or the last day when the hour has under 20; under 20 in a day is too few to judge), boundary (imports against the tiers in or out of it), fresh (the table and the cluster were scanned in the last three rounds). Overall is the worst light. Hollow is unknown: not measured, never counted as good. Map coverage: for every repository, the platform database and the cluster, total (counted from the tree) = on map (a row covers it) + off map (in the pool of what the map cannot perceive); a gap that is not 0 means the scan missed something and turns every fresh light bad. A tenant&apos;s repository is one entry: its inside is not looked at this round. Covered only by the front end&apos;s root group: directories no narrower group names, owned only because the root group catches everything (a new directory lands there). A compared class (node) lists what its registry misses (live, not registered) and what it holds that is not live. Rows covering nothing: rows of the place&apos;s table whose directory, file, workload or table is no longer there (removed but still registered). A place opens to its off-map entries (and, for the cluster and the database, how many are on the map only by a whole namespace&apos;s row). Cross-domain uses: who uses whom above the base; import rows count pairs of packages by production and by test code, unregistered the production pairs the used domain&apos;s contract does not name; event rows count the event types one domain publishes that the other consumes. Package users: for each Go package, the packages that use it by production imports, test imports and events (event stays empty while the bus registry names components, not packages).</p>
      </details>
    </section>
  )
}
