import { STATUS_WORDS, type StatusWord } from '@bifrost/ui/city/world'

/**
 * The Console's state tags speak the engine's closed seven words (ok / warn / fail / boundary / unknown /
 * pending / info: `STATUS_WORDS` in @bifrost/ui/city/world), the same the WorldFrame legend, the City, Signals
 * and Agents show, so a state keeps its name when you change place (箭10 2026-11-23). The table is the engine's;
 * the Console only maps its own states onto it. What exactly is meant (Healthy, policy drift, off on purpose)
 * goes in the sentence beside the tag, never into the tag.
 */
export type { StatusWord }
export type Reach = 'ok' | 'degraded' | 'fail' | 'unknown'
export type TagVariant = 'success' | 'warning' | 'danger' | 'neutral'

/** The seven words by name (`STATE_WORD.warn` is 'warn'), so a typo is a type error, not a new word. */
export const STATE_WORD = Object.fromEntries(STATUS_WORDS.map(w => [w, w])) as { readonly [W in StatusWord]: W }

/** The tag colour of a word: severity only; boundary, unknown, pending and info are not findings. */
export const TAG_VARIANT: Record<StatusWord, TagVariant> = {
  ok: 'success', warn: 'warning', fail: 'danger', boundary: 'neutral', unknown: 'neutral', pending: 'neutral', info: 'neutral',
}

/** A probe's reach in the seven words. */
export const WORD_OF_REACH: Record<Reach, StatusWord> = { ok: 'ok', degraded: 'warn', fail: 'fail', unknown: 'unknown' }

/** The lamp, word and colour of a reach, or pending while the read has not answered. */
export function reachTag(reach: Reach, loading = false): { lamp: Reach; tagLabel: StatusWord; tagVariant: TagVariant } {
  if (loading) return { lamp: 'unknown', tagLabel: 'pending', tagVariant: 'neutral' }
  const word = WORD_OF_REACH[reach]
  return { lamp: reach, tagLabel: word, tagVariant: TAG_VARIANT[word] }
}
