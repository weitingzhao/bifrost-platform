import { authedFetch, pathSegment, parseError } from '@/api/client'
import { serverFetch } from '@/api/serverContact'

export const LANDING_ACTIONS = ['approve_guardrail_landing', 'approve_core_landing'] as const
export type LandingAction = typeof LANDING_ACTIONS[number]
export const isLandingAction = (v: unknown): v is LandingAction => LANDING_ACTIONS.some(a => a === v)
export const CORE_LANDING_TITLE = 'Trust-core change'
export const CORE_LANDING_NOTICE = 'Approval authorizes the landing identity to push this commit to main. It changes approval, identity or landing-gate code; subsequent approvals will be handled by the new code.'
export const STG_LANDING_NOTICE = 'Approval also authorizes this commit to go to STG with the B5 batch.'
export const GITEA_NOTICE = "Gitea's own page is not rendered by this platform's code."

export type LandingGrant = {
  grant_id: string; approval_id: string; approval_number: number; action: LandingAction; op: string
  waives: string[]; stg_release: boolean; approved_at: string; granted_at: string; expires_at: string
  revoked: boolean; revoked_at?: string; handed_to_push: boolean; main_intent_seq?: number; valid: boolean
  candidate_digest: string; material_digest: string
}
export type LandingWait = {
  phase: string; shown: string; since: string; soft_at_request: string[]; files: number; lines: number
  waiting_snapshot: string; R: string; M0: string; execution_seconds: number; waiting_seconds: number; operations: number
  approval_id?: string; approval_number?: number; approval_status?: string; approval_url?: string
  main_known: boolean; main_changed: boolean; owner_todo: boolean; grants: LandingGrant[]
}
export type LandingCandidate = {
  env: string; tenant: string; repo: string; mission: string; plan_step: string; task: string; attempt: string; op: string
  A: string; base: string; R: string; M0: string; committer_date: string; patch_sha256: string; files: number; lines: number
  changes: { old: string; new: string; old_mode: string; new_mode: string; added: number; deleted: number }[]
  acceptance: { verification_id: string; result: string }[]
  review: { task: string; attempt: string; executor: string; provider: string; conclusion: string; artifact: string }
  review_patch_match: boolean
  ci: { pipeline: string; run: string; uid: string; created_at: string; status: string; sha: string; siblings: Record<string, string> }
  stg_release: boolean; hard_core_paths: { path: string; entry: string }[]; decision_chain: boolean; trust_review: boolean
}
export type LandingMaterial = {
  candidate: LandingCandidate; candidate_digest: string; material_digest: string; soft_at_request: string[]; waiting_soft: string[]
  rule: { id: string; version: number; sha256: string }; checks_version: number; action: LandingAction; title: string; first_line?: string
  phase: string; latest: boolean; pinned: boolean; grant_enabled: boolean; patch?: string; patch_truncated: boolean; patch_size: number
  compare_url: string; gitea_notice: string; quota_reserved_today: number; quota_max_per_day: number; quota_note: string; stg_notice?: string
  grants: LandingGrant[]; problems: string[]; computed_at: string
}
export type LandingMaterialResponse = { material: LandingMaterial; main: string; main_known: boolean; main_is_M0: boolean }
export type LandingRevoke = { grant_id: string; op: string; revoked: boolean; already: boolean; handed_to_push: boolean; main_intent_seq?: number; message: string }

const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const string = (v: unknown): v is string => typeof v === 'string'
const bool = (v: unknown): v is boolean => typeof v === 'boolean'
const count = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0
const delta = (v: unknown): boolean => v === -1 || count(v) // Git numstat uses -1 for binary files.
const time = (v: unknown): boolean => string(v) && Number.isFinite(Date.parse(v))
const list = (v: unknown, check: (v: unknown) => boolean): boolean => Array.isArray(v) && v.every(check)
const strings = (v: unknown): boolean => list(v, string)
const fields = (v: Record<string, unknown>, keys: string, check: (v: unknown) => boolean): boolean => keys.split(' ').every(k => check(v[k]))
const optional = (v: Record<string, unknown>, keys: string, check: (v: unknown) => boolean): boolean => keys.split(' ').every(k => !(k in v) || check(v[k]))

