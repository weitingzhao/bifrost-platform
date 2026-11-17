import type { CiChange } from '@/api/ciDomains'
import { hhmm, placeLabel, type PageRun } from './ciPageModel'

/*
 * Work > CI > the selected run: how long it took and in which parts, what it tested (the packages when it was
 * selective), and what its commit changed by domain. A part that was not read is left out and said once.
 */
export function CiRunPanel({ run, change, changesRead }: { run: PageRun; change?: CiChange; changesRead: boolean }) {
  // The test tasks run side by side: their seconds are listed, not added.
  const timed = run.tests.filter(t => t.seconds != null)
  const tests = timed.length > 1 ? `the longest of ${timed.map(t => `${t.seconds} ${t.task}`).join(' | ')} (side by side)` : timed.map(t => `${t.seconds} ${t.task}`).join('')
  const parts = [run.lag != null ? `${run.lag} waiting` : '', run.setup != null ? `${run.setup} clone and scope` : '', tests].filter(Boolean)
  const sides = run.modules.kind === 'all' ? 'every module' : run.modules.kind === 'some' ? (run.modules.names.join(', ') || 'no module') : ''
  const tested = run.scope === 'incremental' ? `${run.packages?.length ?? 0} Go packages (selective)` : run.scope === 'full' ? 'every Go package' : run.scope === 'none' ? 'no backend scope reported' : ''
  const rows: [string, React.ReactNode][] = [
    ['Took', <span className="font-mono-tabular">{`${run.took ?? '…'} s${run.running ? ' so far' : ''}${parts.length ? ` = ${parts.join(' + ')}` : ''}`}</span>],
  ]
  if (tested || sides) rows.push(['Tested', [tested, sides ? `sides: ${sides}` : ''].filter(Boolean).join('; ')])
  if (run.scope === 'incremental' && run.packages?.length) rows.push(['Packages', <span className="font-mono-tabular text-xs [overflow-wrap:anywhere]">{run.packages.join(' ')}</span>])
  if (change?.read) rows.push(['Changed', Object.entries(change.domains).map(([k, v]) => `${placeLabel(k)} (${v} ${v === 1 ? 'file' : 'files'})`).join(', ') || 'no file'])
  else if (changesRead && change) rows.push(['Changed', 'not read: the commit is not in the repository'])
  return (
    <section aria-label="Selected run" className="min-w-0 rounded-xl border border-[var(--table-rule)] bg-[var(--card-fill)] p-3.5">
      <h3 className="ops-section-title m-0">{`Run of ${run.sha} · ${hhmm(run.at)} · `}{run.ok === false ? <span className="text-[var(--destructive)]">{`failed in ${run.bad.join(', ') || 'an unnamed task'}`}</span> : run.ok ? 'passed' : run.status.toLowerCase()}</h3>
      <dl className="m-0 mt-2.5 grid grid-cols-[minmax(72px,120px)_minmax(0,1fr)] gap-x-2.5 gap-y-1.5 text-sm">
        {rows.map(([label, value]) => <div key={label} className="contents"><dt className="text-[var(--muted-foreground)]">{label}</dt><dd className="m-0 min-w-0">{value}</dd></div>)}
      </dl>
      <p className="mb-0 mt-2 break-all text-xs text-[var(--muted-foreground)]">{run.name}</p>
    </section>
  )
}
