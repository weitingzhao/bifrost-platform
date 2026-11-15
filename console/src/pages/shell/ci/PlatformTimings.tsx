import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCITimings } from '@/api/ciTimings'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { RunTimingDetail, READ_BUTTON } from './RunTimingDetail'
import { TimingTrends } from './TimingTrends'
import { elapsed, reusedBy, runCommit, secondsText } from './timingModel'

const pipelines = [
  ['bifrost-ci-platform','CI'], ['bifrost-land','Merge'],
  ['bifrost-deliver-platform','STG delivery'], ['bifrost-deliver-platform-prod','PROD delivery'],
] as const
const control = 'min-h-11 max-w-full rounded border border-[var(--border)] bg-[var(--field-fill)] px-2 text-sm'

export function PlatformTimings() {
  const [pipeline,setPipeline] = useState<string>(pipelines[0][0])
  const [hours,setHours] = useState(72)
  const [picked,setPicked] = useState('')
  const [typed,setTyped] = useState('')
  const [lookup,setLookup] = useState('')
  const query = useQuery({ queryKey:['ci-timings',pipeline], queryFn:({signal})=>fetchCITimings(pipeline,undefined,signal), staleTime:30_000, refetchInterval:30_000 })
  const direct = useQuery({ queryKey:['ci-timing-run',pipeline,lookup], queryFn:({signal})=>fetchCITimings(pipeline,lookup,signal), enabled:!!lookup, retry:false, staleTime:30_000 })
  const cutoff = Date.parse(query.data?.as_of ?? '') - hours*3600_000
  const runs = (query.data?.runs ?? []).filter(r=>Date.parse(r.created_at)>=cutoff)
  const selected = lookup ? direct.data?.runs[0] : runs.find(r=>r.name===picked) ?? runs[0]
  const pick = (name:string) => { setLookup(''); setPicked(name) }
  return <>
    <OpsVerdictStrip title="Platform CI & delivery" lamp={query.error ? 'degraded' : 'unknown'} tagLabel="Read only" tagVariant="neutral" summary="See where each run spends its time, what it tested, and what it reused." />
    <OpsSection title="Run explorer" description="Cluster records are retained for a few days; outside retention there is no history here. At most the newest 60 runs per pipeline are loaded. No missing duration is treated as zero." bodyPadding="compact">
      <div className="flex min-w-0 flex-wrap gap-2" role="group" aria-label="Pipeline">
        {pipelines.map(([id,label])=><button key={id} className={`${READ_BUTTON} ${pipeline===id?'bg-[var(--accent)]':''}`} aria-pressed={pipeline===id} onClick={()=>{setPipeline(id);setPicked('');setLookup('')}}>{label}</button>)}
        <label className="flex items-center gap-2 text-xs">Window<select className={control} value={hours} onChange={e=>setHours(Number(e.target.value))}><option value={24}>24 hours</option><option value={72}>72 hours</option></select></label>
      </div>
      {query.error && <p role="alert" className="text-sm text-destructive">Run timings could not be read: {query.error.message}. {query.data ? 'Showing the last successful read.' : 'Retained history is unknown.'}</p>}
      {query.isPending && <p className="text-sm">Reading run and task records…</p>}
      {query.data && <>
        <p className="text-xs text-muted-foreground">{`${runs.length} runs in this window · ${query.data.runs.length} loaded / ${query.data.retained} retained · read ${query.data.as_of}`}</p>
        {runs.length ? <label className="grid min-w-0 gap-1 text-xs">Run<select className={`${control} w-full min-w-0`} value={lookup ? '' : selected?.name ?? ''} onChange={e=>pick(e.target.value)}>
          {lookup && <option value="">Run name lookup</option>}
          {runs.map(run=><option key={run.name} value={run.name}>{run.created_at.slice(5,16).replace('T',' ')}Z · {runCommit(run).slice(0,8)} · {run.status} · {secondsText(elapsed(run))}{reusedBy(run)?' · reused':''} · {run.name}</option>)}
        </select></label> : <p className="text-sm">No run retained in this window. Older runs outside retention are unavailable.</p>}
      </>}
      <form className="mt-3 flex min-w-0 flex-wrap items-end gap-2" onSubmit={e=>{e.preventDefault();setLookup(typed.trim())}}>
        <label className="grid min-w-0 flex-1 gap-1 text-xs">Find retained run by name<input className={control} value={typed} onChange={e=>setTyped(e.target.value)} placeholder="PipelineRun name" /></label>
        <button className={READ_BUTTON} type="submit" disabled={!typed.trim()}>Find run</button>
      </form>
      {lookup && direct.isPending && <p className="text-sm">Reading named run…</p>}
      {direct.error && lookup && <p role="alert" className="text-sm text-destructive">Named run could not be read: {direct.error.message}. A deleted run is unavailable outside retention.</p>}
      {lookup && direct.data?.runs.length === 0 && <p className="text-sm">This name is not a retained platform run of the selected pipeline.</p>}
    </OpsSection>
    {selected && <RunTimingDetail key={selected.name} run={selected} />}
    {pipeline === 'bifrost-ci-platform' && query.data && <TimingTrends runs={runs} onPick={pick} />}
    <OpsSection title="Designed vs actual" description="Reserved for the CI lane’s time accounting." bodyPadding="compact">
      <p className="m-0 text-sm text-muted-foreground">Awaiting the benchmark time budget. Designed time, comparable actual time, and variance will appear here when that accounting is supplied. No estimate is filled in.</p>
    </OpsSection>
  </>
}
