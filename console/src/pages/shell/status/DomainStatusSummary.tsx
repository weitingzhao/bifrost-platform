import { useQuery } from '@tanstack/react-query'
import { fetchDomainStatus, LIGHTS, type DomainStatusRow } from '@/domains/foundation/catalog/api/domainStatus'

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

function Row({ d }: { d: DomainStatusRow }) {
  return (
    <li className="list-none border-t border-[var(--table-rule)] first:border-t-0" data-domain={d.id}>
      <details>
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
      </details>
    </li>
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
        </>
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="min-h-11 cursor-pointer py-3">?</summary>
        <p className="m-0">Four lights per domain, dots in this order: alive (ready replicas of its workloads on the map), interfaces (requests and 5xx to its contract routes in the last hour; under 20 requests is too few to judge), boundary (imports against the tiers in or out of it), fresh (the table and the cluster were scanned in the last three rounds). Overall is the worst light. Hollow is unknown: not measured, never counted as good.</p>
      </details>
    </section>
  )
}
