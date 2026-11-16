import { useState } from 'react'
import { useQueries } from '@tanstack/react-query'
import { fetchCITimings } from '@/api/ciTimings'
import { commitJourney } from './journeyModel'
import { elapsed, reusedBy, runStatus, secondsText } from './timingModel'

const pipelines = ['bifrost-ci-platform', 'bifrost-land', 'bifrost-deliver-platform']
export function CommitJourney({ commit, selectedRun, autoRead = false }: { commit: string; selectedRun?: string; autoRead?: boolean }) {
  const [enabled, setEnabled] = useState(autoRead)
  const queries = useQueries({ queries: pipelines.map(pipeline => ({
    queryKey: ['ci-timings', pipeline], queryFn: ({ signal }: {signal: AbortSignal}) => fetchCITimings(pipeline, undefined, signal),
    enabled, staleTime:30_000, refetchInterval: enabled ? 30_000 : false as const,
  })) })
  const available = queries.every(q => q.data != null)
  const view = available ? commitJourney(commit, queries[0].data!.runs, queries[1].data!.runs, queries[2].data!.runs, selectedRun) : null
  return <div className="m-3 min-w-0 rounded border border-[var(--table-rule)] p-3">
    <h4 className="m-0 text-sm font-medium">{selectedRun ? 'Selected CI → main → STG' : 'This commit: CI → main → STG'}</h4>
    <p className="text-xs text-muted-foreground">Same-commit retained timestamps. Elapsed time is measured between start and finish, including waits; overlapping stages are not added. No production release is included.</p>
    {!enabled && <button className="min-h-11 rounded bg-[var(--control-fill)] px-3 text-sm" onClick={() => setEnabled(true)}>Read this commit’s delivery journey</button>}
    {enabled && queries.some(q => q.error) && <p role="alert" className="text-sm">Some journey records could not be read. {queries.filter(q => q.error).map(q => q.error!.message).join('; ')}{available ? ' Showing the last successful reads.' : ' End-to-end time is unknown.'}</p>}
    {enabled && !available && !queries.some(q => q.error) && <p className="text-sm">Reading CI, merge and STG records…</p>}
    {enabled && view && <>
      <ol aria-label="Delivery timeline" className="m-0 mb-4 grid list-none gap-3 p-0 sm:grid-cols-3">{[
        {label:'CI', run:view.first, at:view.first?.start_time, end:view.first?.completion_time},
        {label:'Main merged', run:view.landed, at:view.landed?.completion_time, end:undefined},
        {label:'STG live', run:view.delivered ?? (view.first?.start_time ? [...view.ships].reverse().find(r => Date.parse(r.created_at) >= Date.parse(view.first!.start_time!)) : undefined), at:view.delivered?.start_time, end:view.delivered?.completion_time},
      ].map(({label,run,at,end},i) => <li key={label} className="min-w-0 rounded border border-[var(--table-rule)] p-3">
        <strong className="text-sm">{i+1}. {label}</strong><p className="my-2 text-sm">{run ? runStatus(run) : 'Not reached / not retained'}</p>
        <p className="my-1 break-all font-mono text-xs">{at ?? run?.start_time ?? 'Time unavailable'}{end ? ` → ${end}` : ''}</p>
        {run && <p className="mb-0 break-all text-xs text-muted-foreground">{run.name}</p>}
      </li>)}</ol>
      {selectedRun && <p className="break-all text-xs">Attempt: {selectedRun} · commit {commit.slice(0,8)}. Later CI retries for the same commit may contribute to delivery; this is not proof that the selected attempt alone released it.</p>}
      <div className="grid gap-3 sm:grid-cols-2">
        <div><p className="mb-1 text-xs text-muted-foreground">{selectedRun ? 'Selected CI start → successful STG' : 'First retained CI → first successful STG'}</p><strong className="font-mono text-lg">{view.total == null ? 'Not completed / not retained' : secondsText(view.total)}</strong></div>
        <div><p className="mb-1 text-xs text-muted-foreground">Design scenario · estimate</p><strong className="font-mono text-lg">~220s to STG</strong><p className="text-xs text-muted-foreground">45s leaf backend CI + 10s merge + 165s STG, without waits. A full/shared change is not a like-for-like comparison.</p></div>
      </div>
      <dl className="grid gap-2 text-xs sm:grid-cols-3">{[
        ['CI → main',secondsText(view.toMain)], ['After main → STG starts',secondsText(view.afterMain)], ['STG delivery',secondsText(view.delivery)],
      ].map(([label,value]) => <div key={label}><dt>{label}</dt><dd className="m-0 font-mono">{value}</dd></div>)}</dl>
      <p className="text-xs">{view.failed} failed CI attempts · {view.reused} reused verdicts · {view.tested.length} own test runs in retained records.</p>
      <details><summary className="min-h-11 cursor-pointer py-3 text-sm">Evidence & attempt history</summary>
        {[['CI', view.attempts], ['Merge',view.lands], ['STG',view.ships]].map(([label,runs]) => <div key={label as string} className="mb-3">
          <strong className="text-xs">{label as string}</strong>
          {(runs as typeof view.attempts).length === 0 && <p className="text-xs">No matching retained record; not proof that it never ran.</p>}
          <ul className="m-0 list-none space-y-2 p-0 text-xs">{(runs as typeof view.attempts).map(r => <li key={r.name} className="break-all">{r.name} · {runStatus(r)} · {secondsText(elapsed(r))}{reusedBy(r) ? ` · reused ${reusedBy(r)} (no new test sample)` : ''}</li>)}</ul>
        </div>)}
        <p className="text-xs text-muted-foreground">Newest 60 records per pipeline only; earlier retries may be absent. Timings begin at CI, not the git push. Other-commit rebases/amendments are not silently joined. Parallel task times and merge runs containing CI are never added together.</p>
        <p className="break-all text-xs text-muted-foreground">Read at: {queries.map((q,i) => `${pipelines[i]} ${q.data?.as_of ?? 'unknown'}`).join(' · ')}</p>
      </details>
    </>}
  </div>
}
