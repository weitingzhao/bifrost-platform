import type { AgentThread } from '@/api/agentThreads'
import { isAwaitingDecision, isUnsetDecidedAt, type ApprovalItem } from '@/api/approvals'
import { visibleTimestamp, youClosedLine } from '@/pages/approvalDecision'

export const TERMINAL_NOT_RUN = 'Approved. It has not run yet.'

export const CHECK_SAY_WHAT_HAPPENED = 'Open it and say what happened.'

export const EARLIER_NOTIFICATIONS =
  'Notifications you received earlier are under Recently handled.'

/** Closed approvals and recovered threads stay visible for a day after the push. */
export const RECENT_WINDOW_MS = 24 * 60 * 60 * 1000

/** The handled block lists at most this many closed approvals. Threads are separate. */
export const RECENT_APPROVAL_LIMIT = 10

export type NeedsYouGroups = {
  decide: ApprovalItem[]
  terminal: ApprovalItem[]
  check: ApprovalItem[]
}

export type HandledRow =
  | { kind: 'approval'; item: ApprovalItem; line: string; at: number }
  | { kind: 'thread'; thread: AgentThread; line: string; at: number }

function runnerOf(item: Pick<ApprovalItem, 'runner'>): string {
  return item.runner?.trim().toLowerCase() ?? ''
}

function executionBits(value: unknown): { exitCode: number | null; finishedAt: string | null } {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) {
    return { exitCode: null, finishedAt: null }
  }
  const raw = value as Record<string, unknown>
  const exitCode = typeof raw.exit_code === 'number' && Number.isFinite(raw.exit_code) ? raw.exit_code : null
  const finishedAt = typeof raw.finished_at === 'string' ? raw.finished_at : null
  return { exitCode, finishedAt }
}

/** A real finish time or an exit code means the command already ran. */
function hasRun(item: ApprovalItem): boolean {
  const bits = executionBits(item.execution)
  if (bits.exitCode != null) return true
  return visibleTimestamp(bits.finishedAt ?? undefined) != null
}

/**
 * What the Owner still has to do with open approvals.
 * Pending stays the Decide list. Approved + owner that has not run is the
 * terminal list. Unknown is Check. Running, and approved work the platform
 * runs itself, are left out.
 */
export function groupOpenApprovals(items: readonly ApprovalItem[], now = Date.now()): NeedsYouGroups {
  const decide: ApprovalItem[] = []
  const terminal: ApprovalItem[] = []
  const check: ApprovalItem[] = []
  for (const item of items) {
    const status = item.status.trim().toLowerCase()
    if (status === 'pending') {
      if (isAwaitingDecision(item, now)) decide.push(item)
      continue
    }
    if (status === 'approved' && runnerOf(item) === 'owner' && !hasRun(item)) {
      terminal.push(item)
      continue
    }
    if (status === 'unknown') check.push(item)
  }
  return { decide, terminal, check }
}

/** Sidebar and the page heading: the three groups plus the silent-section count. */
export function waitingCount(groups: NeedsYouGroups, silentSectionCount: number): number {
  return groups.decide.length + groups.terminal.length + groups.check.length + silentSectionCount
}

export function checkLines(error: string | undefined): string[] {
  const text = error?.trim() ?? ''
  const first = text === '' ? 'The result could not be confirmed.' : `The result could not be confirmed: ${text}`
  return [first, CHECK_SAY_WHAT_HAPPENED]
}

function instant(value: string | undefined): number | null {
  if (isUnsetDecidedAt(value)) return null
  const ms = Date.parse(value ?? '')
  if (!Number.isFinite(ms)) return null
  if (new Date(ms).getUTCFullYear() <= 1) return null
  return ms
}

function inWindow(ms: number, now: number): boolean {
  return ms <= now && now - ms <= RECENT_WINDOW_MS
}

