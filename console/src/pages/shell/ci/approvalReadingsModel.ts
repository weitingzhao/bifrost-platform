import type { ApprovalReadings, ApprovalReadingsGroup } from '@/api/readings'
import { formatInstant } from '@/pages/shell/releases/releaseView'
import { formatDuration } from './ciViewModel'

/*
 * Work > CI > Approvals, as rows and strings. Everything here is computed from what the readings route answers:
 * every count keeps the request numbers behind it. Nothing is ranked and nothing is judged.
 */

export const NO_APPROVAL_RECORD_TEXT = 'No approval record is kept.'
export const APPROVALS_UNREAD_TEXT = 'How many requests were made, and what became of them, is not known.'
export const APPROVAL_COLUMNS = [
  'Day', 'Requester', 'Requests', 'Approved', 'Time to approve', 'Done', 'Bought nothing', 'Not approved', 'Open', 'Drills',
] as const
/** A cell lists at most this many request numbers; the rest are counted, and all of them are in its title. */
export const MAX_LISTED_NUMBERS = 12

const WHAT = 'What was asked for approval, by day and by who asked: how many requests, how many were approved and how fast, and what the approved ones came to.'

/** What the block is, and how far back it can see: days before the oldest kept record are not kept, not empty. */
export function approvalsDescription(readings: ApprovalReadings | undefined): string {
  if (readings == null) return WHAT
  const kept = `${readings.retained} ${readings.retained === 1 ? 'record is' : 'records are'} kept`
  const reach = readings.oldest != null && readings.oldest !== ''
    ? `${kept}, the oldest from ${formatInstant(readings.oldest)}: earlier days are not kept, not empty.`
    : `${kept}.`
  return `${WHAT} Computed from the approval records at ${formatInstant(readings.as_of)}; days are in ${readings.zone}; ${reach}`
}

export type NumbersCell = {
  count: number
  /** The request numbers shown, ascending. */
  shown: number[]
  /** How many are counted and not shown. */
  more: number
  /** Every number, for the title; empty when all are shown. */
  title: string
}

export function numbersCell(...lists: number[][]): NumbersCell {
  const all = [...new Set(lists.flat())].sort((a, b) => a - b)
  const shown = all.slice(0, MAX_LISTED_NUMBERS)
  const more = all.length - shown.length
  return { count: all.length, shown, more, title: more > 0 ? all.map(n => `#${n}`).join(' ') : '' }
}

export type ApprovalRow = {
  key: string
  day: string
  /** The day is shown once, on the first row of its group. */
  showDay: boolean
  /** How many rows the day has. */
  dayRows: number
  requester: string
  /** The requester's own description when it gave one, else its id. */
  label: string
  workIds: string
  requests: NumbersCell
  approved: NumbersCell
  timeToApprove: string
  done: NumbersCell
  boughtNothing: NumbersCell
  notApproved: NumbersCell
  open: NumbersCell
  drills: NumbersCell
}

/**
 * An approved request that then expired was approved and came to nothing: it counts under Approved and under Bought
 * nothing. Only an expired request nobody approved is Not approved.
 */
function groupCells(group: ApprovalReadingsGroup) {
  const approved = new Set(group.approved)
  return {
    requests: numbersCell(group.requests),
    approved: numbersCell(group.approved),
    timeToApprove: group.approve_seconds_median != null ? formatDuration(group.approve_seconds_median) : '—',
    done: numbersCell(group.executed),
    boughtNothing: numbersCell(group.failed, group.unknown, group.expired.filter(n => approved.has(n))),
    notApproved: numbersCell(group.rejected, group.expired.filter(n => !approved.has(n))),
    open: numbersCell(group.pending, group.waiting),
    drills: numbersCell(group.drills),
  }
}

/** One row per day and requester, in the order the route answers: newest day first. */
export function approvalRows(readings: ApprovalReadings): ApprovalRow[] {
  return readings.days.flatMap(day => day.groups.map((group, index) => ({
    key: `${day.day}/${group.requester}`,
    day: day.day,
    showDay: index === 0,
    dayRows: day.groups.length,
    requester: group.requester,
    label: (group.thread ?? '').trim() !== '' ? (group.thread as string) : group.requester,
    workIds: group.work_ids.join(', '),
    ...groupCells(group),
  })))
}

export type ApprovalsView = { state: 'none' } | { state: 'rows'; rows: ApprovalRow[] }

export function approvalsView(readings: ApprovalReadings): ApprovalsView {
  const rows = approvalRows(readings)
  return rows.length === 0 ? { state: 'none' } : { state: 'rows', rows }
}
