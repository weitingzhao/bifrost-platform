/**
 * Lane detail purpose contract — Launch Rocket / Deploy Satellite / Launch Plugin detail pages.
 *
 * Mission Launch TCC stays the mission cockpit and verdict SSOT. Detail pages
 * only serve single-lane operate & evidence work. This module defines the
 * minimal "why you're here" contract those pages share, plus an optional
 * `#<tab>?detail=<reason>` hash deep-link parser. When no reason is present
 * (all current TCC links), the contract falls back to `direct`.
 */

export const LANE_DETAIL_REASONS = [
  'manual-deploy',
  'manual-gate',
  'failed-run',
  'acceptance-detail',
  'audit-history',
  'recovery',
  'direct',
] as const

export type LaneDetailReason = (typeof LANE_DETAIL_REASONS)[number]

/** Hash query param carrying the deep-link reason: `#trade-release?detail=failed-run`. */
export const LANE_DETAIL_QUERY_PARAM = 'detail'

/** Shared page subtitle for both lane detail pages. */
export const LANE_DETAIL_SUBTITLE =
  'Lane operate & evidence · Primary launch workflow remains in Mission Launch TCC'

export interface LaneDetailReasonCopy {
  label: string
  description: string
}

export const LANE_DETAIL_REASON_COPY: Record<LaneDetailReason, LaneDetailReasonCopy> = {
  'manual-deploy': {
    label: 'Manual deploy',
    description: 'Run a single-lane deploy and follow its evidence here.',
  },
  'manual-gate': {
    label: 'Manual gate',
    description: 'Run or inspect a release gate for this lane.',
  },
  'failed-run': {
    label: 'Failed run triage',
    description: 'Inspect the failing step evidence, then recover or re-run.',
  },
  'acceptance-detail': {
    label: 'Acceptance evidence',
    description: 'Review smoke / checklist acceptance evidence for the selected step.',
  },
  'audit-history': {
    label: 'Audit history',
    description: 'Review historical gate and deploy evidence for this lane.',
  },
  recovery: {
    label: 'Recovery',
    description: 'Use advanced recovery paths for this lane.',
  },
  direct: {
    label: 'Direct navigation',
    description:
      'Single-lane manual deploy/gate, step evidence, and recovery. Mission-level launch stays in Mission Launch TCC.',
  },
}

function isLaneDetailReason(value: string): value is LaneDetailReason {
  return (LANE_DETAIL_REASONS as readonly string[]).includes(value)
}

/** Pure parser — resolve the detail reason from a `#tab?detail=<reason>` hash. */
export function parseLaneDetailReason(hash: string): LaneDetailReason {
  const raw = hash.replace(/^#/, '')
  const qIdx = raw.indexOf('?')
  if (qIdx < 0) return 'direct'
  try {
    const params = new URLSearchParams(raw.slice(qIdx + 1))
    const reason = params.get(LANE_DETAIL_QUERY_PARAM)
    if (reason != null && isLaneDetailReason(reason)) return reason
  } catch {
    // malformed query — fall through to direct
  }
  return 'direct'
}

/** Read the detail reason from the current window location (default `direct`). */
export function readLaneDetailReasonFromLocation(): LaneDetailReason {
  return parseLaneDetailReason(window.location.hash)
}
