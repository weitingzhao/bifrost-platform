import type { ApprovalItem } from '@/api/approvals'
import { isApprovalId } from '@/api/agentProgressExtra'
import { formatLocalTime, type ProgressStep, type ProgressWork } from '@/api/agentProgress'
import type { ProgressGroup, ProgressQueueItem, ProgressRoadmap } from '@/api/agentProgressBoard'
import type { CountRead, OwnerAsk, PlanDetail, PlanEstimate } from '@/api/progressFields'
import { formatHourMinute, queuedNextLine } from '@/pages/shell/progress/progressModel'
import { boundDocumentText } from '@/pages/shell/progress/progressPart'

const SIX_HOURS_MS = 6 * 60 * 60 * 1000

/** Agent prose in the new blocks. Longer text keeps an ellipsis; controls and bidi marks are dropped. */
export const AGENT_TEXT_MAX = 200

export function agentText(value: string | undefined): string {
  return boundDocumentText(value ?? '', AGENT_TEXT_MAX)
}

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

/** What the row may say about acting. A real request ignores the document's `now`. */
export type AskAct = 'now' | 'later' | 'expired' | 'unknown'

export function askAct(
  ask: OwnerAsk,
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
  nowMs: number,
): AskAct {
  if (ask.unreadable) return 'unknown'
  if (!isApprovalId(ask.approval?.id)) return ask.now === true ? 'now' : 'later'
  if (read !== 'ready') return 'unknown'
  const item = requestItem(ask, items, read)
  if (item == null || item.status !== 'pending') return 'unknown'
  const at = Date.parse(item.expires_at)
  if (!Number.isFinite(at)) return 'unknown'
  if (at <= nowMs) return 'expired'
  return 'now'
}

export function hasNeedsYou(
  ownerItems: OwnerAsk[] | 'unreadable' | undefined,
  standing: OwnerAsk[] | 'unreadable' | undefined,
  ownerNow: readonly { name?: string; work?: string; actions?: OwnerAsk['actions']; approval?: OwnerAsk['approval'] }[] | undefined,
): boolean {
  const gathered = asksFromDocument(ownerItems, standing, ownerNow)
  return gathered.asks.length > 0 || gathered.listUnreadable
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
  return agentText(ask.blocks) !== ''
}

/**
 * Actionable rows first. A request whose list is loading, unreadable, or missing
 * comes after those. Expired is not actionable. Not his turn yet is last.
 */
export function orderedAsks(
  asks: readonly AskSource[],
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
  nowMs: number,
): AskSource[] {
  return asks
    .map((ask, index) => ({ ask, index }))
    .filter(row => askStillWaiting(row.ask, items, read))
    .sort((a, b) => rankAsk(a.ask, items, read, nowMs) - rankAsk(b.ask, items, read, nowMs) || a.index - b.index)
    .map(row => row.ask)
}

function rankAsk(ask: OwnerAsk, items: readonly ApprovalItem[] | undefined, read: ApprovalRead, nowMs: number): number {
  if (ask.unreadable) return 0
  const act = askAct(ask, items, read, nowMs)
  const urgent = holdsUp(ask) || hasDeadline(ask, items, read)
  if (act === 'now') return urgent ? 0 : 1
  if (act === 'unknown') return 2
  if (act === 'expired') return 3
  return urgent ? 4 : 5
}

export function notYetDueCount(
  asks: readonly AskSource[],
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
  nowMs: number,
): number {
  return asks.filter(ask => !ask.unreadable && askAct(ask, items, read, nowMs) === 'later').length
}

export function waitingNowCount(
  asks: readonly AskSource[],
  items: readonly ApprovalItem[] | undefined,
  read: ApprovalRead,
  nowMs: number,
): number {
  return asks.filter(ask => !ask.unreadable && askAct(ask, items, read, nowMs) === 'now').length
}

export type TurnMarker = { text: string; tone: 'now' | 'later' | 'neutral' }

/** The console's own sentence. What the agent says it waits for is not part of this sentence. */
export function turnMarker(ask: OwnerAsk, act: AskAct): TurnMarker {
  if (act === 'expired') return { text: 'Expired', tone: 'neutral' }
  if (act === 'unknown') return { text: 'Unknown', tone: 'neutral' }
  if (act === 'now') return { text: 'You can do this now', tone: 'now' }
  if (typeof ask.at === 'number') return { text: `Not your turn yet · about ${formatHourMinute(ask.at)}`, tone: 'later' }
  if (ask.at === 'unknown') return { text: 'Not your turn yet · Unknown', tone: 'later' }
  return { text: 'Not your turn yet', tone: 'later' }
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
  const text = agentText(ask.deadline_text)
  if (text !== '') return { text, amber: false }
  return { text: 'No deadline', amber: false }
}

