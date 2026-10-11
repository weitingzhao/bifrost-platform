import { viewerRead } from '@/api/approvals'

/** How often the Progress page re-reads the published document. */
export const AGENT_PROGRESS_POLL_MS = 15_000

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
  urgency?: string
  since?: number
  actions?: ProgressAction[]
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
}

export type AgentProgressResponse = {
  document: ProgressDocument | null
  received_at: string
  age_seconds: number
  stale: boolean
  host: string
}

/** Viewer-level read, with the same token fallback as the other console reads. */
export async function fetchAgentProgress(): Promise<AgentProgressResponse> {
  const r = await viewerRead('Agent progress', '/api/v1/agent/progress')
  const body = (await r.json()) as Partial<AgentProgressResponse>
  return {
    document: body.document ?? null,
    received_at: body.received_at ?? '',
    age_seconds: body.age_seconds ?? 0,
    stale: body.stale === true,
    host: body.host ?? '',
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
  return new Date(unixSeconds * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
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

export function stepStateLabel(state: string | undefined, needsOwner: boolean | undefined): string {
  if (needsOwner) return 'waiting for you'
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
      return 'not started'
    case 'waiting':
      return 'waiting for the coordinator'
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

/** http(s) only. Anything else is shown as text, not opened. */
export function safeLink(link: string | undefined): string {
  const value = link?.trim() ?? ''
  if (value.startsWith('https://') || value.startsWith('http://')) return value
  return ''
}
