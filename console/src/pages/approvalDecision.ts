import {
  approvalEnv,
  formatParams,
  isUnsetDecidedAt,
  type ApprovalItem,
} from '@/api/approvals'

/** `#n · summary`, or the action name when either piece is missing. */
export function decisionTitle(item: Pick<ApprovalItem, 'number' | 'summary' | 'action'>): string {
  const summary = item.summary?.trim() ?? ''
  const number = item.number
  if (typeof number === 'number' && Number.isFinite(number) && number > 0 && summary !== '') {
    return `#${number} · ${summary}`
  }
  const action = item.action.trim()
  return action !== '' ? action : 'Request'
}

/** What the tier letter means, in plain words. */
export function tierWords(tier: string): string {
  switch (tier.trim().toUpperCase()) {
    case 'B':
      return 'runs without approval'
    case 'C':
      return 'runs as soon as you approve'
    case 'D':
      return 'you approve here, then run one command in a terminal'
    default:
      return ''
  }
}

function requestNumber(value: number | undefined): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return null
  return value
}

/**
 * Approve-list title: `#n` plus the reason's first paragraph, else the summary,
 * else the action name. No `#` when the request has no number.
 */
export function approvalRowTitle(
  item: Pick<ApprovalItem, 'number' | 'reason' | 'summary' | 'action'>,
): string {
  const lead = splitReason(item.reason ?? '').lead.trim()
  const summary = item.summary?.trim() ?? ''
  const action = item.action?.trim() ?? ''
  const body = lead !== '' ? lead : summary !== '' ? summary : action
  const number = requestNumber(item.number)
  if (number == null) return body
  if (body === '') return `#${number}`
  return `#${number} · ${body}`
}

export function decisionEnv(item: Pick<ApprovalItem, 'env' | 'params'>): string {
  const named = item.env?.trim() ?? ''
  if (named !== '') return named
  return approvalEnv(item)
}

function runnerOf(runner: string | undefined): string {
  const value = runner?.trim().toLowerCase() ?? ''
  return value === '' ? 'platform' : value
}

type ExecutionFacts = {
  exitCode: number | null
  durationMs: number | null
  finishedAt: string | null
  outputTail: string | null
  attempts: number | null
  executorId: string | null
}

function executionFacts(value: unknown): ExecutionFacts | null {
  if (value == null || typeof value !== 'object' || Array.isArray(value)) return null
  const raw = value as Record<string, unknown>
  const exitCode = typeof raw.exit_code === 'number' && Number.isFinite(raw.exit_code) ? raw.exit_code : null
  const durationMs =
    typeof raw.duration_ms === 'number' && Number.isFinite(raw.duration_ms) && raw.duration_ms >= 0
      ? raw.duration_ms
      : null
  const finishedAt = typeof raw.finished_at === 'string' && raw.finished_at !== '' ? raw.finished_at : null
  const outputTail = typeof raw.output_tail === 'string' && raw.output_tail !== '' ? raw.output_tail : null
  const attempts = typeof raw.attempts === 'number' && Number.isFinite(raw.attempts) ? raw.attempts : null
  const executorId =
    typeof raw.executor_id === 'string' && raw.executor_id.trim() !== '' ? raw.executor_id.trim() : null
  return { exitCode, durationMs, finishedAt, outputTail, attempts, executorId }
}

/** Seconds for an execution, one decimal when it is not whole: 1688 ms → `1.7 s`. */
export function formatTook(durationMs: number): string {
  const tenths = Math.round(durationMs / 100)
  const seconds = tenths / 10
  const text = Number.isInteger(seconds) ? String(seconds) : seconds.toFixed(1)
  return `${text} s`
}

/**
 * What the Owner should do now. Unknown (and any status we do not recognise)
 * says the result was not confirmed. An exit code replaces the executed / failed
 * sentences: 0 finished, anything else (or failed with a code) failed.
 */
export function decisionBanner(
  item: Pick<ApprovalItem, 'status' | 'runner'> & Partial<Pick<ApprovalItem, 'execution'>>,
): string {
  const status = item.status.trim().toLowerCase()
  const runner = runnerOf(item.runner)
  switch (status) {
    case 'pending':
      return 'Waiting for your decision.'
    case 'approved':
      if (runner === 'owner') return 'Approved. It has not run: run the command below in a terminal.'
      if (runner === 'platform') return 'Approved. Running now.'
      return 'Approved. Waiting for it to run.'
    case 'running':
      if (runner === 'platform') return 'Approved. Running now.'
      return 'Running now.'
    case 'executed':
    case 'failed': {
      const code = executionFacts(item.execution)?.exitCode ?? null
      if (code == null) return status === 'failed' ? 'This failed.' : 'This has run.'
      if (status === 'failed' || code !== 0) return `It ran and failed (exit code ${code}).`
      return 'It ran and finished.'
    }
    case 'rejected':
      return 'This was rejected.'
    case 'expired':
      return 'This has expired.'
    default:
      return 'The result could not be confirmed and must be checked before anything is retried.'
  }
}

/**
 * The one line under a finished or failed banner: exit code, how long it took,
 * and the local finish time. Missing pieces are left out. Go's year 0001 is not a finish time.
 */
