/**
 * Cross-page navigation helpers for Engineer → Delivery pipeline:
 * Agent Briefing (Plan) → Active Session (Execute) → Delivery Board (Archive).
 */
import {
  BRIEFING_LANE_PARAM,
  BRIEFING_PROGRAM_PARAM,
  writeBriefingUrlState,
  type BriefingUrlState,
} from '@/lib/briefing/briefingUrlState'
import { isLaneId, type LaneId } from '@/lib/briefing/workLanes'

export const DELIVERY_TAB_BRIEFING = 'briefing' as const
export const DELIVERY_TAB_ACTIVE_SESSION = 'active-session' as const
export const DELIVERY_TAB_DELIVERY_BOARD = 'delivery-board' as const

export type DeliveryPipelineTab =
  | typeof DELIVERY_TAB_BRIEFING
  | typeof DELIVERY_TAB_ACTIVE_SESSION
  | typeof DELIVERY_TAB_DELIVERY_BOARD

export interface ActiveSessionFocus {
  laneId?: LaneId
  programId?: string
}

/** Read `lane` (shared with Briefing) from the current URL. */
export function parseActiveSessionFocus(
  url: URL = new URL(window.location.href),
): ActiveSessionFocus {
  const lane = url.searchParams.get(BRIEFING_LANE_PARAM)
  const program = url.searchParams.get(BRIEFING_PROGRAM_PARAM)
  return {
    laneId: lane != null && isLaneId(lane) ? lane : undefined,
    programId: program != null && program.trim() !== '' ? program.trim() : undefined,
  }
}

/** Persist Active Session focus via shared Briefing URL params (lane / program). */
export function writeActiveSessionFocus(
  focus: ActiveSessionFocus,
  url: URL = new URL(window.location.href),
): void {
  writeBriefingUrlState(
    {
      lane: focus.laneId,
      program: focus.programId,
    },
    url,
  )
}

export function buildActiveSessionDeepLink(focus: ActiveSessionFocus = {}): string {
  const url = new URL(window.location.href)
  url.hash = `#${DELIVERY_TAB_ACTIVE_SESSION}`
  writeActiveSessionFocus(focus, url)
  return `${url.pathname}${url.search}${url.hash}`
}

export function buildBriefingDeepLinkFromFocus(opts: BriefingUrlState): string {
  const url = new URL(window.location.href)
  url.hash = `#${DELIVERY_TAB_BRIEFING}`
  writeBriefingUrlState(opts, url)
  return `${url.pathname}${url.search}${url.hash}`
}

export function buildDeliveryBoardDeepLink(opts?: {
  laneId?: LaneId
  scope?: string
  trackType?: string
}): string {
  const url = new URL(window.location.href)
  const params = new URLSearchParams()
  if (opts?.laneId) params.set('lane_id', opts.laneId)
  if (opts?.scope && opts.scope !== 'all') params.set('scope', opts.scope)
  if (opts?.trackType) params.set('tt', opts.trackType)
  const q = params.toString()
  url.hash = q ? `#${DELIVERY_TAB_DELIVERY_BOARD}?${q}` : `#${DELIVERY_TAB_DELIVERY_BOARD}`
  return `${url.pathname}${url.search}${url.hash}`
}
