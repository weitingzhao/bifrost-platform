import { authedFetch } from '@/api/client'
import { signInSafeError } from '@/lib/signInError'
import { record, text } from '@/api/agentRuntime'
import { resolveOpsToolUrl } from '@/lib/architecture/opsToolRackCatalog'
import { approvalHref, goalHref } from '@/lib/shell/consoleRoutes'

/**
 * The AI assessment of a card or an approval (TD-391, the Owner 2026-10-20): advice beside the runtime
 * rule's recommendation, never an answer. The runtime keeps it on the item's own stream,
 * `assess:card:<question id>` or `assess:approval:<number>` (api/internal/agentrt/store/assess.go), read
 * through GET /api/v1/agentrt/events?stream=: first the Owner request, then the ask (its refs), then the assessment (its reasons,
 * each ref resolved to the record it names) or why there is none. A stream with no events is an item
 * that was never asked (one older than assessments, or a platform that gives none).
 */
export type AssessmentKind = 'card' | 'approval'

export type AssessmentRef = { ref: string; kind: string; label: string; href: string | null; external: boolean }
export type AssessmentReason = { text: string; evidence: AssessmentRef[] }
export type Confidence = 'high' | 'medium' | 'low'

export type Assessment =
  | { state: 'none' }
  | { state: 'requested'; requestedAt: string }
  | { state: 'pending'; askedAt: string }
  | { state: 'unavailable'; reason: string }
  | { state: 'ready'; recommended: string; confidence: Confidence; reasons: AssessmentReason[]; unchecked: string[]; by: string }

const CONFIDENCES: readonly string[] = ['high', 'medium', 'low']

export function assessmentStream(kind: AssessmentKind, id: string): string {
  return `assess:${kind}:${id}`
}

/**
 * Where a cited record opens: a commit or a ledger item in Gitea (a new tab), a runtime event on its
 * goal's card, an approval on its request page. null for a record this Console cannot place.
 */
export function refHref(r: Record<string, unknown>, gitea = resolveOpsToolUrl('gitea')): { href: string | null; external: boolean } {
  const org = text(r.org)
  const repo = text(r.repo)
  switch (text(r.kind)) {
    case 'commit': {
      const sha = text(r.sha)
      return org !== '' && repo !== '' && /^[0-9a-f]{7,40}$/.test(sha)
        ? { href: `${gitea}/${encodeURIComponent(org)}/${encodeURIComponent(repo)}/commit/${sha}`, external: true }
        : { href: null, external: false }
    }
    case 'ledger': {
      const id = text(r.id)
      const path = text(r.path).split('/').map(encodeURIComponent).join('/')
      return org !== '' && repo !== '' && path !== '' && id !== ''
        ? { href: `${gitea}/${encodeURIComponent(org)}/${encodeURIComponent(repo)}/src/branch/main/${path}#user-content-${id.toLowerCase()}`, external: true }
        : { href: null, external: false }
    }
    case 'event': {
      const mission = text(r.mission)
      return mission !== '' ? { href: goalHref(mission), external: false } : { href: null, external: false }
    }
    case 'approval': {
      const n = text(r.number)
      return /^\d+$/.test(n) ? { href: approvalHref(n), external: false } : { href: null, external: false }
    }
  }
  return { href: null, external: false }
}

function evidenceOf(value: unknown): AssessmentRef[] {
  if (!Array.isArray(value)) return []
  return value.map(v => {
    const r = record(v)
    const { href, external } = refHref(r)
    return { ref: text(r.ref), kind: text(r.kind), label: text(r.label) || text(r.ref), href, external }
  }).filter(e => e.ref !== '')
}

