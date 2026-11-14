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






/** Snapshot carried by the ordinary approval/question read, never persisted in its material. */
export type AssessmentSnapshot = { events?: unknown; error?: string }



