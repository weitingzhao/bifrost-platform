import { describe, expect, it } from 'vitest'
import { ageText, countsLine, minutesText, openCases, type ProblemCase } from '../busProblems'

const c = (event: string, letter_at: string, closed: boolean): ProblemCase => ({
  kind: 'alert', subject: 's', event, row: 'r', receiver: 'x', letter_at, acked_at: null, verdict: '', outcome: '', ref: '', verdict_by: '', verdict_at: null,
  closed, state: closed ? 'closed' : 'sent',
})

// 2026-11-18, LAW-8: the Bus page's problem loop shows only what the
// platform computed. Guards: closed cases are not in the open table, the
// longest-waiting open case is first, a median that is not measured reads
// "unknown" and not 0, and the counts line carries every count.
describe('problem loop section', () => {
  it('lists open cases oldest first and leaves closed ones out', () => {
    const got = openCases([c('ev-2', '2026-11-18T10:00:00Z', false), c('ev-1', '2026-11-18T09:00:00Z', false), c('ev-3', '2026-11-18T08:00:00Z', true)])
    expect(got.map(x => x.event)).toEqual(['ev-1', 'ev-2'])
  })
  it('says a median that is not measured is unknown, not zero', () => {
    expect(minutesText(null)).toBe('unknown')
    expect(minutesText(0)).toBe('0m')
    expect(minutesText(90)).toBe('2h')
    expect(minutesText(60 * 72)).toBe('3d')
    expect(ageText('2026-11-18T10:00:00Z', Date.parse('2026-11-18T10:30:00Z'))).toBe('30m')
  })
  it('puts every count in the line', () => {
    const line = countsLine({ raised: 5, seen: 4, judged: 3, real: 2, false: 1, closed: 1, open: 4, ack_median_minutes: 12, verdict_median_minutes: null })
    expect(line).toBe('raised 5 · seen 4 · judged 3 · real 2 · false 1 · closed 1 · open 4 · median to seen 12m · to verdict unknown · to closed unknown (1 case)')
  })
})
