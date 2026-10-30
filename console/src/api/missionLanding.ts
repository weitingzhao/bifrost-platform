import { authHeaders } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import { parseLandingWait, type LandingWait } from '@/api/landingGrant'

export type StgReleaseStatus = {
  batch: string; try: number; phase: string; budget_used: number; budget_gen: number; quarantined: boolean
  blocked?: { why: string; since: string }
}

export type MissionLandingRoot = {
  /** Absent only on responses from the previous platform-only server. */
  repo?: 'bifrost-platform' | 'bifrost-trade-infra'
  grant_supported?: boolean
  floor?: { soft?: unknown[]; grant_supported?: boolean; [key: string]: unknown }

  root: string; op: string; class: 'delivered' | 'disposed' | 'unresolved'; reason: string
  needs_evidence: boolean; card_kind: string; question_id: string
  /** A card of this root ended with its answer unknown (review 670 R-07). */
  card_alert?: string
  /** A parked root: since when, its retry, what it shows and when it is told (review 672 R-07). */
  parked?: { since: string; shown: string; retry_at?: string; remind_at?: string }
  /** The Owner's answer holds the root until then, on that question (review 675 R-07). */
  held?: { until: string; question_id: string }
  /** A card for the root is prepared and not yet asked. */
  card_pending?: boolean
  authorization_wait?: LandingWait
  stg?: StgReleaseStatus
}
export type MissionLanding = {
  mission_id: string; in_scope: boolean; rule: string; roots: MissionLandingRoot[]
  /** Present only while the round is open. */
  closing_round?: { round: number; at: string; until: string; ops: string[] }
  /** The close as decided then (review 672 N-01). */
  closed?: { to: 'done' | 'stopped'; at: string; rule: string; round: number; delivered: number; disposed: number; briefs: number }
}

const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const time = (v: unknown): v is string => typeof v === 'string' && Number.isFinite(Date.parse(v))

function isStg(v: unknown): v is StgReleaseStatus {
  return object(v) && typeof v.batch === 'string' && typeof v.phase === 'string' && v.phase.trim() !== ''
    && ['try', 'budget_used', 'budget_gen'].every(k => typeof v[k] === 'number' && Number.isSafeInteger(v[k]) && (v[k] as number) >= 0)
    && (v.budget_gen as number) >= 1 && (v.budget_used as number) <= 3 && typeof v.quarantined === 'boolean'
    && (!('blocked' in v) || (object(v.blocked) && typeof v.blocked.why === 'string' && time(v.blocked.since)))
}

function isRoot(v: unknown): v is MissionLandingRoot {
  return object(v) && typeof v.root === 'string' && v.root.trim() !== ''
    && (!('repo' in v) || v.repo === 'bifrost-platform' || v.repo === 'bifrost-trade-infra')
    && (!('grant_supported' in v) || typeof v.grant_supported === 'boolean')
    && (!('floor' in v) || (object(v.floor) && (!('soft' in v.floor) || Array.isArray(v.floor.soft))
      && (!('grant_supported' in v.floor) || typeof v.floor.grant_supported === 'boolean')))
    && ['op', 'reason', 'card_kind', 'question_id'].every(key => typeof v[key] === 'string')
    && ['delivered', 'disposed', 'unresolved'].some(value => value === v.class)
    && typeof v.needs_evidence === 'boolean'
    && (!('card_alert' in v) || typeof v.card_alert === 'string')
    && (!('parked' in v) || (object(v.parked) && time(v.parked.since) && typeof v.parked.shown === 'string'
      && (!('retry_at' in v.parked) || time(v.parked.retry_at)) && (!('remind_at' in v.parked) || time(v.parked.remind_at))))
    && (!('held' in v) || (object(v.held) && time(v.held.until) && typeof v.held.question_id === 'string'))
    && (!('card_pending' in v) || typeof v.card_pending === 'boolean')
    && (!('authorization_wait' in v) || parseLandingWait(v.authorization_wait) !== null)
    && (!('stg' in v) || isStg(v.stg))
}

/** Missing or mistyped fields are unreadable; unrelated additions remain compatible. */
export function parseMissionLanding(v: unknown): MissionLanding | null {
  if (!object(v) || typeof v.mission_id !== 'string' || !v.mission_id.trim()
    || typeof v.in_scope !== 'boolean' || typeof v.rule !== 'string'
    || !Array.isArray(v.roots) || !v.roots.every(isRoot)) return null
  if ('closing_round' in v) {
    const r = v.closing_round
    if (!object(r) || typeof r.round !== 'number' || !Number.isSafeInteger(r.round) || r.round < 1
      || !time(r.at) || !time(r.until) || Date.parse(r.until) <= Date.parse(r.at)
      || !Array.isArray(r.ops) || !r.ops.every(op => typeof op === 'string')) return null
  }
  if ('closed' in v) {
    const c = v.closed
    if (!object(c) || (c.to !== 'done' && c.to !== 'stopped') || !time(c.at) || typeof c.rule !== 'string'
      || !['round', 'delivered', 'disposed', 'briefs'].every(key => typeof c[key] === 'number' && Number.isSafeInteger(c[key]) && (c[key] as number) >= 0)) return null
  }
  return v as MissionLanding
}

/** An optional read: unavailable, malformed and non-200 responses never block the goal. */
export async function fetchMissionLanding(id: string, signal?: AbortSignal): Promise<MissionLanding | null> {
  try {
    const r = await serverFetch(`/api/v1/agentrt/landing/missions/${encodeURIComponent(id)}`, { headers: authHeaders(), signal })
    if (r.status !== 200) return null
    const landing = parseMissionLanding(await r.json())
    return landing?.mission_id === id ? landing : null
  } catch {
    return null
  }
}
