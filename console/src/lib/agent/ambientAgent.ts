/** Shell-level ambient agent — survives Console tab switches. */

export type AmbientAgentJob = {
  id: string
  scope: string
  label: string
}

export type AmbientAgentShellProps = {
  ambientJobId?: string | null
  /** Scope of the ambient job (e.g. trade-deploy) — used to open Launch Live View. */
  ambientJobScope?: string | null
  onStartAgentJob?: (job: AmbientAgentJob) => void
  /** Expand the shell Agent Execution Dock (in-place Fix) — do not force Agent Desk tab. */
  onExpandAgentDock?: () => void
}

export function ambientAgentBlockedReason(
  canOperate: boolean,
  ambientJobId: string | null | undefined,
  onStartAgentJob?: unknown,
): string | undefined {
  if (!canOperate) return 'Operator token required'
  if (ambientJobId != null) return 'Agent task already running — expand the execution dock'
  if (onStartAgentJob == null) return 'Ambient agent shell not available'
  return undefined
}

export function isAmbientAgentActive(ambientJobId: string | null | undefined): boolean {
  return ambientJobId != null && ambientJobId !== ''
}
