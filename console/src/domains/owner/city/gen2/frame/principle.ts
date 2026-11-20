import { GRID, MAX_THINGS } from './grid'
import { LABEL_CHARS } from './labels'
import { PORTS } from './links'

/**
 * The frame's core idea, in the page itself (the Owner, 2026-11-20: it is the heart of this UI and must be seen
 * in it, not only in RULES.md). Bottom up, the system the agents build is as complex as it gets; top down, a
 * person takes in only so much. The two meet at a boundary: set the boundary first, then see what fits in it.
 * What does not fit does not come in, until the boundary grows or the detail is shown coarser.
 *
 * One source for every place the page says it (the help card, the refusal, a refused drop), and its numbers are
 * the frame's own constants, so the words cannot drift from what the frame does.
 */
export const PRINCIPLE = {
  title: 'Boundaries first',
  idea: 'Bottom up, the system the agents build can be as complex as it gets. Top down, a person takes in only so much. The map joins the two at a boundary: set the boundary first, then see what fits in it. What does not fit does not come in.',
  rules: [
    `A ground is ${GRID}×${GRID} and holds at most ${MAX_THINGS} things.`,
    `Each side has ${PORTS} named ports, the busiest partners of what stands there; the rest go through Other.`,
    'The land comes first and is shaped empty: tap an empty plot to join it with the one beside it, turn it or cut it; then move a building onto a plot that fits it.',
    'Each level sums up the one below. A bigger district has more sheets; step between them with [ and ].',
    `A label is one line of at most ${LABEL_CHARS} characters; the look of a building says what it is.`,
  ],
  /** What to do when something does not fit. */
  remedy: 'Give it a bigger boundary (a larger district, more sheets), or show it coarser (a ruler, a summary).',
  /** A building dropped on a plot too small for it. */
  noFit: 'Does not fit this plot. The land comes first: tap a plot to join, turn or cut it, then move the building.',
  /** Built land picked to reshape. */
  builtLand: 'A building stands here. Land is shaped empty: move the building off first.',
} as const
