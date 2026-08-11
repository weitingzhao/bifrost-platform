import type { ReleaseCycleStepView, ReleaseCycleView } from '@/api/deliveryTypes'

const STEP_LABEL: Record<string, string> = {
  stg_deploy: 'Staging Deploy',
  stg_gate: 'Staging Gate',
  prod_deploy: 'Production Deploy',
  prod_gate: 'Production Gate',
}

function durationSeconds(started?: string, completed?: string): number | null {
  if (!started) return null
  const start = new Date(started).getTime()
  const end = completed ? new Date(completed).getTime() : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null
  return Math.round((end - start) / 1000)
}

function exportStep(step: ReleaseCycleStepView) {
  return {
    kind: step.kind,
    label: STEP_LABEL[step.kind] ?? step.kind,
    duration_s: durationSeconds(step.started_at, step.completed_at),
    result: step.result ?? '',
    run_name: step.run_name ?? '',
    detail: step.detail ?? '',
    checks: (step.gate_checks ?? []).map(c => ({
      id: c.id,
      label: c.label,
      required: c.required,
      reachability: c.reachability,
      detail: c.detail,
    })),
  }
}

/**
 * Structured JSON export of a release cycle for pasting into an AI assistant
 * to analyze CI/CD improvement opportunities.
 */
export function buildCycleExportBundle(cycle: ReleaseCycleView): string {
  const duration = durationSeconds(cycle.started_at, cycle.completed_at)
  const payload = {
    cycle_id: cycle.id,
    lane: cycle.lane,
    revision: cycle.revision,
    outcome: cycle.outcome,
    duration_seconds: duration,
    started_at: cycle.started_at,
    completed_at: cycle.completed_at ?? null,
    triggered_by: cycle.triggered_by ?? null,
    agent_session_id: cycle.agent_session_id ?? null,
    steps: (cycle.steps ?? []).map(exportStep),
    analysis_prompt:
      'Analyze this release cycle for CI/CD improvement opportunities. ' +
      'Focus on stage durations, failures/retries, gate check patterns, and concrete process or pipeline changes.',
  }
  return JSON.stringify(payload, null, 2)
}

export function formatCycleDuration(cycle: ReleaseCycleView): string {
  const secs = durationSeconds(cycle.started_at, cycle.completed_at)
  if (secs == null) return '—'
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  const s = secs % 60
  if (m < 60) return s > 0 ? `${m}m ${s}s` : `${m}m`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm > 0 ? `${h}h ${rm}m` : `${h}h`
}

export function cycleStepLabel(kind: string): string {
  return STEP_LABEL[kind] ?? kind
}
