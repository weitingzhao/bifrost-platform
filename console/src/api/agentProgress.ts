import { authedFetch, operatorToken, parseError } from '@/api/client'
import { readApprovalToken } from '@/api/approvals'
import { boundDocumentText, sanitizeDocumentText } from '@/pages/shell/progress/progressPart'
import {
  parseEvents,
  parseQueue,
  parseRoadmap,
  parseUsage,
  type ProgressEvent,
  type ProgressQueueItem,
  type ProgressRoadmap,
  type ProgressUsage,
} from '@/api/agentProgressBoard'
import { parseApproval, parseOwnerTurn, type ProgressApproval, type ProgressOwnerTurn } from '@/api/agentProgressExtra'

/** How often the Progress page re-reads the published document. */
export const AGENT_PROGRESS_POLL_MS = 15_000

/** One progress read gives up after this. Other console reads are unchanged. */
export const AGENT_PROGRESS_TIMEOUT_MS = 10_000

/** Older than this, the page treats the publication as not current. */
export const STALE_AFTER_SECONDS = 5 * 60

/** Document `at` more than this far from `received_at` is not the present. */
export const GENERATION_SKEW_SECONDS = 2 * 60

/** Hosts a published link may open. http and https only. */
export const TRUSTED_PROGRESS_HOSTS = ['ops.bifrost.lan'] as const

export type ProgressLevel = 'run' | 'slow' | 'bad' | 'idle'

export type ProgressStepState =
  | 'waiting'
  | 'gap'
  | 'running'
  | 'slow'
  | 'quiet'
  | 'dead'
  | 'done'
  | 'failed'

export type ProgressAction = {
  text?: string
  done?: boolean
  link?: string
  link_text?: string
}

export type OwnerNow = {
  name?: string
  work?: string
  urgency?: string
  since?: number
  actions?: ProgressAction[]
  approval?: ProgressApproval
}

export type OwnerNext = {
  name?: string
  at?: number
  work?: string
}

export type ProgressStep = {
  name?: string
  who?: string
  state?: string
  label?: string
  started?: number
  ended?: number
  last?: number
  usual_min?: number
  eta_start?: number
  eta_end?: number
  overdue?: number
  note?: string
  doing?: string
  needs_owner?: boolean
  parallel?: boolean
  waits_for?: string
  approval?: ProgressApproval
  events?: ProgressEvent[]
}

export type ProgressNext = {
  name?: string
  who?: string
  at?: number
  needs_owner?: boolean
}

export type ProgressWork = {
  title?: string
  short?: string
  goal?: string
  round?: number
  risk?: string
  after?: string
  sure?: boolean
  end?: number
  finished?: boolean
  next?: ProgressNext | null
  owner?: ProgressOwnerTurn | null
  steps?: ProgressStep[]
}

export type ProgressFeedItem = {
  at?: number
  work?: string
  step?: string
  who?: string
  state?: string
  text?: string
}

export type ProgressCoordinator = {
  at?: number
  state?: string
  note?: string
  doing?: string
  next?: string
}

/** The document the local board publishes. Unknown fields are kept by the API. */
export type ProgressDocument = {
  at?: number
  verdict?: string
  level?: string
  coordinator?: ProgressCoordinator
  owner_now?: OwnerNow[]
  owner_next?: OwnerNext | null
  needs_you?: string[]
  sure?: boolean
  works?: ProgressWork[]
  feed?: ProgressFeedItem[]
  recent?: string[]
  roadmap?: ProgressRoadmap
  usage?: ProgressUsage
  queue?: ProgressQueueItem[]
}

