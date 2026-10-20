import { useEffect, useRef, useState, type ReactNode } from 'react'
import { computedAgeLine, generationIsSkewed, publishedAgeSeconds, receivedIsStale, skewSentence, staleSentence } from '@/api/agentProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { useNeedsYou } from '../needs-you/useNeedsYou'
import { AgentsPlan } from '../agents/AgentsPlan'
import { useRuntimePlan } from '../agents/useRuntimePlan'
import { ProgressOverview } from './ProgressOverview'
import { NowClock, Tick } from './progressClock'
import { WORK_GOAL_CARDS } from './ProgressHistory'
import { GoalCard } from './GoalCard'
import { missionIds, relative } from './goalCardModel'
import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'
import { GOALS_SECTION, formatShellHash } from '@/lib/shell/consoleRoutes'
import { scrollAfterShell, useHashSection } from '@/lib/shell/useHashSection'

function goalInHash(): string {
  const id = new URLSearchParams(window.location.hash.split('?')[1]).get('goal')
  return id && /^M-\d+$/.test(id) ? id : ''
}

/**
 * Work's detail, below its cards (it was the Progress page; W-31 2.2b): the goal cards, the
 * overview, plan and architecture, hosts, history, commit lineage and code health. What waits on
 * the Owner is For you's; new goals are given on Chief of staff. `#work?goal=M-n` puts that goal's
 * card first and brings it into view.
 */
export function ProgressPage() {
  const needs = useNeedsYou()
  const plan = useRuntimePlan()
  const goals = useRef<HTMLElement>(null)
  useHashSection(GOALS_SECTION, goals)
  const [submitted, setSubmitted] = useState<{ id: string; goal: string } | null>(() => {
    const id = goalInHash()
    return id ? { id, goal: '' } : null
  })
  useEffect(() => {
    const change = () => {
      const id = goalInHash()
      if (id) { setSubmitted(current => current?.id === id ? current : { id, goal: '' }); setHighlighted(id) }
    }
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [])
  const [highlighted, setHighlighted] = useState(goalInHash)
  useEffect(() => {
    if (!highlighted) return
    const timer = window.setTimeout(() => setHighlighted(''), 6000)
    return () => window.clearTimeout(timer)
  }, [highlighted])
  const { count, runtimeQuery: runtimeQ, progressQuery: boardQ } = needs
  const runtime = runtimeQ.data, board = boardQ.data?.document
  const ids = missionIds(runtime, submitted?.id)
  function runtimeState(children: ReactNode, empty: boolean, message: string) {
    return <>
      <p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime?.generated_at}><Tick text={now => relative(runtime?.generated_at || '', now)} /></time></p>
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
      <p className="text-xs text-muted-foreground">Board updated: <time title={env?.generated_at}><Tick text={now => relative(env?.generated_at || '', now)} /></time>{board?.at && <> · <Tick text={t => computedAgeLine(board.at!, t)} /></>}</p>
      {boardQ.isPending && <p role="status">Loading coordinator board…</p>}
      {boardQ.isError && <p role="alert">Could not load coordinator board: {boardQ.error.message}. Last known data may be shown.</p>}
      {stale && <p>{staleSentence(publishedAgeSeconds(env.received_at, env.age_seconds, now))}</p>}
      {skewed && board?.at && <p>{skewSentence(board.at, now)}</p>}
      {boardQ.isSuccess && !board && <p>No progress has been published.</p>}
    </>
  }
  return <NowClock><div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden" data-testid="merged-progress">
    <section id={GOALS_SECTION} ref={goals} aria-label="Goals" className="flex min-w-0 scroll-mt-4 flex-col gap-4">
      <p className="m-0 text-sm text-muted-foreground">New goals are given on <a href={formatShellHash('chief')}>Chief of staff</a>.</p>
      {runtimeState(null, !ids.length, 'No goals yet.')}
      {ids.slice(0, WORK_GOAL_CARDS).map(id => <GoalCard key={id} id={id} runtime={runtime} updatedAt={runtimeQ.dataUpdatedAt} questions={needs.questions} highlighted={highlighted === id} submittedGoal={submitted?.id === id ? submitted.goal : ''} />)}
    </section>
    <section aria-label="Overview">{boardState()}<ProgressOverview count={count} runtime={runtime} board={board} plan={plan} failed={runtimeQ.isError} silentThreads={needs.silent.length} /></section>
    <section aria-label="Plan and architecture"><AgentsPlan /></section>
    <section aria-label="Hosts"><OpsSection title="Hosts" bodyPadding="compact">{runtimeState(<ul className="flex flex-col gap-2">{runtime?.hosts.map(host => <li key={host.host_id} className="text-sm">{host.host_id} · Online: {host.online === undefined ? 'Unknown' : String(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s · Live attempts: {host.live_attempts ?? '?'} · Last poll: <time title={host.last_poll_at}><Tick text={now => relative(host.last_poll_at, now)} /></time></li>)}</ul>, !runtime?.hosts.length, 'No hosts reported.')}</OpsSection></section>
    <section aria-label="History"><OpsSection title="History" bodyPadding="compact">
      <p className="m-0 text-sm">Older goals, the runtime's event timeline, recent tasks and failed attempts are in <a href={formatShellHash('records', 'tab=work')}>Records › Work history</a>.</p>
    </OpsSection></section>
    <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col"><ProgressReference id="commit-lineage" title="Commit lineage"><CommitLineagePage /></ProgressReference></section>
    <section aria-label="Code health" className="flex w-full min-w-0 flex-col"><ProgressReference id="code-health" title="Code health"><CodeHealthPage /></ProgressReference></section>
  </div></NowClock>
}

// targeted: the location names this section (#<id>, or a redirect's ?section=<id>).
function targeted(id: string): boolean {
  return window.location.hash === `#${id}` || new URLSearchParams(window.location.hash.split('?')[1]).get('section') === id
}

function ProgressReference({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  const [open, setOpen] = useState(() => targeted(id))
  // Open and scroll to it on arrival too, not only on a later hash change (review 424 B5).
  useEffect(() => {
    let cancel = () => {}
    const change = () => {
      if (!targeted(id)) return
      setOpen(true)
      cancel()
      cancel = scrollAfterShell(() => document.getElementById(id))
    }
    change()
    window.addEventListener('hashchange', change)
    return () => { cancel(); window.removeEventListener('hashchange', change) }
  }, [id])
  return <details id={id} open={open} onToggle={event => setOpen(event.currentTarget.open)} className="panel-elevated ops-section"><summary className="cursor-pointer p-3 text-sm font-medium">{title}</summary>{open && children}</details>
}