export function holdsMarker(ask: OwnerAsk): { text: string; amber: boolean } {
  const blocks = agentText(ask.blocks)
  if (blocks !== '') return { text: `Holding up: ${blocks}`, amber: true }
  const text = agentText(ask.blocks_text)
  if (text !== '') return { text, amber: false }
  return { text: 'Holds up nothing else', amber: false }
}

export function takesMarker(ask: OwnerAsk): string | null {
  const takes = agentText(ask.takes)
  if (takes === '') return null
  return `Takes effect in ${takes}`
}

export function countOrNull(value: CountRead | undefined): number | 'unknown' | null {
  if (value === undefined) return null
  if (value === 'unknown') return 'unknown'
  return value
}

/**
 * A usable pair, Unknown when the pair contradicts itself, or absent when a side is missing.
 * `0/0` is usable. `done > total`, a non-integer, a negative, or `total: 0` with `done > 0` is not.
 */
function sliceNumbers(
  done: CountRead | undefined,
  total: CountRead | undefined,
): { done: number; total: number } | 'unknown' | 'absent' {
  if (done === undefined || total === undefined) return 'absent'
  if (done === 'unknown' || total === 'unknown') return 'unknown'
  if (!Number.isInteger(done) || !Number.isInteger(total) || done < 0 || total < 0) return 'unknown'
  if (done === 0 && total === 0) return { done: 0, total: 0 }
  if (!(total > 0) || done > total) return 'unknown'
  return { done, total }
}

export function sliceIsUnknown(done: CountRead | undefined, total: CountRead | undefined): boolean {
  return sliceNumbers(done, total) === 'unknown'
}

/** Slices done of total. A legitimate `0/0` is an empty bar. A contradiction is not a percent. */
export function slicePercent(done: CountRead | undefined, total: CountRead | undefined): number | null {
  const read = sliceNumbers(done, total)
  if (read === 'unknown' || read === 'absent') return null
  if (read.total === 0) return 0
  const pct = (read.done / read.total) * 100
  if (!Number.isFinite(pct)) return null
  return Math.min(100, Math.max(0, pct))
}

