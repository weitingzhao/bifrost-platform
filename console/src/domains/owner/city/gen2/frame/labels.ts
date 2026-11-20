/**
 * Where the plates go so none covers another (the Owner, 2026-11-20: labels of any size, on blocks that move;
 * a program, not hand-placed). Pure, so it is tested on its own and runs on every draw:
 *   fixed    keep their places (the port signs: the district's edge, its contract)
 *   movable  each, in the order given, takes the first free place in its own column: where it is, then one
 *            row down, one up, two down, ... up to `reach` rows; a row is its own height plus the gap
 *   none     free within reach: it stays where it is and is marked, to be drawn see-through
 */
export type Box = { x: number; y: number; w: number; h: number }
export type Placement = { dy: number; free: boolean }

export function placeLabels(fixed: readonly Box[], movable: readonly Box[], gap = 3, reach = 3): Placement[] {
  const hits = (a: Box, b: Box) => a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap
  const taken: Box[] = [...fixed]
  const tries = [0, ...Array.from({ length: reach }, (_, i) => [i + 1, -(i + 1)]).flat()]
  return movable.map(b0 => {
    const step = b0.h + gap
    for (const k of tries) {
      const b = { ...b0, y: b0.y + k * step }
      if (!taken.some(t => hits(b, t))) { taken.push(b); return { dy: k * step, free: true } }
    }
    taken.push(b0)
    return { dy: 0, free: false }
  })
}

/**
 * A plate's size has a ceiling (the Owner, 2026-11-20: however well placed, a plate too long covers a whole
 * stretch): one line of at most LABEL_CHARS characters (the Owner, the same day: one line is enough, 16 or 18
 * characters); further lines are folded into it, what is cut ends in "…", and the whole text is there on hover.
 */
export const LABEL_CHARS = 18
export const LABEL_LINES = 1
export type LabelText = { text: string; full: string; cut: boolean }
export function clampLabel(text: string, chars = LABEL_CHARS, lines = LABEL_LINES): LabelText {
  const raw = text.split('\n')
  const kept = [...raw.slice(0, lines - 1), raw.slice(lines - 1).join(' · ')].filter((l, i) => l !== '' || i === 0)
  const fit = (l: string) => l.length > chars ? `${l.slice(0, chars - 1).trimEnd()}…` : l
  const out = kept.map(fit).join('\n')
  return { text: out, full: text, cut: out !== text }
}

/** How plates show (the Owner, 2026-11-20): all of them; dimmed, so the city shows through; or only on hover. */
export type LabelMode = 'all' | 'dim' | 'hover'
export const LABEL_MODES: readonly LabelMode[] = ['all', 'dim', 'hover']
