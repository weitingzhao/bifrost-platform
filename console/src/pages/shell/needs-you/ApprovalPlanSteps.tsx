import type { ApprovalStep } from '@/api/agentRuntimePlan'
import { useApprovalSteps } from '@/pages/shell/agents/useRuntimePlan'
import { PLAN_SECTION_HREF, planStepLine } from '@/pages/shell/progress/progressPart'

/** Each plan step a request is the approval for, as a link to the plan (v4 §20.5 scenario 10). Nothing for none. */
export function PlanStepLinks({ steps, className = '' }: { steps: readonly ApprovalStep[]; className?: string }) {
  return <>{steps.map(step => (
    <a key={step.id} href={PLAN_SECTION_HREF} data-plan-step={step.id} title={step.id}
      className={`block min-w-0 max-w-full break-words underline ${className}`}>{planStepLine(step)}</a>
  ))}</>
}

/**
 * Under a request in a list: the plan steps it is the approval for, read here and not by the list, so the plan read
 * landing repaints only these lines (CI 2026-10-21: it repainted Needs you's Answer group). Nothing while unread.
 */
export function ApprovalRowPlanSteps({ number }: { number: number | undefined }) {
  const plan = useApprovalSteps(number != null && number > 0)
  return plan.read === 'ok' ? <PlanStepLinks steps={plan.stepsOf(number)} className="mt-1 text-sm text-muted-foreground" /> : null
}

/**
 * On a request's page: which step of the plan it is the approval for, read from the runtime's plan (a step whose done
 * rule names this request's number); that none is, or that the plan could not be read, is said too.
 */
export function ApprovalPlanSteps({ number }: { number: number | undefined }) {
  const plan = useApprovalSteps(number != null && number > 0)
  if (number == null || number <= 0) return null
  const steps = plan.read === 'ok' ? plan.stepsOf(number) : []
  return (
    <section aria-label="Plan step" data-approval-plan-step className="min-w-0 max-w-full text-sm">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">Plan step</div>
      {plan.read === 'loading' ? <p className="m-0 text-muted-foreground">Reading the plan…</p> : null}
      {plan.read === 'error' ? <p className="m-0 text-muted-foreground">The plan could not be read: which step waits on this request is not known.</p> : null}
      {plan.read === 'ok' && steps.length === 0 ? <p className="m-0 text-muted-foreground">No step of the plan waits on this request.</p> : null}
      <PlanStepLinks steps={steps} />
    </section>
  )
}
