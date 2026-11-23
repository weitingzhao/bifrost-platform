/**
 * The Ops design system's state words (.design-sync/project/guidelines/state-vocabulary.md): one table for the
 * Console's tags, Title case like the vocabulary, colour by severity only. Before 2026-11-23 each page spelled
 * its own (OK / FAIL / HEALTHY / CAUTION / NOMINAL / UNREACHABLE…, 箭10's unified-words slice).
 *
 * `Not observed` and `Expected off` are not in the vocabulary yet; they say something its words do not
 * (an optional contract nobody registered, a node off on purpose) and are asked for, not invented here.
 */
export type Reach = 'ok' | 'degraded' | 'fail' | 'unknown'
export type TagVariant = 'success' | 'warning' | 'danger' | 'neutral'

export const STATE_WORD = {
  ok: 'Healthy',
  ready: 'Ready',
  degraded: 'Degraded',
  fail: 'Failed',
  unknown: 'Unknown',
  /** The read has not answered yet: never a finding (README §2, "not arrived is not a finding"). */
  pending: '…',
  notObserved: 'Not observed',
  expectedOff: 'Expected off',
} as const

const VARIANT: Record<Reach, TagVariant> = { ok: 'success', degraded: 'warning', fail: 'danger', unknown: 'neutral' }

/** The lamp, word and colour of a reach, or the pending tag while it loads. */
export function reachTag(reach: Reach, loading = false): { lamp: Reach; tagLabel: string; tagVariant: TagVariant } {
  if (loading) return { lamp: 'unknown', tagLabel: STATE_WORD.pending, tagVariant: 'neutral' }
  return { lamp: reach, tagLabel: STATE_WORD[reach], tagVariant: VARIANT[reach] }
}