export type AgentProgressResponse = {
  document: ProgressDocument | null
  received_at: string
  age_seconds: number
  stale: boolean
  host: string
  generated_at: string
  generation_lag_seconds: number
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function parseAction(value: Record<string, unknown>): ProgressAction {
  const action: ProgressAction = {}
  const text = asString(value.text)
  const link = asString(value.link)
  const linkText = asString(value.link_text)
  const done = asBoolean(value.done)
  if (text !== undefined) action.text = text
  if (link !== undefined) action.link = link
  if (linkText !== undefined) action.link_text = linkText
  if (done !== undefined) action.done = done
  return action
}

function parseActions(value: unknown): ProgressAction[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.filter(isRecord).map(parseAction)
}

function parseOwnerNow(value: Record<string, unknown>): OwnerNow {
  const entry: OwnerNow = {}
  const name = asString(value.name)
  const work = asString(value.work)
  const urgency = asString(value.urgency)
  const since = asNumber(value.since)
  if (name !== undefined) entry.name = name
  if (work !== undefined) entry.work = work
  if (urgency !== undefined) entry.urgency = urgency
  if (since !== undefined) entry.since = since
  const actions = parseActions(value.actions)
  if (actions !== undefined) entry.actions = actions
  const approval = parseApproval(value.approval)
  if (approval !== undefined) entry.approval = approval
  return entry
}

function parseOwnerNext(value: unknown): OwnerNext | null | undefined {
  if (value == null) return null
  if (!isRecord(value)) return undefined
  const next: OwnerNext = {}
  const name = asString(value.name)
  const at = asNumber(value.at)
  const work = asString(value.work)
  if (name !== undefined) next.name = name
  if (at !== undefined) next.at = at
  if (work !== undefined) next.work = work
  return next
}

function parseCoordinator(value: unknown): ProgressCoordinator | undefined {
  if (!isRecord(value)) return undefined
  const coordinator: ProgressCoordinator = {}
  const at = asNumber(value.at)
  const state = asString(value.state)
  const note = asString(value.note)
  const doing = asString(value.doing)
  const next = asString(value.next)
  if (at !== undefined) coordinator.at = at
  if (state !== undefined) coordinator.state = state
  if (note !== undefined) coordinator.note = note
  if (doing !== undefined) coordinator.doing = doing
  if (next !== undefined) coordinator.next = next
  return coordinator
}

function parseNext(value: unknown): ProgressNext | null | undefined {
  if (value == null) return null
  if (!isRecord(value)) return undefined
  const next: ProgressNext = {}
  const name = asString(value.name)
  const who = asString(value.who)
  const at = asNumber(value.at)
  const needs = asBoolean(value.needs_owner)
  if (name !== undefined) next.name = name
  if (who !== undefined) next.who = who
  if (at !== undefined) next.at = at
  if (needs !== undefined) next.needs_owner = needs
  return next
}

function parseStep(value: Record<string, unknown>): ProgressStep {
  const step: ProgressStep = {}
  const strings: Array<keyof Pick<ProgressStep, 'name' | 'who' | 'state' | 'label' | 'note' | 'doing'>> = [
    'name',
    'who',
    'state',
    'label',
    'note',
    'doing',
  ]
  for (const key of strings) {
    const text = asString(value[key])
    if (text !== undefined) step[key] = text
  }
  const numbers: Array<keyof Pick<ProgressStep, 'started' | 'ended' | 'last' | 'usual_min' | 'eta_start' | 'eta_end' | 'overdue'>> = [
    'started',
    'ended',
    'last',
    'usual_min',
    'eta_start',
    'eta_end',
    'overdue',
  ]
  for (const key of numbers) {
    const n = asNumber(value[key])
    if (n !== undefined) step[key] = n
  }
  const needs = asBoolean(value.needs_owner)
  if (needs !== undefined) step.needs_owner = needs
  const parallel = asBoolean(value.parallel)
  if (parallel !== undefined) step.parallel = parallel
  const waits = asString(value.waits_for)
  if (waits !== undefined) step.waits_for = waits
  const approval = parseApproval(value.approval)
  if (approval !== undefined) step.approval = approval
  const events = parseEvents(value.events)
  if (events !== undefined) step.events = events
  return step
}

function parseWork(value: Record<string, unknown>): ProgressWork {
  const work: ProgressWork = {}
  const strings: Array<keyof Pick<ProgressWork, 'title' | 'short' | 'goal' | 'risk' | 'after'>> = [
    'title',
    'short',
    'goal',
    'risk',
    'after',
  ]
  for (const key of strings) {
    const text = asString(value[key])
    if (text !== undefined) work[key] = text
  }
  const round = asNumber(value.round)
  const end = asNumber(value.end)
  const sure = asBoolean(value.sure)
  const finished = asBoolean(value.finished)
  if (round !== undefined) work.round = round
  if (end !== undefined) work.end = end
  if (sure !== undefined) work.sure = sure
  if (finished !== undefined) work.finished = finished
  if ('next' in value) {
    const next = parseNext(value.next)
    if (next !== undefined) work.next = next
  }
  if ('owner' in value) {
    const owner = parseOwnerTurn(value.owner)
    if (owner !== undefined) work.owner = owner
  }
  if (Array.isArray(value.steps)) work.steps = value.steps.filter(isRecord).map(parseStep)
  return work
}

function parseFeedItem(value: Record<string, unknown>): ProgressFeedItem {
  const item: ProgressFeedItem = {}
  const at = asNumber(value.at)
  if (at !== undefined) item.at = at
  const strings: Array<keyof Pick<ProgressFeedItem, 'work' | 'step' | 'who' | 'state' | 'text'>> = [
    'work',
    'step',
    'who',
    'state',
    'text',
  ]
  for (const key of strings) {
    const text = asString(value[key])
    if (text !== undefined) item[key] = text
  }
  return item
}

/** Keep fields the board can render. Elements of the wrong kind are dropped. */
export function parseProgressDocument(raw: unknown): ProgressDocument | null {
  if (!isRecord(raw)) return null
  const doc: ProgressDocument = {}
  const at = asNumber(raw.at)
  const verdict = asString(raw.verdict)
  const level = asString(raw.level)
  const sure = asBoolean(raw.sure)
  if (at !== undefined) doc.at = at
  if (verdict !== undefined) doc.verdict = verdict
  if (level !== undefined) doc.level = level
  if (sure !== undefined) doc.sure = sure
  if (isRecord(raw.coordinator)) doc.coordinator = parseCoordinator(raw.coordinator)
  if (Array.isArray(raw.owner_now)) doc.owner_now = raw.owner_now.filter(isRecord).map(parseOwnerNow)
  if ('owner_next' in raw) {
    const next = parseOwnerNext(raw.owner_next)
    if (next !== undefined) doc.owner_next = next
  }
  if (Array.isArray(raw.needs_you)) doc.needs_you = raw.needs_you.filter((item): item is string => typeof item === 'string')
  if (Array.isArray(raw.works)) doc.works = raw.works.filter(isRecord).map(parseWork)
  if (Array.isArray(raw.feed)) doc.feed = raw.feed.filter(isRecord).map(parseFeedItem)
  if (Array.isArray(raw.recent)) doc.recent = raw.recent.filter((item): item is string => typeof item === 'string')
  const roadmap = parseRoadmap(raw.roadmap)
  if (roadmap !== undefined) doc.roadmap = roadmap
  const usage = parseUsage(raw.usage)
  if (usage !== undefined) doc.usage = usage
  const queue = parseQueue(raw.queue)
  if (queue !== undefined) doc.queue = queue
  return doc
}

/** Runtime parse. A weird body becomes an empty envelope instead of a throw. */
export function parseAgentProgress(body: unknown): AgentProgressResponse {
  if (!isRecord(body)) {
    return {
      document: null,
      received_at: '',
      age_seconds: 0,
      stale: false,
      host: '',
      generated_at: '',
      generation_lag_seconds: 0,
    }
  }
  return {
    document: parseProgressDocument(body.document),
    received_at: asString(body.received_at) ?? '',
    age_seconds: asNumber(body.age_seconds) ?? 0,
    stale: body.stale === true,
    host: asString(body.host) ?? '',
    generated_at: asString(body.generated_at) ?? '',
    generation_lag_seconds: asNumber(body.generation_lag_seconds) ?? 0,
  }
}

function isAbortError(err: unknown): boolean {
  return (err instanceof DOMException || err instanceof Error) && err.name === 'AbortError'
}

async function readAgentProgress(signal: AbortSignal): Promise<Response> {
  if (operatorToken() !== '') return authedFetch('Agent progress', '/api/v1/agent/progress', { signal })
  const headers = new Headers()
  const token = readApprovalToken()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch('/api/v1/agent/progress', { headers, signal })
  if (!response.ok) throw await parseError('Agent progress', response)
  return response
}

/** Viewer-level read, with the same token fallback as the other console reads. */
export async function fetchAgentProgress(): Promise<AgentProgressResponse> {
  const controller = new AbortController()
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, AGENT_PROGRESS_TIMEOUT_MS)
  try {
    const response = await readAgentProgress(controller.signal)
    return parseAgentProgress(await response.json())
  } catch (err) {
    if (timedOut || isAbortError(err)) throw new Error('timed out', { cause: err })
    throw err
  } finally {
    clearTimeout(timer)
  }
}

