import { plural } from '@/pages/shell/owner/ownerModel'
import { SIGNOFF_SECTION_HREF } from '@/pages/shell/progress/progressPart'
import { SignOffButtons } from './StepSignoffs'
import { StepBadge, StepTitle } from './StepText'
import { useSignoffWaits } from './useRuntimePlan'

/**
 * At the top of the plan: the steps waiting on the Owner's sign-off, by number, each with its one-line title and its
 * Sign off (the Owner 2026-10-21: he could not find the six steps to sign in the plan). A signed step leaves the block
 * when the plan is read again; the plan below is unchanged. Nothing is drawn while none waits or the plan is unread.
 */
export function SignoffWaiting() {
  const read = useSignoffWaits()
  if (read.read !== 'ok' || read.waits.length === 0) return null
  return <section id="signoff" aria-label="Waiting for your sign-off"
    className="flex min-w-0 scroll-mt-4 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--color-warning)] bg-[var(--card-fill)] p-3">
    <h3 className="m-0 text-base font-semibold">Waiting for your sign-off · {read.waits.length}</h3>
    <ul className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">{read.waits.map(w =>
      <li key={w.node} data-signoff-wait={w.node}
        className="flex min-w-0 flex-col gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5 sm:flex-row sm:items-center">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-sm"><StepBadge label={w.label} /><StepTitle text={w.title} /></div>
        <SignOffButtons node={w.node} label={w.label} refs={w.refs} named={w.named} />
      </li>)}</ul>
  </section>
}

/** For you's way to the block: how many steps wait on the Owner's sign-off. Nothing while none does or it is unread. */
export function SignoffWaitingLink() {
  const read = useSignoffWaits()
  if (read.read !== 'ok' || read.waits.length === 0) return null
  const n = read.waits.length
  return <a href={SIGNOFF_SECTION_HREF} data-signoff-link
    className="inline-flex min-h-11 items-center self-start text-sm font-semibold text-[var(--color-warning)] underline underline-offset-2">
    {plural(n, 'step')} {n === 1 ? 'waits' : 'wait'} for your sign-off ›</a>
}
