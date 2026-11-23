import { STATUS, STATUS_WORDS } from '@bifrost/ui/city/world'
import { describe, expect, it } from 'vitest'
import { formatGapSummaryLine, GAP_LEGEND, GAP_WORD } from '../observabilityFormat'

// Origin (2026-11-23, 箭10 on the Status screenshot): the Apollo legend and the reference chips used words of
// their own (blind, by-design, reference); the gap words are the engine's seven and the legend quotes its meanings.
describe('gap words', () => {
  it('maps every gap onto the seven words: blind cannot be judged, by design is not a fault', () => {
    for (const w of Object.values(GAP_WORD)) expect(STATUS_WORDS).toContain(w)
    expect(GAP_WORD.blind).toBe('unknown')
    expect(GAP_WORD.by_design).toBe('boundary')
  })

  it('the legend lists the words used, in the WorldFrame legend words, and none of the old ones', () => {
    for (const w of ['ok', 'fail', 'unknown', 'boundary'] as const) expect(GAP_LEGEND).toContain(STATUS[w].means)
    expect(GAP_LEGEND).not.toMatch(/blind|by-design|reference =/)
  })

  it('the card line speaks the same words', () => {
    expect(formatGapSummaryLine({ ok: 1, fail: 1, blind: 2, byDesign: 0, total: 4 }).line).toBe('1 ok · 1 fail · 2 unknown')
  })
})