/** `45s`, `12m`, `1h 05m`, `2d 3h`. */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(Number.isFinite(seconds) ? seconds : 0))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ${String(m % 60).padStart(2, '0')}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}

/** Local clock time for a unix-seconds timestamp. */
export function formatLocalTime(unixSeconds: number): string {
  if (!Number.isFinite(unixSeconds)) return ''
  const date = new Date(unixSeconds * 1000)
  if (Number.isNaN(date.getTime())) return 'unknown time'
  return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
}

export function ageSeconds(fromUnix: number, nowMs: number): number {
  if (!Number.isFinite(fromUnix) || fromUnix <= 0) return 0
  return Math.max(0, Math.floor(nowMs / 1000 - fromUnix))
}

export function waitingLine(sinceUnix: number, nowMs: number): string {
  if (!(sinceUnix > 0)) return 'waiting'
  return `waiting ${formatDuration(ageSeconds(sinceUnix, nowMs))}`
}

export function publishedAgeSeconds(receivedAt: string, fallback: number, nowMs: number): number {
  const at = Date.parse(receivedAt)
  if (!Number.isFinite(at)) return Math.max(0, Math.floor(fallback))
  return Math.max(0, Math.floor((nowMs - at) / 1000))
}

export function staleSentence(age: number): string {
  return `Progress was last published ${formatDuration(age)} ago. The coordinating thread may be offline.`
}

