import { useQuery } from '@tanstack/react-query'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress, parseRuntimeTask, record, text } from '@/api/agentRuntime'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { TaskRow } from './RuntimeWork'
import { NowClock, Tick } from './progressClock'
import { ProgressTimeline } from './ProgressTimeline'
import { missionIds, relative } from './goalCardModel'

/** Work shows this many goal cards; older goals are listed here. */
export const WORK_GOAL_CARDS = 8

/**
 * Finished work, on Records (it was Progress's History; W-31 2.2b, "做完的进 Records"): the
 * goals older than Work's cards, the runtime's event timeline, recent tasks and failed attempts.
 * Reads the same runtime progress Work and For you read.
 */
export function ProgressHistory() {
  const runtimeQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const runtime = runtimeQ.data
  const ids = missionIds(runtime)
  const empty = !runtime?.recent.tasks.length && !runtime?.recent.failed_attempts.length
  return <NowClock><section aria-label="Work history" className="flex w-full min-w-0 flex-col gap-3">
    <p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime?.generated_at}><Tick text={now => relative(runtime?.generated_at || '', now)} /></time></p>
    {runtimeQ.isPending && <p role="status">Loading agent runtime…</p>}
    {runtimeQ.isError && <p role="alert">Could not load agent runtime: {runtimeQ.error.message}</p>}
    {runtime && <>
      <h4 className="m-0">Older goals</h4>
      {ids.length <= WORK_GOAL_CARDS && <p className="m-0 text-sm text-muted-foreground">None: every goal is a card on <a href={goalHref()}>Work</a>.</p>}
      {ids.slice(WORK_GOAL_CARDS).map(id => <p key={id} className="m-0 text-sm">{id} · {runtime.work.missions.find(m => m.mission_id === id)?.title || text(runtime.recent.tasks.find(t => text(t.mission_id) === id)?.title) || 'Finished goal'} · <a href={goalHref(id)} className="underline">View goal</a></p>)}
    </>}
    <ProgressTimeline runtime={runtime} now={Date.now()} />
    {runtime && <>
      {empty && <p>No runtime history.</p>}
      <h4>Recent runtime tasks</h4>{runtime.recent.tasks.length === 0 && <p>No recent tasks.</p>}
      {runtime.recent.tasks.map((task, i) => <details key={i}><summary className="cursor-pointer break-words text-sm">{text(task.title) || text(task.task_id)} · {text(task.state)} · {text(task.ended_at)}</summary><TaskRow mission={text(task.mission_id)} task={parseRuntimeTask(task)} /></details>)}
      <h4>Failed attempts</h4>{runtime.recent.failed_attempts.length === 0 && <p>No failed attempts.</p>}
      {runtime.recent.failed_attempts.map((value, i) => { const a = record(value); return <details key={i}><summary className="cursor-pointer break-words text-sm">{text(a.attempt_id)} · {text(a.task_id)} · {text(a.ended_at)}</summary><TaskRow mission={text(a.mission_id)} task={parseRuntimeTask(a)} /></details> })}
    </>}
  </section></NowClock>
}
