import { useCallback, useRef, useState } from 'react'
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

type StepVisualState = 'done' | 'active' | 'pending' | 'failed'

export function stepVisualState(
  index: number,
  current: number,
  failed: boolean,
  currentPhase: RemediationPhase | undefined,
): StepVisualState {
  if (failed && index === current) return 'failed'
  if (index < current || currentPhase === 'done') return 'done'
  if (index === current) return 'active'
  return 'pending'
}

export function phaseProgressSummary(
  current: number,
  currentPhase: RemediationPhase | undefined,
): { doneCount: number; remainingCount: number; total: number } {
  const total = PHASE_STEPS.length
  if (currentPhase === 'done') return { doneCount: total, remainingCount: 0, total }
  if (current < 0) return { doneCount: 0, remainingCount: total - 1, total }
  const doneCount = current
  const remainingCount = Math.max(0, total - doneCount - 1)
  return { doneCount, remainingCount, total }
}

const STATE_HINT: Record<StepVisualState, string> = {
  done: 'Completed',
  active: 'Current',
  pending: 'Upcoming',
  failed: 'Failed',
}

function indexFromPointer(clientX: number, rect: DOMRect, stepCount: number): number {
  const x = Math.max(0, Math.min(rect.width, clientX - rect.left))
  const ratio = rect.width > 0 ? x / rect.width : 0
  return Math.min(stepCount - 1, Math.max(0, Math.floor(ratio * stepCount)))
}

interface AgentPhaseIndicatorProps {
  currentPhase: RemediationPhase | undefined
  failed: boolean
  /** Compact renders smaller dots; interactive adds hover scrub + step summary */
  compact?: boolean
  interactive?: boolean
}

export function AgentPhaseIndicator({
  currentPhase,
  failed,
  compact = false,
  interactive = false,
}: AgentPhaseIndicatorProps) {
  const current = phaseIndex(currentPhase)
  const trackRef = useRef<HTMLDivElement>(null)
  const [hover, setHover] = useState<{ index: number; x: number } | null>(null)

  const summary = phaseProgressSummary(current, currentPhase)
  const activeStep = current >= 0 ? PHASE_STEPS[current] : null

  const updateHover = useCallback((clientX: number) => {
    const el = trackRef.current
    if (el == null) return
    const rect = el.getBoundingClientRect()
    const index = indexFromPointer(clientX, rect, PHASE_STEPS.length)
    const rawX = Math.max(0, Math.min(rect.width, clientX - rect.left))
    const x = Math.max(28, Math.min(rect.width - 28, rawX))
    setHover({ index, x })
  }, [])

  const clearHover = useCallback(() => setHover(null), [])

  if (compact) {
    const scrubStep = hover != null ? PHASE_STEPS[hover.index] : null
    const scrubState =
      hover != null ? stepVisualState(hover.index, current, failed, currentPhase) : null

    return (
      <div
        className={[
          'agent-phase-shell',
          interactive ? 'agent-phase-shell--interactive' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <div
          ref={trackRef}
          className="agent-phase-track"
          role="slider"
          aria-label="Agent progress"
          aria-valuemin={1}
          aria-valuemax={PHASE_STEPS.length}
          aria-valuenow={current >= 0 ? current + 1 : 1}
          aria-valuetext={
            activeStep != null
              ? `${activeStep.label} — ${summary.doneCount} of ${summary.total} completed, ${summary.remainingCount} remaining`
              : 'Agent progress'
          }
          tabIndex={interactive ? 0 : undefined}
          onMouseMove={interactive ? e => updateHover(e.clientX) : undefined}
          onMouseLeave={interactive ? clearHover : undefined}
          onFocus={interactive ? () => {
            if (activeStep != null && current >= 0) {
              const el = trackRef.current
              if (el == null) return
              const stepWidth = el.offsetWidth / PHASE_STEPS.length
              setHover({ index: current, x: stepWidth * current + stepWidth / 2 })
            }
          } : undefined}
          onBlur={interactive ? clearHover : undefined}
          onKeyDown={
            interactive
              ? e => {
                  const base = hover?.index ?? (current >= 0 ? current : 0)
                  if (e.key === 'ArrowRight') {
                    e.preventDefault()
                    const next = Math.min(PHASE_STEPS.length - 1, base + 1)
                    const el = trackRef.current
                    if (el == null) return
                    const stepWidth = el.offsetWidth / PHASE_STEPS.length
                    setHover({ index: next, x: stepWidth * next + stepWidth / 2 })
                  } else if (e.key === 'ArrowLeft') {
                    e.preventDefault()
                    const next = Math.max(0, base - 1)
                    const el = trackRef.current
                    if (el == null) return
                    const stepWidth = el.offsetWidth / PHASE_STEPS.length
                    setHover({ index: next, x: stepWidth * next + stepWidth / 2 })
                  }
                }
              : undefined
          }
        >
          <nav className="agent-phase-indicator agent-phase-indicator--compact">
            {PHASE_STEPS.map((step, i) => {
              const state = stepVisualState(i, current, failed, currentPhase)
              const isHover = hover?.index === i
              return (
                <div
                  key={step.key}
                  className={[
                    'agent-phase-step',
                    `agent-phase-step--${state}`,
                    isHover ? 'agent-phase-step--hover' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                >
                  <div className="agent-phase-dot" />
                  {i < PHASE_STEPS.length - 1 && <div className="agent-phase-line" />}
                </div>
              )
            })}
          </nav>

          {interactive && hover != null && scrubStep != null && scrubState != null && (
            <div
              className="agent-phase-scrub-tip"
              style={{ left: `${hover.x}px` }}
              role="tooltip"
            >
              <span className="agent-phase-scrub-tip__name">{scrubStep.label}</span>
              <span className={`agent-phase-scrub-tip__state agent-phase-scrub-tip__state--${scrubState}`}>
                {STATE_HINT[scrubState]}
              </span>
            </div>
          )}
        </div>

        {interactive && (
          <div className="agent-phase-summary" title="Completed steps · remaining steps">
            <span className="agent-phase-summary__count">
              {summary.doneCount}/{summary.total}
            </span>
            {summary.remainingCount > 0 && (
              <span className="agent-phase-summary__remaining">{summary.remainingCount} left</span>
            )}
            {hover == null && activeStep != null && currentPhase !== 'done' && (
              <span className="agent-phase-summary__current">{activeStep.label}</span>
            )}
          </div>
        )}
      </div>
    )
  }

  return (
    <nav className="remediation-stepper" aria-label="Remediation progress">
      {PHASE_STEPS.map((step, i) => {
        const state = stepVisualState(i, current, failed, currentPhase)
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