export function computedAgeLine(at: number, nowMs: number): string {
  return `computed ${formatDuration(ageSeconds(at, nowMs))} ago`
}

export function skewSentence(at: number, nowMs: number): string {
  const delta = Math.abs(Math.floor(nowMs / 1000 - at))
  return `The published progress was computed ${formatDuration(delta)} ago; it may not reflect the present.`
}

/** True when the publication is older than 5 minutes on this clock. */
export function receivedIsStale(receivedAt: string, fallbackAge: number, nowMs: number): boolean {
  return publishedAgeSeconds(receivedAt, fallbackAge, nowMs) > STALE_AFTER_SECONDS
}

/**
 * True when the document's `at` is more than 2 minutes away from when it was
 * received, or more than 2 minutes ahead of now.
 */
export function generationIsSkewed(at: number | undefined, receivedAt: string, nowMs: number): boolean {
  if (typeof at !== 'number' || !Number.isFinite(at)) return false
  const receivedMs = Date.parse(receivedAt)
  if (Number.isFinite(receivedMs)) {
    const lag = receivedMs / 1000 - at
    if (lag > GENERATION_SKEW_SECONDS || lag < -GENERATION_SKEW_SECONDS) return true
  }
  return at - nowMs / 1000 > GENERATION_SKEW_SECONDS
}

export function stepStateLabel(state: string | undefined, needsOwner: boolean | undefined): string {
  if (needsOwner === true && (state === 'running' || state === 'gap')) return 'waiting for you'
  if (needsOwner === true && state === 'waiting') return 'not your turn yet'
  switch (state) {
    case 'running':
      return 'running'
    case 'slow':
      return 'slower than usual'
    case 'quiet':
      return 'may be stuck'
    case 'dead':
      return 'process gone'
    case 'failed':
      return 'failed'
    case 'done':
      return 'done'
    case 'gap':
      return 'waiting for the coordinator'
    case 'waiting':
      return 'not started'
    default:
      return state != null && state.trim() !== '' ? state : 'unknown'
  }
}

export function elapsedLine(
  step: Pick<ProgressStep, 'started' | 'ended' | 'usual_min'>,
  nowMs: number,
): string | null {
  const started = step.started ?? 0
  if (!(started > 0)) return null
  const ended = step.ended ?? 0
  const end = ended > 0 ? ended : nowMs / 1000
  const elapsed = Math.max(0, Math.floor(end - started))
  const usual = step.usual_min ?? 0
  const usualText = Number.isFinite(usual) ? String(usual) : '0'
  return `elapsed ${formatDuration(elapsed)} of usual ${usualText} min`
}

export function expectationLine(step: Pick<ProgressStep, 'overdue' | 'eta_end' | 'eta_start'>): string | null {
  const overdue = step.overdue ?? 0
  if (overdue > 0) return `late by ${formatDuration(overdue)}`
  const etaEnd = step.eta_end ?? 0
  const at = etaEnd > 0 ? etaEnd : (step.eta_start ?? 0)
  if (!(at > 0)) return null
  return `expected ${formatLocalTime(at)}`
}

export function lastOutputLine(step: Pick<ProgressStep, 'state' | 'last'>, nowMs: number): string | null {
  if (step.state !== 'running' && step.state !== 'slow') return null
  const last = step.last ?? 0
  if (!(last > 0)) return null
  return `last output ${formatDuration(ageSeconds(last, nowMs))} ago`
}