/** When a closed request ended: the Owner's close, else finish time, else the decision, else expiry. */
function closedInstant(item: ApprovalItem): number | null {
  const resolved = instant(item.resolution?.at)
  if (resolved != null) return resolved
  const status = item.status.trim().toLowerCase()
  if (status === 'executed' || status === 'failed') {
    const finished = instant(executionBits(item.execution).finishedAt ?? undefined)
    if (finished != null) return finished
  }
  const decided = instant(item.decided_at)
  if (decided != null) return decided
  if (status === 'expired') return instant(item.expires_at)
  return null
}

function resolvedHandledLine(outcome: string | undefined): string | null {
  return youClosedLine(outcome)
}

function handledApprovalLine(item: ApprovalItem, at: number): string | null {
  const closed = resolvedHandledLine(item.resolution?.outcome)
  if (closed != null) return closed
  const status = item.status.trim().toLowerCase()
  const time = new Date(at).toLocaleString()
  switch (status) {
    case 'executed':
      return `You approved it; it ran and finished at ${time}`
    case 'failed': {
      const code = executionBits(item.execution).exitCode
      if (code == null) return 'You approved it; it ran and failed.'
      return `You approved it; it ran and failed (exit code ${code})`
    }
    case 'rejected':
      return `Rejected at ${time}`
    case 'expired':
      return `Expired at ${time}`
    default:
      return null
  }
}

function rowId(row: HandledRow): string {
  return row.kind === 'approval' ? row.item.id : row.thread.thread
}

function byNewest(a: { at: number; id: string }, b: { at: number; id: string }): number {
  if (b.at !== a.at) return b.at - a.at
  if (a.id < b.id) return -1
  if (a.id > b.id) return 1
  return 0
}

/** Newest first. Approvals keep the cap applied before this merge. */
export function mergeHandled(rows: readonly HandledRow[]): HandledRow[] {
  return [...rows].sort((a, b) => byNewest({ at: a.at, id: rowId(a) }, { at: b.at, id: rowId(b) }))
}

/** Closed in the last 24 hours, newest first, capped. */
export function recentlyHandledApprovals(items: readonly ApprovalItem[], now = Date.now()): HandledRow[] {
  const rows: HandledRow[] = []
  for (const item of items) {
    const status = item.status.trim().toLowerCase()
    if (status !== 'executed' && status !== 'failed' && status !== 'rejected' && status !== 'expired') continue
    const at = closedInstant(item)
    if (at == null || !inWindow(at, now)) continue
    const line = handledApprovalLine(item, at)
    if (line == null) continue
    rows.push({ kind: 'approval', item, line, at })
  }
  return mergeHandled(rows).slice(0, RECENT_APPROVAL_LIMIT)
}

function threadLabel(thread: Pick<AgentThread, 'title' | 'thread'>): string {
  const title = thread.title?.trim() ?? ''
  return title !== '' ? title : thread.thread
}

/** A push went out, and the thread is no longer the silent row. */
export function handledThreadLine(thread: Pick<AgentThread, 'title' | 'thread' | 'status' | 'event'>): string {
  const label = threadLabel(thread)
  if (thread.status === 'idle' || thread.event === 'turn_end') {
    return `${label} was silent; its turn ended`
  }
  return `${label} was silent; it is active again`
}

export function recentlyHandledThreads(threads: readonly AgentThread[], now = Date.now()): HandledRow[] {
  const rows: HandledRow[] = []
  for (const thread of threads) {
    if (thread.status === 'silent') continue
    const at = instant(thread.notified_at)
    if (at == null || !inWindow(at, now)) continue
    rows.push({ kind: 'thread', thread, line: handledThreadLine(thread), at })
  }
  return mergeHandled(rows)
}

/** Approvals (capped) and recovered threads, newest first. */
export function recentlyHandled(
  items: readonly ApprovalItem[],
  threads: readonly AgentThread[],
  now = Date.now(),
): HandledRow[] {
  return mergeHandled([...recentlyHandledApprovals(items, now), ...recentlyHandledThreads(threads, now)])
}
