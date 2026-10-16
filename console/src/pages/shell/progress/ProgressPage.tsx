import type { ReactNode } from 'react'
import { record, text, parseRuntimeTask } from '@/api/agentRuntime'
import { computedAgeLine, generationIsSkewed, publishedAgeSeconds, receivedIsStale, skewSentence, staleSentence } from '@/api/agentProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { useNeedsYou } from '../needs-you/useNeedsYou'
import { needsYouCountText } from '../needs-you/needsYouModel'
import { GoalForm } from '../agents/GoalForm'
import { AgentsPlan } from '../agents/AgentsPlan'
import { Missions, TaskRow } from './RuntimeWork'
import { ProgressOverview } from './ProgressOverview'
import { NowClock, Tick } from './progressClock'
import { ProgressActivity, ProgressDoneToday } from './ProgressActivity'
import { FinishedWorks, WorkCard } from './ProgressWorks'
import { splitWorks } from './progressModel'
import { workRunning, workWaiting } from './mergedProgressModel'
import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'

/** One project surface, ordered by the Owner's questions. */
export function ProgressPage() {
  const needs = useNeedsYou()
  const { count, runtimeQuery: runtimeQ, progressQuery: boardQ } = needs
  const runtime = runtimeQ.data, board = boardQ.data?.document
  const { active, finished } = splitWorks(board?.works ?? [])
  function runtimeState(children: ReactNode, empty: boolean, message: string) {
    return <>
      <p className="text-xs text-muted-foreground">Runtime generated: {runtime?.generated_at || 'Unknown'}</p>
      {runtimeQ.isPending && <p role="status">Loading agent runtime…</p>}
      {runtimeQ.isError && <p role="alert">Could not load agent runtime: {runtimeQ.error.message}</p>}
      {runtime && empty && <p>{message}</p>}
      {runtime && children}
    </>
  }
  function boardState() {
    const env = boardQ.data, now = Date.now()
    const stale = env && (env.stale || receivedIsStale(env.received_at, env.age_seconds, now))
    const skewed = env && generationIsSkewed(board?.at, env.received_at, now)
    return <>
      <p className="text-xs text-muted-foreground">Board generated: {env?.generated_at || 'Unknown'}{board?.at && <> · <Tick text={t => computedAgeLine(board.at!, t)} /></>}</p>
      {boardQ.isPending && <p role="status">Loading coordinator board…</p>}
      {boardQ.isError && <p role="alert">Could not load coordinator board: {boardQ.error.message}. Last known data may be shown.</p>}
      {stale && <p>{staleSentence(publishedAgeSeconds(env.received_at, env.age_seconds, now))}</p>}
      {skewed && board?.at && <p>{skewSentence(board.at, now)}</p>}
      {boardQ.isSuccess && !board && <p>No progress has been published.</p>}
    </>
  }
  return <NowClock><div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden" data-testid="merged-progress">
    <section aria-label="Needs you"><OpsSection title={`Needs you · ${needsYouCountText(count)}`} bodyPadding="compact">
      <p data-needs-you-count={needsYouCountText(count)}>{count.state === 'loading' ? 'Checking what needs you…' : count.state === 'unknown' ? `Count is incomplete: ${count.reason}` : count.count === 0 ? 'Nothing needs you right now' : `${count.count} waiting for you`}</p>
      <ul className="flex flex-col gap-1">{needs.waitingItems.slice(0, 5).map(item => <li key={item.id} className="min-w-0 text-sm"><a href={item.href} title={`${item.title} · ${item.instruction}`} className="block truncate underline">{item.title} · {item.instruction}</a></li>)}</ul>
      <a href="#needs-you" className="text-sm underline">All on Needs you</a>
    </OpsSection></section>
    <section aria-label="Overview">{boardState()}<ProgressOverview count={count} runtime={runtime} board={board} failed={runtimeQ.isError || boardQ.isError} /></section>
    <section aria-label="Give the agents a goal"><GoalForm /></section>
    <section aria-label="Running"><OpsSection title="Running" bodyPadding="compact">
      <div className="grid min-w-0 gap-4 lg:grid-cols-2">
        <section aria-label="Agents"><h4>Agents</h4>{runtimeState(<>
          {runtime && <Missions data={runtime} />}
          {runtime?.live.map(a => <p key={a.attempt_id} className="break-words text-sm">{a.task_id} · {a.host_id} · {a.executor} / {a.model || 'Model unknown'} · {a.state} · Elapsed: {a.elapsed_s ?? '?'} s · Heartbeat age: {a.heartbeat_age_s ?? '?'} s · {a.reason}</p>)}
        </>, !runtime?.work.missions.some(m => !['stopped', 'done', 'accepted', 'failed'].includes(m.state)) && !runtime?.live.length, 'No missions or attempts running.')}</section>
        <section aria-label="Coordinator"><h4>Coordinator</h4>{boardState()}
          {board?.coordinator && <p className="text-sm">{board.coordinator.doing || board.coordinator.note || board.coordinator.state} {board.coordinator.next && `· Then: ${board.coordinator.next}`}</p>}
          {board && active.length === 0 && <p>No coordinator work running.</p>}
          {active.map((work, i) => <details key={`${work.title}-${i}`} open={workRunning(work) || workWaiting(work)}><summary className="cursor-pointer text-sm">{work.title} {workWaiting(work) && <a href="#needs-you">· Waiting for you</a>}</summary><WorkCard work={work} index={i} at={board?.at} /></details>)}
        </section>
      </div>
    </OpsSection></section>
    <section aria-label="Plan and architecture"><AgentsPlan /></section>
    <section aria-label="Hosts"><OpsSection title="Hosts" bodyPadding="compact">{runtimeState(<ul className="flex flex-col gap-2">{runtime?.hosts.map(host => <li key={host.host_id} className="text-sm">{host.host_id} · Online: {host.online === undefined ? 'Unknown' : String(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s · Live attempts: {host.live_attempts ?? '?'} · Last poll: {host.last_poll_at || 'Unknown'}</li>)}</ul>, !runtime?.hosts.length, 'No hosts reported.')}</OpsSection></section>
    <section aria-label="History"><OpsSection title="History" collapsible defaultCollapsed bodyPadding="compact">
      {boardState()}<ProgressDoneToday recent={board?.recent} /><FinishedWorks works={finished} at={board?.at} /><ProgressActivity feed={board?.feed} />
      {runtimeState(<>
        <h4>Recent runtime tasks</h4>{runtime?.recent.tasks.length === 0 && <p>No recent tasks.</p>}
        {runtime?.recent.tasks.map((task, i) => <details key={i}><summary className="cursor-pointer break-words text-sm">{text(task.title) || text(task.task_id)} · {text(task.state)} · {text(task.ended_at)}</summary><TaskRow mission={text(task.mission_id)} task={parseRuntimeTask(task)} /></details>)}
        <h4>Failed attempts</h4>{runtime?.recent.failed_attempts.length === 0 && <p>No failed attempts.</p>}
        {runtime?.recent.failed_attempts.map((value, i) => { const a = record(value); return <details key={i}><summary className="cursor-pointer break-words text-sm">{text(a.attempt_id)} · {text(a.task_id)} · {text(a.ended_at)}</summary><TaskRow mission={text(a.mission_id)} task={parseRuntimeTask(a)} /></details> })}
      </>, !runtime?.recent.tasks.length && !runtime?.recent.failed_attempts.length, 'No runtime history.')}
    </OpsSection></section>
    {/* Outside the seven blocks: Progress has always carried these two, and #commit-lineage and #code-health still land here. */}
    <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col"><CommitLineagePage /></section>
    <section aria-label="Code health" className="flex w-full min-w-0 flex-col"><CodeHealthPage /></section>
  </div></NowClock>
}