export function sliceLabel(done: CountRead | undefined, total: CountRead | undefined): string {
  const read = sliceNumbers(done, total)
  if (read === 'unknown') return 'Unknown'
  if (read === 'absent') {
    const left = done === 'unknown' || done === undefined ? '?' : formatCount(done)
    const right = total === 'unknown' || total === undefined ? '?' : formatCount(total)
    return `${left}/${right}`
  }
  return `${formatCount(read.done)}/${formatCount(read.total)}`
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

export type OpenFigure =
  | { kind: 'none' }
  | { kind: 'unknown' }
  | { kind: 'full'; value: number }
  | { kind: 'partial'; value: number; missing: number }

/** Sum of a slice field over open items. A missing field is a partial subtotal, not a zero. */
export function sumOpen(details: readonly OpenDetail[], key: 'mine' | 'owner'): OpenFigure {
  let anyEst = false
  let missing = 0
  let sum = 0
  let saw = false
  for (const row of details) {
    if (row.detail.unreadable || row.detail.est?.unreadable) return { kind: 'unknown' }
    const est = row.detail.est
    if (est == null) {
      missing += 1
      continue
    }
    anyEst = true
    const value = est[key]
    if (value === undefined) {
      missing += 1
      continue
    }
    if (value === 'unknown' || !Number.isInteger(value) || value < 0) return { kind: 'unknown' }
    saw = true
    sum += value
  }
  if (!anyEst) return { kind: 'none' }
  if (missing > 0) return { kind: 'partial', value: sum, missing }
  if (!saw) return { kind: 'none' }
  return { kind: 'full', value: sum }
}

function missingItems(missing: number): string {
  return `${missing} ${missing === 1 ? 'item' : 'items'} not estimated`
}

export function slicesHeadline(figure: OpenFigure): string | null {
  if (figure.kind === 'none') return null
  if (figure.kind === 'unknown') return 'slices left Unknown'
  if (figure.kind === 'partial') return `at least ${formatCount(figure.value)} slices (${missingItems(figure.missing)})`
  return `${formatCount(figure.value)} slices left`
}

export function timesHeadline(figure: OpenFigure): string | null {
  if (figure.kind === 'none') return null
  if (figure.kind === 'unknown') return 'times you Unknown'
  if (figure.kind === 'partial') return `at least ${formatCount(figure.value)} times you (${missingItems(figure.missing)})`
  return `${formatCount(figure.value)} times you`
}

type Sum = number | 'unknown' | null

/** Nights from slices left ÷ per night. Absent or unusable pace → null, never NaN or Infinity. */
export function nightsFigure(slicesLeft: Sum, perNight: CountRead | undefined): string | null {
  if (typeof slicesLeft !== 'number') return null
  if (typeof perNight !== 'number' || !Number.isFinite(perNight) || !(perNight > 0)) return null
  const nights = slicesLeft / perNight
  if (!Number.isFinite(nights)) return null
  return formatCount(nights)
}

/** Nights only from a complete set of estimates and a usable pace. A partial subtotal has no nights. */
export function nightsFrom(mine: OpenFigure, owner: OpenFigure, perNight: CountRead | undefined): string | null {
  if (mine.kind !== 'full' || owner.kind !== 'full') return null
  return nightsFigure(mine.value, perNight)
}

export function nightsRange(slicesLeft: Sum, perNight: CountRead | undefined): string | null {
  if (typeof slicesLeft !== 'number') return null
  if (typeof perNight !== 'number' || !Number.isFinite(perNight) || !(perNight > 0)) return null
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

const USUAL_MAX_MIN = 7 * 24 * 60
const START_SKEW_SEC = 60

function usualMinutes(step: ProgressStep): number | null {
  const usual = step.usual_min
  if (typeof usual !== 'number' || !Number.isFinite(usual) || usual <= 0 || usual > USUAL_MAX_MIN) return null
  return usual
}

function startedAt(step: ProgressStep, nowMs: number): number | null {
  const started = step.started
  if (typeof started !== 'number' || !Number.isFinite(started) || !(started > 0) || !Number.isFinite(nowMs)) return null
  if (started > nowMs / 1000 + START_SKEW_SEC) return null
  return started
}

export function pastUsual(step: ProgressStep, nowMs: number): boolean {
  if ((step.overdue ?? 0) > 0) return true
  const usual = usualMinutes(step)
  const started = startedAt(step, nowMs)
  if (usual == null || started == null) return false
  const elapsed = Math.max(0, nowMs / 1000 - started)
  return elapsed > usual * 60
}

export function runningTone(step: ProgressStep, nowMs: number): RunningTone {
  if (step.needs_owner === true) return 'owner'
  const state = step.state ?? ''
  if (state === 'quiet' || state === 'dead' || state === 'failed') return 'bad'
  if (state === 'slow' || state === 'gap' || pastUsual(step, nowMs)) return 'amber'
  return 'run'
}

/** Elapsed against the usual time, capped at 100. Absurd or missing timing is Unknown, not a green 0%. */
export function elapsedPercent(step: ProgressStep, nowMs: number): number | 'unknown' {
  if (step.needs_owner === true) return 100
  const usual = usualMinutes(step)
  const started = startedAt(step, nowMs)
  if (usual == null || started == null) return 'unknown'
  const elapsed = Math.max(0, nowMs / 1000 - started)
  const pct = (elapsed / (usual * 60)) * 100
  if (!Number.isFinite(pct)) return 'unknown'
  return Math.min(100, Math.max(0, pct))
}

/** Clock time for a normal running step. Owner, trouble and overdue keep their own words. */
export function runningWhen(step: ProgressStep): string {
  if (step.needs_owner === true) return 'Waiting for you'
  const state = step.state ?? ''
  if (state === 'failed') return 'Failed'
  if (state === 'quiet' || state === 'dead') return 'Stuck'
  if ((step.overdue ?? 0) > 0) return 'Late'
  const eta = step.eta_end
  if (typeof eta !== 'number' || !Number.isFinite(eta) || eta <= 0) return 'Unknown'
  const date = new Date(eta * 1000)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  const text = formatLocalTime(eta)
  if (text === '' || text === 'unknown time') return 'Unknown'
  return text
}

export function runningRank(step: ProgressStep): number {
  if (step.needs_owner === true) return 0
  const state = step.state ?? ''
  if (state === 'quiet' || state === 'dead' || state === 'failed') return 1
  return 2
}

export type TroubleTone = 'red' | 'amber' | 'green'

/** Red only for stuck or failed. Amber for a gap, a slow step, or a step past its usual time. */
export function troubleTone(works: readonly ProgressWork[] | undefined, nowMs: number): TroubleTone {
  const steps = (works ?? []).flatMap(work => work.steps ?? [])
  if (steps.some(step => step.state === 'quiet' || step.state === 'dead' || step.state === 'failed')) return 'red'
  if (steps.some(step => step.state === 'gap' || step.state === 'slow')) return 'amber'
  if (steps.some(step => step.state === 'running' && pastUsual(step, nowMs))) return 'amber'
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
  return agentText(est.note)
}
