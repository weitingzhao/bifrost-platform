import { describe, expect, it } from 'vitest'
import { clampLabel, LABEL_CHARS, LABEL_LINES, placeLabels, type Box } from '@bifrost/ui/city/labels'

// 事故 2026-11-20: in a 4×8 Utilities the names and the port signs lay on top of one another ("System"
// over "Work · bus 1" over "ubt-k3s-05"); it must be the program that keeps them apart, whatever moves.
const overlap = (a: Box, b: Box) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

describe('plates give way to one another', () => {
  it('leaves the fixed signs where they are and moves a name off one', () => {
    const sign = { x: 0, y: 0, w: 80, h: 20 }, name = { x: 20, y: 5, w: 90, h: 20 }
    const [p] = placeLabels([sign], [name])
    expect(p.free).toBe(true)
    expect(overlap({ ...name, y: name.y + p.dy }, sign)).toBe(false)
  })
  it('on any layout, every plate that found a place overlaps no other', () => {
    let seed = 7
    const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647
    for (let run = 0; run < 50; run++) {
      const fixed = Array.from({ length: 6 }, () => ({ x: rnd() * 600, y: rnd() * 400, w: 60 + rnd() * 80, h: 20 }))
      const names = Array.from({ length: 14 }, () => ({ x: rnd() * 600, y: rnd() * 400, w: 60 + rnd() * 120, h: rnd() < 0.5 ? 20 : 34 }))
      const at = placeLabels(fixed, names)
      const put = names.map((b, i) => ({ ...b, y: b.y + at[i].dy }))
      const placed = put.filter((_, i) => at[i].free)
      for (const a of placed) {
        for (const f of fixed) expect(overlap(a, f)).toBe(false)
        for (const b of placed) if (a !== b) expect(overlap(a, b)).toBe(false)
      }
    }
  })
  it('keeps a plate where it is, marked, when nothing within reach is free', () => {
    const wall = Array.from({ length: 9 }, (_, i) => ({ x: 0, y: (i - 4) * 23, w: 100, h: 20 }))
    const [p] = placeLabels(wall, [{ x: 10, y: 0, w: 50, h: 20 }])
    expect(p).toEqual({ dy: 0, free: false })
  })
})

// 2026-11-20 (ISSUE-9): a plate must have a ceiling on its size, or no placement can keep it from covering a stretch.
describe('a plate has a ceiling on its size', () => {
  it('cuts a long line at the limit with an ellipsis and keeps the whole text', () => {
    const t = clampLabel('mini-pc-a · ubt-k3s-02 · something very long')
    expect(t.cut).toBe(true)
    expect(t.text.length).toBeLessThanOrEqual(LABEL_CHARS)
    expect(t.text.endsWith('…')).toBe(true)
    expect(t.full).toBe('mini-pc-a · ubt-k3s-02 · something very long')
  })
  // The Owner, later that day: one line is enough, 16 or 18 characters.
  it('keeps one line, folding the rest into it, within the limit', () => {
    const t = clampLabel('Utilities\nProd Pool · 21 pods\n+2 kinds not shown')
    expect(LABEL_LINES).toBe(1)
    expect(t.text.split('\n')).toHaveLength(1)
    expect(t.text.length).toBeLessThanOrEqual(LABEL_CHARS)
    expect(t.text.startsWith('Utilities · ')).toBe(true)
  })
  it('leaves a short plate as it is', () => {
    expect(clampLabel('ubt-k3s-07')).toEqual({ text: 'ubt-k3s-07', full: 'ubt-k3s-07', cut: false })
  })
})