function grant(v: unknown): boolean {
  return object(v) && fields(v, 'grant_id approval_id op candidate_digest material_digest', string)
    && count(v.approval_number) && v.approval_number > 0 && isLandingAction(v.action) && strings(v.waives)
    && fields(v, 'stg_release revoked handed_to_push valid', bool) && fields(v, 'approved_at granted_at expires_at', time)
    && optional(v, 'revoked_at', time) && optional(v, 'main_intent_seq', count)
}
function candidate(v: unknown): boolean {
  if (!object(v) || !fields(v, 'env tenant repo mission plan_step task attempt op A base R M0 committer_date patch_sha256', string)
    || !time(v.committer_date) || !fields(v, 'files lines', count)
    || !fields(v, 'review_patch_match stg_release decision_chain trust_review', bool)) return false
  return list(v.changes, c => object(c) && fields(c, 'old new old_mode new_mode', string) && fields(c, 'added deleted', delta))
    && list(v.acceptance, a => object(a) && fields(a, 'verification_id result', string))
    && list(v.hard_core_paths, p => object(p) && fields(p, 'path entry', string))
    && object(v.review) && fields(v.review, 'task attempt executor provider conclusion artifact', string)
    && object(v.ci) && fields(v.ci, 'pipeline run uid created_at status sha', string) && time(v.ci.created_at)
    && object(v.ci.siblings) && Object.values(v.ci.siblings).every(string)
}

/** Read the handler's envelope and Go JSON tags, never substitute empty data for malformed evidence. */
export function parseLandingMaterial(v: unknown): LandingMaterialResponse | null {
  if (!object(v) || !string(v.main) || !fields(v, 'main_known main_is_M0', bool) || !object(v.material)) return null
  const m = v.material
  if (!candidate(m.candidate) || !fields(m, 'candidate_digest material_digest title phase compare_url gitea_notice quota_note', string)
    || !fields(m, 'soft_at_request waiting_soft problems', strings) || !isLandingAction(m.action)
    || !fields(m, 'latest pinned grant_enabled patch_truncated', bool)
    || !fields(m, 'checks_version patch_size quota_reserved_today quota_max_per_day', count)
    || !optional(m, 'patch first_line stg_notice', string) || !time(m.computed_at) || !list(m.grants, grant)
    || !object(m.rule) || !fields(m.rule, 'id sha256', string) || !count(m.rule.version)) return null
  return v as LandingMaterialResponse
}
export function parseLandingWait(v: unknown): LandingWait | null {
  if (!object(v) || !fields(v, 'phase shown waiting_snapshot R M0', string) || !time(v.since)
    || !strings(v.soft_at_request) || !fields(v, 'files lines execution_seconds waiting_seconds operations', count)
    || !fields(v, 'main_known main_changed owner_todo', bool) || !list(v.grants, grant)
    || !optional(v, 'approval_id approval_status approval_url', string) || !optional(v, 'approval_number', count)) return null
  return v as LandingWait
}
export function parseLandingRevoke(v: unknown): LandingRevoke | null {
  if (!object(v) || !fields(v, 'grant_id op message', string) || !fields(v, 'revoked already handed_to_push', bool)
    || !optional(v, 'main_intent_seq', count) || (v.revoked === v.handed_to_push)
    || (v.handed_to_push && (!count(v.main_intent_seq) || v.main_intent_seq < 1))) return null
  return v as LandingRevoke
}
export function landingGrantHref(op: string, stg = true): string {
  return `#landing-grant?${new URLSearchParams({ op, stg_release: String(stg) })}`
}
/** Only HTTP(S) links are navigable; evidence is otherwise plain text. */
export function giteaCompareHref(value: string): string | undefined {
  try { const url = new URL(value); return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined } catch { return undefined }
}
export async function fetchLandingMaterial(op: string, stg = true, signal?: AbortSignal): Promise<LandingMaterialResponse> {
  const r = await authedFetch('Landing material', `/api/v1/agentrt/landing/${pathSegment(op)}/material?stg_release=${stg}`, { signal }, async (prefix, response, token) => {
    if (response.status === 503) return new Error('Landing material unavailable: Git or CI evidence cannot be read in this process (503).')
    return parseError(prefix, response, token)
  })
  const data = parseLandingMaterial(await r.json())
  if (!data || data.material.candidate.op !== op || data.material.candidate.stg_release !== stg) throw new Error('Landing material: invalid platform response.')
  return data
}
export async function fetchLandingWait(op: string, signal?: AbortSignal): Promise<LandingWait> {
  const r = await authedFetch('Landing status', `/api/v1/agentrt/landing/operations/${pathSegment(op)}`, { signal })
  const data = parseLandingWait(await r.json())
  if (!data || data.grants.some(g => g.op !== op)) throw new Error('Landing status: invalid platform response.')
  return data
}
export async function revokeLandingGrant(id: string, token: string): Promise<LandingRevoke> {
  const headers = new Headers({ 'Content-Type': 'application/json', Authorization: `Bearer ${token}` })
  const r = await serverFetch(`/api/v1/agentrt/landing/grants/${pathSegment(id)}/revoke`, { method: 'POST', headers })
  if (r.status === 503) throw new Error('Revocation result unknown. Refresh to read the landing stream before trying again.')
  if (!r.ok) throw await parseError('Revoke grant', r, token)
  const data = parseLandingRevoke(await r.json())
  if (!data || data.grant_id !== id) throw new Error('Revocation result unreadable. Refresh to read the landing stream.')
  return data
}
