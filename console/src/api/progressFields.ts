import { parseApproval, type ProgressApproval } from '@/api/agentProgressExtra'

/**
 * Optional document fields this page reads. A present field of the wrong type
 * makes that item unreadable. A number that is missing, not finite, negative,
 * or absurd is Unknown — never a healthy stand-in.
 */

export const ABSURD_COUNT = 100_000

export type CountRead = number | 'unknown'

export type OwnerAsk = {
  unreadable: boolean
  name?: string
  work?: string
  now?: boolean
  at?: CountRead
  not_before?: string
  deadline?: CountRead
  deadline_text?: string
  blocks?: string
  blocks_text?: string
  takes?: string
  actions?: OwnerAskAction[]
  approval?: ProgressApproval
}

export type OwnerAskAction = {
  text?: string
  done?: boolean
  link?: string
  link_text?: string
}

/** Absent field, a parsed list, or a field that was not a list. */
export type OwnerAskList = OwnerAsk[] | 'unreadable'

export type PlanEstimate = {
  unreadable: boolean
  done?: CountRead
  total?: CountRead
  mine?: CountRead
  owner?: CountRead
  blocked?: string
  sure?: string
  note?: string
  as_of?: string
}

export type PlanDetail = {
  unreadable: boolean
  name?: string
  est?: PlanEstimate
}

export type PlanDetailList = PlanDetail[] | 'unreadable'

export type PlanPace = {
  unreadable: boolean
  per_night?: CountRead
  basis?: string
  owner_wait?: string
}

