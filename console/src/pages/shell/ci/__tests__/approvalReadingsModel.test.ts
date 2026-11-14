import { describe, expect, it } from 'vitest'
import { MAX_LISTED_NUMBERS, approvalRows, approvalsDescription, approvalsView, numbersCell } from '../approvalReadingsModel'
import { contract } from './contract'

/*
 * Origin of every case in this file — 2026-11-14, the Owner: the Ops page is a window on the system and shows what the
 * system measured, with no drift from a ledger kept by hand; that day he was told by word how many cards he was asked
 * for and how many bought nothing, and the page showed none of it.
 */

describe('approval rows, from the contract', () => {
  // Origin: as above (2026-11-14, the Owner) — the rows are the route's days and requesters, newest day first.
  it('one row per day and requester, newest day first, the day shown once', () => {
    const rows = approvalRows(contract.approvals())
    expect(rows.map(r => [r.day, r.requester, r.showDay])).toEqual([
      ['2026-11-14', 'line-a', true], ['2026-11-14', 'line-b', false], ['2026-11-13', 'line-b', true],
    ])
    expect(rows[0]).toMatchObject({ label: 'line a', workIds: 'LANE-a, TD-9' })
    expect(rows[1]).toMatchObject({ label: 'line-b', workIds: '' })
  })

  // Origin: as above — every count he was told by word is on the page with the request numbers behind it.
  it('the first group: each column counts the numbers behind it', () => {
    const [row] = approvalRows(contract.approvals())
    const numbers = (cell: { count: number; shown: number[] }) => { expect(cell.count).toBe(cell.shown.length); return cell.shown }
    expect(numbers(row.requests)).toEqual([1, 2, 3, 4, 5, 6, 7])
    expect(numbers(row.approved)).toEqual([1, 2, 3, 7])
    expect(numbers(row.done)).toEqual([1])
    expect(numbers(row.boughtNothing)).toEqual([2, 3])
    expect(numbers(row.notApproved)).toEqual([4, 6])
    expect(numbers(row.open)).toEqual([5, 7])
    expect(numbers(row.drills)).toEqual([8])
    expect(row.timeToApprove).toBe('40s')
  })

  // Origin: as above — "bought nothing" is the count he asked for: an approval that expired afterwards is one of them.
  it('a request approved and then expired counts under Approved and Bought nothing, not under Not approved', () => {
    const row = approvalRows(contract.approvals())[1]
    expect(row.approved.shown).toEqual([9, 10])
    expect(row.boughtNothing.shown).toEqual([9])
    expect(row.notApproved.count).toBe(0)
    expect(row.done.shown).toEqual([10])
    expect(row.timeToApprove).toBe('30s')
  })

  // Origin: as above — no drift: the page says how far back the records reach, so a day not kept is not read as empty.
  it('the description says when it was computed, the zone, and that earlier days are not kept', () => {
    const text = approvalsDescription(contract.approvals())
    expect(text).toContain('Computed from the approval records at 2026-11-15 05:00Z; days are in America/Chicago; ')
    expect(text).toContain('11 records are kept, the oldest from 2026-11-13 23:00Z: earlier days are not kept, not empty.')
  })

  // Origin: as above — no record kept is said as that, not drawn as an empty table.
  it('no retained record gives the "No approval record is kept." state', () => {
    expect(approvalsView(contract.approvalsNone())).toEqual({ state: 'none' })
    expect(approvalsDescription(contract.approvalsNone())).toContain('0 records are kept.')
    expect(approvalsDescription(contract.approvalsNone())).not.toContain('oldest')
  })

  // Origin: as above — a long list is cut for reading, never for counting: the count and the title keep all of them.
  it('a cell lists the first 12 numbers and counts the rest', () => {
    const cell = numbersCell(Array.from({ length: 15 }, (_, i) => 15 - i))
    expect(cell.count).toBe(15)
    expect(cell.shown).toHaveLength(MAX_LISTED_NUMBERS)
    expect(cell.shown[0]).toBe(1)
    expect(cell.more).toBe(3)
    expect(cell.title.split(' ')).toHaveLength(15)
    expect(numbersCell([1, 2]).title).toBe('')
  })
})