export function executionOutcome(
  item: Pick<ApprovalItem, 'status' | 'execution'>,
): string | null {
  const status = item.status.trim().toLowerCase()
  if (status !== 'executed' && status !== 'failed') return null
  const facts = executionFacts(item.execution)
  if (facts?.exitCode == null) return null
  const parts = [`Exit code ${facts.exitCode}`]
  if (facts.durationMs != null) parts.push(`took ${formatTook(facts.durationMs)}`)
  const finished = facts.finishedAt == null ? null : visibleTimestamp(facts.finishedAt)
  if (finished != null) parts.push(`finished ${finished}`)
  return parts.join(' · ')
}

export const UNKNOWN_CANNOT_CLOSE =
  'This page cannot close it yet. Check whether it ran before asking again.'

/** Plain words for an unknown request: what was recorded, and that this page cannot close it. */
export function unknownFollowup(item: Pick<ApprovalItem, 'status' | 'error'>): {
  knows: string | null
  cannotClose: string | null
} {
  if (item.status.trim().toLowerCase() !== 'unknown') return { knows: null, cannotClose: null }
  const text = item.error?.trim() ?? ''
  const knows =
    text === '' ? null : `What the platform knows: ${/[.!?]$/.test(text) ? text : `${text}.`}`
  return { knows, cannotClose: UNKNOWN_CANNOT_CLOSE }
}

export function executionOutputTail(execution: unknown): string | null {
  return executionFacts(execution)?.outputTail ?? null
}

export function executionAttempts(execution: unknown): number | null {
  return executionFacts(execution)?.attempts ?? null
}

export function executionExecutor(execution: unknown): string | null {
  return executionFacts(execution)?.executorId ?? null
}

/**
 * First paragraph is the text before the first newline (a blank line starts
 * there too). The rest keeps its line breaks for the Why section.
 */
export function splitReason(reason: string): { lead: string; rest: string } {
  const text = reason.replace(/\r\n/g, '\n')
  const newline = text.indexOf('\n')
  if (newline < 0) return { lead: text.trim(), rest: '' }
  const lead = text.slice(0, newline).trim()
  let rest = text.slice(newline + 1)
  if (rest.startsWith('\n')) rest = rest.slice(1)
  if (rest.trim() === '') return { lead, rest: '' }
  return { lead, rest }
}

const WORK_ITEM = /\b(W-\d+|TD-\d+|LANE-[A-Za-z0-9][A-Za-z0-9_-]*)/g

/** Work items named in the reason: W-n, TD-n, LANE-…. */
export function workItems(reason: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const match of reason.matchAll(WORK_ITEM)) {
    const id = match[1]
    if (id == null || seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  return out
}

export function commandText(params: Record<string, unknown> | null | undefined): string | null {
  if (params == null) return null
  const command = params.command
  if (typeof command !== 'string' || command === '') return null
  return command
}

const HIDDEN_PARAM_KEYS = new Set(['command', 'reason'])

/** Params JSON without the keys already shown in the decision card. Null when nothing remains. */
export function technicalParams(params: Record<string, unknown> | null | undefined): string | null {
  if (params == null) return null
  const rest: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(params)) {
    if (HIDDEN_PARAM_KEYS.has(key)) continue
    rest[key] = value
  }
  if (Object.keys(rest).length === 0) return null
  return formatParams(rest)
}

/**
 * Result JSON for Technical details. An owner-run result repeats `params.command`;
 * that key is left out when the two strings are equal. Other keys stay.
 * Null when nothing remains.
 */
export function technicalResult(
  result: unknown,
  params: Record<string, unknown> | null | undefined,
): string | null {
  if (result == null) return null
  if (typeof result !== 'object' || Array.isArray(result)) {
    const text = formatParams(result)
    return text === '—' ? null : text
  }
  const rest: Record<string, unknown> = { ...(result as Record<string, unknown>) }
  const command = commandText(params)
  if (command != null && rest.command === command) delete rest.command
  if (Object.keys(rest).length === 0) return null
  return formatParams(rest)
}

export type OwnerTerminal = { line: string; fallback: boolean }

/**
 * The one-line command the Owner runs, when the API already put it on the
 * result (`message`, usually `Run: …`). Otherwise the request id, and the
 * page tells him to run the owner-run script with that id. No path is invented here.
 */
export function ownerTerminal(item: Pick<ApprovalItem, 'id' | 'runner' | 'result'>): OwnerTerminal | null {
  if (item.runner?.trim().toLowerCase() !== 'owner') return null
  const exposed = exposedRunLine(item.result)
  if (exposed != null) return { line: exposed, fallback: false }
  return { line: item.id, fallback: true }
}

/** The API's one-liner is relative, so the Owner has to start in the trade-infra checkout. */
export function runFromTradeInfra(terminal: OwnerTerminal | null): boolean {
  if (terminal == null || terminal.fallback) return false
  return !terminal.line.startsWith('/')
}

function exposedRunLine(result: unknown): string | null {
  if (result == null || typeof result !== 'object' || Array.isArray(result)) return null
  const message = (result as { message?: unknown }).message
  if (typeof message !== 'string') return null
  const trimmed = message.trim()
  if (trimmed === '' || /[\r\n]/.test(trimmed)) return null
  const line = trimmed.replace(/^Run:\s*/i, '').trim()
  if (line === '' || /[\r\n]/.test(line)) return null
  return line
}

/** Locale timestamp, or null for empty / Go zero / year 0001. */
export function visibleTimestamp(value: string | undefined): string | null {
  if (isUnsetDecidedAt(value)) return null
  const ms = Date.parse(value ?? '')
  if (!Number.isFinite(ms)) return null
  if (new Date(ms).getUTCFullYear() <= 1) return null
  return new Date(ms).toLocaleString()
}
