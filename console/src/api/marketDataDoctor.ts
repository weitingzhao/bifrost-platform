/**
 * GET /market/doctor · POST /market/doctor/heal — check now, fix now.
 *
 * The doctor names what the last session should hold and what it does, one
 * finding per check, each with a prescription the plugin can execute. GET is
 * a bare fetch through the platform-api proxy like the other reads; heal is
 * an operator POST (platform-api swaps the bearer for the plugin write token).
 */
import { authedFetch } from './client'

/**
 * `boundary` is the coverage matrix's own word, reused: a boundary is not a
 * gap, the vendor cannot backfill it. It separates a limit no action on our
 * side moves from a fault a refetch would fix, so neither gets painted as the
 * other. Only `crit` and `warn` move a verdict.
 */
export type DoctorSeverity = 'ok' | 'warn' | 'boundary' | 'crit'
export type DoctorVerdict = 'healthy' | 'degraded' | 'critical'

export type DoctorFix = {
  action:
    | 'enqueue-slot'
    | 'enqueue'
    | 'retry-jobs'
    | 'rollout-restart'
    | 'check-vendor-key'
    | string
  slot?: string
  date?: string
  force?: boolean
  kind?: string
  job_ids?: number[]
  deployment?: string
  /** `enqueue` of a single job. */
  payload?: Record<string, unknown>
  /**
   * `enqueue` of a named set — the degraded-chain repair refetches only the
   * underlyings that came back wrong, rather than re-running their whole slot.
   */
  payloads?: Record<string, unknown>[]
}

export type DoctorFinding = {
  id: string
  slot: string
  severity: DoctorSeverity
  title: string
  expected: unknown
  actual: unknown
  detail: string
  session?: string | null
  fix?: DoctorFix | null
  auto_fixable: boolean
  missing_sample?: string[]
}

export type DoctorPrescription = DoctorFix & { finding_ids: string[] }

export type DoctorEodCritical = {
  verdict: DoctorVerdict
  checks: string[]
  findings: string[]
  detail: string
}

export type DoctorReport = {
  ok: boolean
  generated_at: string
  session: string
  session_is_today: boolean
  universe: { watchlist: number; underlyings: number; optionable: number }
  verdict: DoctorVerdict
  summary: string
  findings: DoctorFinding[]
  prescriptions: DoctorPrescription[]
  /** What gates the Research dbt batch: the session's own data, not cron adherence. */
  eod_critical?: DoctorEodCritical
  retired_slots: string[]
}

/**
 * What /market/doctor answers while a recompute runs and nothing is cached yet (after a plugin
 * restart): no session, no universe, no verdict — only the computing flag. Rendering it as a
 * report blanked the whole Ingest tab (TD-165).
 */
export type DoctorComputing = {
  ok: boolean
  computing: true
  generated_at?: string | null
  age_sec?: number | null
  findings?: DoctorFinding[]
}

export type DoctorResponse = DoctorReport | DoctorComputing

/** True for the computing stub: a real report always carries `universe`. */
export function isDoctorComputing(r: DoctorResponse | null | undefined): r is DoctorComputing {
  return r != null && (r as Partial<DoctorReport>).universe == null
}

export type HealAction = DoctorFix & {
  finding_ids: string[]
  result: 'dry_run' | string | Record<string, unknown>
}

export type HealResponse = {
  ok: boolean
  dry_run: boolean
  session: string | null
  verdict_before: DoctorVerdict | null
  actions: HealAction[]
  enqueued: number
}

const BASE = '/api/v1/plugins/market-data/api/market/doctor'

export async function fetchMarketDataDoctor(probes = true): Promise<DoctorResponse> {
  const r = await fetch(probes ? BASE : `${BASE}?probes=false`)
  if (!r.ok) throw new Error(`market doctor: HTTP ${r.status}`)
  return (await r.json()) as DoctorResponse
}

export async function healMarketData(body: {
  dry_run?: boolean
  finding_ids?: string[]
}): Promise<HealResponse> {
  const r = await authedFetch('market-data heal', `${BASE}/heal`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return (await r.json()) as HealResponse
}
