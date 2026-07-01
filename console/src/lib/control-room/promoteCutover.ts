/**
 * Control Room Phase 5 — Promote / cutover go-no-go model (aligned with PromotePage).
 */

import type {
  MatrixResponse,
  OpsContextResponse,
  ReleaseGateResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { buildPromotePack } from '@/lib/control-room/agentContextPacks'
import {
  evaluatePromoteStatus,
  evaluateStgReleaseStatus,
  type PromoteStatus,
  type StgReleaseStatus,
} from '@/lib/control-room/matrixSummary'
import { type Signal } from '@/lib/control-room/missionSignals'

export const PROMOTE_PREFLIGHT_STORAGE_KEY = 'bifrost_promote_preflight_pack'

export type PromoteTrackSummary = {
  id: 'stg-release' | 'prod-cutover'
  label: string
  ready: boolean
  lamp: Signal
  detail: string
}

export type SpineBlockerAlignment = {
  focusHeadline: string | null
  focusBlocker: string | null
  milestoneBlocker: string | null
  /** focus.blocker matches cutover milestone blocker when BLOCKED_ON */
  aligned: boolean
  note: string
}

export type PromoteCutoverModel = {
  promote: PromoteStatus
  stgRelease: StgReleaseStatus | null
  prodLamp: Signal
  prodHeadline: string
  prodDetail: string
  tracks: PromoteTrackSummary[]
  spine: SpineBlockerAlignment
  preflightPack: string
  tierAItems: readonly string[]
  tierBItems: readonly string[]
}

export const PROMOTE_TIER_A_CHECKS = [
  'npm run lint',
  'npm run build',
  'npm run check:legacy-css',
  'Page-by-page Legacy equivalence (Phase 1)',
] as const

export const PROMOTE_TIER_B_CHECKS = [
  'bifrost-deliver-stg success + STG release gate pass',
  'Tier B Owner sign-off (IB / Massive manual)',
  'Prod cutover gate (blocked until D1)',
] as const

function prodLampFromPromote(promote: PromoteStatus): Signal {
  if (promote.ready) return 'ok'
  if (promote.blockedByDecision || promote.prodFails) return 'fail'
  return 'degraded'
}

function stgLampFromRelease(stg: StgReleaseStatus): Signal {
  if (stg.releaseReady) return 'ok'
  if (stg.smokeFails) return 'fail'
  return 'degraded'
}

/** Same verdict strings as PromotePage TrackSummary + banner. */
export function promoteProdHeadline(promote: PromoteStatus): string {
  return promote.ready ? 'Promote ready (narrative)' : 'Promote blocked'
}

export function promoteProdDetail(promote: PromoteStatus): string {
  return promote.ready ? 'Narrative ready' : promote.reasons[0] ?? 'Blocked'
}

export function buildSpineBlockerAlignment(
  context: OpsContextResponse,
  promote: PromoteStatus,
): SpineBlockerAlignment {
  const focusHeadline = context.focus.headline?.trim() || null
  const focusBlocker = context.focus.blocker?.trim() || null
  const milestoneBlocker = promote.cutoverBlocker?.trim() || null

  let aligned = true
  let note = 'Spine focus and coupling gate use the same promote evaluation.'

  if (promote.blockedByDecision && milestoneBlocker != null) {
    if (focusBlocker == null || focusBlocker === '') {
      aligned = false
      note = `Milestone blocked on ${milestoneBlocker} — spine focus has no blocker field.`
    } else {
      const same =
        focusBlocker === milestoneBlocker ||
        focusBlocker.includes(milestoneBlocker) ||
        milestoneBlocker.includes(focusBlocker)
      aligned = same
      note = same
        ? 'Spine focus blocker matches milestone BLOCKED_ON.'
        : `Spine focus blocker (“${focusBlocker}”) differs from milestone (“${milestoneBlocker}”).`
    }
  } else if (focusBlocker != null && focusBlocker !== '' && !promote.ready) {
    note = `Spine lists blocker “${focusBlocker}”; coupling gate: ${promote.reasons[0] ?? 'blocked'}.`
  }

  return {
    focusHeadline,
    focusBlocker,
    milestoneBlocker,
    aligned,
    note,
  }
}

export function buildPromoteCutoverModel(input: {
  context: OpsContextResponse
  matrices: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
}): PromoteCutoverModel {
  const { context, matrices, stgSmoke, stgGate, lastDeliverSucceeded = false, tierB } = input
  const promote = evaluatePromoteStatus(context, matrices)
  const stgRelease =
    stgSmoke != null
      ? evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded, stgGate, tierB)
      : null

  const tracks: PromoteTrackSummary[] = []

  if (stgRelease != null) {
    tracks.push({
      id: 'stg-release',
      label: 'STG release',
      ready: stgRelease.releaseReady,
      lamp: stgLampFromRelease(stgRelease),
      detail: stgRelease.releaseReady
        ? 'Deliver + smoke + STG gate + Tier B complete'
        : stgRelease.releaseReasons[0] ?? 'In progress',
    })
  }

  tracks.push({
    id: 'prod-cutover',
    label: 'Prod cutover',
    ready: promote.ready,
    lamp: prodLampFromPromote(promote),
    detail: promoteProdDetail(promote),
  })

  return {
    promote,
    stgRelease,
    prodLamp: prodLampFromPromote(promote),
    prodHeadline: promoteProdHeadline(promote),
    prodDetail: promoteProdDetail(promote),
    tracks,
    spine: buildSpineBlockerAlignment(context, promote),
    preflightPack: buildPromotePack(context, matrices),
    tierAItems: PROMOTE_TIER_A_CHECKS,
    tierBItems: PROMOTE_TIER_B_CHECKS,
  }
}

export function stashPromotePreflightPack(pack: string): void {
  try {
    sessionStorage.setItem(PROMOTE_PREFLIGHT_STORAGE_KEY, pack)
  } catch {
    // storage unavailable
  }
}

export function readPromotePreflightPack(): string | null {
  try {
    return sessionStorage.getItem(PROMOTE_PREFLIGHT_STORAGE_KEY)
  } catch {
    return null
  }
}

export function clearPromotePreflightPack(): void {
  try {
    sessionStorage.removeItem(PROMOTE_PREFLIGHT_STORAGE_KEY)
  } catch {
    // storage unavailable
  }
}
