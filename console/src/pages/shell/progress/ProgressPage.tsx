import { GovernanceReferences } from './GovernanceReferences'
import { useSettledSignoffTarget } from './useSettledSignoffTarget'
import { runtimeUnreadLine } from '../owner/ownerModel'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import { useNeedsYou } from '../needs-you/useNeedsYou'
import { AgentsPlan } from '../agents/AgentsPlan'
import { useRuntimeArch, useRuntimePlan } from '../agents/useRuntimePlan'
import { ProgressOverview } from './ProgressOverview'
import { NowClock, Tick } from './progressClock'
import { WORK_GOAL_CARDS } from './ProgressHistory'
import { GoalCard } from './GoalCard'
import { goalTaskId, missionIds, relative } from './goalCardModel'
import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'
import { GOALS_SECTION, formatShellHash, hashQueryParam } from '@/lib/shell/consoleRoutes'
import { canonicalQuery, hashNamesSection, scrollAfterShell } from '@/lib/shell/useHashSection'

function goalInHash(): string {
  const id = hashQueryParam(canonicalQuery(window.location.hash), 'goal')
  return id && /^M-\d+$/.test(id) ? id : ''
}

/**
 * What the address points at on Work: a goal's card before a section (review 425 B1); with a task of the goal
 * (`&task=`, T36), that task's details on the card once the card has them, else the card. The card brings the
 * task into view itself when its tasks come later.
 */
function addressTarget(): (() => HTMLElement | null) | null {
  const goal = goalInHash()
  if (goal) {
    const task = hashQueryParam(canonicalQuery(window.location.hash), 'task')
    return () => (task ? document.getElementById(goalTaskId(goal, task)) : null) ?? document.getElementById(`goal-${goal}`)
  }
  const section = hashQueryParam(canonicalQuery(window.location.hash), 'section')
  return section ? () => document.getElementById(section) : null
}

/**
 * Work's detail, below its cards (it was the Progress page; W-31 2.2b): the goal cards, the
 * overview, plan and architecture, hosts, history, commit lineage and code health. What waits on
 * the Owner is For you's; new goals are given on Chief of staff. `#work?goal=M-n` puts that goal's
 * card first and brings it into view.
 */
export function ProgressPage() {
  const openedAt = useRef(Date.now())
  const needs = useNeedsYou()
  const plan = useRuntimePlan()
  // One scroll per address, on arrival and on every hash change, to what the address points at:
  // returning to the same goal scrolls again, and goal and section together scroll once (review 425 B1).
  useEffect(() => {
    let cancel = () => {}
    const go = () => {
      const target = addressTarget()
      if (target == null) return
      cancel()
      cancel = scrollAfterShell(target)
    }
    go()
    window.addEventListener('hashchange', go)
    return () => { cancel(); window.removeEventListener('hashchange', go) }
  }, [])
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
  const arch = useRuntimeArch()
  const { count, runtimeQuery: runtimeQ } = needs
  const readAt = runtimeQ.dataUpdatedAt > 0 ? runtimeQ.dataUpdatedAt : null
  // A failed or paused read is not the present (review 488 A1): what runs, the hosts and the counts are not told
  // from the last answer, as Now, Open goals and Crew above do not; the goal cards stay, each saying its own read.
  const unreadable = runtimeQ.isError || runtimeQ.isPaused
  const runtime = unreadable ? undefined : runtimeQ.data
  const ids = missionIds(runtimeQ.data, submitted?.id)
  useSettledSignoffTarget(ids.slice(0, WORK_GOAL_CARDS),
    (!runtimeQ.isPending || runtimeQ.isPaused) && plan.from !== 'loading')
  function runtimeState(children: ReactNode, empty: boolean, message: string) {
    return <>
      {runtime && <p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime.generated_at}><Tick text={now => relative(runtime.generated_at || '', now)} /></time></p>}
      {runtimeQ.isPending && !runtimeQ.isPaused && <p role="status">Loading agent runtime…</p>}
      {unreadable && <p role="alert"><Tick text={now => `${runtimeUnreadLine(readAt, openedAt.current, now)}${runtimeQ.error ? ` Error: ${runtimeQ.error.message}` : ' Offline.'}`} /></p>}
      {runtime && empty && <p>{message}</p>}
      {runtime && children}
    </>
  }
  return <NowClock><div className="flex w-full min-w-0 flex-col gap-4 overflow-x-hidden" data-testid="merged-progress">
    <a href="#work?section=references" className="self-start text-sm underline">Governance references · Blueprint, Standards, MCP and protocols</a>
    <section id={GOALS_SECTION} aria-label="Goals" className="flex min-w-0 scroll-mt-4 flex-col gap-4">
      <p className="m-0 text-sm text-muted-foreground">New goals are given on <a href={formatShellHash('chief')}>Chief of staff</a>.</p>
      {runtimeState(null, !ids.length, 'No goals yet.')}
      {ids.slice(0, WORK_GOAL_CARDS).map(id => <GoalCard key={id} id={id} runtime={runtime} updatedAt={runtimeQ.dataUpdatedAt} questions={needs.questions} highlighted={highlighted === id} submittedGoal={submitted?.id === id ? submitted.goal : ''} />)}
    </section>
    <section aria-label="Overview"><ProgressOverview count={count} runtime={runtime} plan={plan} arch={arch} failed={unreadable} unread={runtimeUnreadLine(readAt, openedAt.current, Date.now())} silentThreads={needs.silent.length} openedAt={openedAt.current} /></section>
    <section id="plan" aria-label="Plan and architecture" className="scroll-mt-4"><AgentsPlan deferSignoffScroll /></section>
    <section aria-label="Hosts"><OpsSection title="Hosts" bodyPadding="compact">{runtimeState(<ul className="flex flex-col gap-2">{runtime?.hosts.map(host => <li key={host.host_id} className="text-sm">{host.host_id} · Online: {host.online === undefined ? 'Unknown' : String(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s · Live attempts: {host.live_attempts ?? '?'} · Last poll: <time title={host.last_poll_at}><Tick text={now => relative(host.last_poll_at, now)} /></time></li>)}</ul>, !runtime?.hosts.length, 'No hosts reported.')}</OpsSection></section>
    <section aria-label="History"><OpsSection title="History" bodyPadding="compact">
      <p className="m-0 text-sm">Older goals, the runtime's event timeline, recent tasks and failed attempts are in <a href={formatShellHash('records', 'tab=work')}>Records › Work history</a>.</p>
    </OpsSection></section>
    <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col"><ProgressReference id="commit-lineage" title="Commit lineage"><CommitLineagePage /></ProgressReference></section>
    <section aria-label="Code health" className="flex w-full min-w-0 flex-col"><ProgressReference id="code-health" title="Code health"><CodeHealthPage /></ProgressReference></section>
    <GovernanceReferences />
  </div></NowClock>
}

// targeted: the address names this section (`?section=<id>`, or the old `#<id>` in its new form).
function targeted(id: string): boolean {
  return hashNamesSection(window.location.hash, id)
}

function ProgressReference({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  const [open, setOpen] = useState(() => targeted(id))
  // Opened on arrival and on a later hash change; Work's one scroll brings it into view (review 425 B1).
  useEffect(() => {
    const change = () => { if (targeted(id)) setOpen(true) }
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [id])
  return <details id={id} open={open} onToggle={event => setOpen(event.currentTarget.open)} className="panel-elevated ops-section"><summary className="cursor-pointer p-3 text-sm font-medium">{title}</summary>{open && children}</details>
}
