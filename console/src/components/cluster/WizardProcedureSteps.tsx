import { STATUS } from '@bifrost/ui/city/world'
import type { NodeWizardStep, WizardStepStatus } from '@/lib/cluster/nodeWizard'

interface WizardProcedureStepsProps {
  steps: NodeWizardStep[]
  flowLabel?: string
}

/** Done and blocked take the engine's ok / fail (STATUS, 2026-11-23); the current step is the selection (primary). */
const tint = (s: { fill: string; ink: string }, pct: number) => ({ background: `color-mix(in srgb, ${s.fill} ${pct}%, transparent)`, color: s.ink })

function stepIcon(status: WizardStepStatus): React.ReactNode {
  switch (status) {
    case 'done':
      return (
        <span className="flex h-5 w-5 items-center justify-center rounded-full" style={tint(STATUS.ok, 20)}>
          <svg className="h-3 w-3" fill="none" viewBox="0 0 12 12" stroke="currentColor" strokeWidth={2.5}>
            <path d="M2.5 6.5 5 9l4.5-6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      )
    case 'current':
      return (
        <span className="relative flex h-5 w-5 items-center justify-center">
          <span className="absolute h-5 w-5 animate-ping rounded-full bg-[color-mix(in_srgb,var(--primary)_30%,transparent)]" />
          <span className="relative h-3 w-3 rounded-full bg-[var(--primary)] ring-2 ring-[color-mix(in_srgb,var(--primary)_30%,transparent)]" />
        </span>
      )
    case 'blocked':
      return (
        <span className="flex h-5 w-5 items-center justify-center rounded-full" style={tint(STATUS.fail, 20)}>
          <svg className="h-3 w-3" fill="none" viewBox="0 0 12 12" stroke="currentColor" strokeWidth={2.5}>
            <path d="M3 3l6 6M9 3l-6 6" strokeLinecap="round" />
          </svg>
        </span>
      )
    default:
      return <span className="flex h-5 w-5 items-center justify-center"><span className="h-2 w-2 rounded-full border-2 border-[var(--muted-foreground)]" /></span>
  }
}

function stepBadge(status: WizardStepStatus): React.ReactNode {
  const base = 'inline-flex items-center rounded px-1.5 py-0.5 text-dense-micro font-semibold uppercase tracking-wider'
  switch (status) {
    case 'done':
      return <span className={base} style={tint(STATUS.ok, 10)}>Done</span>
    case 'current':
      return <span className={`${base} bg-[color-mix(in_srgb,var(--primary)_15%,transparent)] text-[var(--primary)]`}>Next</span>
    case 'blocked':
      return <span className={base} style={tint(STATUS.fail, 10)}>Blocked</span>
    default:
      return null
  }
}

function connectorBackground(status: WizardStepStatus): string {
  if (status === 'done') return `color-mix(in srgb, ${STATUS.ok.fill} 40%, transparent)`
  if (status === 'current') return 'color-mix(in srgb, var(--primary) 40%, transparent)'
  return 'color-mix(in srgb, var(--muted-foreground) 40%, transparent)'
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
                  className="absolute left-[9px] top-6 h-[calc(100%-12px)] w-0.5"
                  style={{ background: connectorBackground(step.status) }}
                  aria-hidden
                />
              )}
              <span className="relative z-10 shrink-0">{stepIcon(step.status)}</span>
              <div
                className={`min-w-0 flex-1 rounded-md px-2.5 py-1.5 ${
                  isCurrent
                    ? 'border border-[color-mix(in_srgb,var(--primary)_30%,transparent)] bg-[color-mix(in_srgb,var(--primary)_5%,transparent)]'
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
