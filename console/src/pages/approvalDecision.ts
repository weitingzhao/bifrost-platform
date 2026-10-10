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

export function decisionEnv(item: Pick<ApprovalItem, 'env' | 'params'>): string {
  const named = item.env?.trim() ?? ''
  if (named !== '') return named
  return approvalEnv(item)
}

function runnerOf(runner: string | undefined): string {
  const value = runner?.trim().toLowerCase() ?? ''
  return value === '' ? 'platform' : value
}

/**
 * What the Owner should do now. Unknown (and any status we do not recognise)
 * says the result was not confirmed.
 */
export function decisionBanner(item: Pick<ApprovalItem, 'status' | 'runner'>): string {
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
      return 'This has run.'
    case 'failed':
      return 'This failed.'
    case 'rejected':
      return 'This was rejected.'
    case 'expired':
      return 'This has expired.'
    default:
      return 'The result could not be confirmed and must be checked before anything is retried.'
  }
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
