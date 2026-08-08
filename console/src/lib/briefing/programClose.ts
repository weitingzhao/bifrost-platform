import type { ProgramSummary } from '@/api/programsTypes'

/** Fields needed to evaluate Delivery close predicates. */
export type ProgramCloseFields = Pick<
  ProgramSummary,
  | 'complete'
  | 'signed'
  | 'phases_signed'
  | 'sign_off_required_count'
  | 'phase_count'
  | 'phases_done'
  | 'assessment_status'
  | 'requires_post_completion'
>

const CATALOG_COMPLETE_ASSESSMENT = new Set(['no_handoff', 'closed'])
const SESSION_RELEASED_ASSESSMENT = new Set(['no_handoff', 'closed', 'approved', 'in_operate'])

function assessmentOf(p: ProgramCloseFields): string {
  return (p.assessment_status ?? '').trim().toLowerCase()
}

/**
 * gatesComplete = existing API `complete` (gates signed, else all phases done).
 * Keep in sync with api/internal/devagent/close_predicate.go.
 */
export function isGatesComplete(p: ProgramCloseFields): boolean {
  const signed = p.signed ?? p.phases_signed ?? 0
  const gates = p.sign_off_required_count ?? p.phase_count
  return (
    p.complete === true ||
    (gates > 0 ? signed === gates : p.phase_count > 0 && p.phases_done === p.phase_count)
  )
}

/**
 * catalogComplete (Delivery Board Complete band):
 *   if requires_post_completion: assessment ∈ {no_handoff, closed}
 *   else: gatesComplete
 */
export function isProgramCatalogComplete(p: ProgramCloseFields): boolean {
  if (!isGatesComplete(p)) return false
  if (p.requires_post_completion) {
    return CATALOG_COMPLETE_ASSESSMENT.has(assessmentOf(p))
  }
  return true
}

/**
 * sessionReleased (lane may leave Active Session Doing):
 *   if requires_post_completion: assessment ∈ {no_handoff, closed, approved, in_operate}
 *   else: gatesComplete
 * pending_review or empty assessment + requires_post_completion → NOT released.
 */
export function isProgramSessionReleased(p: ProgramCloseFields): boolean {
  if (!isGatesComplete(p)) return false
  if (p.requires_post_completion) {
    return SESSION_RELEASED_ASSESSMENT.has(assessmentOf(p))
  }
  return true
}

/** Alias of catalogComplete — Board / catalog close, not session release. */
export function isProgramDeliveryClosed(p: ProgramCloseFields): boolean {
  return isProgramCatalogComplete(p)
}

export type BoardCloseTag = 'close_pending' | 'in_operate' | null

/** In-progress Board tag when gates are done but catalog is not Complete. */
export function boardCloseTag(p: ProgramCloseFields): BoardCloseTag {
  if (isProgramCatalogComplete(p) || !isGatesComplete(p) || !p.requires_post_completion) {
    return null
  }
  const a = assessmentOf(p)
  if (a === 'in_operate' || a === 'approved') return 'in_operate'
  return 'close_pending'
}
