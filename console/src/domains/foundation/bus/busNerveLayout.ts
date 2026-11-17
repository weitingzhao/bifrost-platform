/**
 * Names the nerve map's body shares (2026-11-17). The layout itself is the
 * graph library's force simulation (BusNerveGraph); this keeps only what the
 * section and the renderer agree on.
 */

/** The bus itself: the spine every organ hangs on. */
export const CORE = 'core:bus'

/** A topic's domain when the register names none: the part of its type before the first dot ("ci.run.finished" → "ci"). */
export function domainOf(type: string): string {
  const i = type.indexOf('.')
  return i < 0 ? type : type.slice(0, i)
}
