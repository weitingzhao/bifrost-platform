import type { ObservationPanel } from '@bifrost/ui/city/observation'
import type { Word } from '@bifrost/ui/city/cards'
import { statusOfWord } from '@bifrost/ui/city/world'

/**
 * A failed read in words the Owner reads (2026-11-23, 箭10: every domain card showed
 * "SyntaxError: Unexpected token '<'"). The card says unknown and why in plain English;
 * the source's own text stays in the evidence drawer (ReadEvidence.detail), never dropped.
 * The reason classifies the failure only; it does not judge the source's health.
 */
export function plainReason(raw: string | undefined): string {
  const t = raw ?? ''
  if (/\b401\b|viewer token|unauthori[sz]ed|sign(ed)?[- ]in/i.test(t)) return 'needs a signed-in viewer'
  if (/\b403\b|forbidden|not allowed/i.test(t)) return 'this viewer may not read it'
  if (/SyntaxError|Unexpected token|not valid JSON/i.test(t)) return 'the source did not answer with data'
  if (/Failed to fetch|NetworkError|fetch failed|ECONN|unreachable/i.test(t)) return 'the source could not be reached'
  if (/timed? ?out|AbortError|aborted/i.test(t)) return 'the read timed out'
  if (/\b5\d\d\b/.test(t)) return 'the source reported an error'
  if (/Incomplete|unavailable/i.test(t)) return 'the source answer was incomplete'
  return 'the read failed'
}

/** The card's sign for a failed read: unknown, the plain reason, and where the source text is. */
export function unknownNote(raw: string | undefined): ObservationPanel['blocks'][number] {
  const text = 'Unknown — ' + plainReason(raw) + '.'
  return { kind: 'note', text, title: text + ' The source text is in evidence.', tone: 'unknown' }
}

/**
 * A source's verdict as a tag (箭10 2026-11-23, brand layer item 5): the tag is one of the Ops seven words, mapped by
 * the engine's one table (statusOfWord; a word it does not know is unknown, never ok); the source's own word goes into
 * the sentence beside it and the tooltip. Colour is severity only.
 */
export function statusTag(source: string | undefined | null, why?: string): Word {
  const w = statusOfWord(source)
  return { text: w, tone: w, title: (source ? 'Source says ' + source : 'Source said nothing') + (why ? ': ' + why : '') }
}
