/** A failed read's line as the Console says it (review 488 B1): the reason, then when it was last read, or that it
 * has not been since the page opened. read picks which; either takes both. */
export function unavailableLine(reason: string, read: 'last' | 'never' | 'either' = 'either'): RegExp {
  const escaped = reason.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const last = 'last read \\d\\d:\\d\\d \\(.+\\)'
  const never = 'not read since this page opened at \\d\\d:\\d\\d'
  return new RegExp(`^${escaped}; (${read === 'last' ? last : read === 'never' ? never : `${last}|${never}`})\\.$`)
}
