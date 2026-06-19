/**
 * Ops Console outcome text colors — align with StatusLamp semantics (lamp-ok / lamp-warn / lamp-fail).
 * Red (lamp-fail / destructive) is for errors, failures, and deleted states only — not success copy.
 */

export type OpsOutcomeKind = 'success' | 'warning' | 'error' | 'neutral'

const SUCCESS_RE =
  /^(success|succeeded|ok|synced|healthy|complete|completed|running)$|successfully/i
const ERROR_RE =
  /^(error|failed|failure|fail|denied|forbidden|invalid|blocked|comparisonerror|syncerror|deletionerror|deleted)$|forbidden:|comparisonerror/i
const WARNING_RE = /^(progressing|pending|warning|degraded|outofsync|unknown|terminating)$/

/** Classify a phase label, status string, or short message. */
export function classifyOpsOutcome(value: string): OpsOutcomeKind {
  const v = value.trim().toLowerCase()
  if (v === '') return 'neutral'
  if (ERROR_RE.test(v)) return 'error'
  if (SUCCESS_RE.test(v)) return 'success'
  if (WARNING_RE.test(v)) return 'warning'
  return 'neutral'
}

/** Combine Argo operation phase + message — errors win over success. */
export function classifyArgoOperation(phase: string, message?: string): OpsOutcomeKind {
  const phaseKind = classifyOpsOutcome(phase)
  const messageKind = message != null && message !== '' ? classifyOpsOutcome(message) : 'neutral'

  if (phaseKind === 'error' || messageKind === 'error') return 'error'
  if (phaseKind === 'warning' || messageKind === 'warning') return 'warning'
  if (phaseKind === 'success') return 'success'
  if (messageKind === 'success') return 'success'
  return phaseKind !== 'neutral' ? phaseKind : messageKind
}

export function opsOutcomeTextClass(kind: OpsOutcomeKind): string {
  switch (kind) {
    case 'success':
      return 'lamp-ok'
    case 'warning':
      return 'lamp-degraded'
    case 'error':
      return 'lamp-fail'
    default:
      return 'text-[var(--muted-foreground)]'
  }
}

export function opsOutcomeTextClassFromValue(value: string): string {
  return opsOutcomeTextClass(classifyOpsOutcome(value))
}

export function opsErrorTextClass(): string {
  return 'lamp-fail'
}

export function opsInlineFeedbackClass(kind: 'success' | 'error'): string {
  return kind === 'success' ? 'lamp-ok' : 'lamp-fail'
}
