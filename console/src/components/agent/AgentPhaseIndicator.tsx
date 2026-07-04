import type { RemediationPhase } from '@/api/types'

export const PHASE_STEPS: { key: RemediationPhase; label: string }[] = [
  { key: 'starting', label: 'Start' },
  { key: 'diagnosing', label: 'Diagnose' },
  { key: 'awaiting_approval', label: 'Decide' },
  { key: 'remediating', label: 'Remediate' },
  { key: 'verifying', label: 'Verify' },
  { key: 'done', label: 'Done' },
]

export function phaseIndex(phase: RemediationPhase | undefined): number {
  if (phase == null) return -1
  if (phase === 'failed' || phase === 'cancelled') return PHASE_STEPS.length
  const idx = PHASE_STEPS.findIndex(s => s.key === phase)
  return idx >= 0 ? idx : -1
}

interface AgentPhaseIndicatorProps {
  currentPhase: RemediationPhase | undefined
  failed: boolean
  /** Compact renders smaller dots with tooltip labels only */
  compact?: boolean
}

export function AgentPhaseIndicator({ currentPhase, failed, compact = false }: AgentPhaseIndicatorProps) {
  const current = phaseIndex(currentPhase)

  if (compact) {
    return (
      <nav className="agent-phase-indicator agent-phase-indicator--compact" aria-label="Agent progress">
        {PHASE_STEPS.map((step, i) => {
          let state: 'done' | 'active' | 'pending' | 'failed' = 'pending'
          if (failed && i === current) state = 'failed'
          else if (i < current || currentPhase === 'done') state = 'done'
          else if (i === current) state = 'active'
          return (
            <div key={step.key} className={`agent-phase-step agent-phase-step--${state}`} title={step.label}>
              <div className="agent-phase-dot" />
              {i < PHASE_STEPS.length - 1 && <div className="agent-phase-line" />}
            </div>
          )
        })}
      </nav>
    )
  }

  return (
    <nav className="remediation-stepper" aria-label="Remediation progress">
      {PHASE_STEPS.map((step, i) => {
        let state: 'done' | 'active' | 'pending' | 'failed' = 'pending'
        if (failed && i === current) state = 'failed'
        else if (i < current || currentPhase === 'done') state = 'done'
        else if (i === current) state = 'active'
        return (
          <div key={step.key} className={`remediation-step remediation-step--${state}`}>
            <div className="remediation-step-dot" />
            <span className="remediation-step-label">{step.label}</span>
            {i < PHASE_STEPS.length - 1 && <div className="remediation-step-line" />}
          </div>
        )
      })}
    </nav>
  )
}