/** The stream's events read as one assessment; a body this Console cannot read is no assessment. */
export function parseAssessmentStream(value: unknown): Assessment {
  if (!Array.isArray(value)) throw new Error('AI assessment: invalid platform response.')
  let asked = ''
  let requested = ''
  for (const v of value) {
    const e = record(v)
    if (text(e.event_type) !== 'note') continue
    const body = record(e.body_json)
    if (body.assessment_requested != null && requested === '') requested = text(e.at)
    if (body.ask != null && asked === '') asked = text(e.at)
    if (typeof body.unavailable === 'string' && body.unavailable !== '') return { state: 'unavailable', reason: body.unavailable }
    if (body.assessment != null) {
      const a = record(body.assessment)
      const confidence = text(a.confidence)
      const reasons = Array.isArray(a.reasons)
        ? a.reasons.map(r => ({ text: text(record(r).text), evidence: evidenceOf(record(r).evidence) })).filter(r => r.text !== '')
        : []
      if (text(a.recommended) === '' || !CONFIDENCES.includes(confidence) || reasons.length === 0) {
        return { state: 'unavailable', reason: 'this Console cannot read the assessment the runtime recorded' }
      }
      const unchecked = Array.isArray(a.unchecked) ? a.unchecked.map(u => text(u)).filter(u => u !== '') : []
      return { state: 'ready', recommended: text(a.recommended), confidence: confidence as Confidence, reasons, unchecked, by: text(a.by) }
    }
  }
  return asked !== '' ? { state: 'pending', askedAt: asked } : requested !== '' ? { state: 'requested', requestedAt: requested } : { state: 'none' }
}

export function assessmentKey(kind: AssessmentKind, id: string) {
  return ['agentrt', 'assessment', kind, id] as const
}

export async function fetchAssessment(kind: AssessmentKind, id: string, signal?: AbortSignal): Promise<Assessment> {
  const params = new URLSearchParams({ stream: assessmentStream(kind, id) })
  const r = await authedFetch('AI assessment', `/api/v1/agentrt/events?${params}`, { signal }, assessmentResponseError)
  return parseAssessmentStream(await r.json())
}

/** An approval's options as its assessment names them. */
export function approvalOptionLabel(key: string): string {
  return key === 'approve' ? 'Approve' : key === 'reject' ? 'Reject' : key
}

/** How often a page reads an assessment that may still come. */
export const ASSESSMENT_PENDING_REFRESH_MS = 60_000

/**
 * One line for a list row: the runtime rule's recommendation and the AI assessment as one when they agree,
 * both when they differ; an approval has the assessment alone. `label` names an option by its key.
 */
export function assessmentLines(
  a: Assessment | undefined,
  rule: { key: string; line: string } | null,
  label: (key: string) => string,
): string[] {
  const ruleLine = rule != null ? [rule.line] : []
  if (a == null || a.state === 'none') return ruleLine
  if (a.state === 'requested') return [...ruleLine, 'AI assessment: requested']
  if (a.state === 'pending') return [...ruleLine, 'AI assessment: in progress']
  if (a.state === 'unavailable') return [...ruleLine, `AI assessment unavailable: ${a.reason}`]
  const ai = `${label(a.recommended)} (${a.confidence} confidence)`
  if (rule != null && rule.key === a.recommended) return [`${rule.line} · the AI assessment agrees (${a.confidence} confidence)`]
  if (rule != null) return [`Runtime rule: ${rule.line.replace(/^Recommended: /, '')}`, `AI assessment: ${ai}`]
  return [`AI assessment: ${ai}`]
}

/** Snapshot carried by the ordinary approval/question read, never persisted in its material. */
export type AssessmentSnapshot = { events?: unknown; error?: string }

export function assessmentFromSnapshot(snapshot?: AssessmentSnapshot): Assessment {
  if (snapshot?.error) return { state: 'unavailable', reason: snapshot.error }
  try {
    return parseAssessmentStream(snapshot?.events ?? [])
  } catch {
    return { state: 'unavailable', reason: 'this Console cannot read the assessment record' }
  }
}

export async function requestAssessment(kind: AssessmentKind, id: string): Promise<Assessment> {
  const r = await authedFetch('Request AI assessment', `/api/v1/agentrt/assessments/${kind}/${encodeURIComponent(id)}`, { method: 'POST' }, assessmentResponseError)
  return parseAssessmentStream(await r.json())
}

// Runtime errors carry the human reason in message, and a short code in error.
async function assessmentResponseError(prefix: string, response: Response, token = ''): Promise<Error> {
  let message = `HTTP ${response.status}`
  try {
    const body = record(await response.json())
    message = text(body.message) || text(body.error) || message
  } catch { /* Keep the HTTP status for non-JSON failures. */ }
  return new Error(signInSafeError(`${prefix}: ${message}`, token, response.status))
}
