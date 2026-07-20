import { getPlatformOperatorToken } from '@/lib/platformAuth'

export type BriefDecision = 'approved_run' | 'dismissed' | 'hold'
export type BriefSuggestion = 'RUN' | 'DISMISS' | 'HOLD'

export type DecisionBrief = {
  id: string
  item_id: string
  title: string
  created_at: string
  fleet_signal: string
  fleet_detail: string
  item_age: string
  fix_scope: string | null
  risk_level: string
  suggestion: BriefSuggestion | string
  suggestion_reason: string
  open_question?: string
  full_brief: string
  decision?: BriefDecision | ''
  decided_at?: string
  /** RFC3339 — set when decision=hold */
  hold_until?: string
  failing_standards?: string
  source?: string
}

export type SweepVerdict = 'STALE' | 'STILL_NEEDED' | 'NEEDS_DECISION' | 'IN_PROGRESS'

export type SweepResult = {
  item_id: string
  title: string
  verdict: SweepVerdict | string
  reason: string
}

export type SweepRequest = {
  auto_drain?: boolean
}

export type SweepResponse = {
  dismissed: SweepResult[]
  queued: SweepResult[]
  decisions: DecisionBrief[]
  in_progress: SweepResult[]
  next_sweep_at?: string
}

/** Mirrors api/internal/operatequeue DrainStatus JSON. */
export type OperateDrainStatus = {
  active?: boolean
  current_item_id?: string
  current_job_id?: string
  current_title?: string
  queued_count?: number
  queued_item_ids?: string[]
  paused?: boolean
  pause_reason?: string
  last_error?: string
  last_completed_at?: string
  last_sweep_summary?: string
}

export type DecideBriefResponse = {
  brief: DecisionBrief
  item?: unknown
  drain?: OperateDrainStatus
}

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string }
    detail = body.error ?? body.message ?? detail
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

function authHeaders(json = false): Headers {
  const headers = new Headers()
  if (json) headers.set('Content-Type', 'application/json')
  const token = getPlatformOperatorToken()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  return headers
}

export const OPERATE_BRIEFS_QUERY_KEY = ['operate', 'briefs'] as const
export const OPERATE_DRAIN_STATUS_QUERY_KEY = ['operate', 'drain', 'status'] as const
export const OPERATE_SWEEP_LAST_KEY = ['operate', 'sweep', 'last'] as const

export async function fetchDecisionBriefs(): Promise<DecisionBrief[]> {
  const r = await fetch('/api/v1/operate/briefs')
  if (!r.ok) throw await parseError('operate briefs', r)
  const body = (await r.json()) as DecisionBrief[] | { briefs?: DecisionBrief[]; items?: DecisionBrief[] }
  if (Array.isArray(body)) return body
  return body.briefs ?? body.items ?? []
}

export async function decideOnBrief(
  id: string,
  decision: BriefDecision,
): Promise<DecideBriefResponse> {
  const r = await fetch(`/api/v1/operate/briefs/${encodeURIComponent(id)}/decide`, {
    method: 'POST',
    headers: authHeaders(true),
    body: JSON.stringify({ decision }),
  })
  if (!r.ok) throw await parseError('operate brief decide', r)
  if (r.status === 204) {
    throw new Error('operate brief decide: empty response')
  }
  const text = await r.text()
  if (text.trim() === '') {
    throw new Error('operate brief decide: empty response')
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(text) as unknown
  } catch {
    throw new Error('operate brief decide: invalid JSON')
  }
  if (parsed != null && typeof parsed === 'object' && 'brief' in parsed) {
    const wrapped = parsed as DecideBriefResponse
    if (wrapped.brief != null && typeof wrapped.brief === 'object') {
      return wrapped
    }
  }
  // Legacy flat DecisionBrief body
  if (parsed != null && typeof parsed === 'object' && 'id' in parsed && 'item_id' in parsed) {
    return { brief: parsed as DecisionBrief }
  }
  throw new Error('operate brief decide: unexpected response shape')
}

export async function postOperateSweep(body: SweepRequest = {}): Promise<SweepResponse> {
  const r = await fetch('/api/v1/operate/sweep', {
    method: 'POST',
    headers: authHeaders(true),
    body: JSON.stringify({ auto_drain: body.auto_drain ?? false }),
  })
  if (!r.ok) throw await parseError('operate sweep', r)
  return r.json() as Promise<SweepResponse>
}

export async function fetchOperateDrainStatus(): Promise<OperateDrainStatus | null> {
  const r = await fetch('/api/v1/operate/drain/status')
  if (r.status === 404) return null
  if (!r.ok) throw await parseError('operate drain status', r)
  return r.json() as Promise<OperateDrainStatus>
}

/** Human-readable sweep summary for toast / OpsFeedback. */
export function formatSweepSummary(result: SweepResponse): string {
  const dismissed = result.dismissed?.length ?? 0
  const decisions = result.decisions?.length ?? 0
  const queued = result.queued?.length ?? 0
  const inProgress = result.in_progress?.length ?? 0
  const parts: string[] = []
  if (dismissed > 0) {
    parts.push(`Dismissed ${dismissed} stale item${dismissed === 1 ? '' : 's'}`)
  }
  if (decisions > 0) {
    parts.push(`${decisions} need${decisions === 1 ? 's' : ''} decision`)
  }
  if (queued > 0) {
    parts.push(`Queued ${queued} for drain`)
  }
  if (inProgress > 0) {
    parts.push(`${inProgress} in progress`)
  }
  if (parts.length === 0) return 'Sweep complete — queue already clean'
  return parts.join(', ')
}
