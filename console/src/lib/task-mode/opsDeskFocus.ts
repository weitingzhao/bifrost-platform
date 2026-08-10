import type { Reachability } from '@bifrost/ui'

/**
 * Daily Ops TCC Body focus — Summary chips under Verdict.
 * Agent · Release · Environment (plus All). Not a second dashboard.
 */
export type OpsDeskFocus = 'all' | 'agent' | 'release' | 'environment'

export type OpsDeskFocusCategory = Exclude<OpsDeskFocus, 'all'>

export const OPS_DESK_FOCUS_CATEGORIES: readonly OpsDeskFocusCategory[] = [
  'agent',
  'release',
  'environment',
] as const

export const OPS_DESK_FOCUS_LABEL: Record<OpsDeskFocusCategory, string> = {
  agent: 'Agent',
  release: 'Release',
  environment: 'Environment',
}

const REACH_RANK: Record<Reachability, number> = {
  ok: 0,
  unknown: 1,
  degraded: 2,
  fail: 3,
}

export function worseReachability(a: Reachability, b: Reachability): Reachability {
  return REACH_RANK[a] >= REACH_RANK[b] ? a : b
}

/** Whether a Body bucket should render for the active Summary focus. */
export function opsDeskFocusShows(
  focus: OpsDeskFocus,
  category: OpsDeskFocusCategory,
): boolean {
  return focus === 'all' || focus === category
}

export type OpsDeskFocusChip = {
  id: OpsDeskFocusCategory
  label: string
  lamp: Reachability
  summary: string
  attention: boolean
}

export function buildOpsDeskFocusChip(
  id: OpsDeskFocusCategory,
  lamp: Reachability,
  summary: string,
): OpsDeskFocusChip {
  return {
    id,
    label: OPS_DESK_FOCUS_LABEL[id],
    lamp,
    summary,
    attention: lamp === 'fail' || lamp === 'degraded',
  }
}
