import type { ApprovalItem } from '@/api/approvals'
import { isApprovalId } from '@/api/agentProgressExtra'
import type { ProgressStep, ProgressWork } from '@/api/agentProgress'
import type { ProgressGroup, ProgressQueueItem, ProgressRoadmap } from '@/api/agentProgressBoard'
import type { CountRead, OwnerAsk, PlanDetail, PlanEstimate } from '@/api/progressFields'
import { formatHourMinute, queuedNextLine } from '@/pages/shell/progress/progressModel'

const SIX_HOURS_MS = 6 * 60 * 60 * 1000

export type ByWhen = { text: string; amber: boolean }

/** `By Oct 12, 11:25 (23 h left)`. Amber under six hours, and once it is past. */
export function byWhenText(atMs: number, nowMs: number): ByWhen | null {
  if (!Number.isFinite(atMs) || !Number.isFinite(nowMs)) return null
  const date = new Date(atMs)
  if (Number.isNaN(date.getTime())) return null
  const month = date.toLocaleString('en-US', { month: 'short' })
  const day = date.getDate()
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  const when = `${month} ${day}, ${hour}:${minute}`
  const left = atMs - nowMs
  const amber = left < SIX_HOURS_MS
  if (left <= 0) return { text: `By ${when} (past)`, amber: true }
  if (left >= 60 * 60 * 1000) {
    const hours = Math.round(left / (60 * 60 * 1000))
    return { text: `By ${when} (${hours} h left)`, amber }
  }
  const mins = Math.max(1, Math.round(left / 60_000))
  return { text: `By ${when} (${mins} min left)`, amber: true }
}

export function waitsOnOwner(blocked: string | undefined): boolean {
  const text = blocked?.trim() ?? ''
  if (text === '') return false
  if (text.startsWith('你')) return true
  if (/^you\b/i.test(text)) return true
  if (/^owner\b/i.test(text)) return true
  return false
}

export type AskSource = OwnerAsk & { index: number; fallback?: boolean }

/** owner_items when that field is present. Otherwise the slice-3 owner_now rows, all due now. */
export function asksFromDocument(
  ownerItems: OwnerAsk[] | 'unreadable' | undefined,
  standing: OwnerAsk[] | 'unreadable' | undefined,
  ownerNow: readonly { name?: string; work?: string; actions?: OwnerAsk['actions']; approval?: OwnerAsk['approval'] }[] | undefined,
): { listUnreadable: boolean; asks: AskSource[] } {
  const standingAsks = standing === 'unreadable' ? [] : (standing ?? [])
  const standingFlag = standing === 'unreadable'
  if (ownerItems === 'unreadable') {
    return { listUnreadable: true, asks: standingAsks.map((ask, index) => ({ ...ask, index })) }
  }
  if (ownerItems !== undefined) {
    const asks = ownerItems.concat(standingAsks).map((ask, index) => ({ ...ask, index }))
    return { listUnreadable: standingFlag, asks }
  }
  const fallback: AskSource[] = (ownerNow ?? []).map((entry, index) => ({
    unreadable: false,
    name: entry.name,
    work: entry.work,
    now: true,
    actions: entry.actions,
    approval: entry.approval,
    index,
    fallback: true,
  }))
  const asks = fallback.concat(standingAsks.map((ask, index) => ({ ...ask, index: fallback.length + index })))
  return { listUnreadable: standingFlag, asks }
}

export type ApprovalRead = 'loading' | 'unreadable' | 'ready'

export function requestItem(ask: OwnerAsk, items: readonly ApprovalItem[] | undefined, read: ApprovalRead): ApprovalItem | undefined {
  if (read !== 'ready' || !isApprovalId(ask.approval?.id)) return undefined
  return items?.find(row => row.id === ask.approval?.id)
}

/** A request that is no longer pending has been acted on. The row leaves Needs you. */
export function askStillWaiting(ask: OwnerAsk, items: readonly ApprovalItem[] | undefined, read: ApprovalRead): boolean {
  if (ask.unreadable) return true
  if (!isApprovalId(ask.approval?.id)) return true
  if (read !== 'ready') return true
  const item = requestItem(ask, items, read)
  if (item == null) return true
  return item.status === 'pending'
}

function hasDeadline(ask: OwnerAsk, items: readonly ApprovalItem[] | undefined, read: ApprovalRead): boolean {
  if (isApprovalId(ask.approval?.id)) {
    const item = requestItem(ask, items, read)
    if (item == null) return false
    return Number.isFinite(Date.parse(item.expires_at))
  }
  return typeof ask.deadline === 'number'
}

function holdsUp(ask: OwnerAsk): boolean {
  return (ask.blocks?.trim() ?? '') !== ''
}

