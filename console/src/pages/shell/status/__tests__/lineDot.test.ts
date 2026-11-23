import { describe, expect, it } from 'vitest'
import type { LineRow } from '@/api/seatStatus'
import { lineDot, linesTitle } from '../linesNowModel'

// Origin (2026-11-23, Console line's design-system inventory, first page: Work → Activity drew its dots in seven raw
// colours with private words): the dot is the engine's seven words; running is activity (info), not health.
const row = (over: Partial<LineRow>): LineRow => ({ session: 's', title: 't', ownerCards: 0, commits: { written: 0, main: 0, stg: 0, prod: 0 }, ...over } as LineRow)

describe('lineDot', () => {
  it('waiting on someone is warn, running info, idle boundary, no heartbeat unknown', () => {
    expect(lineDot(row({ ownerCards: 1 }))).toEqual({ word: 'warn', note: 'waiting' })
    expect(lineDot(row({ thread: { status: 'in_turn' } as LineRow['thread'] }))).toEqual({ word: 'info', note: 'running' })
    expect(lineDot(row({ thread: { status: 'idle' } as LineRow['thread'] }))).toEqual({ word: 'boundary', note: 'idle' })
    expect(lineDot(row({}))).toEqual({ word: 'unknown', note: 'no heartbeat' })
  })
})

// 2026-11-23, guest view on PROD: "Lines · 0 reporting · 30 not yet · 0 for you" while statuses and cards were not read.
describe('linesTitle', () => {
  it('says not read instead of a 0 that never arrived', () => {
    expect(linesTitle({ statusesRead: false, cardsRead: false, reporting: 0, notYet: 30, forOwner: 0 })).toBe('Lines · statuses not read · cards not read')
    expect(linesTitle({ statusesRead: true, cardsRead: true, reporting: 4, notYet: 2, forOwner: 1 })).toBe('Lines · 4 reporting · 2 not yet · 1 for you')
  })
})
