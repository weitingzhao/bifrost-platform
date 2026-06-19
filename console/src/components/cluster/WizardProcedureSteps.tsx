import type { NodeWizardStep, WizardStepStatus } from '@/lib/cluster/nodeWizard'

interface WizardProcedureStepsProps {
  steps: NodeWizardStep[]
  flowLabel?: string
}

function stepIcon(status: WizardStepStatus): React.ReactNode {
  switch (status) {
    case 'done':
      return (
        <span className="flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500/20 text-emerald-400">
          <svg className="h-3 w-3" fill="none" viewBox="0 0 12 12" stroke="currentColor" strokeWidth={2.5}>
            <path d="M2.5 6.5 5 9l4.5-6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      )
    case 'current':
      return (
        <span className="relative flex h-5 w-5 items-center justify-center">
          <span className="absolute h-5 w-5 animate-ping rounded-full bg-blue-500/30" />
          <span className="relative h-3 w-3 rounded-full bg-blue-500 ring-2 ring-blue-500/30" />
        </span>
      )
    case 'blocked':
      return (
        <span className="flex h-5 w-5 items-center justify-center rounded-full bg-red-500/20 text-red-400">
          <svg className="h-3 w-3" fill="none" viewBox="0 0 12 12" stroke="currentColor" strokeWidth={2.5}>
            <path d="M3 3l6 6M9 3l-6 6" strokeLinecap="round" />
          </svg>
        </span>
      )
    default:
      return <span className="flex h-5 w-5 items-center justify-center"><span className="h-2 w-2 rounded-full border-2 border-zinc-500" /></span>
  }
}

function stepBadge(status: WizardStepStatus): React.ReactNode {
  const base = 'inline-flex items-center rounded px-1.5 py-0.5 text-dense-micro font-semibold uppercase tracking-wider'
  switch (status) {
    case 'done':
      return <span className={`${base} bg-emerald-500/10 text-emerald-400`}>Done</span>
    case 'current':
      return <span className={`${base} bg-blue-500/15 text-blue-400`}>Next</span>
    case 'blocked':
      return <span className={`${base} bg-red-500/10 text-red-400`}>Blocked</span>
    default:
      return null
  }
}

function connectorClass(status: WizardStepStatus): string {
  switch (status) {
    case 'done':
      return 'bg-emerald-500/40'
    case 'current':
      return 'bg-blue-500/40'
    default:
      return 'bg-zinc-600/40'
  }
}

export function WizardProcedureSteps({ steps, flowLabel }: WizardProcedureStepsProps) {
  return (
    <section aria-label="Procedure steps">
      <div className="mb-2 flex items-baseline gap-2">
        <h4 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Procedure
        </h4>
        {flowLabel != null && flowLabel !== '' && (
          <span className="rounded bg-[var(--secondary)] px-1.5 py-0.5 text-dense-caption font-medium text-[var(--foreground)]">
            {flowLabel}
          </span>
        )}
      </div>
      <ol className="relative m-0 flex list-none flex-col p-0 pl-[9px]">
        {steps.map((step, index) => {
          const isLast = index === steps.length - 1
          const isCurrent = step.status === 'current'
          return (
            <li key={step.id} className="relative flex gap-3 pb-3 last:pb-0">
              {!isLast && (
                <span
                  className={`absolute left-[9px] top-6 h-[calc(100%-12px)] w-0.5 ${connectorClass(step.status)}`}
                  aria-hidden
                />
              )}
              <span className="relative z-10 shrink-0">{stepIcon(step.status)}</span>
              <div
                className={`min-w-0 flex-1 rounded-md px-2.5 py-1.5 ${
                  isCurrent
                    ? 'border border-blue-500/30 bg-blue-500/5'
                    : 'border border-transparent'
                }`}
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className={`text-sm ${isCurrent ? 'font-semibold text-[var(--foreground)]' : 'font-medium text-[var(--foreground)]/80'}`}>
                    {step.label}
                  </span>
                  {stepBadge(step.status)}
                </div>
                <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">{step.description}</p>
              </div>
            </li>
          )
        })}
      </ol>
    </section>
  )
}

/** @deprecated Use WizardProcedureSteps — kept for gradual import migration */
export function WizardStepsList({ steps }: { steps: NodeWizardStep[] }) {
  return <WizardProcedureSteps steps={steps} />
}
