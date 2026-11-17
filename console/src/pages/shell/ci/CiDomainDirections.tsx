import type { CiDomainGraph } from '@/api/ciDomains'
import { domainDirections, type DirectionSide } from './ciPageModel'

/*
 * Work > CI, By dependency: every domain with both directions in words. The drawing shows the shape; this says, for
 * each domain, what it depends on and who depends on it, with counts, and under each row the packages on both sides.
 * Owner 2026-11-16, reading the page: "does foundation depend on the other packages, or do they depend on it?"
 */

const MUTED = 'text-[var(--muted-foreground)]'
const RED = 'text-[var(--color-lamp-red)]'
const short = (pkg: string) => pkg.replace(/^api\/internal\//, '')
const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

function Sides({ list }: { list: DirectionSide[] }) {
  return <>{list.map((s, i) => (
    <span key={s.id} className={s.wrongWay ? `font-medium ${RED}` : undefined}>
      {i > 0 && <span className={MUTED}>, </span>}{s.id} <span className={s.wrongWay ? undefined : MUTED}>({plural(s.imports, 'import')}{s.wrongWay ? ', wrong way' : ''})</span>
    </span>
  ))}</>
}

function Pairs({ title, list, none }: { title: string; list: DirectionSide[]; none: string }) {
  return (
    <div className="min-w-0">
      <p className={`m-0 text-xs font-medium ${MUTED}`}>{title}</p>
      {list.length === 0 ? <p className={`m-0 text-xs ${MUTED}`}>{none}</p> : list.map(s => (
        <div key={s.id} className="mt-1 min-w-0">
          <p className={`m-0 text-xs ${s.wrongWay ? RED : ''}`}>{s.id}{s.wrongWay ? ' · wrong way' : ''}</p>
          {s.pairs.length === 0
            ? <p className={`m-0 text-xs ${MUTED}`}>{`${plural(s.imports, 'import')}; the packages were not sent`}</p>
            : <ul className="m-0 list-none p-0 font-mono text-[11px] leading-5">
              {s.pairs.map(p => <li key={`${p.from}>${p.to}`} className="[overflow-wrap:anywhere]">{short(p.from)} <span className={MUTED}>→</span> {short(p.to)}</li>)}
            </ul>}
        </div>
      ))}
    </div>
  )
}

export function CiDomainDirections({ graph, domain, onSelectDomain }: { graph: CiDomainGraph; domain: string; onSelectDomain: (id: string) => void }) {
  const rows = domainDirections(graph)
  const wrong = graph.edges.filter(e => e.wrong_way).reduce((n, e) => n + e.imports, 0)
  return (
    <section aria-label="Who depends on whom" className="min-w-0" data-testid="ci-domain-directions">
      <h3 className="ops-section-title m-0">Who depends on whom</h3>
      <p className={`mb-2 mt-1 text-xs ${MUTED}`}>
        {'An arrow points from the user to the used: A → B means A imports B, so A depends on B. '}
        {graph.base ? `${graph.base} is the base every other domain stands on; an import by the base of a domain above it points the wrong way. ` : ''}
        <span className={wrong > 0 ? RED : undefined}>{`Wrong-way imports now: ${wrong}.`}</span>
        {' A wall in the code holds that number: it only goes down.'}
      </p>
      <div className="flex min-w-0 flex-col">
        {rows.map(row => {
          const above = row.isBase ? 'no domain above it' : 'no other domain'
          return (
            <details key={row.id} className={`min-w-0 border-t border-[var(--table-rule)] py-1.5 ${row.id === domain ? 'bg-[var(--accent)]' : ''}`} data-testid={`ci-direction-${row.id}`}>
              <summary className="grid min-h-11 cursor-pointer grid-cols-1 items-baseline gap-x-3 gap-y-0.5 px-1 text-sm sm:grid-cols-[minmax(110px,150px)_minmax(0,1fr)_minmax(0,1fr)]">
                <span className="font-medium">{row.id}{row.isBase ? <span className={`font-normal ${MUTED}`}> · base</span> : null}</span>
                <span className="min-w-0 [overflow-wrap:anywhere]"><span className={MUTED}>Depends on: </span>
                  {row.uses.length === 0 ? above : <>{plural(row.uses.length, 'domain')}<span className={MUTED}> — </span><Sides list={row.uses} /></>}</span>
                <span className="min-w-0 [overflow-wrap:anywhere]"><span className={MUTED}>Depended on by: </span>
                  {row.usedBy.length === 0 ? 'nobody' : <>{plural(row.usedBy.length, 'domain')}<span className={MUTED}> — </span><Sides list={row.usedBy} /></>}</span>
              </summary>
              <div className="grid min-w-0 gap-3 px-1 pb-2 pt-1 sm:grid-cols-2">
                <Pairs title={`Packages of ${row.id} and what they use`} list={row.uses} none={`${row.id} imports ${above}.`} />
                <Pairs title={`Packages that use ${row.id}`} list={row.usedBy} none={`No other domain imports ${row.id}.`} />
              </div>
              <button type="button" className="mx-1 mb-1 min-h-11 text-xs underline underline-offset-4" onClick={() => onSelectDomain(row.id === domain ? '' : row.id)}>
                {row.id === domain ? `Clear the selection of ${row.id}` : `Select ${row.id} in the drawing and the other views`}
              </button>
            </details>
          )
        })}
      </div>
    </section>
  )
}
