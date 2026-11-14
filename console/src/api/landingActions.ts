// The three approval actions of the retired landing lane (removed from the
// platform's catalog on 2026-11-13). No new request of them can be made; the
// plan pages still tell an old one apart from the requests they count.
export const LANDING_ACTIONS = ['approve_guardrail_landing', 'approve_core_landing', 'approve_floor_landing'] as const
export type LandingAction = typeof LANDING_ACTIONS[number]
export const isLandingAction = (v: unknown): v is LandingAction => LANDING_ACTIONS.some(a => a === v)
