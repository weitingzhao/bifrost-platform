import { useRef } from 'react'
import { archCounts, manualAndStopped, plainTitle, type ArchTick, type RuntimeArchTable, type RuntimePlanStep, type RuntimeRound } from '@/api/agentRuntimePlan'
import { OpsSection } from '@/components/layout/OpsSection'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { hashNamesSection } from '@/lib/shell/useHashSection'
import { LastRead } from '../owner/LastRead'
import { reasonText } from '../owner/ownerModel'
import { SignoffWaiting } from './SignoffWaiting'
import { StepSignoffControls } from './StepSignoffs'
import { StepBadge, StepTitle } from './StepText'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED, useRuntimeArch, useRuntimePlan } from './useRuntimePlan'

/** A step's mark from the runtime: Owner (owner_step) or ★ (effect_step), whatever its title says. */
function Mark({ title, children }: { title: string; children: string }) {
  return <span title={title} className="mr-1 rounded-full bg-[var(--control-fill)] px-1.5 text-xs font-semibold">{children}</span>
}
function Steps({ steps }: { steps: RuntimePlanStep[] }) {
  // Each step by its number and one line of its title (the Owner 2026-10-21: several steps are titled alike), then its
  // state, marks and what it waits on.
  return <ol className="flex min-w-0 flex-col gap-2 pt-2 text-sm">{steps.map((step, j) => <li key={`${step.id}-${j}`} data-step={step.id} className="min-w-0 break-words">
    <div className="flex min-w-0 items-center gap-2"><StepBadge label={step.id} /><StepTitle text={plainTitle(step.t)} /></div>
    <span className="text-xs text-muted-foreground"><strong>{step.state || 'Unknown'}</strong>{(step.owner || step.star) && ' · '}</span>{step.owner && <Mark title="The Owner's step (owner_step)">Owner</Mark>}{step.star && <Mark title="An effect step (effect_step)">★</Mark>}
    {/* The request this step is done by: the same request Needs you lists (v4 §20.5 scenario 10). */}
    {step.approvals?.map(n => <a key={n} href={approvalHref(String(n))} data-step-approval={n} className="ml-1 text-xs pointer-coarse:inline-flex pointer-coarse:min-h-11 pointer-coarse:items-center">Request #{n}</a>)}
    {/* The Owner's sign-off its done rule waits on: who signed and when, or the button while it can still move. */}
    {step.signoffs && <StepSignoffControls label={step.id} state={step.state} signoffs={step.signoffs} />}
    {step.when && <p className="text-xs text-muted-foreground">{step.when}</p>}
    {step.line && <details><summary className="cursor-pointer text-xs text-muted-foreground pointer-coarse:min-h-11 pointer-coarse:py-3">Ledger line</summary><p className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{step.line.trim()}</p></details>}
  </li>)}</ol>
}
function Items({ round }: { round: RuntimeRound }) {
  return <>{round.items.map((item, i) => <details open={['doing', 'running', 'current'].includes(item.state)} key={`${item.key}-${i}`} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2">
    <summary className="cursor-pointer text-sm pointer-coarse:min-h-11 pointer-coarse:py-3">{item.key} {plainTitle(item.name)} · {item.done ?? '?'}/{item.total ?? '?'}{manualAndStopped(item)} · {item.state}</summary>
    {item.steps.length === 0 && <p>No steps published.</p>}
    {item.steps.some(s => s.state === 'done') && <details><summary className="cursor-pointer text-sm pointer-coarse:min-h-11 pointer-coarse:py-3">Completed steps</summary><Steps steps={item.steps.filter(s => s.state === 'done')} /></details>}
    <Steps steps={item.steps.filter(s => s.state !== 'done')} />
  </details>)}</>
}
const TICK: Record<ArchTick, { mark: string; word: string; color: string }> = {
  true: { mark: '✓', word: 'passed', color: 'var(--color-success)' },
  false: { mark: '✕', word: 'not yet', color: 'var(--color-lamp-red)' },
  nogate: { mark: '–', word: 'no gate yet', color: 'var(--muted-foreground)' },
  unknown: { mark: '?', word: 'unknown', color: 'var(--muted-foreground)' },
}
/** Each row's tick is its gate tests against what is deployed now; the note is what it actually is, in words. */
function Architecture({ table }: { table: RuntimeArchTable }) {
  const counts = archCounts(table)
  return <div className="mt-3 flex flex-col gap-3">
    <h4>{table.title || 'Architecture'} · {counts.passed} passed · {counts.notYet} not yet · {counts.noGate} no gate yet · {counts.unknown} unknown</h4>
    {table.rows.length === 0 && <p>No architecture rows in the runtime.</p>}
    {table.rows.map(row => <details key={row.id} data-arch-row={row.id} data-tick={row.tick} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
      <summary className="cursor-pointer pointer-coarse:min-h-11 pointer-coarse:py-3"><span aria-hidden style={{ color: TICK[row.tick].color }}>{TICK[row.tick].mark}</span> <strong>{TICK[row.tick].word}</strong> · {row.title || row.id}</summary>
      <p className="break-words text-xs text-muted-foreground">{row.reason}</p>
      {row.note ? <p className="break-words whitespace-pre-line">{row.note.text} <span className="text-xs text-muted-foreground" title={row.note.at}>· {row.note.by}, {row.note.at.slice(0, 16).replace('T', ' ')}Z</span></p>
        : <p className="text-xs text-muted-foreground">No note on what it is now.</p>}
    </details>)}
  </div>
}
/** The round's plan (GET /plan?root=R2 with GET /progress's counts) and the architecture table (GET /progress), both from the agent runtime. */
export function AgentsPlan() {
  const openedAt = useRef(Date.now())
  const plan = useRuntimePlan()
  const arch = useRuntimeArch()
  // Opened when the address names it (Work's "Plan ›", `#work?section=plan`). Above it, always in view, the steps
  // waiting on the Owner's sign-off (`#work?section=signoff`, For you's link).
  return <div className="flex min-w-0 flex-col gap-3"><SignoffWaiting /><OpsSection title="Plan and architecture" description="From the agent runtime; each architecture row is ticked by its gate tests" collapsible
    defaultCollapsed={typeof window === 'undefined' || !hashNamesSection(window.location.hash, 'plan')} bodyPadding="compact">
    {plan.from === 'loading' && <p role="status">Reading the plan from the agent runtime…</p>}
    {plan.from === 'error' && <p role="alert">Plan unavailable: {reasonText(plan.error)}; <LastRead readAt={plan.readAt} openedAt={openedAt.current} />.</p>}
    {plan.from === 'absent' && <p>{PLAN_NOT_IMPORTED}</p>}
    {plan.from === 'runtime' && <div className="flex flex-col gap-3">
      <h4>Round {plan.round.n ?? 'unknown'} · from the agent runtime</h4>
      {plan.round.ledgerUnread !== undefined && <p role="status" className="m-0 break-words text-sm text-muted-foreground" data-ledger-unread>
        The ledger lines could not be read: {reasonText(plan.round.ledgerUnread)}. The steps are shown without them, and an imported step without its date.</p>}
      {plan.round.items.length === 0 && <p>No items in the runtime's plan.</p>}
      <Items round={plan.round} />
    </div>}
    {arch.from === 'loading' && <p role="status">Reading the architecture table…</p>}
    {arch.from === 'error' && <p role="alert">Architecture unavailable: {reasonText(arch.error)}; <LastRead readAt={arch.readAt} openedAt={openedAt.current} />.</p>}
    {arch.from === 'absent' && <p>{ARCH_NOT_IMPORTED}</p>}
    {arch.from === 'runtime' && <Architecture table={arch.table} />}
  </OpsSection></div>
}