/** Blocking or a deadline first. Not his turn yet last. Stable for equal rank. */
export function orderedAsks(
  asks: readonly AskSource[],
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
): AskSource[] {
  return asks
    .map((ask, index) => ({ ask, index }))
    .filter(row => askStillWaiting(row.ask, items, read))
    .sort((a, b) => rankAsk(a.ask, items, read) - rankAsk(b.ask, items, read) || a.index - b.index)
    .map(row => row.ask)
}

function rankAsk(ask: OwnerAsk, items: readonly ApprovalItem[] | undefined, read: ApprovalRead): number {
  if (ask.unreadable) return 0
  const later = ask.now !== true
  const urgent = holdsUp(ask) || hasDeadline(ask, items, read)
  return (later ? 2 : 0) + (urgent ? 0 : 1)
}

export function notYetDueCount(asks: readonly AskSource[]): number {
  return asks.filter(ask => !ask.unreadable && ask.now !== true).length
}

export type TurnMarker = { text: string; now: boolean }

export function turnMarker(ask: OwnerAsk): TurnMarker {
  if (ask.now === true) return { text: 'You can do this now', now: true }
  const before = ask.not_before?.trim() ?? ''
  if (before !== '') return { text: `Not your turn yet · ${before}`, now: false }
  if (typeof ask.at === 'number') return { text: `Not your turn yet · about ${formatHourMinute(ask.at)}`, now: false }
  if (ask.at === 'unknown') return { text: 'Not your turn yet · Unknown', now: false }
  return { text: 'Not your turn yet', now: false }
}

/**
 * A real request's expiry wins. The document's deadline is not consulted.
 * An unreadable list invents no deadline.
 */
export function deadlineMarker(
  ask: OwnerAsk,
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
  nowMs: number,
): ByWhen | 'none' | 'unknown' {
  if (isApprovalId(ask.approval?.id)) {
    if (read === 'loading' || read === 'unreadable') return 'none'
    const item = requestItem(ask, items, read)
    if (item == null) return 'none'
    const at = Date.parse(item.expires_at)
    if (!Number.isFinite(at)) return 'unknown'
    return byWhenText(at, nowMs) ?? 'unknown'
  }
  if (ask.deadline === 'unknown') return 'unknown'
  if (typeof ask.deadline === 'number') return byWhenText(ask.deadline * 1000, nowMs) ?? 'unknown'
  const text = ask.deadline_text?.trim() ?? ''
  if (text !== '') return { text, amber: false }
  return { text: 'No deadline', amber: false }
}

export function holdsMarker(ask: OwnerAsk): { text: string; amber: boolean } {
  const blocks = ask.blocks?.trim() ?? ''
  if (blocks !== '') return { text: `Holding up: ${blocks}`, amber: true }
  const text = ask.blocks_text?.trim() ?? ''
  if (text !== '') return { text, amber: false }
  return { text: 'Holds up nothing else', amber: false }
}

export function takesMarker(ask: OwnerAsk): string | null {
  const takes = ask.takes?.trim() ?? ''
  if (takes === '') return null
  return `Takes effect in ${takes}`
}

export function countOrNull(value: CountRead | undefined): number | 'unknown' | null {
  if (value === undefined) return null
  if (value === 'unknown') return 'unknown'
  return value
}

/** Slices done of total. Zero total is an empty bar, never NaN. */
export function slicePercent(done: CountRead | undefined, total: CountRead | undefined): number | null {
  if (done === 'unknown' || total === 'unknown') return null
  if (typeof done !== 'number' || typeof total !== 'number') return null
  if (!(total > 0)) return 0
  const pct = (done / total) * 100
  if (!Number.isFinite(pct)) return null
  return Math.min(100, Math.max(0, pct))
}

export function sliceLabel(done: CountRead | undefined, total: CountRead | undefined): string {
  const left = done === 'unknown' || done === undefined ? '?' : formatCount(done)
  const right = total === 'unknown' || total === undefined ? '?' : formatCount(total)
  return `${left}/${right}`
}

export function formatCount(value: number): string {
  if (!Number.isFinite(value)) return 'Unknown'
  const rounded = Math.round(value * 10) / 10
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)
}

export type OpenDetail = { detail: PlanDetail; group: string }

export function openDetails(roadmap: ProgressRoadmap | undefined): OpenDetail[] {
  const rows: OpenDetail[] = []
  for (const group of roadmap?.groups ?? []) {
    if (group.state !== 'doing' && group.state !== 'todo') continue
    if (!Array.isArray(group.detail)) continue
    for (const detail of group.detail) rows.push({ detail, group: group.state })
  }
  return rows
}

export function doingDetails(roadmap: ProgressRoadmap | undefined): PlanDetail[] {
  const group = (roadmap?.groups ?? []).find(row => row.state === 'doing')
  if (group == null || !Array.isArray(group.detail)) return []
  return group.detail
}

type Sum = number | 'unknown' | null