export type PlanDelta = {
  unreadable: boolean
  since?: string
  lines: string[]
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function has(raw: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(raw, key) && raw[key] !== undefined
}

/** Right type, or absent. Wrong type is a signal to drop the whole item. */
function readString(raw: Record<string, unknown>, key: string): { bad: boolean; value?: string } {
  if (!has(raw, key)) return { bad: false }
  if (typeof raw[key] !== 'string') return { bad: true }
  return { bad: false, value: raw[key] }
}

function readBool(raw: Record<string, unknown>, key: string): { bad: boolean; value?: boolean } {
  if (!has(raw, key)) return { bad: false }
  if (typeof raw[key] !== 'boolean') return { bad: true }
  return { bad: false, value: raw[key] }
}

/**
 * Absent, a usable number, Unknown (negative or absurd), or a wrong type.
 * Wrong type is not Unknown: the caller marks the item unreadable.
 */
export function readCount(value: unknown, present: boolean, absurd = ABSURD_COUNT): 'absent' | 'bad' | CountRead {
  if (!present) return 'absent'
  if (typeof value !== 'number' || !Number.isFinite(value)) return 'bad'
  if (value < 0 || value > absurd) return 'unknown'
  return value
}

function takeCount(raw: Record<string, unknown>, key: string): { bad: boolean; value?: CountRead } {
  const read = readCount(raw[key], has(raw, key))
  if (read === 'absent') return { bad: false }
  if (read === 'bad') return { bad: true }
  return { bad: false, value: read }
}

/**
 * Unix seconds. A wrong type makes the item unreadable.
 * A negative value, or one that is not a date, is Unknown — never a healthy time.
 */
export function readTime(value: unknown, present: boolean): 'absent' | 'bad' | CountRead {
  if (!present) return 'absent'
  if (typeof value !== 'number' || !Number.isFinite(value)) return 'bad'
  if (value < 0) return 'unknown'
  const date = new Date(value * 1000)
  if (Number.isNaN(date.getTime())) return 'unknown'
  return value
}

function takeTime(raw: Record<string, unknown>, key: string): { bad: boolean; value?: CountRead } {
  const read = readTime(raw[key], has(raw, key))
  if (read === 'absent') return { bad: false }
  if (read === 'bad') return { bad: true }
  return { bad: false, value: read }
}

function parseAction(value: Record<string, unknown>): OwnerAskAction {
  const action: OwnerAskAction = {}
  if (typeof value.text === 'string') action.text = value.text
  if (typeof value.link === 'string') action.link = value.link
  if (typeof value.link_text === 'string') action.link_text = value.link_text
  if (typeof value.done === 'boolean') action.done = value.done
  return action
}

function parseActions(value: unknown): { bad: boolean; actions?: OwnerAskAction[] } {
  if (value === undefined) return { bad: false }
  if (!Array.isArray(value)) return { bad: true }
  return { bad: false, actions: value.filter(isRecord).map(parseAction) }
}

function parseAskApproval(value: unknown): { bad: boolean; approval?: ProgressApproval } {
  if (value === undefined) return { bad: false }
  if (!isRecord(value)) return { bad: true }
  if (has(value, 'id') && typeof value.id !== 'string') return { bad: true }
  const approval = parseApproval(value)
  return { bad: false, approval }
}

const ITEM_STRINGS = ['name', 'work', 'not_before', 'blocks', 'takes'] as const
const STANDING_STRINGS = ['name', 'deadline_text', 'blocks_text', 'takes'] as const

function parseAsk(value: Record<string, unknown>, standing: boolean): OwnerAsk {
  const ask: OwnerAsk = { unreadable: false }
  const strings = standing ? STANDING_STRINGS : ITEM_STRINGS
  for (const key of strings) {
    const read = readString(value, key)
    if (read.bad) return { unreadable: true }
    if (read.value !== undefined) ask[key] = read.value
  }
  if (!standing) {
    const blocksText = readString(value, 'blocks_text')
    if (blocksText.bad) return { unreadable: true }
    if (blocksText.value !== undefined) ask.blocks_text = blocksText.value
    const deadlineText = readString(value, 'deadline_text')
    if (deadlineText.bad) return { unreadable: true }
    if (deadlineText.value !== undefined) ask.deadline_text = deadlineText.value
  }
  const now = readBool(value, 'now')
  if (now.bad) return { unreadable: true }
  if (now.value !== undefined) ask.now = now.value
  if (!standing) {
    const at = takeTime(value, 'at')
    if (at.bad) return { unreadable: true }
    if (at.value !== undefined) ask.at = at.value
    const deadline = takeTime(value, 'deadline')
    if (deadline.bad) return { unreadable: true }
    if (deadline.value !== undefined) ask.deadline = deadline.value
    const approval = parseAskApproval(value.approval)
    if (approval.bad) return { unreadable: true }
    if (approval.approval !== undefined) ask.approval = approval.approval
  }
  const actions = parseActions(has(value, 'actions') ? value.actions : undefined)
  if (actions.bad) return { unreadable: true }
  if (actions.actions !== undefined) ask.actions = actions.actions
  return ask
}

/** `undefined` when the field is absent. A non-array is unreadable, not a fallback. */
export function parseOwnerAsks(value: unknown, standing = false): OwnerAskList | undefined {
  if (value === undefined) return undefined
  if (!Array.isArray(value)) return 'unreadable'
  return value.map(row => (isRecord(row) ? parseAsk(row, standing) : { unreadable: true }))
}

function parseEstimate(value: unknown): PlanEstimate | undefined {
  if (value === undefined) return undefined
  if (!isRecord(value)) return { unreadable: true }
  const est: PlanEstimate = { unreadable: false }
  for (const key of ['done', 'total', 'mine', 'owner'] as const) {
    const read = takeCount(value, key)
    if (read.bad) return { unreadable: true }
    if (read.value !== undefined) est[key] = read.value
  }
  for (const key of ['blocked', 'sure', 'note', 'as_of'] as const) {
    const read = readString(value, key)
    if (read.bad) return { unreadable: true }
    if (read.value !== undefined) est[key] = read.value
  }
  return est
}

export function parsePlanDetail(value: unknown): PlanDetailList | undefined {
  if (value === undefined) return undefined
  if (!Array.isArray(value)) return 'unreadable'
  return value.map(row => {
    if (!isRecord(row)) return { unreadable: true }
    const name = readString(row, 'name')
    if (name.bad) return { unreadable: true }
    const est = parseEstimate(has(row, 'est') ? row.est : undefined)
    if (est?.unreadable) return { unreadable: true, name: name.value }
    const detail: PlanDetail = { unreadable: false }
    if (name.value !== undefined) detail.name = name.value
    if (est !== undefined) detail.est = est
    return detail
  })
}

export function parsePlanPace(value: unknown): PlanPace | undefined {
  if (value === undefined) return undefined
  if (!isRecord(value)) return { unreadable: true }
  const pace: PlanPace = { unreadable: false }
  const per = takeCount(value, 'per_night')
  if (per.bad) return { unreadable: true }
  if (per.value !== undefined) pace.per_night = per.value
  const basis = readString(value, 'basis')
  if (basis.bad) return { unreadable: true }
  if (basis.value !== undefined) pace.basis = basis.value
  const wait = readString(value, 'owner_wait')
  if (wait.bad) return { unreadable: true }
  if (wait.value !== undefined) pace.owner_wait = wait.value
  return pace
}

export function parsePlanDelta(value: unknown): PlanDelta | undefined {
  if (value === undefined) return undefined
  if (!isRecord(value)) return { unreadable: true, lines: [] }
  const since = readString(value, 'since')
  if (since.bad) return { unreadable: true, lines: [] }
  if (has(value, 'lines') && !Array.isArray(value.lines)) return { unreadable: true, lines: [] }
  const lines: string[] = []
  if (Array.isArray(value.lines)) {
    for (const line of value.lines) {
      if (typeof line !== 'string') return { unreadable: true, lines: [] }
      lines.push(line)
    }
  }
  return { unreadable: false, since: since.value, lines }
}

/** A present non-string is unreadable. Absent stays absent. */
export function parsePlainText(value: unknown): string | 'unreadable' | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string') return 'unreadable'
  return value
}

/** Older finished steps the publisher left out. Wrong type is Unknown, not zero. */
export function parseEarlierDone(value: unknown): CountRead | undefined {
  if (value === undefined) return undefined
  const read = readCount(value, true)
  if (read === 'absent') return undefined
  if (read === 'bad') return 'unknown'
  return read
}
