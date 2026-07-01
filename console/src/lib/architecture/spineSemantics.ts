/**
 * Spine milestone status semantics — SIGNED/CLOSED = historical Owner sign-off;
 * live gate readiness comes from Projection (matrix / Promote evaluation).
 */

export const SPINE_STATUS_SEMANTICS_NOTE =
  'SIGNED = Owner historical sign-off; live gate readiness from Projection (matrix / Promote).'

export const SPINE_MILESTONE_STATUS_DEFINITIONS = [
  { status: 'SIGNED', meaning: 'Owner historically approved this milestone scope' },
  { status: 'CLOSED', meaning: 'Fully completed and archived (no further action)' },
  { status: 'IN_PROGRESS', meaning: 'Active work ongoing' },
  { status: 'NOT_STARTED', meaning: 'Planned, no work started' },
  { status: 'BLOCKED_ON', meaning: 'Blocked by named dependency' },
] as const

const HISTORICAL_STATUSES = new Set(['SIGNED', 'CLOSED'])

export function isHistoricalSpineStatus(status: string): boolean {
  return HISTORICAL_STATUSES.has(status)
}

/** Display label — clarifies SIGNED/CLOSED are historical spine states, not live gate verdicts. */
export function formatSpineStatusLabel(status: string): string {
  if (status === 'SIGNED') return 'SIGNED (historically)'
  if (status === 'CLOSED') return 'CLOSED (archived)'
  return status
}

export function formatGateProjectionLabel(gateReady: boolean): string {
  return gateReady ? 'gate: ready' : 'gate: pending'
}

export type MilestoneDualLabel = {
  spineLabel: string
  gateLabel: string
}

/** Dual labels when milestone is historically complete but Projection gate is not ready. */
export function resolveMilestoneDualLabels(
  milestoneStatus: string | undefined,
  gateReady: boolean,
): MilestoneDualLabel | null {
  if (milestoneStatus == null || !isHistoricalSpineStatus(milestoneStatus)) return null
  return {
    spineLabel: formatSpineStatusLabel(milestoneStatus),
    gateLabel: formatGateProjectionLabel(gateReady),
  }
}

export function shouldShowMilestoneDualLabels(
  milestoneStatus: string | undefined,
  gateReady: boolean,
): boolean {
  return resolveMilestoneDualLabels(milestoneStatus, gateReady) != null && !gateReady
}
