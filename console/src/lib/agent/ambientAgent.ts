/** Shell-level ambient agent — survives Console tab switches. */

export type AmbientAgentJob = {
  id: string
  scope: string
  label: string
  /**
   * Optional status from Recent / jobs list.
   * Terminal statuses skip live SSE and load archive snapshot in the dock.
   */
  status?: 'running' | 'done' | 'failed' | 'cancelled'
}

export type AmbientAgentShellProps = {
  ambientJobId?: string | null
  /** Scope of the ambient job (e.g. trade-deploy) — used to open Launch Live View. */
  ambientJobScope?: string | null
  /**
   * When the dock still shows a completed archive job, new Agent CTAs must stay enabled.
   * Omit / undefined while live ⇒ treat as in-flight when ambientJobId is set.
   */
  ambientJobStatus?: AmbientAgentJob['status'] | null
  onStartAgentJob?: (job: AmbientAgentJob) => void
  /** Expand the shell Agent Execution Dock (in-place Fix) — do not force Agent Desk tab. */
  onExpandAgentDock?: () => void
}

export function isAmbientAgentTerminal(
  status: AmbientAgentJob['status'] | null | undefined,
): boolean {
  return status === 'done' || status === 'failed' || status === 'cancelled'
}

export function ambientAgentBlockedReason(
  canOperate: boolean,
  ambientJobId: string | null | undefined,
  onStartAgentJob?: unknown,
  ambientJobStatus?: AmbientAgentJob['status'] | null,
): string | undefined {
  if (!canOperate) return 'Operator token required'
  if (ambientJobId != null && !isAmbientAgentTerminal(ambientJobStatus)) {
    return 'Agent task already running — expand the execution dock'
  }
  if (onStartAgentJob == null) return 'Ambient agent shell not available'
  return undefined
}

export function isAmbientAgentActive(
  ambientJobId: string | null | undefined,
  ambientJobStatus?: AmbientAgentJob['status'] | null,
): boolean {
  if (ambientJobId == null || ambientJobId === '') return false
  if (isAmbientAgentTerminal(ambientJobStatus)) return false
  return true
}
