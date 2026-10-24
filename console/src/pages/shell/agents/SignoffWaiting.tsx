import { useEffect, useRef } from 'react'
import type { SignoffWait } from '@/api/agentRuntimePlan'
import { hashNamesSection } from '@/lib/shell/useHashSection'
import { plural } from '@/pages/shell/owner/ownerModel'
import { SIGNOFF_SECTION_HREF } from '@/pages/shell/progress/progressPart'
import { SignOffButtons } from './StepSignoffs'
import { StepBadge, StepTitle } from './StepText'
import { useSignoffWaits } from './useRuntimePlan'

const box = 'ops-signoff-queue flex min-w-0 scroll-mt-4 flex-col gap-2 rounded-[var(--card-radius)] border bg-[var(--card-fill)] p-3'

/**
 * At the top of the plan: the steps waiting on the Owner's sign-off, by number, each with its one-line title and its
 * Sign off (the Owner 2026-10-21: he could not find the six steps to sign in the plan). A signed step leaves the block
 * when the plan is read again; the plan below is unchanged. While the plan is read, or when it cannot be, the block
 * says so in its place (review 502 B1), so `#work?section=signoff` has a target from the first paint (B2); when its
 * steps arrive and the address names it, it is brought into view again. A step with a sign-off rule whose work has not
 * started is not counted and has no Sign off here: it is under Not started, folded (TD-425). Nothing is drawn when no
 * step has an unsigned sign-off rule.
 */
export function SignoffWaiting({ deferScroll = false }: { deferScroll?: boolean }) {
  const read = useSignoffWaits()
  const ref = useRef<HTMLElement>(null)
  const ready = read.read === 'ok' && read.waits.length + read.notStarted.length > 0
  useEffect(() => {
    if (!deferScroll && ready && typeof window !== 'undefined' && hashNamesSection(window.location.hash, 'signoff')) ref.current?.scrollIntoView?.({ block: 'start' })
  }, [ready, deferScroll])
  if (read.read === 'ok' && read.waits.length === 0 && read.notStarted.length === 0) return null
  if (read.read !== 'ok') {
    return <section ref={ref} id="signoff" aria-label="Waiting for your sign-off" className={`${box} border-[var(--card-border)]`}>
      <p role={read.read === 'error' ? 'alert' : 'status'} className="m-0 text-sm text-muted-foreground">
        {read.read === 'error' ? 'The plan could not be read: which steps wait for your sign-off is not known.' : 'Reading which steps wait for your sign-off…'}</p>
    </section>
  }
  const waiting = read.waits.length > 0
  return <section ref={ref} id="signoff" aria-label="Waiting for your sign-off" className={`${box} ${waiting ? 'border-[var(--color-warning)]' : 'border-[var(--card-border)]'}`}>
    <h3 className="m-0 text-base font-semibold">Waiting for your sign-off · {read.waits.length}</h3>
    {waiting && <ul className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">{read.waits.map(w =>
      <li key={w.node} data-signoff-wait={w.node}
        className="flex min-w-0 flex-col gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5 sm:flex-row sm:items-center">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-sm"><StepBadge label={w.label} /><StepTitle text={w.title} /></div>
        <SignOffButtons node={w.node} label={w.label} refs={w.refs} named={w.named} state={w.state} title={w.title} />
      </li>)}</ul>}
    {read.notStarted.length > 0 && <NotStarted steps={read.notStarted} />}
  </section>
}

/**
 * Under the list, folded: the steps whose done rule waits on a sign-off but whose work has not started (TD-425: the
 * coordinator set such rules ahead of the work, and a sign-off would mark a step done with nothing behind it). Each by
 * its number and title, with no Sign off; it moves up to the list once a task on it starts.
 */
function NotStarted({ steps }: { steps: readonly SignoffWait[] }) {
  return <details data-signoff-not-started-group>
    <summary className="cursor-pointer text-sm pointer-coarse:min-h-11 pointer-coarse:py-3">Not started · {steps.length}</summary>
    <div className="flex min-w-0 flex-col gap-2 pt-2">
      <p className="m-0 text-xs text-muted-foreground">Their done rule waits on your sign-off, but no work on them has started yet. A step moves up to the list once a task on it starts.</p>
      <ul className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">{steps.map(w =>
        <li key={w.node} data-signoff-not-started={w.node}
          className="flex min-w-0 items-center gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5 text-sm"><StepBadge label={w.label} /><StepTitle text={w.title} /></li>)}</ul>
    </div>
  </details>
}

/**
 * For you's way to the block: how many steps wait on the Owner's sign-off. A plan that cannot be read is said so,
 * never as none waiting (review 502 B1); nothing while it is read or none waits.
 */
export function SignoffWaitingLink() {
  const read = useSignoffWaits()
  if (read.read === 'error') {
    return <p role="alert" data-signoff-link className="m-0 text-sm">The plan could not be read: which steps wait for your sign-off is not known. <a href={SIGNOFF_SECTION_HREF}>Work › Plan</a></p>
  }
  if (read.read !== 'ok' || read.waits.length === 0) return null
  const n = read.waits.length
  return <a href={SIGNOFF_SECTION_HREF} data-signoff-link
    className="inline-flex min-h-11 items-center self-start text-sm font-semibold text-[var(--color-warning)] underline underline-offset-2">
    {plural(n, 'step')} {n === 1 ? 'waits' : 'wait'} for your sign-off ›</a>
}
