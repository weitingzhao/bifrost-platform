import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCITimings } from '@/api/ciTimings'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { RunTimingDetail, READ_BUTTON } from './RunTimingDetail'
import { TimingTrends } from './TimingTrends'
import { matchesModule } from './ciViewModel'
import { elapsed, reusedBy, runCommit, runStatus, secondsText, result } from './timingModel'

const pipelines = [
  ['bifrost-ci-platform','CI'], ['bifrost-land','Merge'],
  ['bifrost-deliver-platform','STG delivery'], ['bifrost-deliver-platform-prod','PROD delivery'],
] as const
const control = 'min-h-11 max-w-full rounded border border-transparent bg-[var(--field-fill)] px-2 text-sm'

export function PlatformTimings({ module = '', ciRun, ciCommit, onPickCI }: { module?: string; ciRun?: string; ciCommit?: string; onPickCI?: (name: string) => void }) {
  const [pipeline,setPipeline] = useState<string>(pipelines[0][0])
  const [hours,setHours] = useState(72)
  const [picked,setPicked] = useState('')
  const [focusCommit,setFocusCommit] = useState('')
  const [typed,setTyped] = useState('')
  const [lookup,setLookup] = useState('')
  const query = useQuery({ queryKey:['ci-timings',pipeline], queryFn:({signal})=>fetchCITimings(pipeline,undefined,signal), staleTime:30_000, refetchInterval:30_000 })
  const direct = useQuery({ queryKey:['ci-timing-run',pipeline,lookup], queryFn:({signal})=>fetchCITimings(pipeline,lookup,signal), enabled:!!lookup, retry:false, staleTime:30_000, refetchInterval:30_000 })
  const cutoff = Date.parse(query.data?.as_of ?? '') - hours*3600_000
  const runs = (query.data?.runs ?? []).filter(r=>Date.parse(r.created_at)>=cutoff).filter(r => pipeline !== 'bifrost-ci-platform' || matchesModule({ asked: true, run_modules: result(r.tasks.find(t => t.task === 'changes')?.results ?? [], 'run-modules'), tested_by: reusedBy(r) }, module))
  const commitFilter = focusCommit || (ciRun && pipeline !== 'bifrost-ci-platform' ? ciCommit : '')
  const matching = commitFilter ? runs.filter(r=>runCommit(r)===commitFilter) : runs
  const selected = ciRun && pipeline === 'bifrost-ci-platform' ? query.data?.runs.find(r => r.name === ciRun) : lookup ? direct.data?.runs[0] : matching.find(r=>r.name===picked) ?? matching.find(r=>!reusedBy(r)) ?? matching[0]
  const pick = (name:string) => { if (onPickCI && pipeline === 'bifrost-ci-platform') { onPickCI(name); return }; setLookup(''); setFocusCommit(''); setPicked(name) }
  return <>
    <OpsVerdictStrip title="Platform CI & delivery" lamp={query.error ? 'degraded' : 'unknown'} tagLabel="Read only" tagVariant="neutral" summary="See where each run spends its time, what it tested, and what it reused." />
    <OpsSection title="Run explorer" description="Cluster records are retained for a few days; outside retention there is no history here. At most the newest 60 runs per pipeline are loaded. No missing duration is treated as zero." bodyPadding="compact">
      <p className="text-sm">bifrost-platform{module ? ` / ${module}` : ''} · CI → Merge → STG → PROD. {module ? 'CI history follows the selected module; reused and unreported scope stay visible. Merge and delivery still operate on the whole repository; independent module delivery is not declared.' : 'Select a pipeline stage below. Stages show retained runs, not a release action.'}</p>
      <div className="flex min-w-0 flex-wrap gap-2" role="group" aria-label="Pipeline">
        {pipelines.map(([id,label])=><button key={id} className={`${READ_BUTTON} ${pipeline===id?'bg-[var(--accent)]':''}`} aria-pressed={pipeline===id} onClick={()=>{setPipeline(id);setPicked('');setLookup('');setFocusCommit('')}}>{label}</button>)}
        <label className="flex items-center gap-2 text-xs">Window<select className={control} value={hours} onChange={e=>setHours(Number(e.target.value))}><option value={24}>24 hours</option><option value={72}>72 hours</option></select></label>
      </div>
      {query.error && <p role="alert" className="text-sm text-destructive">Run timings could not be read: {query.error.message}. {query.data ? 'Showing the last successful read.' : 'Retained history is unknown.'}</p>}
      {query.isPending && <p className="text-sm">Reading run and task records…</p>}
      {query.data && <>
        <p className="text-xs text-muted-foreground">{`${runs.length} runs in this window · ${query.data.runs.length} loaded / ${query.data.retained} retained · read ${query.data.as_of}`}</p>
        {ciRun && pipeline === 'bifrost-ci-platform' ? <p className="break-all text-sm">Following selected CI run: {ciRun}{!selected ? ' · detailed record unavailable in the loaded window' : ''}</p> : matching.length ? <label className="grid min-w-0 gap-1 text-xs">Run<select className={`${control} w-full min-w-0`} value={lookup ? '' : selected?.name ?? ''} onChange={e=>pick(e.target.value)}>
          {lookup && <option value="">Run name lookup</option>}
          {matching.map(run=><option key={run.name} value={run.name}>{run.created_at.slice(5,16).replace('T',' ')}Z · {runCommit(run).slice(0,8)} · {runStatus(run)} · {secondsText(elapsed(run))}{reusedBy(run)?' · reused':''} · {run.name}</option>)}
        </select></label> : <p className="text-sm">No run retained in this window{commitFilter ? ' for the selected commit' : ''}. Older runs outside retention are unavailable.</p>}
      </>}
      {!onPickCI && <form className="mt-3 flex min-w-0 flex-wrap items-end gap-2" onSubmit={e=>{e.preventDefault();setLookup(typed.trim())}}>
        <label className="grid min-w-0 flex-1 gap-1 text-xs">Find retained run by name<input className={control} value={typed} onChange={e=>setTyped(e.target.value)} placeholder="PipelineRun name" /></label>
        <button className={READ_BUTTON} type="submit" disabled={!typed.trim()}>Find run</button>
      </form>}
      {lookup && module && <p className="text-xs">Named-run lookup is explicit and may be outside the selected module; inspect its reported scope.</p>}
      {lookup && direct.isPending && <p className="text-sm">Reading named run…</p>}
      {direct.error && lookup && <p role="alert" className="text-sm text-destructive">Named run could not be read: {direct.error.message}. A deleted run is unavailable outside retention.</p>}
      {lookup && direct.data?.runs.length === 0 && <p className="text-sm">This name is not a retained platform run of the selected pipeline.</p>}
    </OpsSection>
    {focusCommit && query.data && <p className="m-0 break-all px-3 text-sm">CI for {focusCommit}: {matching.length ? `${matching.length} matching runs loaded` : 'no matching CI retained in the loaded window; use the run-name lookup if known'}.</p>}
    {selected && pipeline !== 'bifrost-ci-platform' && runCommit(selected) && <button className={READ_BUTTON} onClick={()=>{setFocusCommit(runCommit(selected));setPicked('');setLookup('');setPipeline('bifrost-ci-platform')}}>Inspect CI for this commit</button>}
    {selected && <RunTimingDetail key={selected.name} run={selected} showJourney={!onPickCI} />}
    {pipeline === 'bifrost-ci-platform' && query.data && <TimingTrends runs={runs} onPick={pick} />}
  </>
}
