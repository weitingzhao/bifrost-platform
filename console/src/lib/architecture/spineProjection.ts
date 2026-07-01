/**
 * Spine-driven Projection — resolve catalog phase status from GET /api/v1/context.
 * Constitution catalogs hold structure only; live progress comes from spine milestones.
 */

import type { OpsContextResponse } from '@/api/types'
import type {
  DeliveryPhaseStatus,
  DeliveryReleasePhase,
  DeliveryReleasePhaseDefinition,
} from './deliveryMainlineCatalog'

const COMPLETE_MILESTONE_STATUSES = new Set(['CLOSED', 'SIGNED'])

/** STG release track signed off via this spine milestone. */
export const STG_DELIVER_MILESTONE_ID = 'k3s-stg-v2-deliver'

/** Prod cutover phase maps to this spine milestone. */
export const PROD_CUTOVER_MILESTONE_ID = '2c-b-prod-cutover'

export function findSpineMilestone(
  context: OpsContextResponse | undefined,
  milestoneId: string,
) {
  return context?.milestones.find(m => m.id === milestoneId)
}

export function milestoneToDeliveryPhaseStatus(
  spineStatus: string | undefined,
): DeliveryPhaseStatus {
  if (spineStatus == null || spineStatus === '') return 'planned'
  if (COMPLETE_MILESTONE_STATUSES.has(spineStatus)) return 'done'
  if (spineStatus === 'IN_PROGRESS') return 'active'
  if (spineStatus === 'BLOCKED_ON') return 'blocked'
  return 'planned'
}

function resolveStgTrackPhaseStatus(
  phaseId: string,
  stgMilestoneStatus: string | undefined,
): DeliveryPhaseStatus {
  const stgComplete =
    stgMilestoneStatus != null && COMPLETE_MILESTONE_STATUSES.has(stgMilestoneStatus)
  if (stgComplete) return 'done'
  if (phaseId === 'deliver-stg' || phaseId === 'stg-gate') {
    return milestoneToDeliveryPhaseStatus(stgMilestoneStatus)
  }
  if (phaseId === 'verify-stg') {
    return stgMilestoneStatus === 'IN_PROGRESS' ? 'active' : 'done'
  }
  return 'done'
}

/** Merge spine milestone status into delivery release phase definitions. */
export function resolveStgReleasePhases(
  definitions: DeliveryReleasePhaseDefinition[],
  context?: OpsContextResponse,
): DeliveryReleasePhase[] {
  const stgMilestone = findSpineMilestone(context, STG_DELIVER_MILESTONE_ID)
  const cutoverMilestone = findSpineMilestone(context, PROD_CUTOVER_MILESTONE_ID)

  return definitions.map(def => {
    if (def.id === 'prod-cutover') {
      return {
        ...def,
        status: milestoneToDeliveryPhaseStatus(cutoverMilestone?.status),
        spineMilestoneId: PROD_CUTOVER_MILESTONE_ID,
        spineStatus: cutoverMilestone?.status,
      }
    }
    return {
      ...def,
      status: resolveStgTrackPhaseStatus(def.id, stgMilestone?.status),
      spineMilestoneId: STG_DELIVER_MILESTONE_ID,
      spineStatus: stgMilestone?.status,
    }
  })
}