function addCount(sum: Sum, value: CountRead | undefined, seen: { any: boolean }): Sum {
  if (sum === 'unknown') return 'unknown'
  if (value === undefined) return sum
  if (value === 'unknown') return 'unknown'
  seen.any = true
  return (sum ?? 0) + value
}

/** Sum of a slice field over open items. No estimates → null. Any Unknown → Unknown. */
export function sumOpen(details: readonly OpenDetail[], key: 'mine' | 'owner'): Sum {
  let anyEst = false
  for (const row of details) {
    if (row.detail.est != null) anyEst = true
  }
  if (!anyEst) return null
  const seen = { any: false }
  let sum: Sum = 0
  for (const row of details) {
    if (row.detail.unreadable || row.detail.est?.unreadable) return 'unknown'
    sum = addCount(sum, row.detail.est?.[key], seen)
    if (sum === 'unknown') return 'unknown'
  }
  return seen.any ? sum : null
}

/** Nights from slices left ÷ per night. Absent or unusable pace → null, never NaN or Infinity. */
export function nightsFigure(slicesLeft: Sum, perNight: CountRead | undefined): string | null {
  if (typeof slicesLeft !== 'number') return null
  if (typeof perNight !== 'number' || !(perNight > 0)) return null
  const nights = slicesLeft / perNight
  if (!Number.isFinite(nights)) return null
  return formatCount(nights)
}

export function nightsRange(slicesLeft: Sum, perNight: CountRead | undefined): string | null {
  if (typeof slicesLeft !== 'number') return null
  if (typeof perNight !== 'number' || !(perNight > 0)) return null
  const nights = slicesLeft / perNight
  if (!Number.isFinite(nights)) return null
  const low = Math.round(nights * 0.75 * 10) / 10
  const high = Math.round(nights * 1.5 * 10) / 10
  return `${formatCount(low)}–${formatCount(high)} nights`
}

export function segmentFlex(count: number, total: number): number {
  if (!Number.isFinite(count) || count < 0) return 0
  if (total <= 0) return 1
  return count
}

export type RunningTone = 'owner' | 'bad' | 'amber' | 'run'

export function pastUsual(step: ProgressStep, nowMs: number): boolean {
  const usual = step.usual_min ?? 0
  const started = step.started ?? 0
  if (!(usual > 0) || !(started > 0)) return (step.overdue ?? 0) > 0
  const elapsed = Math.max(0, nowMs / 1000 - started)
  return elapsed > usual * 60 || (step.overdue ?? 0) > 0
}

export function runningTone(step: ProgressStep, nowMs: number): RunningTone {
  if (step.needs_owner === true) return 'owner'
  const state = step.state ?? ''
  if (state === 'quiet' || state === 'dead' || state === 'failed') return 'bad'
  if (state === 'slow' || state === 'gap' || pastUsual(step, nowMs)) return 'amber'
  return 'run'
}

/** Elapsed against the usual time, capped at 100. No usual time → 0, not a stand-in fill. */
export function elapsedPercent(step: ProgressStep, nowMs: number): number {
  if (step.needs_owner === true) return 100
  const usual = step.usual_min ?? 0
  const started = step.started ?? 0
  if (!(usual > 0) || !(started > 0) || !Number.isFinite(nowMs)) return 0
  const elapsed = Math.max(0, nowMs / 1000 - started)
  const pct = (elapsed / (usual * 60)) * 100
  if (!Number.isFinite(pct)) return 0
  return Math.min(100, Math.max(0, pct))
}

export function runningRank(step: ProgressStep): number {
  if (step.needs_owner === true) return 0
  const state = step.state ?? ''
  if (state === 'quiet' || state === 'dead' || state === 'failed') return 1
  return 2
}

export type TroubleTone = 'red' | 'amber' | 'green'

/** Red only for stuck or failed. Amber for the coordinator's gap or a slow step. */
export function troubleTone(works: readonly ProgressWork[] | undefined): TroubleTone {
  const steps = (works ?? []).flatMap(work => work.steps ?? [])
  if (steps.some(step => step.state === 'quiet' || step.state === 'dead' || step.state === 'failed')) return 'red'
  if (steps.some(step => step.state === 'gap' || step.state === 'slow')) return 'amber'
  return 'green'
}

export function runningQuietLine(
  works: readonly ProgressWork[] | undefined,
  queue: readonly ProgressQueueItem[] | undefined,
): string {
  return queuedNextLine(works, queue) ?? 'Nothing is running.'
}

export function groupNames(groups: readonly ProgressGroup[] | undefined, states: readonly string[]): string {
  return (groups ?? [])
    .filter(group => states.includes(group.state ?? ''))
    .map(group => group.name?.trim() ?? '')
    .filter(name => name !== '')
    .join(' · ')
}

export function estimateNote(est: PlanEstimate | undefined): string {
  if (est == null || est.unreadable) return ''
  return est.note?.trim() ?? ''
}