const ACTIVE_STATES = new Set(['running', 'slow', 'quiet', 'dead', 'waiting'])

export function activeStepText(step: ProgressStep): string {
  if (!ACTIVE_STATES.has(step.state ?? '') && step.needs_owner !== true) return ''
  const text = (step.note ?? '').trim() || (step.doing ?? '').trim()
  if (text === '') return ''
  const name = (step.name ?? '').trim()
  return name === '' ? text : `${name}: ${text}`
}

export function nextLine(next: ProgressNext | null | undefined): string {
  const name = next?.name?.trim() ?? ''
  if (name === '') return ''
  const at = next?.at ?? 0
  if (at > 0) return `Next: ${name} · expected ${formatLocalTime(at)}`
  return `Next: ${name}`
}

export function ownerNextLine(next: OwnerNext): string {
  const name = next.name?.trim() ?? ''
  if (name === '') return ''
  const at = next.at ?? 0
  if (at > 0) return `${name} · expected around ${formatLocalTime(at)}`
  return name
}

export function coordinatorLine(c: ProgressCoordinator, nowMs: number): string {
  const state = c.state?.trim() ?? ''
  const note = c.note?.trim() ?? ''
  const parts = [state, note].filter(part => part !== '')
  let line = parts.join(' — ')
  const at = c.at ?? 0
  if (at > 0) {
    const ago = `${formatDuration(ageSeconds(at, nowMs))} ago`
    line = line === '' ? ago : `${line} · ${ago}`
  }
  return line
}

export type ActionLinkView =
  | { kind: 'anchor'; href: string; label: string; external: boolean }
  | { kind: 'blocked'; text: string; host: string }
  | { kind: 'text'; text: string }

/** The scheme's default port: none written, 443 on https, or 80 on http. */
function isDefaultPort(url: URL): boolean {
  if (url.port === '') return true
  if (url.protocol === 'https:' && url.port === '443') return true
  if (url.protocol === 'http:' && url.port === '80') return true
  return false
}

function shownHost(url: URL): string {
  const name = url.hostname.toLowerCase()
  if (url.port === '') return name
  return `${name}:${url.port}`
}

/** Same bound as an action's own text on the Needs you now card. */
const ACTION_LINK_LABEL_MAX = 200

/**
 * Visible label of an anchor. Prefer link text, then the action text, then the
 * link itself. Each candidate goes through the shared document cleaner; the
 * one that is shown is bounded like other action text. Nothing left to read,
 * including a fallback that is only the hash mark, is the console's own
 * "Open link". The href stays the original link, never this label.
 */
function actionAnchorLabel(linkText: string, text: string, raw: string, where: string): string {
  const authored = sanitizeDocumentText(linkText) || sanitizeDocumentText(text)
  const fallback = sanitizeDocumentText(raw)
  const body = authored || (fallback === '#' ? '' : fallback)
  if (body === '') return 'Open link'
  return boundDocumentText(`${body} (${where})`, ACTION_LINK_LABEL_MAX)
}

/**
 * A link opens only as a same-page hash, or as http(s) on TRUSTED_PROGRESS_HOSTS
 * using that scheme's default port. The label names the destination. A trusted
 * host on any other port stays text, and the note includes the host and port.
 */
export function actionLink(action: Pick<ProgressAction, 'text' | 'link' | 'link_text'>): ActionLinkView {
  const text = action.text ?? ''
  const linkText = action.link_text?.trim() ?? ''
  const raw = action.link?.trim() ?? ''
  if (raw === '') return { kind: 'text', text }

  if (raw.startsWith('#')) {
    return {
      kind: 'anchor',
      href: raw,
      label: actionAnchorLabel(linkText, text, raw, 'this console'),
      external: false,
    }
  }

  let url: URL | null = null
  try {
    url = new URL(raw)
  } catch {
    // not a URL; shown as text
  }
  const host = url?.hostname.toLowerCase() ?? ''
  const trusted = (TRUSTED_PROGRESS_HOSTS as readonly string[]).includes(host)
  const http = url != null && (url.protocol === 'http:' || url.protocol === 'https:')
  if (url != null && http && trusted && isDefaultPort(url)) {
    url.username = ''
    url.password = ''
    return {
      kind: 'anchor',
      href: url.href,
      label: actionAnchorLabel(linkText, text, raw, host),
      external: true,
    }
  }
  return { kind: 'blocked', text, host: url != null && http ? shownHost(url) : host }
}
