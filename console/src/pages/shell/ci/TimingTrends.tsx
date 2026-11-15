import { useState } from 'react'
import { useQueries, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { fetchCompare, fetchPipelineRunLogs } from '@/api/delivery'
import type { TimingRun } from '@/api/ciTimings'
import { OpsSection } from '@/components/layout/OpsSection'
import { READ_BUTTON } from './RunTimingDetail'
import { beforeTests, changeKind, CHANGE_KINDS, elapsed, median, reusedBy, runCommit, secondsText, slowTimings, trendRuns } from './timingModel'

/** Bound concurrent reads so a long history never starts a request per row at once. */
async function readChanges(commits: string[], client: QueryClient) {
  const out: Record<string,string[] | undefined> = {}
  let next = 0
  await Promise.all(Array.from({length:3}, async () => {
    while (next < commits.length) {
      const sha = commits[next++]
      try {
        out[sha] = await client.fetchQuery({ queryKey:['ci-commit-paths',sha], staleTime:Infinity, retry:false, queryFn:async () => {
          const data = await fetchCompare('bifrost-platform', `${sha}^`, sha)
          if (data.reachability !== 'ok') throw new Error(data.detail || 'Changed paths unavailable')
          return data.files
        } })
      } catch { /* Unknown stays unknown. */ }
    }
  }))
  return out
}

export function TimingTrends({ runs, onPick }: { runs: TimingRun[]; onPick: (name:string) => void }) {
  const client = useQueryClient()
  const own = trendRuns(runs).slice(0,30)
  const commits = own.map(runCommit)
  const changes = useQuery({ queryKey:['ci-timing-changes',...commits], queryFn:() => readChanges(commits, client), staleTime:5*60_000, enabled:commits.length>0 })
  const [logsEnabled,setLogsEnabled] = useState(false)
  const sample = own.slice(0,5)
  const logs = useQueries({ queries:sample.map(run => ({ queryKey:['ci-timing-logs',run.name], queryFn:() => fetchPipelineRunLogs(run.name,'cicd'), staleTime:60_000, enabled:logsEnabled, retry:false })) })
  const slow = logs.flatMap((q,i) => slowTimings(q.data?.logs ?? '',sample[i].name)).sort((a,b)=>b.seconds-a.seconds).slice(0,10)
  const unknown = own.filter(r => changeKind(changes.data?.[runCommit(r)]) === 'Unknown').length
  return <>
    <OpsSection title="Recent CI timing by change" description="One latest successful own CI run per commit (up to 30). Changed paths against that commit’s first parent: api/ = backend, console/ = console. Shared/root changes are separate. These are measured runs, not scope-replay estimates." bodyPadding="compact">
      <p className="m-0 mb-3 text-xs text-muted-foreground">{`${runs.filter(r=>!!reusedBy(r)).length} reused runs excluded · daily full runs and failures excluded · ${unknown} unclassified${changes.isFetching ? ' (reading changed paths…)' : ''}. Before test tasks includes scope selection, ending when the first test TaskRun is created; task startup is shown separately in the run details.`}</p>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{CHANGE_KINDS.map(kind => {
        const points = own.filter(r => changeKind(changes.data?.[runCommit(r)]) === kind).reverse()
        const totals = points.map(elapsed).filter((n):n is number=>n!=null)
        const waits = points.map(beforeTests).filter((n):n is number=>n!=null)
        const max = Math.max(1,...totals)
        return <div key={kind} className="min-w-0 rounded bg-[var(--card-fill)] p-3">
          <div className="flex flex-wrap justify-between gap-1 text-xs font-medium"><span>{kind}</span><span>{points.length} commits</span></div>
          <p className="my-2 font-mono text-lg">{secondsText(median(totals))}<span className="ml-2 text-xs text-muted-foreground">median CI</span></p>
          <p className="m-0 text-xs text-muted-foreground">{secondsText(median(waits))} before test tasks ({waits.length} measured)</p>
          {points.length > 0 && <svg className="my-2 h-12 w-full" viewBox="0 0 240 48" role="img" aria-label={`${kind} elapsed seconds, oldest to newest: ${totals.join(', ')}`} preserveAspectRatio="none">
            <polyline fill="none" stroke="var(--primary)" strokeWidth="2" points={totals.map((n,i)=>`${totals.length===1?120:4+i*232/(totals.length-1)},${44-n/max*40}`).join(' ')} />
            {totals.map((n,i)=><circle key={i} cx={totals.length===1?120:4+i*232/(totals.length-1)} cy={44-n/max*40} r="2" fill="var(--primary)" />)}
          </svg>}
          <details><summary className="cursor-pointer py-2 text-xs">Samples · oldest to newest</summary>
            <ul className="m-0 list-none space-y-1 p-0">{points.map(run=><li key={run.name}><button className="min-h-11 w-full text-left text-xs text-[var(--primary)]" onClick={()=>onPick(run.name)}>{runCommit(run).slice(0,8)} · {secondsText(elapsed(run))} · before {secondsText(beforeTests(run))}<span className="block text-muted-foreground">{run.created_at}</span></button></li>)}</ul>
          </details>
        </div>
      })}</div>
    </OpsSection>
    <OpsSection title="Slow tests and packages" description="Read observed durations from the latest five successful own CI runs in this window. Individual tests appear only when the logs report their durations; package time is not summed into wall time." bodyPadding="compact">
      <button className={READ_BUTTON} onClick={()=>setLogsEnabled(true)} disabled={logsEnabled || !sample.length}>Read recent test timings</button>
      {logsEnabled && <>
        <p className="text-xs text-muted-foreground">{`${logs.filter(q=>q.data).length}/${sample.length} log reads answered · ${logs.filter(q=>q.isFetching).length} reading · ${logs.filter(q=>q.error).length} failed. Log tails only; cached packages excluded. No logs outside pod retention.`}</p>
        {logs.some(q=>q.error) && <p role="alert" className="text-xs text-destructive">Some logs could not be read. The ranking is partial.</p>}
        <ol className="space-y-2 pl-5 text-xs">{slow.map((row,i)=><li key={`${row.run}-${row.name}-${i}`}><button className="min-h-11 text-left" onClick={()=>onPick(row.run)}><span className="font-mono">{secondsText(row.seconds)}</span> · {row.kind} · <span className="break-all">{row.name}</span><span className="block break-all text-muted-foreground">{row.run}</span></button></li>)}</ol>
        {!logs.some(q=>q.isFetching) && !slow.length && <p className="text-sm">No test/package timings in the retained logs.</p>}
      </>}
    </OpsSection>
  </>
}
